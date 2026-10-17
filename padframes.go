package liveness

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"sort"
	"time"

	face "github.com/hyz-is/arandu-face"
	onnx "github.com/hyz-is/arandu-onnx"
)

// DefaultMaxPADFrames is how many frames PassivePADFrames runs the models on
// when the configuration names no limit.
const DefaultMaxPADFrames = 8

// PADFrame is one frame of a capture for PassivePADFrames: the image, the
// face box found on it, normally by arandu-face's Detect, and when it was
// taken.
type PADFrame struct {
	// Image is the decoded frame.
	Image image.Image
	// Box is the face box on Image, in Image's coordinates.
	Box face.Box
	// CapturedAt is when the frame was taken, in the server's time base. It
	// may be zero, and then the result has no span.
	CapturedAt time.Time
}

// PADSkipReason is why PassivePADFrames did not run the models on a frame.
type PADSkipReason string

// The reasons a frame is skipped.
const (
	// PADSkipNoImage is a frame without an image, or with an empty one.
	PADSkipNoImage PADSkipReason = "no_image"
	// PADSkipImageTooLarge is a frame of more than onnx.MaxImagePixels.
	PADSkipImageTooLarge PADSkipReason = "image_too_large"
	// PADSkipBoxOutsideImage is a face box with no area inside the image.
	PADSkipBoxOutsideImage PADSkipReason = "box_outside_image"
	// PADSkipOverFrameLimit is a frame left out to keep the frames evaluated
	// within Config.MaxPADFrames.
	PADSkipOverFrameLimit PADSkipReason = "over_frame_limit"
)

// PADFrameResult is what PassivePADFrames measured on one frame, or why it
// measured nothing.
type PADFrameResult struct {
	// Index is the frame's position in the frames given.
	Index int
	// CapturedAt is the frame's capture time.
	CapturedAt time.Time
	// SkipReason is why the frame was not evaluated, or empty when it was.
	SkipReason PADSkipReason
	// SkipDetail says more about SkipReason.
	SkipDetail string
	// PAD is PassivePAD's result on the frame, or nil when it was skipped.
	PAD *PADResult
}

// PADSeriesStats summarizes a series of values, in the order given.
type PADSeriesStats struct {
	// Count is how many values there are.
	Count int
	// Mean, Median, Min and Max are the values' mean, median (the mean of
	// the two middle values of an even count), smallest and largest.
	Mean, Median, Min, Max float64
	// Variance is the population variance, and StdDev its square root.
	Variance, StdDev float64
	// MeanAbsStep and MaxAbsStep are the mean and the largest absolute
	// change between consecutive values: how stable the series is from one
	// frame to the next.
	MeanAbsStep, MaxAbsStep float64
}

// SummarizePADSeries summarizes values. No value is NaN or infinite in what
// PassivePAD returns; a series holding one gives NaN statistics. An empty
// series is all zeros.
func SummarizePADSeries(values []float64) PADSeriesStats {
	s := PADSeriesStats{Count: len(values)}
	if len(values) == 0 {
		return s
	}
	s.Mean, s.StdDev = meanStd(values)
	s.Variance = s.StdDev * s.StdDev
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	s.Min, s.Max = sorted[0], sorted[len(sorted)-1]
	if mid := len(sorted) / 2; len(sorted)%2 == 1 {
		s.Median = sorted[mid]
	} else {
		s.Median = (sorted[mid-1] + sorted[mid]) / 2
	}
	if len(values) > 1 {
		total := 0.0
		for i := 1; i < len(values); i++ {
			step := math.Abs(values[i] - values[i-1])
			total += step
			s.MaxAbsStep = math.Max(s.MaxAbsStep, step)
		}
		s.MeanAbsStep = total / float64(len(values)-1)
	}
	return s
}

// PADFramesResult is what PassivePADFrames returns: every frame's PAD result
// or the reason it was skipped, and the fused bona fide probability
// summarized over the frames evaluated, with the reference set beside it. It
// holds no verdict.
type PADFramesResult struct {
	// Frames are the frames' results, in the order given.
	Frames []PADFrameResult
	// Submitted is how many frames were given, Evaluated how many the models
	// ran on, and Skipped how many they did not.
	Submitted, Evaluated, Skipped int
	// SkippedBy counts the skipped frames by reason.
	SkippedBy map[PADSkipReason]int
	// FrameLimit is the most frames the models were allowed to run on.
	FrameLimit int
	// FusedReal summarizes FusedRealProbability over the frames evaluated,
	// in their order.
	FusedReal PADSeriesStats
	// LabelCounts is how many evaluated frames had each FusedLabel, by
	// class.
	LabelCounts [PADClasses]int
	// Span is from the first evaluated frame's CapturedAt to the last's,
	// zero when either is unknown.
	Span time.Duration
	// ReferenceSet is the reference the application's policy reads the
	// fused probabilities against, with its version.
	ReferenceSet PADReferenceSet
}

