package unit_test

import (
	"bytes"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	face "github.com/hyz-is/arandu-face"
	liveness "github.com/hyz-is/arandu-liveness"
	onnx "github.com/hyz-is/arandu-onnx"
)

// The sequences here are drawn from fixed bytes or from a seeded ChaCha8
// stream, and answered with the synthetic landmark sequences of
// challenge_test.go. No face was photographed.

// stepTTL is the window each step of the sequences below is given.
const stepTTL = 5 * time.Second

// drawBytes is a reader that yields 48 bytes of 0x01 (the sequence ID and
// nonce) and then the given bytes, one per kind drawn.
func drawBytes(kindBytes ...byte) *bytes.Reader {
	return bytes.NewReader(append(bytes.Repeat([]byte{0x01}, 48), kindBytes...))
}

// movement is a response that performs the kind: a turn of 0.35 in yaw, a
// tilt of about 0.13 in pitch, a nod forward and back.
func movement(kind liveness.ChallengeKind) (yaw, pitch []float64) {
	switch kind {
	case liveness.ChallengeTurnLeft:
		return turn, constant(len(turn), frontalPitch)
	case liveness.ChallengeTurnRight:
		right := make([]float64, len(turn))
		for i, v := range turn {
			right[i] = -v
		}
		return right, constant(len(turn), frontalPitch)
	case liveness.ChallengeLookUp:
		return constant(6, 0), []float64{frontalPitch, 0.58, 0.55, 0.52, 0.50, 0.48}
	case liveness.ChallengeLookDown:
		return constant(6, 0), []float64{frontalPitch, 0.64, 0.67, 0.70, 0.73, 0.76}
	case liveness.ChallengeNod:
		return constant(7, 0), []float64{frontalPitch, 0.65, 0.70, 0.75, 0.70, 0.65, 0.60}
	}
	return nil, nil
}

// framesFrom turns per-frame yaw and pitch values into frames 100 ms apart
// starting at start.
func framesFrom(start time.Time, yaw, pitch []float64) []liveness.ChallengeFrame {
	out := make([]liveness.ChallengeFrame, len(yaw))
	for i := range yaw {
		out[i] = liveness.ChallengeFrame{
			Timestamp: start.Add(time.Duration(i) * 100 * time.Millisecond),
			Face:      face.Face{Landmarks: landmarks(yaw[i], pitch[i]), Detector: detector},
		}
	}
	return out
}

// stepStart is when step i's first frame is taken in answerAll: 0.5 s after
// issue, then every 1.5 s.
func stepStart(i int) time.Time {
	return issued.Add(500*time.Millisecond + time.Duration(i)*1500*time.Millisecond)
}

// answerAll answers every step of the sequence, in order, each with its own
// nonce and the movement it asks for, received 8 s after issue.
func answerAll(seq liveness.ChallengeSequence) liveness.SequenceResponse {
	resp := liveness.SequenceResponse{ReceivedAt: issued.Add(8 * time.Second)}
	for i, step := range seq.Steps {
		yaw, pitch := movement(step.Kind)
		resp.Steps = append(resp.Steps, liveness.ChallengeResponse{EchoedNonce: step.Nonce, Frames: framesFrom(stepStart(i), yaw, pitch)})
	}
	return resp
}

