package unit_test

import (
	"errors"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	liveness "github.com/hyz-is/arandu-liveness"
	onnx "github.com/hyz-is/arandu-onnx"
)

// The images here are drawn in Go: gratings, a grid of dark lines like the
// gaps between a screen's pixels, smooth ramps and flat fields. None is a
// photograph and none is a capture of a screen.

// drawn fills a w by h image with the grey value of f at each pixel.
func drawn(w, h int, f func(x, y int) float64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(math.Round(math.Max(0, math.Min(255, f(x, y)))))
			img.SetRGBA(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return img
}

// grating is 128 + 100·sin(2π(fx·x + fy·y)).
func grating(w, h int, fx, fy float64) *image.RGBA {
	return drawn(w, h, func(x, y int) float64 { return 128 + 100*math.Sin(2*math.Pi*(fx*float64(x)+fy*float64(y))) })
}

// ramp rises smoothly from 20 at the left to 230 at the right, with a slight
// fall from top to bottom.
func ramp(w, h int) *image.RGBA {
	return drawn(w, h, func(x, y int) float64 {
		return 20 + 210*float64(x)/float64(w-1) - 15*float64(y)/float64(h-1)
	})
}

// gridded draws dark lines two pixels wide every pitch pixels, both ways,
// over base.
func gridded(base *image.RGBA, pitch int) *image.RGBA {
	b := base.Bounds()
	out := image.NewRGBA(b)
	copy(out.Pix, base.Pix)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if x%pitch < 2 || y%pitch < 2 {
				c := base.RGBAAt(x, y)
				out.SetRGBA(x, y, color.RGBA{R: c.R / 4, G: c.G / 4, B: c.B / 4, A: 255})
			}
		}
	}
	return out
}

func screenReplay(t *testing.T, img image.Image, region image.Rectangle) liveness.ScreenReplaySignals {
	t.Helper()
	s, err := liveness.MeasureScreenReplay(img, region, liveness.DefaultScreenReplayReference())
	if err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]float64{
		"PeriodicPeakRatio": s.PeriodicPeakRatio, "BandEnergyRatio": s.BandEnergyRatio, "PeakFrequency": s.PeakFrequency,
		"PeakOrientation": s.PeakOrientation, "SpecularHighlightRatio": s.SpecularHighlightRatio, "MeanLuma": s.MeanLuma,
	} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			t.Errorf("%s is %v", name, v)
		}
	}
	if s.PeriodicPeakRatio > 1+1e-9 || s.BandEnergyRatio > 1+1e-9 || s.PeakOrientation >= 180 {
		t.Errorf("signals out of range: %+v", s)
	}
	return s
}

func TestAGratingIsFoundAtItsFrequencyAndOrientation(t *testing.T) {
	t.Parallel()

	// A period of 8 pixels on a 256 grid is bin 32: 32/256 = 0.125 cycles
	// per pixel. The periodic Hann window spreads it over bins 31 to 33 on
	// each axis, all of which PeriodicPeakRatio holds, so a pure grating
	// keeps nearly all its energy there; rounding to 8 bits leaves a little
	// elsewhere.
	for name, tc := range map[string]struct {
		fx, fy           float64
		radius, ordering float64
	}{
		"left to right":   {0.125, 0, 0.125, 0},
		"top to bottom":   {0, 0.125, 0.125, 90},
		"to bottom right": {0.125, 0.125, math.Hypot(0.125, 0.125), 45},
		"to bottom left":  {-0.125, 0.125, math.Hypot(0.125, 0.125), 135},
	} {
		s := screenReplay(t, grating(256, 256, tc.fx, tc.fy), image.Rectangle{})
		if s.AnalysisSize != 256 || !near2(s.PeakFrequency, tc.radius) || math.Abs(s.PeakOrientation-tc.ordering) > 1e-9 ||
			s.PeriodicPeakRatio < 0.95 || s.BandEnergyRatio < 0.95 {
			t.Errorf("%s: %+v", name, s)
		}
	}
}

func TestAPixelGridScoresAboveASmoothImage(t *testing.T) {
	t.Parallel()

	smooth := screenReplay(t, ramp(256, 256), image.Rectangle{})
	grid := screenReplay(t, gridded(drawn(256, 256, func(int, int) float64 { return 160 }), 8), image.Rectangle{})
	overRamp := screenReplay(t, gridded(ramp(256, 256), 8), image.Rectangle{})

	// The windowed ramp is smooth, so almost nothing of it reaches the band
	// above 0.08 cycles per pixel.
	if smooth.PeriodicPeakRatio > 0.01 || smooth.BandEnergyRatio > 0.01 {
		t.Errorf("a smooth ramp: %+v", smooth)
	}
	if grid.PeriodicPeakRatio < 10*smooth.PeriodicPeakRatio || grid.PeriodicPeakRatio < 0.05 {
		t.Errorf("a grid %v against a ramp %v", grid.PeriodicPeakRatio, smooth.PeriodicPeakRatio)
	}
	if overRamp.PeriodicPeakRatio < 10*smooth.PeriodicPeakRatio || overRamp.BandEnergyRatio < 10*smooth.BandEnergyRatio {
		t.Errorf("a grid over the ramp %+v against the ramp %+v", overRamp, smooth)
	}
	// Lines every 8 pixels repeat at 1/8 and its harmonics, on the axes.
	if k := grid.PeakFrequency * 8; math.Abs(k-math.Round(k)) > 1e-9 || math.Mod(grid.PeakOrientation, 90) != 0 {
		t.Errorf("the grid's peak at %v cycles per pixel, %v degrees", grid.PeakFrequency, grid.PeakOrientation)
	}
	t.Logf("periodic peak ratio: grid %.4f, grid over ramp %.4f, ramp %.6f (synthetic images, uncalibrated)",
		grid.PeriodicPeakRatio, overRamp.PeriodicPeakRatio, smooth.PeriodicPeakRatio)
}

