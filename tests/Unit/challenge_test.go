package unit_test

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"

	face "github.com/kuaa-io/arandu-face"
	liveness "github.com/kuaa-io/arandu-liveness"
	onnx "github.com/kuaa-io/arandu-onnx"
)

// The landmark sequences here are synthetic: the five points of a frontal
// face drawn at (cx, cy) with an inter-ocular distance of 80 pixels -- eyes
// at (cx∓40, cy−15), nose at (cx, cy+40), mouth corners at (cx∓33, cy+75) --
// with the nose moved to make a given yaw or pitch proxy. No face was
// photographed.

// issued is when every challenge below is issued.
var issued = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

// landmarks returns the five points with the nose placed to give a yaw proxy
// of yaw (nose x offset over the 80-pixel eye distance) and a pitch proxy of
// pitch (nose y along the 90-pixel eye-to-mouth axis).
func landmarks(yaw, pitch float64) [5]face.Point {
	const cx, cy = 240.0, 300.0
	return [5]face.Point{
		{X: cx - 40, Y: cy - 15}, {X: cx + 40, Y: cy - 15},
		{X: cx + 80*yaw, Y: cy - 15 + 90*pitch},
		{X: cx - 33, Y: cy + 75}, {X: cx + 33, Y: cy + 75},
	}
}

// frontalPitch is the pitch proxy of the frontal points: (40+15)/90.
const frontalPitch = 55.0 / 90

// detector is the provenance the synthetic faces carry.
var detector = onnx.ModelInfo{ID: "yunet_2026may", Version: "2026may-26cc381e4d25", SHA256: "ebafce4e", Status: onnx.StatusApproved}

// sequence turns per-frame yaw and pitch values into frames every step,
// starting 500 ms after the challenge was issued.
func sequence(step time.Duration, yaw, pitch []float64) []liveness.ChallengeFrame {
	out := make([]liveness.ChallengeFrame, len(yaw))
	for i := range yaw {
		out[i] = liveness.ChallengeFrame{
			Timestamp: issued.Add(500*time.Millisecond + time.Duration(i)*step),
			Face:      face.Face{Landmarks: landmarks(yaw[i], pitch[i]), Detector: detector},
		}
	}
	return out
}

func constant(n int, v float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func challenge(kind liveness.ChallengeKind) liveness.Challenge {
	return liveness.Challenge{ID: "c-1", Kind: kind, IssuedAt: issued, ExpiresAt: issued.Add(10 * time.Second), Nonce: "n-123"}
}

func respond(frames []liveness.ChallengeFrame) liveness.ChallengeResponse {
	return liveness.ChallengeResponse{EchoedNonce: "n-123", ReceivedAt: issued.Add(3 * time.Second), Frames: frames}
}

// turn is a head turn toward the image's right in eight frames, yaw proxy 0
// to 0.35 in steps of 0.05.
var turn = []float64{0, 0.05, 0.10, 0.15, 0.20, 0.25, 0.30, 0.35}

func checkOf(t *testing.T, r liveness.ChallengeResult, signal string) liveness.Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Signal == signal {
			return c
		}
	}
	t.Fatalf("no %s check in %+v", signal, r.Checks)
	return liveness.Check{}
}

