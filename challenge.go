package liveness

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	face "github.com/kuaa-io/arandu-face"
	onnx "github.com/kuaa-io/arandu-onnx"
)

// MaxFrames is the most frames EvaluateChallenge and MeasureInjectionSignals
// read from one capture.
const MaxFrames = 600

// ChallengeKind is the movement a challenge asks the subject to make.
type ChallengeKind string

// The kinds of challenge. Directions are the subject's own: turning left
// moves the nose toward the subject's left, which is the right of an
// unmirrored camera image.
const (
	// ChallengeTurnLeft asks the subject to turn the head to their left.
	ChallengeTurnLeft ChallengeKind = "turn_left"
	// ChallengeTurnRight asks the subject to turn the head to their right.
	ChallengeTurnRight ChallengeKind = "turn_right"
	// ChallengeLookUp asks the subject to tilt the head back.
	ChallengeLookUp ChallengeKind = "look_up"
	// ChallengeLookDown asks the subject to tilt the head forward.
	ChallengeLookDown ChallengeKind = "look_down"
	// ChallengeNod asks the subject to tilt the head forward and back.
	ChallengeNod ChallengeKind = "nod"
	// ChallengeBlink asks the subject to blink. Five-point landmarks cannot
	// measure it.
	ChallengeBlink ChallengeKind = "blink"
	// ChallengeSmile asks the subject to smile. Five-point landmarks cannot
	// measure it.
	ChallengeSmile ChallengeKind = "smile"
	// ChallengeOpenMouth asks the subject to open the mouth. Five-point
	// landmarks cannot measure it.
	ChallengeOpenMouth ChallengeKind = "open_mouth"
)

// The signals a challenge is measured with.
const (
	// SignalYawProxy is YawProxy, frame by frame.
	SignalYawProxy = "yaw_proxy"
	// SignalPitchProxy is PitchProxy, frame by frame.
	SignalPitchProxy = "pitch_proxy"
)

// unsupportedDense is why a challenge on eyelids or lips cannot be measured.
const unsupportedDense = "five-point landmarks (eye centres, nose tip, mouth corners) carry no eyelid or lip " +
	"contour; this challenge needs dense landmarks, which no shipped model provides"

// ChallengeKinds returns every kind, supported or not, in a fixed order.
func ChallengeKinds() []ChallengeKind {
	return []ChallengeKind{
		ChallengeTurnLeft, ChallengeTurnRight, ChallengeLookUp, ChallengeLookDown, ChallengeNod,
		ChallengeBlink, ChallengeSmile, ChallengeOpenMouth,
	}
}

// Supported reports whether five-point landmarks can measure the challenge,
// and why not when they cannot. An unknown kind is not supported.
func (k ChallengeKind) Supported() (bool, string) {
	switch k {
	case ChallengeTurnLeft, ChallengeTurnRight, ChallengeLookUp, ChallengeLookDown, ChallengeNod:
		return true, ""
	case ChallengeBlink, ChallengeSmile, ChallengeOpenMouth:
		return false, unsupportedDense
	}
	return false, fmt.Sprintf("%q is not a challenge kind", string(k))
}

// known reports whether the kind is one of ChallengeKinds.
func (k ChallengeKind) known() bool {
	for _, kind := range ChallengeKinds() {
		if k == kind {
			return true
		}
	}
	return false
}

// Challenge is what the server issues before a capture: a movement to make,
// a validity window and a nonce the client has to echo in its capture
// metadata.
type Challenge struct {
	// ID identifies the challenge.
	ID string
	// Kind is the movement asked for.
	Kind ChallengeKind
	// IssuedAt is when the server issued it.
	IssuedAt time.Time
	// ExpiresAt is when it stops being answerable.
	ExpiresAt time.Time
	// Nonce is the unpredictable value the capture has to carry back.
	Nonce string
}

// Validate reports a challenge that cannot be evaluated.
func (c Challenge) Validate() error {
	switch {
	case c.ID == "":
		return errors.New("liveness: Challenge.ID is required")
	case !c.Kind.known():
		return fmt.Errorf("liveness: Challenge.Kind %q is not a challenge kind", string(c.Kind))
	case c.Nonce == "":
		return errors.New("liveness: Challenge.Nonce is required: a challenge without one binds nothing")
	case c.IssuedAt.IsZero():
		return errors.New("liveness: Challenge.IssuedAt is required")
	case !c.ExpiresAt.After(c.IssuedAt):
		return fmt.Errorf("liveness: Challenge.ExpiresAt %v is not after IssuedAt %v", c.ExpiresAt, c.IssuedAt)
	}
	return nil
}