// PassivePADFrames runs PassivePAD on several frames of one capture and
// summarizes the fused bona fide probability across them. It needs
// Config.EnablePassive.
//
// A frame with no image, one too large, or a box with no area inside its
// image is skipped with its reason rather than failing the call. Of the
// frames left, at most Config.MaxPADFrames are evaluated, spaced evenly from
// the first to the last so the capture's whole duration is covered; the
// others are skipped as over the limit, which bounds the CPU a capture costs.
// The face boxes come from the caller: nothing here detects a face. A model
// error or a done context fails the call.
func (m *Module) PassivePADFrames(ctx context.Context, frames []PADFrame) (PADFramesResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := m.padReady("PassivePADFrames"); err != nil {
		return PADFramesResult{}, err
	}
	if len(frames) == 0 {
		return PADFramesResult{}, errors.New("liveness: PassivePADFrames of no frames")
	}
	if len(frames) > MaxFrames {
		return PADFramesResult{}, fmt.Errorf("liveness: %d frames is more than %d", len(frames), MaxFrames)
	}

	r := PADFramesResult{
		Submitted: len(frames), FrameLimit: m.cfg.MaxPADFrames,
		SkippedBy: map[PADSkipReason]int{}, ReferenceSet: m.cfg.PADReference,
	}
	r.Frames = make([]PADFrameResult, len(frames))
	usable := []int{}
	for i, f := range frames {
		r.Frames[i] = PADFrameResult{Index: i, CapturedAt: f.CapturedAt}
		if reason, detail := padFrameProblem(f); reason != "" {
			r.Frames[i].SkipReason, r.Frames[i].SkipDetail = reason, detail
			continue
		}
		usable = append(usable, i)
	}
	chosen := map[int]bool{}
	for _, k := range EvenlySpacedFrames(len(usable), r.FrameLimit) {
		chosen[usable[k]] = true
	}
	for _, i := range usable {
		if !chosen[i] {
			r.Frames[i].SkipReason = PADSkipOverFrameLimit
			r.Frames[i].SkipDetail = fmt.Sprintf("%d frames could be evaluated and the limit is %d", len(usable), r.FrameLimit)
		}
	}

	values := []float64{}
	var first, last time.Time
	for i := range r.Frames {
		if r.Frames[i].SkipReason != "" {
			r.Skipped++
			r.SkippedBy[r.Frames[i].SkipReason]++
			continue
		}
		if err := ctx.Err(); err != nil {
			return PADFramesResult{}, err
		}
		pad, err := m.passivePAD(ctx, frames[i].Image, frames[i].Box)
		if err != nil {
			return PADFramesResult{}, fmt.Errorf("liveness: frame %d: %w", i, err)
		}
		r.Frames[i].PAD = &pad
		r.Evaluated++
		values = append(values, pad.FusedRealProbability)
		r.LabelCounts[pad.FusedLabel]++
		if r.Evaluated == 1 {
			first = frames[i].CapturedAt
		}
		last = frames[i].CapturedAt
	}
	r.FusedReal = SummarizePADSeries(values)
	if !first.IsZero() && !last.IsZero() {
		r.Span = last.Sub(first)
	}
	return r, nil
}

// padFrameProblem is why a frame cannot be given to PassivePAD, or nothing.
func padFrameProblem(f PADFrame) (PADSkipReason, string) {
	if f.Image == nil || f.Image.Bounds().Empty() {
		return PADSkipNoImage, "the frame has no image, or an empty one"
	}
	b := f.Image.Bounds()
	if int64(b.Dx())*int64(b.Dy()) > onnx.MaxImagePixels {
		return PADSkipImageTooLarge, fmt.Sprintf("%dx%d is more than %d pixels", b.Dx(), b.Dy(), onnx.MaxImagePixels)
	}
	frame := face.Box{X: float64(b.Min.X), Y: float64(b.Min.Y), Width: float64(b.Dx()), Height: float64(b.Dy())}
	box := f.Box
	if !finite(box.X, box.Y, box.Width, box.Height) || box.Area() <= 0 || face.IoU(box, frame) <= 0 {
		return PADSkipBoxOutsideImage, fmt.Sprintf("the face box %+v has no area inside the image %v", box, b)
	}
	return "", ""
}

// EvenlySpacedFrames returns limit positions among count, in increasing
// order, spread evenly from the first to the last and rounded to the nearest:
// the frames PassivePADFrames evaluates when more than its limit could be.
// It returns every position when there are no more than limit, and none when
// limit is not positive.
func EvenlySpacedFrames(count, limit int) []int {
	if count <= limit {
		out := make([]int, count)
		for i := range out {
			out[i] = i
		}
		return out
	}
	if limit <= 0 {
		return nil
	}
	if limit == 1 {
		return []int{0}
	}
	out := make([]int, limit)
	for i := range out {
		// round(i·(count−1)/(limit−1)), in integers.
		out[i] = (2*i*(count-1) + (limit - 1)) / (2 * (limit - 1))
	}
	return out
}