func evaluate(t *testing.T, ch liveness.Challenge, resp liveness.ChallengeResponse) liveness.ChallengeResult {
	t.Helper()
	r, err := liveness.EvaluateChallenge(ch, resp, liveness.DefaultChallengeThresholds())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTheProxiesAreGeometric(t *testing.T) {
	t.Parallel()

	yaw, err := liveness.YawProxy(landmarks(0.1, frontalPitch))
	if err != nil || !near2(yaw, 0.1) {
		t.Errorf("yaw %v (%v), want 0.1", yaw, err)
	}
	pitch, err := liveness.PitchProxy(landmarks(0, frontalPitch))
	if err != nil || !near2(pitch, frontalPitch) {
		t.Errorf("pitch %v (%v), want %v", pitch, err, frontalPitch)
	}
	// A roll of the whole face changes neither proxy.
	rolled := landmarks(0.1, 0.7)
	c, s := math.Cos(0.5), math.Sin(0.5)
	for i, p := range rolled {
		rolled[i] = face.Point{X: 100 + p.X*c - p.Y*s, Y: 50 + p.X*s + p.Y*c}
	}
	if y, _ := liveness.YawProxy(rolled); !near2(y, 0.1) {
		t.Errorf("the yaw proxy of a rolled face is %v", y)
	}
	if p, _ := liveness.PitchProxy(rolled); !near2(p, 0.7) {
		t.Errorf("the pitch proxy of a rolled face is %v", p)
	}
	if _, err := liveness.YawProxy([5]face.Point{}); err == nil {
		t.Error("coincident eyes gave a yaw proxy")
	}
	if _, err := liveness.PitchProxy([5]face.Point{}); err == nil {
		t.Error("coincident eyes and mouth gave a pitch proxy")
	}
}

func near2(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestATurnTowardTheRequestedSideIsCompleted(t *testing.T) {
	t.Parallel()

	// The nose moving toward the image's right is the subject turning to
	// their left, on an unmirrored image.
	r := evaluate(t, challenge(liveness.ChallengeTurnLeft), respond(sequence(100*time.Millisecond, turn, constant(8, frontalPitch))))
	if !r.Completed || !r.Supported || r.FrameCount != 8 || r.Monotonicity != 1 {
		t.Fatalf("completed %v supported %v frames %d monotonicity %v; checks %+v", r.Completed, r.Supported, r.FrameCount, r.Monotonicity, r.Checks)
	}
	o := r.Observed
	if o.Signal != liveness.SignalYawProxy || len(o.Series) != 8 || o.Baseline != 0 || o.PeakIndex != 7 || !near2(o.Excursion, 0.35) {
		t.Errorf("observation %+v", o)
	}
	// From the first frame to the peak: seven steps of 100 ms.
	if r.Timing.MotionDuration != 700*time.Millisecond || r.Timing.ResponseLatency != 500*time.Millisecond ||
		r.Timing.CaptureDuration != 700*time.Millisecond {
		t.Errorf("timing %+v", r.Timing)
	}
	if !r.Nonce.Equal || len(r.LandmarkModels) != 1 || r.LandmarkModels[0] != detector {
		t.Errorf("nonce %+v, landmark models %+v", r.Nonce, r.LandmarkModels)
	}
	if r.Thresholds.Version != "kuaa-liveness-challenge-0.1-uncalibrated" {
		t.Errorf("thresholds %s", r.Thresholds.Version)
	}

	mirrored := make([]float64, len(turn))
	for i, v := range turn {
		mirrored[i] = -v
	}
	right := evaluate(t, challenge(liveness.ChallengeTurnRight), respond(sequence(100*time.Millisecond, mirrored, constant(8, frontalPitch))))
	if !right.Completed || !near2(right.Observed.Excursion, 0.35) {
		t.Errorf("a right turn: completed %v excursion %v", right.Completed, right.Observed.Excursion)
	}
}

func TestATurnTheWrongWayIsNotCompleted(t *testing.T) {
	t.Parallel()

	r := evaluate(t, challenge(liveness.ChallengeTurnRight), respond(sequence(100*time.Millisecond, turn, constant(8, frontalPitch))))
	if r.Completed {
		t.Fatal("a left turn completed a right-turn challenge")
	}
	// Nothing moved the requested way: the peak is the first frame.
	if r.Observed.Excursion != 0 || r.Observed.PeakIndex != 0 || r.Monotonicity != 0 {
		t.Errorf("observation %+v monotonicity %v", r.Observed, r.Monotonicity)
	}
	if c := checkOf(t, r, "Excursion"); c.WithinReference || c.Rule != ">= 0.25" {
		t.Errorf("excursion check %+v", c)
	}
}

func TestATurnTooFastOrTooSlowIsNotCompleted(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		step    time.Duration
		seconds float64
	}{
		"too fast": {10 * time.Millisecond, 0.07},
		"too slow": {time.Second, 7},
	} {
		ch := challenge(liveness.ChallengeTurnLeft)
		ch.ExpiresAt = issued.Add(30 * time.Second)
		resp := respond(sequence(tc.step, turn, constant(8, frontalPitch)))
		resp.ReceivedAt = issued.Add(20 * time.Second)
		r := evaluate(t, ch, resp)
		c := checkOf(t, r, "MotionDurationSeconds")
		if r.Completed || c.WithinReference || !near2(c.Value, tc.seconds) || c.Rule != "in [0.3, 5]" {
			t.Errorf("%s: completed %v, check %+v", name, r.Completed, c)
		}
	}
}

func TestAnExpiredChallengeIsNotCompleted(t *testing.T) {
	t.Parallel()

	ch := challenge(liveness.ChallengeTurnLeft)
	ch.ExpiresAt = issued.Add(time.Second)
	resp := respond(sequence(100*time.Millisecond, turn, constant(8, frontalPitch)))
	resp.ReceivedAt = issued.Add(2 * time.Second)
	r := evaluate(t, ch, resp)
	if r.Completed {
		t.Fatal("an expired challenge completed")
	}
	if c := checkOf(t, r, "ReceivedBeforeExpiry"); c.WithinReference {
		t.Errorf("receipt check %+v", c)
	}
	// Frames are taken 0.5 s + 0.1 s·i after issue: frames 6 and 7, at 1.1
	// and 1.2 s, are after the expiry at 1 s.
	if r.Timing.FramesOutsideValidity != 2 || checkOf(t, r, "FramesOutsideValidity").WithinReference {
		t.Errorf("frames outside validity %d", r.Timing.FramesOutsideValidity)
	}
}

func TestANonceMismatchIsNotCompleted(t *testing.T) {
	t.Parallel()

	for name, echoed := range map[string]string{"different": "n-124", "missing": "", "prefix": "n-12"} {
		resp := respond(sequence(100*time.Millisecond, turn, constant(8, frontalPitch)))
		resp.EchoedNonce = echoed
		r := evaluate(t, challenge(liveness.ChallengeTurnLeft), resp)
		if r.Completed || r.Nonce.Equal || checkOf(t, r, "NonceEchoed").WithinReference {
			t.Errorf("%s: completed %v nonce %+v", name, r.Completed, r.Nonce)
		}
	}
	if b := liveness.BindNonce("", ""); b.Equal || b.ExpectedPresent || b.EchoedPresent {
		t.Errorf("two empty nonces bound: %+v", b)
	}
	if b := liveness.BindNonce("x", "x"); !b.Equal || !b.ExpectedPresent || !b.EchoedPresent {
		t.Errorf("equal nonces did not bind: %+v", b)
	}
}

func TestATurnFromAnAlreadyTurnedPoseIsNotCompleted(t *testing.T) {
	t.Parallel()

	start := []float64{0.2, 0.25, 0.3, 0.35, 0.4, 0.45, 0.5, 0.55}
	r := evaluate(t, challenge(liveness.ChallengeTurnLeft), respond(sequence(100*time.Millisecond, start, constant(8, frontalPitch))))
	if c := checkOf(t, r, "BaselineYaw"); r.Completed || c.WithinReference || !near2(c.Value, 0.2) {
		t.Errorf("completed %v, baseline check %+v", r.Completed, c)
	}
}

func TestAJitteryTurnHasALowerMonotonicity(t *testing.T) {
	t.Parallel()

	// Steps: +0.1, -0.05, +0.1, -0.05, +0.1, -0.05, +0.2: four of seven go
	// the requested way, all before the peak at the last frame.
	jitter := []float64{0, 0.1, 0.05, 0.15, 0.10, 0.20, 0.15, 0.35}
	r := evaluate(t, challenge(liveness.ChallengeTurnLeft), respond(sequence(100*time.Millisecond, jitter, constant(8, frontalPitch))))
	if !near2(r.Monotonicity, 4.0/7) || r.Completed {
		t.Errorf("monotonicity %v completed %v", r.Monotonicity, r.Completed)
	}
}

func TestTiltsAndNodsAreMeasuredWithThePitchProxy(t *testing.T) {
	t.Parallel()

	up := []float64{frontalPitch, 0.58, 0.55, 0.52, 0.50, 0.48}
	r := evaluate(t, challenge(liveness.ChallengeLookUp), respond(sequence(100*time.Millisecond, constant(6, 0), up)))
	if !r.Completed || r.Observed.Signal != liveness.SignalPitchProxy || !near2(r.Observed.Excursion, frontalPitch-0.48) {
		t.Errorf("look up: completed %v observation %+v checks %+v", r.Completed, r.Observed, r.Checks)
	}
	down := evaluate(t, challenge(liveness.ChallengeLookDown), respond(sequence(100*time.Millisecond, constant(6, 0), up)))
	if down.Completed {
		t.Error("tilting back completed a look-down challenge")
	}

	// Forward to 0.75 at frame 3, back to 0.60 at frame 6: the forward half
	// is 0.75 - 0.6111 = 0.1389, the backward 0.15; the smaller counts.
	nod := []float64{frontalPitch, 0.65, 0.70, 0.75, 0.70, 0.65, 0.60}
	n := evaluate(t, challenge(liveness.ChallengeNod), respond(sequence(100*time.Millisecond, constant(7, 0), nod)))
	if !n.Completed || n.Observed.PeakIndex != 3 || n.Observed.EndIndex != 6 || !near2(n.Observed.Excursion, 0.75-frontalPitch) ||
		n.Monotonicity != 1 || n.Timing.MotionDuration != 600*time.Millisecond {
		t.Errorf("nod: completed %v observation %+v monotonicity %v timing %+v", n.Completed, n.Observed, n.Monotonicity, n.Timing)
	}
	// Forward only, never back.
	half := []float64{frontalPitch, 0.65, 0.70, 0.75, 0.76, 0.77, 0.78}
	if h := evaluate(t, challenge(liveness.ChallengeNod), respond(sequence(100*time.Millisecond, constant(7, 0), half))); h.Completed {
		t.Errorf("half a nod completed: %+v", h.Observed)
	}
}

func TestChallengesFiveLandmarksCannotMeasureAreUnsupported(t *testing.T) {
	t.Parallel()

	for _, kind := range []liveness.ChallengeKind{liveness.ChallengeBlink, liveness.ChallengeSmile, liveness.ChallengeOpenMouth} {
		r := evaluate(t, challenge(kind), respond(sequence(100*time.Millisecond, turn, constant(8, frontalPitch))))
		if r.Supported || r.Completed || !strings.Contains(r.UnsupportedReason, "dense landmarks") || len(r.Observed.Series) != 0 {
			t.Errorf("%s: supported %v completed %v reason %q", kind, r.Supported, r.Completed, r.UnsupportedReason)
		}
		if c := checkOf(t, r, "Supported"); c.WithinReference {
			t.Errorf("%s: supported check %+v", kind, c)
		}
	}
	supported := 0
	for _, kind := range liveness.ChallengeKinds() {
		if ok, _ := kind.Supported(); ok {
			supported++
		}
	}
	if supported != 5 {
		t.Errorf("%d supported kinds, want 5 (two turns, two tilts, a nod)", supported)
	}
	if ok, reason := liveness.ChallengeKind("wink").Supported(); ok || reason == "" {
		t.Error("an unknown kind is supported")
	}
}

func TestEvaluateChallengeRefusesWhatCannotBeEvaluated(t *testing.T) {
	t.Parallel()

	frames := sequence(100*time.Millisecond, turn, constant(8, frontalPitch))
	th := liveness.DefaultChallengeThresholds()
	for name, ch := range map[string]liveness.Challenge{
		"no id":          {Kind: liveness.ChallengeTurnLeft, IssuedAt: issued, ExpiresAt: issued.Add(time.Second), Nonce: "n"},
		"unknown kind":   {ID: "c", Kind: "wink", IssuedAt: issued, ExpiresAt: issued.Add(time.Second), Nonce: "n"},
		"no nonce":       {ID: "c", Kind: liveness.ChallengeTurnLeft, IssuedAt: issued, ExpiresAt: issued.Add(time.Second)},
		"expires before": {ID: "c", Kind: liveness.ChallengeTurnLeft, IssuedAt: issued, ExpiresAt: issued, Nonce: "n"},
	} {
		if _, err := liveness.EvaluateChallenge(ch, respond(frames), th); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	noReceipt := respond(frames)
	noReceipt.ReceivedAt = time.Time{}
	if _, err := liveness.EvaluateChallenge(challenge(liveness.ChallengeTurnLeft), noReceipt, th); err == nil {
		t.Error("a response with no receipt time was accepted")
	}
	broken := respond(sequence(100*time.Millisecond, turn, constant(8, frontalPitch)))
	broken.Frames[3].Face.Landmarks = [5]face.Point{}
	if _, err := liveness.EvaluateChallenge(challenge(liveness.ChallengeTurnLeft), broken, th); err == nil || !strings.Contains(err.Error(), "frame 3") {
		t.Errorf("degenerate landmarks = %v, want an error naming frame 3", err)
	}
	many := respond(make([]liveness.ChallengeFrame, liveness.MaxFrames+1))
	if _, err := liveness.EvaluateChallenge(challenge(liveness.ChallengeTurnLeft), many, th); err == nil {
		t.Error("more than MaxFrames frames were accepted")
	}
	if _, err := liveness.EvaluateChallenge(challenge(liveness.ChallengeTurnLeft), respond(frames), liveness.ChallengeThresholds{}); err == nil {
		t.Error("thresholds with no version were accepted")
	}

	// No frames is a measurement, not an error.
	empty := evaluate(t, challenge(liveness.ChallengeTurnLeft), respond(nil))
	if empty.Completed || empty.FrameCount != 0 || checkOf(t, empty, "FrameCount").WithinReference {
		t.Errorf("an empty response: %+v", empty)
	}

	// Timestamps out of order are counted.
	shuffled := respond(sequence(100*time.Millisecond, turn, constant(8, frontalPitch)))
	shuffled.Frames[2].Timestamp, shuffled.Frames[3].Timestamp = shuffled.Frames[3].Timestamp, shuffled.Frames[2].Timestamp
	if r := evaluate(t, challenge(liveness.ChallengeTurnLeft), shuffled); r.Timing.NonIncreasingSteps != 1 || r.Completed {
		t.Errorf("shuffled timestamps: %d non-increasing, completed %v", r.Timing.NonIncreasingSteps, r.Completed)
	}
}

func TestNewChallengeDrawsItsIDAndNonce(t *testing.T) {
	t.Parallel()

	c, err := liveness.NewChallenge(liveness.ChallengeTurnLeft, issued, 15*time.Second, bytes.NewReader(bytes.Repeat([]byte{0xAB}, 48)))
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != strings.Repeat("ab", 16) || c.Nonce != strings.Repeat("ab", 32) || !c.ExpiresAt.Equal(issued.Add(15*time.Second)) {
		t.Errorf("challenge %+v", c)
	}
	random, err := liveness.NewChallenge(liveness.ChallengeNod, issued, time.Second, nil)
	if err != nil || len(random.Nonce) != 64 || random.Nonce == c.Nonce {
		t.Errorf("a random challenge %+v (%v)", random, err)
	}
	if _, err := liveness.NewChallenge(liveness.ChallengeTurnLeft, issued, 0, nil); err == nil {
		t.Error("a zero ttl was accepted")
	}
	if _, err := liveness.NewChallenge(liveness.ChallengeTurnLeft, issued, time.Second, bytes.NewReader(make([]byte, 20))); err == nil {
		t.Error("a short random source was accepted")
	}
	if _, err := liveness.NewChallenge("wink", issued, time.Second, nil); err == nil {
		t.Error("an unknown kind was issued")
	}
}
