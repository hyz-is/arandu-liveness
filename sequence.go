package liveness

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"
)

// The bounds of a challenge sequence's length.
const (
	// MinSequenceSteps is the fewest steps a sequence holds.
	MinSequenceSteps = 2
	// MaxSequenceSteps is the most steps a sequence holds.
	MaxSequenceSteps = 4
)

// SequenceNonceDerivation names how a step's nonce is derived from the
// sequence's nonce: DeriveStepNonce. A change to the derivation is a new name.
const SequenceNonceDerivation = "arandu-liveness-step-nonce-sha256-v1"

// stepNonceDomain separates DeriveStepNonce's hash from every other SHA-256
// computed over the same bytes.
const stepNonceDomain = "arandu-liveness/sequence-step-nonce/v1"

// ChallengeSequence is an ordered series of challenges the server draws at
// random and issues together: the movements to make, in order, each with its
// own nonce and its own deadline.
//
// Every step is a Challenge. Its IssuedAt is the sequence's; its ExpiresAt is
// the deadline its frames have to be taken by, one step ttl later than the
// step before it; its Nonce is DeriveStepNonce of the sequence's nonce, so a
// record holding the sequence can recompute every step's binding.
type ChallengeSequence struct {
	// ID identifies the sequence. Step i's ID is ID + "/" + i.
	ID string
	// IssuedAt is when the server issued the sequence.
	IssuedAt time.Time
	// ExpiresAt is when the last step stops being answerable, and the
	// deadline the whole response has to be received by.
	ExpiresAt time.Time
	// Nonce is the sequence's unpredictable value. It is the input the step
	// nonces are derived from, and is not itself echoed by the client.
	Nonce string
	// NonceDerivation names how the step nonces were derived from Nonce:
	// SequenceNonceDerivation.
	NonceDerivation string
	// Steps are the challenges, in the order they are to be performed.
	Steps []Challenge
}

// DefaultSequenceKinds returns the kinds NewChallengeSequence draws from when
// it is given none: every kind five-point landmarks can measure.
func DefaultSequenceKinds() []ChallengeKind {
	out := []ChallengeKind{}
	for _, k := range ChallengeKinds() {
		if ok, _ := k.Supported(); ok {
			out = append(out, k)
		}
	}
	return out
}

// DeriveStepNonce is the nonce of step index of a sequence: the hex SHA-256
// of a domain string, the sequence ID, the sequence nonce, the step index and
// the step kind, each length-prefixed. It is one-way, so a step nonce sent to
// the client says nothing about the nonce of another step or of the
// sequence; and it is deterministic, so a record that holds the sequence
// nonce recomputes every step nonce without storing them.
func DeriveStepNonce(sequenceID, sequenceNonce string, index int, kind ChallengeKind) string {
	h := sha256.New()
	field := func(s string) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	field(stepNonceDomain)
	field(sequenceID)
	field(sequenceNonce)
	var i [8]byte
	binary.BigEndian.PutUint64(i[:], uint64(index))
	h.Write(i[:])
	field(string(kind))
	return hex.EncodeToString(h.Sum(nil))
}

// stepID is the ID of step index of a sequence.
func stepID(sequenceID string, index int) string { return fmt.Sprintf("%s/%d", sequenceID, index) }