func TestScreenReplaySignalsAreDeterministicAndVersioned(t *testing.T) {
	t.Parallel()

	img := gridded(ramp(300, 200), 6)
	first, again := screenReplay(t, img, image.Rectangle{}), screenReplay(t, img, image.Rectangle{})
	if first != again {
		t.Errorf("two runs on one image: %+v and %+v", first, again)
	}
	ref := liveness.DefaultScreenReplayReference()
	if first.Method != liveness.ScreenReplayMethod || first.Version != ref.Version || first.ReferenceSet != ref ||
		!strings.Contains(ref.Version, "experimental-uncalibrated") || !strings.Contains(ref.Status, "uncalibrated") {
		t.Errorf("method %q version %q reference %+v", first.Method, first.Version, first.ReferenceSet)
	}
	// A flat field has no spectral energy to share out: every spectral
	// signal is 0, not NaN.
	flat := screenReplay(t, drawn(64, 64, func(int, int) float64 { return 128 }), image.Rectangle{})
	if flat.PeriodicPeakRatio != 0 || flat.BandEnergyRatio != 0 || flat.PeakFrequency != 0 || flat.SpecularHighlightRatio != 0 ||
		math.Abs(flat.MeanLuma-128) > 1e-9 {
		t.Errorf("a flat field: %+v", flat)
	}
}

func TestSpecularHighlightsAreCountedOnTheRegion(t *testing.T) {
	t.Parallel()

	// 100×100 of grey 128, a white patch of 10×20 = 200 pixels, and a
	// near-white patch (250, 250, 200) of 10×10: luma 0.299·250 + 0.587·250
	// + 0.114·200 = 244.3, bright enough, but a channel spread of 50 is not
	// the colour of the light. 200 of 10 000 pixels are highlights; the mean
	// luma is (9700·128 + 200·255 + 100·244.3) / 10 000 = 131.703.
	img := drawn(100, 100, func(int, int) float64 { return 128 })
	for y := 10; y < 30; y++ {
		for x := 10; x < 20; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	for y := 50; y < 60; y++ {
		for x := 50; x < 60; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 250, G: 250, B: 200, A: 255})
		}
	}
	s := screenReplay(t, img, image.Rectangle{})
	if !near2(s.SpecularHighlightRatio, 0.02) || math.Abs(s.MeanLuma-131.703) > 1e-6 {
		t.Errorf("highlights %v mean luma %v", s.SpecularHighlightRatio, s.MeanLuma)
	}
	// The region holding only the white patch's 200 pixels in 40×40.
	region := screenReplay(t, img, image.Rect(0, 0, 40, 40))
	if !near2(region.SpecularHighlightRatio, 200.0/1600) || region.Region != image.Rect(0, 0, 40, 40) || region.AnalysisSize != 32 {
		t.Errorf("a region: %+v", region)
	}
}

func TestTheRegionIsTheFaceCrop(t *testing.T) {
	t.Parallel()

	// The left half is a smooth ramp, the right half the same ramp with a
	// grid on it. Measured on each half, only the right shows the grid.
	left, right := image.Rect(0, 0, 200, 200), image.Rect(200, 0, 400, 200)
	img := ramp(400, 200)
	g := gridded(ramp(400, 200), 8)
	for y := 0; y < 200; y++ {
		copy(img.Pix[y*img.Stride+200*4:y*img.Stride+400*4], g.Pix[y*g.Stride+200*4:y*g.Stride+400*4])
	}
	l, r := screenReplay(t, img, left), screenReplay(t, img, right)
	if r.PeriodicPeakRatio < 10*l.PeriodicPeakRatio || l.Region != left || r.Region != right || r.AnalysisSize != 128 {
		t.Errorf("left %+v right %+v", l, r)
	}
	// A sub-image keeps its parent's coordinates: the same region of either
	// gives the same signals.
	if sub := screenReplay(t, img.SubImage(image.Rect(150, 0, 400, 200)), right); sub != r {
		t.Errorf("the sub-image gives %+v, the image %+v", sub, r)
	}
	// A region partly outside the image is cut to it.
	if cut := screenReplay(t, img, image.Rect(200, -50, 450, 200)); cut.Region != right || cut != r {
		t.Errorf("a region partly outside: %+v", cut.Region)
	}
	// 600×400 is analysed at 256; 100×80 at the largest power of two that
	// fits, 64.
	if s := screenReplay(t, ramp(600, 400), image.Rectangle{}); s.AnalysisSize != 256 {
		t.Errorf("600x400 analysed at %d", s.AnalysisSize)
	}
	if s := screenReplay(t, ramp(100, 80), image.Rectangle{}); s.AnalysisSize != 64 {
		t.Errorf("100x80 analysed at %d", s.AnalysisSize)
	}
}