// NewChallenge issues a challenge of a kind, valid for ttl from issuedAt,
// with a 128-bit ID and a 256-bit nonce read from random, or from
// crypto/rand when random is nil.
func NewChallenge(kind ChallengeKind, issuedAt time.Time, ttl time.Duration, random io.Reader) (Challenge, error) {
	if random == nil {
		random = rand.Reader
	}
	if ttl <= 0 {
		return Challenge{}, fmt.Errorf("liveness: a challenge's ttl has to be positive, and is %v", ttl)
	}
	id := make([]byte, 16)
	nonce := make([]byte, 32)
	if _, err := io.ReadFull(random, id); err != nil {
		return Challenge{}, fmt.Errorf("liveness: reading the challenge id: %w", err)
	}
	if _, err := io.ReadFull(random, nonce); err != nil {
		return Challenge{}, fmt.Errorf("liveness: reading the challenge nonce: %w", err)
	}
	c := Challenge{
		ID: hex.EncodeToString(id), Kind: kind, IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(ttl),
		Nonce: hex.EncodeToString(nonce),
	}
	return c, c.Validate()
}

// NonceBinding is whether a capture echoed the nonce it was issued.
type NonceBinding struct {
	// ExpectedPresent is whether there was a nonce to echo.
	ExpectedPresent bool
	// EchoedPresent is whether the capture carried one.
	EchoedPresent bool
	// Equal is whether both are present and equal, compared in constant time.
	Equal bool
}

// BindNonce compares the nonce a challenge was issued with and the one the
// client echoed in its capture metadata.
func BindNonce(expected, echoed string) NonceBinding {
	return NonceBinding{
		ExpectedPresent: expected != "",
		EchoedPresent:   echoed != "",
		Equal:           expected != "" && subtle.ConstantTimeCompare([]byte(expected), []byte(echoed)) == 1,
	}
}

// ChallengeFrame is one frame of a challenge's capture: when it was taken, in
// the server's time base, and the face arandu-face's Detect found on it, in
// the coordinates of the unmirrored image.
type ChallengeFrame struct {
	// Timestamp is when the frame was taken.
	Timestamp time.Time
	// Face is the face detected on the frame; its landmarks and its detector
	// are what is read.
	Face face.Face
}

// ChallengeResponse is what came back for a challenge.
type ChallengeResponse struct {
	// EchoedNonce is the nonce the client put in the capture metadata.
	EchoedNonce string
	// ReceivedAt is when the server received the capture.
	ReceivedAt time.Time
	// Frames are the capture's frames, in the order they were taken.
	Frames []ChallengeFrame
}

// ChallengeThresholds are the references a challenge's measurements are
// checked against. They are versioned parameters, not a policy.
type ChallengeThresholds struct {
	// Version names this set of values. A change to any value is a new
	// version.
	Version string
	// MinFrames is the fewest frames a capture should hold.
	MinFrames int
	// MinMotionDuration and MaxMotionDuration bound the time from the first
	// frame to the end of the movement: shorter is too fast, longer too slow.
	MinMotionDuration, MaxMotionDuration time.Duration
	// MaxBaselineYaw is the largest |YawProxy| of the first frame of a turn:
	// a turn starts from a frontal pose.
	MaxBaselineYaw float64
	// MinYawExcursion is the smallest change of YawProxy, toward the
	// requested side, a turn should show.
	MinYawExcursion float64
	// MinPitchExcursion is the smallest change of PitchProxy, in the
	// requested direction, a tilt or each half of a nod should show.
	MinPitchExcursion float64
	// MinMonotonicity is the smallest share of the movement's steps that
	// should go the requested way.
	MinMonotonicity float64
	// StepEpsilon is the change between two frames below which a step is
	// ignored by Monotonicity, as landmark noise.
	StepEpsilon float64
}

// DefaultChallengeThresholds returns the references EvaluateChallenge uses
// when the configuration names none. They are starting values, not
// calibrated on any population; their version says so.
func DefaultChallengeThresholds() ChallengeThresholds {
	return ChallengeThresholds{
		Version:           "kuaa-liveness-challenge-0.1-uncalibrated",
		MinFrames:         5,
		MinMotionDuration: 300 * time.Millisecond,
		MaxMotionDuration: 5 * time.Second,
		MaxBaselineYaw:    0.12,
		MinYawExcursion:   0.25,
		MinPitchExcursion: 0.10,
		MinMonotonicity:   0.7,
		StepEpsilon:       0.005,
	}
}