// NewChallengeSequence draws a sequence of steps challenges from kinds, or
// from DefaultSequenceKinds when kinds is empty, and issues it at issuedAt.
//
// Each step's kind is drawn uniformly from the kinds other than the one
// before it, so no kind follows itself; the draw is rejection-sampled from
// bytes of random, or of crypto/rand when random is nil, so it carries no
// modulo bias. Steps has to be between MinSequenceSteps and MaxSequenceSteps,
// and kinds has to hold at least two distinct kinds, every one of them
// supported. Step i's frames have to be taken by issuedAt + (i+1)·ttl, and the
// whole response received by issuedAt + steps·ttl. The sequence ID (128 bits)
// and nonce (256 bits) are read from random before the kinds are.
func NewChallengeSequence(kinds []ChallengeKind, steps int, issuedAt time.Time, ttl time.Duration, random io.Reader) (ChallengeSequence, error) {
	if random == nil {
		random = rand.Reader
	}
	if steps < MinSequenceSteps || steps > MaxSequenceSteps {
		return ChallengeSequence{}, fmt.Errorf("liveness: a sequence of %d steps; it has to hold between %d and %d", steps, MinSequenceSteps, MaxSequenceSteps)
	}
	if ttl <= 0 {
		return ChallengeSequence{}, fmt.Errorf("liveness: a sequence step's ttl has to be positive, and is %v", ttl)
	}
	if issuedAt.IsZero() {
		return ChallengeSequence{}, errors.New("liveness: a sequence's issuedAt is required")
	}
	pool, err := sequencePool(kinds)
	if err != nil {
		return ChallengeSequence{}, err
	}
	id := make([]byte, 16)
	nonce := make([]byte, 32)
	if _, err := io.ReadFull(random, id); err != nil {
		return ChallengeSequence{}, fmt.Errorf("liveness: reading the sequence id: %w", err)
	}
	if _, err := io.ReadFull(random, nonce); err != nil {
		return ChallengeSequence{}, fmt.Errorf("liveness: reading the sequence nonce: %w", err)
	}
	s := ChallengeSequence{
		ID: hex.EncodeToString(id), IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(time.Duration(steps) * ttl),
		Nonce: hex.EncodeToString(nonce), NonceDerivation: SequenceNonceDerivation,
	}
	previous := ChallengeKind("")
	for i := 0; i < steps; i++ {
		candidates := make([]ChallengeKind, 0, len(pool))
		for _, k := range pool {
			if k != previous {
				candidates = append(candidates, k)
			}
		}
		pick, err := uniformIndex(random, len(candidates))
		if err != nil {
			return ChallengeSequence{}, fmt.Errorf("liveness: drawing step %d: %w", i, err)
		}
		kind := candidates[pick]
		s.Steps = append(s.Steps, Challenge{
			ID: stepID(s.ID, i), Kind: kind, IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(time.Duration(i+1) * ttl),
			Nonce: DeriveStepNonce(s.ID, s.Nonce, i, kind),
		})
		previous = kind
	}
	return s, s.Validate()
}

// sequencePool is kinds without repeats, in their order, or the default
// kinds; it refuses an unsupported kind and a pool of fewer than two.
func sequencePool(kinds []ChallengeKind) ([]ChallengeKind, error) {
	if len(kinds) == 0 {
		return DefaultSequenceKinds(), nil
	}
	seen := map[ChallengeKind]bool{}
	pool := []ChallengeKind{}
	for _, k := range kinds {
		if ok, reason := k.Supported(); !ok {
			return nil, fmt.Errorf("liveness: a sequence cannot ask for %q: %s", string(k), reason)
		}
		if !seen[k] {
			seen[k] = true
			pool = append(pool, k)
		}
	}
	if len(pool) < 2 {
		return nil, fmt.Errorf("liveness: a sequence needs at least two distinct kinds to draw from, and was given %d", len(pool))
	}
	return pool, nil
}

// uniformIndex reads bytes from random until one falls below the largest
// multiple of n that fits in a byte, and returns it modulo n.
func uniformIndex(random io.Reader, n int) (int, error) {
	if n < 1 || n > 256 {
		return 0, fmt.Errorf("liveness: cannot draw among %d values", n)
	}
	limit := 256 - 256%n
	var b [1]byte
	for {
		if _, err := io.ReadFull(random, b[:]); err != nil {
			return 0, err
		}
		if int(b[0]) < limit {
			return int(b[0]) % n, nil
		}
	}
}