// claimsHuge reports more pixels than onnx.MaxImagePixels without holding
// them.
type claimsHuge struct{}

func (claimsHuge) ColorModel() color.Model { return color.RGBAModel }
func (claimsHuge) Bounds() image.Rectangle { return image.Rect(0, 0, 10000, 10000) }
func (claimsHuge) At(int, int) color.Color { return color.RGBA{A: 255} }

func TestMeasureScreenReplayRefusesWhatCannotBeMeasured(t *testing.T) {
	t.Parallel()

	ref := liveness.DefaultScreenReplayReference()
	img := ramp(100, 100)
	for name, region := range map[string]image.Rectangle{
		"outside the image": image.Rect(200, 200, 300, 300),
		"too small":         image.Rect(0, 0, 20, 100),
	} {
		if _, err := liveness.MeasureScreenReplay(img, region, ref); err == nil {
			t.Errorf("%s: measured", name)
		}
	}
	if _, err := liveness.MeasureScreenReplay(nil, image.Rectangle{}, ref); err == nil {
		t.Error("a nil image was measured")
	}
	if _, err := liveness.MeasureScreenReplay(claimsHuge{}, image.Rectangle{}, ref); !errors.Is(err, onnx.ErrImageTooLarge) {
		t.Errorf("an image too large = %v, want ErrImageTooLarge", err)
	}
	for name, tc := range map[string]struct {
		mutate func(*liveness.ScreenReplayReferenceSet)
		field  string
	}{
		"no version":          {func(r *liveness.ScreenReplayReferenceSet) { r.Version = "" }, "Version"},
		"not a power of two":  {func(r *liveness.ScreenReplayReferenceSet) { r.AnalysisSize = 100 }, "AnalysisSize"},
		"too small a grid":    {func(r *liveness.ScreenReplayReferenceSet) { r.AnalysisSize = 16 }, "AnalysisSize"},
		"too large a grid":    {func(r *liveness.ScreenReplayReferenceSet) { r.AnalysisSize = 2048 }, "AnalysisSize"},
		"no band":             {func(r *liveness.ScreenReplayReferenceSet) { r.BandLow = 0 }, "BandLow"},
		"a band past corners": {func(r *liveness.ScreenReplayReferenceSet) { r.BandHigh = 0.8 }, "BandHigh"},
		"an inverted band":    {func(r *liveness.ScreenReplayReferenceSet) { r.BandHigh = r.BandLow }, "BandHigh"},
		"a luma above 255":    {func(r *liveness.ScreenReplayReferenceSet) { r.HighlightLuma = 300 }, "HighlightLuma"},
		"a NaN chroma":        {func(r *liveness.ScreenReplayReferenceSet) { r.HighlightMaxChroma = math.NaN() }, "HighlightMaxChroma"},
	} {
		bad := ref
		tc.mutate(&bad)
		_, err := liveness.MeasureScreenReplay(img, image.Rectangle{}, bad)
		if err == nil || !strings.Contains(err.Error(), tc.field) {
			t.Errorf("%s: %v, want an error naming %s", name, err, tc.field)
		}
		// A config whose reference has no version takes the default.
		if err := (liveness.Config{ScreenReplay: bad}).Validate(); err == nil && bad.Version != "" {
			t.Errorf("%s: a config carrying it was accepted", name)
		}
	}
}

func TestTheModuleMeasuresScreenReplayWithItsConfiguredReference(t *testing.T) {
	t.Parallel()

	rt, err := onnx.NewRuntime(onnx.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ref := liveness.DefaultScreenReplayReference()
	ref.Version = "test-screen-replay-64"
	ref.AnalysisSize = 64
	m, err := liveness.New(liveness.Config{ScreenReplay: ref}, rt)
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.MeasureScreenReplay(ramp(200, 200), image.Rectangle{})
	if err != nil {
		t.Fatal(err)
	}
	if s.AnalysisSize != 64 || s.Version != "test-screen-replay-64" {
		t.Errorf("analysed at %d under %q", s.AnalysisSize, s.Version)
	}
	if c := (liveness.Config{}).WithDefaults(); c.ScreenReplay != liveness.DefaultScreenReplayReference() {
		t.Errorf("default reference %+v", c.ScreenReplay)
	}
}