// Validate reports a set of references that cannot be checked against.
func (t ChallengeThresholds) Validate() error {
	if t.Version == "" {
		return errors.New("liveness: ChallengeThresholds.Version is required: a threshold nobody can name is one nobody can audit")
	}
	if t.MinFrames < 2 || t.MinFrames > MaxFrames {
		return fmt.Errorf("liveness: ChallengeThresholds.MinFrames is %d, and has to be between 2 and %d", t.MinFrames, MaxFrames)
	}
	if t.MinMotionDuration < 0 || t.MaxMotionDuration <= t.MinMotionDuration {
		return fmt.Errorf("liveness: ChallengeThresholds.MinMotionDuration %v and MaxMotionDuration %v do not bound a range", t.MinMotionDuration, t.MaxMotionDuration)
	}
	for _, f := range []struct {
		name  string
		value float64
	}{
		{"MaxBaselineYaw", t.MaxBaselineYaw}, {"MinYawExcursion", t.MinYawExcursion},
		{"MinPitchExcursion", t.MinPitchExcursion}, {"MinMonotonicity", t.MinMonotonicity},
		{"StepEpsilon", t.StepEpsilon},
	} {
		if !finite(f.value) || f.value < 0 {
			return fmt.Errorf("liveness: ChallengeThresholds.%s is %v, and has to be a finite value of at least 0", f.name, f.value)
		}
	}
	if t.MinMonotonicity > 1 {
		return fmt.Errorf("liveness: ChallengeThresholds.MinMonotonicity is %v, and has to be at most 1", t.MinMonotonicity)
	}
	return nil
}

// ChallengeObservation is the movement measured on the frames.
type ChallengeObservation struct {
	// Signal is SignalYawProxy or SignalPitchProxy.
	Signal string
	// Series is the signal on every frame, in order.
	Series []float64
	// Baseline is the signal on the first frame.
	Baseline float64
	// Peak is the signal on PeakIndex: the farthest the movement went the
	// requested way (for a nod, the forward extreme).
	Peak float64
	// PeakIndex is the frame of Peak.
	PeakIndex int
	// EndIndex is the frame the movement is measured to: PeakIndex, or for a
	// nod the backward extreme after it.
	EndIndex int
	// Excursion is how far the signal moved the requested way from Baseline;
	// for a nod, the smaller of the forward and the backward half.
	Excursion float64
}

// ChallengeTiming is when things happened.
type ChallengeTiming struct {
	// IssuedAt, ExpiresAt and ReceivedAt are the challenge's and the
	// response's times.
	IssuedAt, ExpiresAt, ReceivedAt time.Time
	// FirstFrame and LastFrame are the first and last frames' timestamps.
	FirstFrame, LastFrame time.Time
	// ResponseLatency is from IssuedAt to the first frame.
	ResponseLatency time.Duration
	// CaptureDuration is from the first frame to the last.
	CaptureDuration time.Duration
	// MotionDuration is from the first frame to the frame the movement is
	// measured to.
	MotionDuration time.Duration
	// FramesOutsideValidity is how many frames were taken before IssuedAt or
	// after ExpiresAt.
	FramesOutsideValidity int
	// NonIncreasingSteps is how many frames are not later than the frame
	// before them.
	NonIncreasingSteps int
}

// ChallengeResult is what EvaluateChallenge returns: the movement observed,
// its timing, the nonce binding, and each measurement checked against the
// versioned references.
type ChallengeResult struct {
	// ChallengeID is the challenge's ID.
	ChallengeID string
	// Kind is the challenge's kind.
	Kind ChallengeKind
	// Supported is whether five-point landmarks can measure the kind.
	Supported bool
	// UnsupportedReason says why not, when Supported is false.
	UnsupportedReason string
	// Observed is the movement measured, empty for an unsupported kind.
	Observed ChallengeObservation
	// Completed is whether every check is within its reference: an
	// observation that the recorded movement matches what was asked, under
	// Thresholds.Version, and not a conclusion about the person.
	Completed bool
	// Timing is when things happened.
	Timing ChallengeTiming
	// FrameCount is how many frames were read.
	FrameCount int
	// Monotonicity is the share of the movement's steps larger than
	// StepEpsilon that went the requested way, 0..1.
	Monotonicity float64
	// Nonce is whether the capture echoed the challenge's nonce.
	Nonce NonceBinding
	// LandmarkModels are the distinct detectors that found the faces the
	// landmarks come from.
	LandmarkModels []onnx.ModelInfo
	// Checks hold each measurement against its reference, in a fixed order.
	Checks []Check
	// Thresholds are the references the checks used, with their version.
	Thresholds ChallengeThresholds
}