// Validate reports a sequence that cannot be evaluated: a missing ID, nonce
// or issue time, a length out of bounds, a step that is not a valid supported
// challenge, a kind following itself, a step not issued with the sequence, a
// deadline earlier than the one before it or later than the sequence's, a
// step ID other than ID/i, and a step nonce other than DeriveStepNonce's.
func (s ChallengeSequence) Validate() error {
	switch {
	case s.ID == "":
		return errors.New("liveness: ChallengeSequence.ID is required")
	case s.Nonce == "":
		return errors.New("liveness: ChallengeSequence.Nonce is required: a sequence without one binds nothing")
	case s.NonceDerivation != SequenceNonceDerivation:
		return fmt.Errorf("liveness: ChallengeSequence.NonceDerivation is %q, and this package derives %q", s.NonceDerivation, SequenceNonceDerivation)
	case s.IssuedAt.IsZero():
		return errors.New("liveness: ChallengeSequence.IssuedAt is required")
	case !s.ExpiresAt.After(s.IssuedAt):
		return fmt.Errorf("liveness: ChallengeSequence.ExpiresAt %v is not after IssuedAt %v", s.ExpiresAt, s.IssuedAt)
	case len(s.Steps) < MinSequenceSteps || len(s.Steps) > MaxSequenceSteps:
		return fmt.Errorf("liveness: a sequence of %d steps; it has to hold between %d and %d", len(s.Steps), MinSequenceSteps, MaxSequenceSteps)
	}
	for i, step := range s.Steps {
		if err := step.Validate(); err != nil {
			return fmt.Errorf("liveness: step %d: %w", i, err)
		}
		if ok, reason := step.Kind.Supported(); !ok {
			return fmt.Errorf("liveness: step %d asks for %q: %s", i, string(step.Kind), reason)
		}
		if i > 0 && step.Kind == s.Steps[i-1].Kind {
			return fmt.Errorf("liveness: step %d repeats the kind %q of the step before it", i, string(step.Kind))
		}
		if !step.IssuedAt.Equal(s.IssuedAt) {
			return fmt.Errorf("liveness: step %d was issued at %v, not with the sequence at %v", i, step.IssuedAt, s.IssuedAt)
		}
		if i > 0 && step.ExpiresAt.Before(s.Steps[i-1].ExpiresAt) {
			return fmt.Errorf("liveness: step %d expires at %v, before step %d", i, step.ExpiresAt, i-1)
		}
		if step.ExpiresAt.After(s.ExpiresAt) {
			return fmt.Errorf("liveness: step %d expires at %v, after the sequence", i, step.ExpiresAt)
		}
		if step.ID != stepID(s.ID, i) {
			return fmt.Errorf("liveness: step %d carries the ID %q, not %q", i, step.ID, stepID(s.ID, i))
		}
		if subtle.ConstantTimeCompare([]byte(step.Nonce), []byte(DeriveStepNonce(s.ID, s.Nonce, i, step.Kind))) != 1 {
			return fmt.Errorf("liveness: step %d's nonce is not derived from the sequence's nonce", i)
		}
	}
	return nil
}

// SequenceThresholds are the references a sequence's measurements are checked
// against: the per-step ChallengeThresholds and the sequence's own. They are
// versioned parameters, not a policy.
type SequenceThresholds struct {
	// Version names this set of values, Step's included. A change to any
	// value is a new version.
	Version string
	// Step are the references every step is evaluated against.
	Step ChallengeThresholds
	// MaxStepGap is the longest time from one step's last frame to the next
	// step's first frame.
	MaxStepGap time.Duration
	// MaxTotalDuration is the longest time from the first step's first frame
	// to the last step's last frame.
	MaxTotalDuration time.Duration
}

// DefaultSequenceThresholds returns the references EvaluateChallengeSequence
// uses when the configuration names none. They are starting values, not
// calibrated on any population; their version says so.
func DefaultSequenceThresholds() SequenceThresholds {
	return SequenceThresholds{
		Version:          "arandu-liveness-sequence-0.1-uncalibrated",
		Step:             DefaultChallengeThresholds(),
		MaxStepGap:       3 * time.Second,
		MaxTotalDuration: 30 * time.Second,
	}
}