// threeSteps is the sequence drawn from drawBytes(0, 1, 1): turn_left,
// look_up, turn_right (see TestNewChallengeSequenceDrawsFromTheReader).
func threeSteps(t *testing.T) liveness.ChallengeSequence {
	t.Helper()
	seq, err := liveness.NewChallengeSequence(nil, 3, issued, stepTTL, drawBytes(0, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	return seq
}

func evaluateSequence(t *testing.T, seq liveness.ChallengeSequence, resp liveness.SequenceResponse) liveness.ChallengeSequenceResult {
	t.Helper()
	r, err := liveness.EvaluateChallengeSequence(seq, resp, liveness.DefaultSequenceThresholds())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func sequenceCheck(t *testing.T, r liveness.ChallengeSequenceResult, signal string) liveness.Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Signal == signal {
			return c
		}
	}
	t.Fatalf("no %s check in %+v", signal, r.Checks)
	return liveness.Check{}
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestNewChallengeSequenceDrawsFromTheReader(t *testing.T) {
	t.Parallel()

	// The default pool is turn_left, turn_right, look_up, look_down, nod.
	// Step 0 draws among five: byte 0 → 0 % 5 = 0, turn_left. Step 1 draws
	// among the four others (turn_right, look_up, look_down, nod): byte 1 →
	// look_up. Step 2 among turn_left, turn_right, look_down, nod: byte 1 →
	// turn_right.
	seq := threeSteps(t)
	want := []liveness.ChallengeKind{liveness.ChallengeTurnLeft, liveness.ChallengeLookUp, liveness.ChallengeTurnRight}
	if len(seq.Steps) != 3 {
		t.Fatalf("%d steps, want 3", len(seq.Steps))
	}
	if seq.ID != strings.Repeat("01", 16) || seq.Nonce != strings.Repeat("01", 32) ||
		seq.NonceDerivation != liveness.SequenceNonceDerivation || !seq.ExpiresAt.Equal(issued.Add(15*time.Second)) {
		t.Errorf("sequence %+v", seq)
	}
	for i, step := range seq.Steps {
		if step.Kind != want[i] {
			t.Errorf("step %d is %s, want %s", i, step.Kind, want[i])
		}
		// Step i's deadline is issue + (i+1)·5 s.
		if !step.IssuedAt.Equal(issued) || !step.ExpiresAt.Equal(issued.Add(time.Duration(i+1)*stepTTL)) {
			t.Errorf("step %d window %v to %v", i, step.IssuedAt, step.ExpiresAt)
		}
		if step.ID != seq.ID+"/"+string(rune('0'+i)) {
			t.Errorf("step %d id %q", i, step.ID)
		}
		if step.Nonce != liveness.DeriveStepNonce(seq.ID, seq.Nonce, i, step.Kind) || len(step.Nonce) != 64 || step.Nonce == seq.Nonce {
			t.Errorf("step %d nonce %q", i, step.Nonce)
		}
	}
	// The same bytes draw the same sequence.
	if again := threeSteps(t); again.Steps[2].Nonce != seq.Steps[2].Nonce {
		t.Error("the same reader drew a different sequence")
	}
}

func TestNewChallengeSequenceRejectsBiasedBytes(t *testing.T) {
	t.Parallel()

	// Among five kinds a byte is kept below 255 = 256 - 256 % 5, so 255 is
	// read and discarded, and 7 → 7 % 5 = 2, look_up. Step 1 then draws
	// among four, where every byte is kept: 4 → 4 % 4 = 0, turn_left.
	seq, err := liveness.NewChallengeSequence(nil, 2, issued, stepTTL, drawBytes(255, 7, 4))
	if err != nil {
		t.Fatal(err)
	}
	if seq.Steps[0].Kind != liveness.ChallengeLookUp || seq.Steps[1].Kind != liveness.ChallengeTurnLeft {
		t.Errorf("kinds %s, %s", seq.Steps[0].Kind, seq.Steps[1].Kind)
	}
	// A caller's pool is drawn from in its order, without its repeats: from
	// nod and look_up, byte 1 → look_up, then only nod is left.
	pool := []liveness.ChallengeKind{liveness.ChallengeNod, liveness.ChallengeLookUp, liveness.ChallengeNod}
	two, err := liveness.NewChallengeSequence(pool, 3, issued, stepTTL, drawBytes(1, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if two.Steps[0].Kind != liveness.ChallengeLookUp || two.Steps[1].Kind != liveness.ChallengeNod || two.Steps[2].Kind != liveness.ChallengeLookUp {
		t.Errorf("kinds from a pool of two: %s, %s, %s", two.Steps[0].Kind, two.Steps[1].Kind, two.Steps[2].Kind)
	}
}

func TestSequencesNeverRepeatAKindAndSpreadEvenly(t *testing.T) {
	t.Parallel()

	const draws = 5000
	random := rand.NewChaCha8([32]byte{7})
	kinds := liveness.DefaultSequenceKinds()
	if len(kinds) != 5 {
		t.Fatalf("%d default kinds, want the five supported ones", len(kinds))
	}
	first := map[liveness.ChallengeKind]int{}
	pairs := map[[2]liveness.ChallengeKind]int{}
	for d := 0; d < draws; d++ {
		steps := liveness.MinSequenceSteps + d%(liveness.MaxSequenceSteps-liveness.MinSequenceSteps+1)
		seq, err := liveness.NewChallengeSequence(nil, steps, issued, stepTTL, random)
		if err != nil {
			t.Fatal(err)
		}
		if len(seq.Steps) != steps {
			t.Fatalf("%d steps, want %d", len(seq.Steps), steps)
		}
		for i := 1; i < len(seq.Steps); i++ {
			if seq.Steps[i].Kind == seq.Steps[i-1].Kind {
				t.Fatalf("draw %d repeats %s at step %d", d, seq.Steps[i].Kind, i)
			}
		}
		first[seq.Steps[0].Kind]++
		pairs[[2]liveness.ChallengeKind{seq.Steps[0].Kind, seq.Steps[1].Kind}]++
	}
	// Each kind opens a sequence with probability 1/5: 1000 of 5000, with a
	// standard deviation of √(5000·0.2·0.8) ≈ 28. ±150 is more than five.
	for _, k := range kinds {
		if c := first[k]; c < 850 || c > 1150 {
			t.Errorf("%s opened %d of %d sequences, want about 1000", k, c, draws)
		}
	}
	// Each of the 20 ordered pairs of distinct kinds is the first transition
	// with probability 1/20: 250, standard deviation ≈ 15.4. ±80 is five.
	if len(pairs) != 20 {
		t.Errorf("%d distinct first transitions, want 20", len(pairs))
	}
	for p, c := range pairs {
		if c < 170 || c > 330 {
			t.Errorf("%s → %s drawn %d times, want about 250", p[0], p[1], c)
		}
	}
}

func TestNewChallengeSequenceRefusesWhatCannotBeDrawn(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		kinds []liveness.ChallengeKind
		steps int
		ttl   time.Duration
		at    time.Time
		want  string
	}{
		"one step":          {nil, 1, stepTTL, issued, "between 2 and 4"},
		"five steps":        {nil, 5, stepTTL, issued, "between 2 and 4"},
		"no ttl":            {nil, 3, 0, issued, "ttl"},
		"no issue time":     {nil, 3, stepTTL, time.Time{}, "issuedAt"},
		"an unsupported":    {[]liveness.ChallengeKind{liveness.ChallengeTurnLeft, liveness.ChallengeBlink}, 2, stepTTL, issued, "blink"},
		"an unknown":        {[]liveness.ChallengeKind{liveness.ChallengeTurnLeft, "wink"}, 2, stepTTL, issued, "wink"},
		"one distinct kind": {[]liveness.ChallengeKind{liveness.ChallengeNod, liveness.ChallengeNod}, 2, stepTTL, issued, "two distinct"},
	} {
		if _, err := liveness.NewChallengeSequence(tc.kinds, tc.steps, tc.at, tc.ttl, drawBytes(0, 0, 0, 0)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want an error naming %q", name, err, tc.want)
		}
	}
	if _, err := liveness.NewChallengeSequence(nil, 3, issued, stepTTL, drawBytes(0)); err == nil {
		t.Error("a reader that ran out drew a sequence")
	}
	if seq, err := liveness.NewChallengeSequence(nil, 4, issued, stepTTL, nil); err != nil || len(seq.Nonce) != 64 {
		t.Errorf("a sequence from crypto/rand: %+v (%v)", seq, err)
	}
}

func TestStepNoncesAreDerivedOneWayAndApart(t *testing.T) {
	t.Parallel()

	base := liveness.DeriveStepNonce("s", "n", 0, liveness.ChallengeNod)
	if base != liveness.DeriveStepNonce("s", "n", 0, liveness.ChallengeNod) {
		t.Error("the derivation is not deterministic")
	}
	for name, other := range map[string]string{
		"another index":    liveness.DeriveStepNonce("s", "n", 1, liveness.ChallengeNod),
		"another kind":     liveness.DeriveStepNonce("s", "n", 0, liveness.ChallengeLookUp),
		"another sequence": liveness.DeriveStepNonce("t", "n", 0, liveness.ChallengeNod),
		"another nonce":    liveness.DeriveStepNonce("s", "m", 0, liveness.ChallengeNod),
		// Length prefixes keep "sn" + "" apart from "s" + "n".
		"shifted fields": liveness.DeriveStepNonce("sn", "", 0, liveness.ChallengeNod),
	} {
		if other == base {
			t.Errorf("%s derives the same nonce", name)
		}
	}
	if len(base) != 64 || strings.Trim(base, "0123456789abcdef") != "" {
		t.Errorf("%q is not 64 hex digits", base)
	}
}

func TestATamperedSequenceIsRefused(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		mutate func(*liveness.ChallengeSequence)
		want   string
	}{
		"a step nonce": {func(s *liveness.ChallengeSequence) { s.Steps[1].Nonce = strings.Repeat("0", 64) }, "not derived"},
		"a kind": {func(s *liveness.ChallengeSequence) {
			s.Steps[1].Kind = liveness.ChallengeTurnLeft
		}, "repeats"},
		"a later kind": {func(s *liveness.ChallengeSequence) {
			s.Steps[2].Kind = liveness.ChallengeNod
		}, "not derived"},
		"a step id":       {func(s *liveness.ChallengeSequence) { s.Steps[0].ID = "x" }, "ID"},
		"the derivation":  {func(s *liveness.ChallengeSequence) { s.NonceDerivation = "" }, "NonceDerivation"},
		"a deadline":      {func(s *liveness.ChallengeSequence) { s.Steps[1].ExpiresAt = issued.Add(time.Second) }, "before step 0"},
		"the expiry":      {func(s *liveness.ChallengeSequence) { s.ExpiresAt = issued.Add(10 * time.Second) }, "after the sequence"},
		"a step issue":    {func(s *liveness.ChallengeSequence) { s.Steps[2].IssuedAt = issued.Add(time.Second) }, "not with the sequence"},
		"too few steps":   {func(s *liveness.ChallengeSequence) { s.Steps = s.Steps[:1] }, "between 2 and 4"},
		"a blink":         {func(s *liveness.ChallengeSequence) { s.Steps[2].Kind = liveness.ChallengeBlink }, "blink"},
		"the nonce":       {func(s *liveness.ChallengeSequence) { s.Nonce = "" }, "Nonce"},
		"the sequence id": {func(s *liveness.ChallengeSequence) { s.ID = "" }, "ID"},
	} {
		seq := threeSteps(t)
		seq.Steps = append([]liveness.Challenge(nil), seq.Steps...)
		tc.mutate(&seq)
		err := seq.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want an error naming %q", name, err, tc.want)
		}
		if _, err := liveness.EvaluateChallengeSequence(seq, answerAll(threeSteps(t)), liveness.DefaultSequenceThresholds()); err == nil {
			t.Errorf("%s: a tampered sequence was evaluated", name)
		}
	}
}

func TestASequencePerformedInOrderIsWithinEveryReference(t *testing.T) {
	t.Parallel()

	seq := threeSteps(t)
	r := evaluateSequence(t, seq, answerAll(seq))
	for _, c := range r.Checks {
		if !c.WithinReference {
			t.Errorf("check %+v is outside its reference", c)
		}
	}
	if r.StepCount != 3 || r.AnsweredSteps != 3 || r.CompletedSteps != 3 || r.MissingSteps != 0 || r.OrderInversions != 0 ||
		len(r.UnboundResponses) != 0 || len(r.DuplicateResponses) != 0 {
		t.Errorf("counts %+v", r)
	}
	if !sameInts(r.ExpectedOrder, []int{0, 1, 2}) || !sameInts(r.ObservedOrder, []int{0, 1, 2}) ||
		r.ObservedKinds[1] != liveness.ChallengeLookUp || r.ExpectedKinds[2] != liveness.ChallengeTurnRight {
		t.Errorf("order expected %v %v observed %v %v", r.ExpectedOrder, r.ExpectedKinds, r.ObservedOrder, r.ObservedKinds)
	}
	for i, s := range r.Steps {
		if s.Index != i || s.ResponseIndex != i || !s.Result.Completed || s.Result.ChallengeID != seq.Steps[i].ID || !s.Result.Nonce.Equal {
			t.Errorf("step %d: %+v", i, s)
		}
	}
	// Step 0 (turn, 8 frames) runs 0.5–1.2 s, step 1 (look up, 6 frames)
	// 2.0–2.5 s, step 2 (turn, 8 frames) 3.5–4.2 s: gaps of 0.8 s and 1.0 s,
	// 3.7 s in all.
	tm := r.Timing
	if len(tm.StepGaps) != 2 || tm.StepGaps[0] != 800*time.Millisecond || tm.StepGaps[1] != time.Second ||
		tm.MaxStepGap != time.Second || tm.TotalDuration != 3700*time.Millisecond || tm.OverlappingSteps != 0 ||
		!tm.FirstFrame.Equal(stepStart(0)) || !tm.ExpiresAt.Equal(seq.ExpiresAt) {
		t.Errorf("timing %+v", tm)
	}
	if c := sequenceCheck(t, r, "CompletedSteps"); c.Value != 3 || c.Rule != ">= 3" {
		t.Errorf("completed steps check %+v", c)
	}
	if r.Thresholds.Version != "arandu-liveness-sequence-0.1-uncalibrated" || r.Thresholds.Step != liveness.DefaultChallengeThresholds() {
		t.Errorf("thresholds %+v", r.Thresholds)
	}

	// Which step a response answers comes from its nonce, not its position:
	// the same responses sent in reverse give the same result.
	resp := answerAll(seq)
	resp.Steps[0], resp.Steps[2] = resp.Steps[2], resp.Steps[0]
	reversed := evaluateSequence(t, seq, resp)
	if reversed.CompletedSteps != 3 || reversed.OrderInversions != 0 || reversed.Steps[0].ResponseIndex != 2 {
		t.Errorf("responses sent in reverse: %+v", reversed)
	}
}

func TestStepsPerformedOutOfOrderAreCounted(t *testing.T) {
	t.Parallel()

	// Steps 0 and 1 swap times: step 1's frames are taken first. Each step
	// still shows its own movement, but the observed order is 1, 0, 2, one
	// inversion.
	seq := threeSteps(t)
	resp := answerAll(seq)
	for i, start := range []time.Time{stepStart(1), stepStart(0)} {
		yaw, pitch := movement(seq.Steps[i].Kind)
		resp.Steps[i].Frames = framesFrom(start, yaw, pitch)
	}
	r := evaluateSequence(t, seq, resp)
	if !sameInts(r.ObservedOrder, []int{1, 0, 2}) || r.OrderInversions != 1 {
		t.Errorf("observed %v with %d inversions", r.ObservedOrder, r.OrderInversions)
	}
	if c := sequenceCheck(t, r, "OrderInversions"); c.WithinReference || c.Value != 1 {
		t.Errorf("order check %+v", c)
	}

	// Fully reversed: 2, 1, 0 holds three inverted pairs.
	resp = answerAll(seq)
	for i := range seq.Steps {
		yaw, pitch := movement(seq.Steps[i].Kind)
		resp.Steps[i].Frames = framesFrom(stepStart(2-i), yaw, pitch)
	}
	if r := evaluateSequence(t, seq, resp); !sameInts(r.ObservedOrder, []int{2, 1, 0}) || r.OrderInversions != 3 {
		t.Errorf("reversed: observed %v with %d inversions", r.ObservedOrder, r.OrderInversions)
	}

	// The wrong movement in step 1 -- a turn where a look up was asked --
	// leaves the order intact and step 1 not completed.
	resp = answerAll(seq)
	resp.Steps[1].Frames = framesFrom(stepStart(1), turn, constant(len(turn), frontalPitch))
	wrong := evaluateSequence(t, seq, resp)
	if wrong.CompletedSteps != 2 || wrong.Steps[1].Result.Completed || wrong.OrderInversions != 0 ||
		sequenceCheck(t, wrong, "CompletedSteps").WithinReference {
		t.Errorf("a wrong movement: %d completed, step 1 %v", wrong.CompletedSteps, wrong.Steps[1].Result.Completed)
	}
}

func TestOverlappingStepsAndLongGapsAreCounted(t *testing.T) {
	t.Parallel()

	seq := threeSteps(t)
	resp := answerAll(seq)
	// Step 1 starts at 1.0 s, before step 0's last frame at 1.2 s.
	yaw, pitch := movement(seq.Steps[1].Kind)
	resp.Steps[1].Frames = framesFrom(issued.Add(time.Second), yaw, pitch)
	r := evaluateSequence(t, seq, resp)
	if r.Timing.OverlappingSteps != 1 || r.Timing.StepGaps[0] != -200*time.Millisecond || sequenceCheck(t, r, "OverlappingSteps").WithinReference {
		t.Errorf("overlap: %+v", r.Timing)
	}

	// Step 2 starts at 5.9 s, 3.4 s after step 1 ends at 2.5 s and still
	// before its own deadline: the gap is above the 3 s reference.
	resp = answerAll(seq)
	yaw, pitch = movement(seq.Steps[2].Kind)
	resp.Steps[2].Frames = framesFrom(issued.Add(5900*time.Millisecond), yaw, pitch)
	long := evaluateSequence(t, seq, resp)
	if c := sequenceCheck(t, long, "MaxStepGapSeconds"); c.WithinReference || !near2(c.Value, 3.4) || c.Rule != "<= 3" {
		t.Errorf("long gap check %+v", c)
	}
	if long.CompletedSteps != 3 {
		t.Errorf("a long gap changed the steps: %d completed", long.CompletedSteps)
	}
}

func TestAMissingStepIsEvaluatedEmpty(t *testing.T) {
	t.Parallel()

	seq := threeSteps(t)
	resp := answerAll(seq)
	resp.Steps = append(resp.Steps[:1], resp.Steps[2])
	r := evaluateSequence(t, seq, resp)
	if r.MissingSteps != 1 || r.AnsweredSteps != 2 || r.CompletedSteps != 2 || !sameInts(r.ObservedOrder, []int{0, 2}) {
		t.Errorf("counts %+v", r)
	}
	missing := r.Steps[1]
	if missing.ResponseIndex != -1 || missing.Result.Completed || missing.Result.FrameCount != 0 || missing.Result.Nonce.Equal {
		t.Errorf("the missing step: %+v", missing)
	}
	if c := sequenceCheck(t, r, "MissingSteps"); c.WithinReference || c.Value != 1 {
		t.Errorf("missing check %+v", c)
	}
	// Nothing at all answered: every step is missing, and there is no
	// timing to check.
	none := evaluateSequence(t, seq, liveness.SequenceResponse{ReceivedAt: issued.Add(time.Second)})
	if none.MissingSteps != 3 || len(none.ObservedOrder) != 0 || none.Timing.TotalDuration != 0 {
		t.Errorf("an empty response: %+v", none)
	}
	for _, c := range none.Checks {
		if c.Signal == "MaxStepGapSeconds" || c.Signal == "TotalDurationSeconds" {
			t.Errorf("an empty response carries the %s check", c.Signal)
		}
	}
}

func TestAStepNonceMismatchLeavesTheStepUnanswered(t *testing.T) {
	t.Parallel()

	seq := threeSteps(t)
	for name, echoed := range map[string]string{
		"the sequence nonce": seq.Nonce,
		"another value":      strings.Repeat("ab", 32),
		"nothing":            "",
		"a prefix":           seq.Steps[1].Nonce[:63],
	} {
		resp := answerAll(seq)
		resp.Steps[1].EchoedNonce = echoed
		r := evaluateSequence(t, seq, resp)
		if r.MissingSteps != 1 || !sameInts(r.UnboundResponses, []int{1}) || r.Steps[1].ResponseIndex != -1 ||
			r.Steps[1].Result.Completed || r.CompletedSteps != 2 {
			t.Errorf("%s: missing %d unbound %v step 1 %+v", name, r.MissingSteps, r.UnboundResponses, r.Steps[1].ResponseIndex)
		}
		if sequenceCheck(t, r, "UnboundResponses").WithinReference {
			t.Errorf("%s: the unbound check is within its reference", name)
		}
	}

	// Step 2's response echoing step 0's nonce: a duplicate for step 0, and
	// step 2 missing.
	resp := answerAll(seq)
	resp.Steps[2].EchoedNonce = seq.Steps[0].Nonce
	r := evaluateSequence(t, seq, resp)
	if !sameInts(r.DuplicateResponses, []int{2}) || r.MissingSteps != 1 || r.Steps[0].ResponseIndex != 0 || r.Steps[2].ResponseIndex != -1 {
		t.Errorf("duplicates %v missing %d", r.DuplicateResponses, r.MissingSteps)
	}
}

func TestStepDeadlinesAndTheSequenceExpiry(t *testing.T) {
	t.Parallel()

	seq := threeSteps(t)
	// Step 0's deadline is 5 s after issue. Its frames taken from 4.5 s run
	// to 5.2 s: frames at 5.1 and 5.2 s are outside its window. Steps 1 and
	// 2 are moved after it, still within theirs.
	resp := answerAll(seq)
	for i, start := range []time.Time{issued.Add(4500 * time.Millisecond), issued.Add(6 * time.Second), issued.Add(7500 * time.Millisecond)} {
		yaw, pitch := movement(seq.Steps[i].Kind)
		resp.Steps[i].Frames = framesFrom(start, yaw, pitch)
	}
	resp.ReceivedAt = issued.Add(9 * time.Second)
	r := evaluateSequence(t, seq, resp)
	if got := r.Steps[0].Result.Timing.FramesOutsideValidity; got != 2 || r.Steps[0].Result.Completed {
		t.Errorf("step 0: %d frames outside its window, completed %v", got, r.Steps[0].Result.Completed)
	}
	if !r.Steps[1].Result.Completed || !r.Steps[2].Result.Completed || r.CompletedSteps != 2 {
		t.Errorf("steps 1 and 2 within their windows: %d completed", r.CompletedSteps)
	}
	if !r.Steps[0].Result.Timing.ExpiresAt.Equal(seq.Steps[0].ExpiresAt) {
		t.Errorf("step 0 timing carries the expiry %v", r.Steps[0].Result.Timing.ExpiresAt)
	}

	// Received after the sequence expired, at 16 s: the sequence's receipt
	// check and every step's are outside their references.
	late := answerAll(seq)
	late.ReceivedAt = issued.Add(16 * time.Second)
	lr := evaluateSequence(t, seq, late)
	if sequenceCheck(t, lr, "ReceivedBeforeExpiry").WithinReference || lr.CompletedSteps != 0 {
		t.Errorf("a late response: %d completed", lr.CompletedSteps)
	}
	for _, s := range lr.Steps {
		if checkOf(t, s.Result, "ReceivedBeforeExpiry").WithinReference {
			t.Errorf("step %d's receipt check is within its reference after the sequence expired", s.Index)
		}
	}

	// A step response received before its own deadline keeps its own
	// receipt time; one that leaves it zero takes the sequence's.
	own := answerAll(seq)
	own.Steps[0].ReceivedAt = issued.Add(2 * time.Second)
	or := evaluateSequence(t, seq, own)
	if !or.Steps[0].Result.Timing.ReceivedAt.Equal(issued.Add(2*time.Second)) || !or.Steps[1].Result.Timing.ReceivedAt.Equal(own.ReceivedAt) {
		t.Errorf("receipt times %v %v", or.Steps[0].Result.Timing.ReceivedAt, or.Steps[1].Result.Timing.ReceivedAt)
	}
}

func TestEvaluateChallengeSequenceRefusesWhatCannotBeEvaluated(t *testing.T) {
	t.Parallel()

	seq := threeSteps(t)
	th := liveness.DefaultSequenceThresholds()
	noReceipt := answerAll(seq)
	noReceipt.ReceivedAt = time.Time{}
	if _, err := liveness.EvaluateChallengeSequence(seq, noReceipt, th); err == nil {
		t.Error("a response with no receipt time was evaluated")
	}
	for name, mutate := range map[string]func(*liveness.SequenceThresholds){
		"no version":     func(s *liveness.SequenceThresholds) { s.Version = "" },
		"no step set":    func(s *liveness.SequenceThresholds) { s.Step = liveness.ChallengeThresholds{} },
		"no gap":         func(s *liveness.SequenceThresholds) { s.MaxStepGap = 0 },
		"no total limit": func(s *liveness.SequenceThresholds) { s.MaxTotalDuration = -time.Second },
	} {
		bad := th
		mutate(&bad)
		if _, err := liveness.EvaluateChallengeSequence(seq, answerAll(seq), bad); err == nil {
			t.Errorf("%s: thresholds were accepted", name)
		}
	}
	many := answerAll(seq)
	many.Steps[0].Frames = make([]liveness.ChallengeFrame, liveness.MaxFrames)
	if _, err := liveness.EvaluateChallengeSequence(seq, many, th); err == nil {
		t.Error("more than MaxFrames frames across the sequence were evaluated")
	}
	broken := answerAll(seq)
	broken.Steps[2].Frames[1].Face.Landmarks = [5]face.Point{}
	if _, err := liveness.EvaluateChallengeSequence(seq, broken, th); err == nil || !strings.Contains(err.Error(), "step 2") {
		t.Errorf("degenerate landmarks = %v, want an error naming step 2", err)
	}
}

func TestTheModuleEvaluatesASequenceWithItsConfiguredThresholds(t *testing.T) {
	t.Parallel()

	rt, err := onnx.NewRuntime(onnx.Config{})
	if err != nil {
		t.Fatal(err)
	}
	strict := liveness.DefaultSequenceThresholds()
	strict.Version = "test-sequence-strict"
	strict.MaxTotalDuration = time.Second
	m, err := liveness.New(liveness.Config{Sequence: strict}, rt)
	if err != nil {
		t.Fatal(err)
	}
	seq := threeSteps(t)
	r, err := m.EvaluateChallengeSequence(seq, answerAll(seq))
	if err != nil {
		t.Fatal(err)
	}
	if r.Thresholds.Version != "test-sequence-strict" || sequenceCheck(t, r, "TotalDurationSeconds").WithinReference {
		t.Errorf("thresholds %s, total check %+v", r.Thresholds.Version, sequenceCheck(t, r, "TotalDurationSeconds"))
	}
	if c := (liveness.Config{}).WithDefaults(); c.Sequence != liveness.DefaultSequenceThresholds() {
		t.Errorf("default sequence thresholds %+v", c.Sequence)
	}
	bad := liveness.DefaultSequenceThresholds()
	bad.MaxStepGap = 0
	if err := (liveness.Config{Sequence: bad}).Validate(); err == nil || !strings.Contains(err.Error(), "MaxStepGap") {
		t.Errorf("a config with no step gap = %v", err)
	}
}