// YawProxy is the nose tip's offset from the eyes' midpoint along the eye
// line, in inter-ocular distances: 0 for a frontal face, positive when the
// nose points toward the image's right (the subject's left, on an unmirrored
// image). It is the yaw signal arandu-face's Quality reports, a geometric
// proxy, not an angle.
func YawProxy(landmarks [5]face.Point) (float64, error) {
	re, le, nose := landmarks[0], landmarks[1], landmarks[2]
	dx, dy := le.X-re.X, le.Y-re.Y
	iod := math.Hypot(dx, dy)
	if !finite(iod, nose.X, nose.Y) || iod == 0 {
		return 0, fmt.Errorf("liveness: the eye landmarks %+v and %+v give no eye line", re, le)
	}
	mid := face.Point{X: (re.X + le.X) / 2, Y: (re.Y + le.Y) / 2}
	return ((nose.X-mid.X)*dx + (nose.Y-mid.Y)*dy) / (iod * iod), nil
}

// PitchProxy is the nose tip's position along the line from the eyes'
// midpoint to the mouth corners' midpoint, as a share of that line: about
// 0.5 for a frontal face on the ArcFace template, smaller when the head tilts
// back and larger when it tilts forward. It is a geometric proxy, not an
// angle.
func PitchProxy(landmarks [5]face.Point) (float64, error) {
	re, le, nose, rm, lm := landmarks[0], landmarks[1], landmarks[2], landmarks[3], landmarks[4]
	eyes := face.Point{X: (re.X + le.X) / 2, Y: (re.Y + le.Y) / 2}
	mouth := face.Point{X: (rm.X + lm.X) / 2, Y: (rm.Y + lm.Y) / 2}
	dx, dy := mouth.X-eyes.X, mouth.Y-eyes.Y
	length2 := dx*dx + dy*dy
	if !finite(length2, nose.X, nose.Y) || length2 == 0 {
		return 0, fmt.Errorf("liveness: the eye and mouth landmarks give no vertical axis")
	}
	return ((nose.X-eyes.X)*dx + (nose.Y-eyes.Y)*dy) / length2, nil
}

// EvaluateChallenge measures a challenge's response from the landmarks of its
// frames, in pure Go, and checks every measurement against the references.
//
// It always measures the nonce binding, the timing and the frame count. For a
// supported kind it also measures the movement: YawProxy for a turn,
// PitchProxy for a tilt or a nod, its excursion from the first frame, the
// share of steps that went the requested way, and how long it took. An
// unsupported kind returns Supported false with the reason, and no movement.
// Completed is true only when every check is within its reference. Nothing
// runs a model, and nothing here concludes the subject is present: a
// replayed video of the right movement completes a challenge too, which is
// what the nonce, the timing and MeasureInjectionSignals are for.
func EvaluateChallenge(ch Challenge, resp ChallengeResponse, thresholds ChallengeThresholds) (ChallengeResult, error) {
	if err := ch.Validate(); err != nil {
		return ChallengeResult{}, err
	}
	if err := thresholds.Validate(); err != nil {
		return ChallengeResult{}, err
	}
	if resp.ReceivedAt.IsZero() {
		return ChallengeResult{}, errors.New("liveness: ChallengeResponse.ReceivedAt is required: it is the server's receipt time")
	}
	if len(resp.Frames) > MaxFrames {
		return ChallengeResult{}, fmt.Errorf("liveness: %d frames is more than %d", len(resp.Frames), MaxFrames)
	}
	supported, reason := ch.Kind.Supported()
	r := ChallengeResult{
		ChallengeID: ch.ID, Kind: ch.Kind, Supported: supported, UnsupportedReason: reason,
		FrameCount: len(resp.Frames), Nonce: BindNonce(ch.Nonce, resp.EchoedNonce), Thresholds: thresholds,
	}
	r.Timing = ChallengeTiming{IssuedAt: ch.IssuedAt, ExpiresAt: ch.ExpiresAt, ReceivedAt: resp.ReceivedAt}
	seen := map[onnx.ModelInfo]bool{}
	for i, f := range resp.Frames {
		if f.Timestamp.Before(ch.IssuedAt) || f.Timestamp.After(ch.ExpiresAt) {
			r.Timing.FramesOutsideValidity++
		}
		if i > 0 && !f.Timestamp.After(resp.Frames[i-1].Timestamp) {
			r.Timing.NonIncreasingSteps++
		}
		if d := f.Face.Detector; d.ID != "" && !seen[d] {
			seen[d] = true
			r.LandmarkModels = append(r.LandmarkModels, d)
		}
	}
	if n := len(resp.Frames); n > 0 {
		r.Timing.FirstFrame, r.Timing.LastFrame = resp.Frames[0].Timestamp, resp.Frames[n-1].Timestamp
		r.Timing.ResponseLatency = r.Timing.FirstFrame.Sub(ch.IssuedAt)
		r.Timing.CaptureDuration = r.Timing.LastFrame.Sub(r.Timing.FirstFrame)
	}

	r.Checks = []Check{
		isTrue("Supported", supported),
		isTrue("NonceEchoed", r.Nonce.Equal),
		isTrue("ReceivedBeforeExpiry", !resp.ReceivedAt.After(ch.ExpiresAt)),
		atMost("FramesOutsideValidity", float64(r.Timing.FramesOutsideValidity), 0),
		atMost("NonIncreasingSteps", float64(r.Timing.NonIncreasingSteps), 0),
		atLeast("FrameCount", float64(r.FrameCount), float64(thresholds.MinFrames)),
	}
	if supported && len(resp.Frames) > 0 {
		if err := r.measureMovement(resp.Frames, thresholds); err != nil {
			return ChallengeResult{}, err
		}
	}
	r.Completed = supported && len(resp.Frames) > 0
	for _, c := range r.Checks {
		r.Completed = r.Completed && c.WithinReference
	}
	return r, nil
}