// Validate reports a set of references that cannot be checked against.
func (t SequenceThresholds) Validate() error {
	if t.Version == "" {
		return errors.New("liveness: SequenceThresholds.Version is required: a threshold nobody can name is one nobody can audit")
	}
	if err := t.Step.Validate(); err != nil {
		return fmt.Errorf("liveness: SequenceThresholds.Step: %w", err)
	}
	if t.MaxStepGap <= 0 {
		return fmt.Errorf("liveness: SequenceThresholds.MaxStepGap is %v, and has to be positive", t.MaxStepGap)
	}
	if t.MaxTotalDuration <= 0 {
		return fmt.Errorf("liveness: SequenceThresholds.MaxTotalDuration is %v, and has to be positive", t.MaxTotalDuration)
	}
	return nil
}

// SequenceResponse is what came back for a sequence: one ChallengeResponse
// per step the client answered, in the order the client sent them. Which step
// a response answers is read from the step nonce it echoes, not from its
// position.
type SequenceResponse struct {
	// ReceivedAt is when the server received the last of the responses. A
	// step response whose own ReceivedAt is zero takes it.
	ReceivedAt time.Time
	// Steps are the step responses.
	Steps []ChallengeResponse
}

// SequenceStepResult is one step's evaluation.
type SequenceStepResult struct {
	// Index is the step's position in the sequence.
	Index int
	// Kind is the step's kind.
	Kind ChallengeKind
	// ResponseIndex is the position, in SequenceResponse.Steps, of the
	// response that echoed this step's nonce, or -1 when none did.
	ResponseIndex int
	// Result is the step's challenge evaluated on that response, or on an
	// empty response when there was none. Its frames are held against the
	// step's own deadline; its ReceivedBeforeExpiry against the sequence's
	// ExpiresAt.
	Result ChallengeResult
}

// SequenceTiming is when the sequence's steps happened, in the order they
// were observed.
type SequenceTiming struct {
	// IssuedAt, ExpiresAt and ReceivedAt are the sequence's and the
	// response's times.
	IssuedAt, ExpiresAt, ReceivedAt time.Time
	// FirstFrame and LastFrame are the earliest and the latest frames of any
	// answered step.
	FirstFrame, LastFrame time.Time
	// TotalDuration is from FirstFrame to LastFrame.
	TotalDuration time.Duration
	// StepGaps are, between each pair of steps consecutive in ObservedOrder,
	// the time from the earlier one's last frame to the later one's first
	// frame. A gap of zero or less is an overlap.
	StepGaps []time.Duration
	// MaxStepGap is the largest of StepGaps.
	MaxStepGap time.Duration
	// OverlappingSteps is how many of StepGaps are zero or less.
	OverlappingSteps int
}

// ChallengeSequenceResult is what EvaluateChallengeSequence returns: every
// step's ChallengeResult, the order the steps were observed in against the
// order they were issued in, the timing between them, and each sequence-level
// measurement checked against the versioned references.
type ChallengeSequenceResult struct {
	// SequenceID is the sequence's ID.
	SequenceID string
	// Steps are the steps' evaluations, in the issued order.
	Steps []SequenceStepResult
	// ExpectedOrder is the step indices in the issued order: 0, 1, …
	ExpectedOrder []int
	// ExpectedKinds are the steps' kinds in the issued order.
	ExpectedKinds []ChallengeKind
	// ObservedOrder is the indices of the steps answered with frames, in the
	// order of their first frames' timestamps.
	ObservedOrder []int
	// ObservedKinds are the kinds of ObservedOrder.
	ObservedKinds []ChallengeKind
	// OrderInversions is how many pairs of steps in ObservedOrder appear in
	// the opposite order to the issued one: 0 when they were performed in
	// order.
	OrderInversions int
	// StepCount is how many steps the sequence holds.
	StepCount int
	// AnsweredSteps is how many steps a response echoed the nonce of.
	AnsweredSteps int
	// MissingSteps is how many steps no response echoed the nonce of.
	MissingSteps int
	// CompletedSteps is how many steps' results are Completed.
	CompletedSteps int
	// UnboundResponses are the positions of the responses whose nonce is no
	// step's.
	UnboundResponses []int
	// DuplicateResponses are the positions of the responses that echoed a
	// step nonce an earlier response had already echoed; the earlier one is
	// the one evaluated.
	DuplicateResponses []int
	// Timing is when the steps happened.
	Timing SequenceTiming
	// Checks hold each sequence-level measurement against its reference, in
	// a fixed order. The steps' own checks are in their results.
	Checks []Check
	// Thresholds are the references the checks used, with their version.
	Thresholds SequenceThresholds
}

