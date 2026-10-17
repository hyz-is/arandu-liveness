package feature_test

import (
	"context"
	"errors"
	"image"
	"image/color"
	"testing"
	"time"

	face "github.com/hyz-is/arandu-face"
	liveness "github.com/hyz-is/arandu-liveness"
)

// hugeImage claims more pixels than onnx.MaxImagePixels without holding any,
// so a frame too large to read can be given without allocating it.
type hugeImage struct{}

func (hugeImage) ColorModel() color.Model { return color.RGBAModel }
func (hugeImage) Bounds() image.Rectangle { return image.Rect(0, 0, 10000, 10000) }
func (hugeImage) At(int, int) color.Color { return color.RGBA{A: 255} }

func TestPassivePADFramesSummarizesTheFramesItEvaluates(t *testing.T) {
	m := bootModule(t, liveness.Config{MaxPADFrames: 3})
	ctx := context.Background()
	start := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

	// Five drawable frames of one cartoon moving across the canvas, and
	// three that cannot be read: no image, a box off the image, too many
	// pixels.
	frames := []liveness.PADFrame{}
	for i := 0; i < 5; i++ {
		cx := 200 + 10*float64(i)
		frames = append(frames, liveness.PADFrame{
			Image: cartoonFace(480, 640, cx, 300, 60), Box: drawnBox(cx, 300, 60),
			CapturedAt: start.Add(time.Duration(i) * 100 * time.Millisecond),
		})
	}
	frames = append(frames[:2], append([]liveness.PADFrame{
		{CapturedAt: start},
		{Image: cartoonFace(480, 640, 240, 300, 60), Box: face.Box{X: 900, Y: 900, Width: 50, Height: 50}},
		{Image: hugeImage{}, Box: face.Box{X: 10, Y: 10, Width: 100, Height: 100}},
	}, frames[2:]...)...)
	// Positions: 0, 1 drawable; 2 no image; 3 box outside; 4 too large;
	// 5, 6, 7 drawable. Of the five drawable frames, EvenlySpacedFrames(5, 3)
	// keeps 0, 2, 4 of them: positions 0, 5 and 7.

	r, err := m.PassivePADFrames(ctx, frames)
	if err != nil {
		t.Fatal(err)
	}
	if r.Submitted != 8 || r.Evaluated != 3 || r.Skipped != 5 || r.FrameLimit != 3 {
		t.Fatalf("submitted %d evaluated %d skipped %d limit %d", r.Submitted, r.Evaluated, r.Skipped, r.FrameLimit)
	}
	want := map[liveness.PADSkipReason]int{
		liveness.PADSkipNoImage: 1, liveness.PADSkipBoxOutsideImage: 1, liveness.PADSkipImageTooLarge: 1, liveness.PADSkipOverFrameLimit: 2,
	}
	for reason, n := range want {
		if r.SkippedBy[reason] != n {
			t.Errorf("%s skipped %d, want %d (%v)", reason, r.SkippedBy[reason], n, r.SkippedBy)
		}
	}
	values := []float64{}
	labels := 0
	for i, f := range r.Frames {
		evaluated := i == 0 || i == 5 || i == 7
		if f.Index != i || (f.PAD != nil) != evaluated || (f.SkipReason == "") != evaluated {
			t.Errorf("frame %d: %+v", i, f)
			continue
		}
		if !evaluated {
			if f.SkipDetail == "" {
				t.Errorf("frame %d was skipped as %s with no detail", i, f.SkipReason)
			}
			continue
		}
		single, err := m.PassivePAD(ctx, frames[i].Image, frames[i].Box)
		if err != nil {
			t.Fatal(err)
		}
		for k := range single.PerModel {
			if single.PerModel[k].Logits != f.PAD.PerModel[k].Logits {
				t.Errorf("frame %d model %d: %v here, %v alone", i, k, f.PAD.PerModel[k].Logits, single.PerModel[k].Logits)
			}
		}
		values = append(values, f.PAD.FusedRealProbability)
	}
	for _, c := range r.LabelCounts {
		labels += c
	}
	if r.FusedReal != liveness.SummarizePADSeries(values) || labels != 3 {
		t.Errorf("stats %+v, want %+v; labels %v", r.FusedReal, liveness.SummarizePADSeries(values), r.LabelCounts)
	}
	// Frame 0 was taken at 0 ms and frame 7 (the fifth drawable) at 400 ms.
	if r.Span != 400*time.Millisecond || r.ReferenceSet != liveness.MiniFASNetUpstreamReference() {
		t.Errorf("span %v reference %+v", r.Span, r.ReferenceSet)
	}
	t.Logf("fused real over %d frames: mean %.4f median %.4f min %.4f std %.4f (synthetic images, not a measurement)",
		r.FusedReal.Count, r.FusedReal.Mean, r.FusedReal.Median, r.FusedReal.Min, r.FusedReal.StdDev)
}

func TestPassivePADFramesRefusesWhatCannotBeMeasured(t *testing.T) {
	m := bootModule(t, liveness.Config{})
	img := cartoonFace(480, 640, 240, 300, 60)
	frames := []liveness.PADFrame{{Image: img, Box: drawnBox(240, 300, 60)}}

	if _, err := m.PassivePADFrames(context.Background(), nil); err == nil {
		t.Error("no frames were measured")
	}
	if _, err := m.PassivePADFrames(context.Background(), make([]liveness.PADFrame, liveness.MaxFrames+1)); err == nil {
		t.Error("more than MaxFrames frames were measured")
	}
	done, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.PassivePADFrames(done, frames); !errors.Is(err, context.Canceled) {
		t.Errorf("a done context = %v, want context.Canceled", err)
	}
	// Every frame unreadable is a measurement of nothing, not an error.
	r, err := m.PassivePADFrames(context.Background(), []liveness.PADFrame{{}, {Image: hugeImage{}, Box: face.Box{Width: 5, Height: 5}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Evaluated != 0 || r.Skipped != 2 || r.FusedReal.Count != 0 || r.Span != 0 {
		t.Errorf("nothing readable: %+v", r)
	}
	if r.FrameLimit != liveness.DefaultMaxPADFrames {
		t.Errorf("default frame limit %d", r.FrameLimit)
	}
}