// measureMovement fills Observed, Monotonicity and MotionDuration, and
// appends the movement's checks.
func (r *ChallengeResult) measureMovement(frames []ChallengeFrame, t ChallengeThresholds) error {
	signal, measure, direction := SignalYawProxy, YawProxy, 1.0
	switch r.Kind {
	case ChallengeTurnRight:
		direction = -1
	case ChallengeLookUp:
		signal, measure, direction = SignalPitchProxy, PitchProxy, -1
	case ChallengeLookDown, ChallengeNod:
		signal, measure = SignalPitchProxy, PitchProxy
	}
	series := make([]float64, len(frames))
	for i, f := range frames {
		v, err := measure(f.Face.Landmarks)
		if err != nil {
			return fmt.Errorf("liveness: frame %d: %w", i, err)
		}
		series[i] = v
	}
	o := ChallengeObservation{Signal: signal, Series: series, Baseline: series[0]}
	// toward is the signal's change from the baseline, positive the requested
	// way.
	toward := func(i int) float64 { return direction * (series[i] - series[0]) }
	for i := range series {
		if toward(i) > toward(o.PeakIndex) {
			o.PeakIndex = i
		}
	}
	o.Peak, o.EndIndex, o.Excursion = series[o.PeakIndex], o.PeakIndex, toward(o.PeakIndex)

	steps, along := 0, 0
	count := func(from, to int, sign float64) {
		for i := from + 1; i <= to; i++ {
			delta := sign * (series[i] - series[i-1])
			if math.Abs(delta) <= t.StepEpsilon {
				continue
			}
			steps++
			if delta > 0 {
				along++
			}
		}
	}
	count(0, o.PeakIndex, direction)
	excursionLimit := t.MinYawExcursion
	if signal == SignalPitchProxy {
		excursionLimit = t.MinPitchExcursion
	}
	if r.Kind == ChallengeNod {
		// The backward half: from the forward extreme to the lowest value
		// after it.
		for i := o.PeakIndex; i < len(series); i++ {
			if series[i] < series[o.EndIndex] {
				o.EndIndex = i
			}
		}
		o.Excursion = math.Min(o.Excursion, series[o.PeakIndex]-series[o.EndIndex])
		count(o.PeakIndex, o.EndIndex, -1)
	}
	if steps > 0 {
		r.Monotonicity = float64(along) / float64(steps)
	}
	r.Observed = o
	r.Timing.MotionDuration = frames[o.EndIndex].Timestamp.Sub(frames[0].Timestamp)

	if signal == SignalYawProxy {
		r.Checks = append(r.Checks, absAtMost("BaselineYaw", o.Baseline, t.MaxBaselineYaw))
	}
	r.Checks = append(r.Checks,
		atLeast("Excursion", o.Excursion, excursionLimit),
		atLeast("Monotonicity", r.Monotonicity, t.MinMonotonicity),
		between("MotionDurationSeconds", r.Timing.MotionDuration.Seconds(),
			t.MinMotionDuration.Seconds(), t.MaxMotionDuration.Seconds()),
	)
	return nil
}

// EvaluateChallenge is the package's EvaluateChallenge with the configured
// thresholds. It needs no boot.
func (m *Module) EvaluateChallenge(ch Challenge, resp ChallengeResponse) (ChallengeResult, error) {
	return EvaluateChallenge(ch, resp, m.cfg.Challenge)
}