// EvaluateChallengeSequence evaluates every step of a sequence with
// EvaluateChallenge and measures the sequence as a whole, in pure Go.
//
// A response answers the step whose nonce it echoes, compared in constant
// time; a response echoing no step's nonce is counted unbound, and a second
// response for a step is counted a duplicate. A step nobody answered is
// evaluated on an empty response, so that every step has a result. Each
// step's frames are held against its own deadline and its receipt against the
// sequence's expiry. The answered steps are ordered by their first frames,
// and the measurements are that order against the issued one, the gaps and
// overlaps between consecutive steps, the total duration, and the counts of
// completed, missing, unbound and duplicate responses, each checked against
// the references. Nothing here concludes the subject is present: a sequence
// performed in front of a camera by a real-time face reenactment completes
// as well, which is why the injection signals are measured separately.
func EvaluateChallengeSequence(seq ChallengeSequence, resp SequenceResponse, thresholds SequenceThresholds) (ChallengeSequenceResult, error) {
	if err := seq.Validate(); err != nil {
		return ChallengeSequenceResult{}, err
	}
	if err := thresholds.Validate(); err != nil {
		return ChallengeSequenceResult{}, err
	}
	if resp.ReceivedAt.IsZero() {
		return ChallengeSequenceResult{}, errors.New("liveness: SequenceResponse.ReceivedAt is required: it is the server's receipt time")
	}
	total := 0
	for _, r := range resp.Steps {
		total += len(r.Frames)
	}
	if total > MaxFrames {
		return ChallengeSequenceResult{}, fmt.Errorf("liveness: %d frames across the sequence is more than %d", total, MaxFrames)
	}
	if len(resp.Steps) > MaxFrames {
		return ChallengeSequenceResult{}, fmt.Errorf("liveness: %d step responses is more than %d", len(resp.Steps), MaxFrames)
	}

	n := len(seq.Steps)
	r := ChallengeSequenceResult{SequenceID: seq.ID, StepCount: n, Thresholds: thresholds}
	r.Timing = SequenceTiming{IssuedAt: seq.IssuedAt, ExpiresAt: seq.ExpiresAt, ReceivedAt: resp.ReceivedAt}

	answeredBy := make([]int, n)
	for i := range answeredBy {
		answeredBy[i] = -1
	}
	for j, sr := range resp.Steps {
		step := -1
		for i, ch := range seq.Steps {
			if BindNonce(ch.Nonce, sr.EchoedNonce).Equal {
				step = i
			}
		}
		switch {
		case step < 0:
			r.UnboundResponses = append(r.UnboundResponses, j)
		case answeredBy[step] >= 0:
			r.DuplicateResponses = append(r.DuplicateResponses, j)
		default:
			answeredBy[step] = j
		}
	}

	type span struct {
		index       int
		first, last time.Time
	}
	spans := []span{}
	for i, ch := range seq.Steps {
		r.ExpectedOrder = append(r.ExpectedOrder, i)
		r.ExpectedKinds = append(r.ExpectedKinds, ch.Kind)
		stepResp := ChallengeResponse{ReceivedAt: resp.ReceivedAt}
		if j := answeredBy[i]; j >= 0 {
			stepResp = resp.Steps[j]
			if stepResp.ReceivedAt.IsZero() {
				stepResp.ReceivedAt = resp.ReceivedAt
			}
			r.AnsweredSteps++
		} else {
			r.MissingSteps++
		}
		result, err := evaluateChallenge(ch, stepResp, thresholds.Step, seq.ExpiresAt)
		if err != nil {
			return ChallengeSequenceResult{}, fmt.Errorf("liveness: step %d: %w", i, err)
		}
		if result.Completed {
			r.CompletedSteps++
		}
		r.Steps = append(r.Steps, SequenceStepResult{Index: i, Kind: ch.Kind, ResponseIndex: answeredBy[i], Result: result})
		if f := stepResp.Frames; len(f) > 0 {
			first, last := f[0].Timestamp, f[0].Timestamp
			for _, frame := range f {
				if frame.Timestamp.Before(first) {
					first = frame.Timestamp
				}
				if frame.Timestamp.After(last) {
					last = frame.Timestamp
				}
			}
			spans = append(spans, span{index: i, first: first, last: last})
		}
	}

	sort.SliceStable(spans, func(a, b int) bool { return spans[a].first.Before(spans[b].first) })
	for k, s := range spans {
		r.ObservedOrder = append(r.ObservedOrder, s.index)
		r.ObservedKinds = append(r.ObservedKinds, seq.Steps[s.index].Kind)
		for _, earlier := range spans[:k] {
			if earlier.index > s.index {
				r.OrderInversions++
			}
		}
		if k == 0 {
			r.Timing.FirstFrame, r.Timing.LastFrame = s.first, s.last
			continue
		}
		if s.last.After(r.Timing.LastFrame) {
			r.Timing.LastFrame = s.last
		}
		gap := s.first.Sub(spans[k-1].last)
		r.Timing.StepGaps = append(r.Timing.StepGaps, gap)
		if len(r.Timing.StepGaps) == 1 || gap > r.Timing.MaxStepGap {
			r.Timing.MaxStepGap = gap
		}
		if gap <= 0 {
			r.Timing.OverlappingSteps++
		}
	}
	r.Timing.TotalDuration = r.Timing.LastFrame.Sub(r.Timing.FirstFrame)

	r.Checks = []Check{
		atLeast("CompletedSteps", float64(r.CompletedSteps), float64(n)),
		atMost("MissingSteps", float64(r.MissingSteps), 0),
		atMost("UnboundResponses", float64(len(r.UnboundResponses)), 0),
		atMost("DuplicateResponses", float64(len(r.DuplicateResponses)), 0),
		atMost("OrderInversions", float64(r.OrderInversions), 0),
		atMost("OverlappingSteps", float64(r.Timing.OverlappingSteps), 0),
		isTrue("ReceivedBeforeExpiry", !resp.ReceivedAt.After(seq.ExpiresAt)),
	}
	if len(r.Timing.StepGaps) > 0 {
		r.Checks = append(r.Checks, atMost("MaxStepGapSeconds", r.Timing.MaxStepGap.Seconds(), thresholds.MaxStepGap.Seconds()))
	}
	if len(spans) > 0 {
		r.Checks = append(r.Checks, atMost("TotalDurationSeconds", r.Timing.TotalDuration.Seconds(), thresholds.MaxTotalDuration.Seconds()))
	}
	return r, nil
}

// EvaluateChallengeSequence is the package's EvaluateChallengeSequence with
// the configured thresholds. It needs no boot.
func (m *Module) EvaluateChallengeSequence(seq ChallengeSequence, resp SequenceResponse) (ChallengeSequenceResult, error) {
	return EvaluateChallengeSequence(seq, resp, m.cfg.Sequence)
}
