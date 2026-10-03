package unit_test

import (
	"image"
	"math"
	"math/rand/v2"
	"testing"

	face "github.com/kuaa-io/arandu-face"
	liveness "github.com/kuaa-io/arandu-liveness"
)

// The expected regions below are worked out by hand from upstream
// CropImage._get_new_box, in the comment beside each case; none comes from
// running the code under test.
func TestPADCropFollowsUpstreamCropImage(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		w, h      int
		box       face.Box
		scale     float64
		want      image.Rectangle
		effective float64
	}{
		// 640x480, box 100x100 at (270,190), scale 2.7: min(479/100, 639/100,
		// 2.7) = 2.7; 270x270 about (320,240): (185,105)-(455,375), inside;
		// the slice is inclusive, so the region ends at 456,376.
		"inside": {640, 480, face.Box{X: 270, Y: 190, Width: 100, Height: 100}, 2.7, image.Rect(185, 105, 456, 376), 2.7},
		// Box at (10,190): centre (60,240), left -75, so right 195 moves to
		// 270 and left to 0.
		"clamped left, shifted right": {640, 480, face.Box{X: 10, Y: 190, Width: 100, Height: 100}, 2.7, image.Rect(0, 105, 271, 376), 2.7},
		// 640x480, box 60x60 at (560,400), scale 4: 240x240 about (590,430):
		// (470,310)-(710,550). Right is 71 past 639, so left becomes 399;
		// bottom is 71 past 479, so top becomes 239.
		"shifted left and up": {640, 480, face.Box{X: 560, Y: 400, Width: 60, Height: 60}, 4, image.Rect(399, 239, 640, 480), 4},
		// 513x513, box 200x256 at (100,200), scale 4: min(512/256, 512/200, 4)
		// = 2; 400x512 about (200,328): (0,72)-(400,584). Bottom is 72 past
		// 512, so top becomes 0.
		"scale reduced to fit": {513, 513, face.Box{X: 100, Y: 200, Width: 200, Height: 256}, 4, image.Rect(0, 0, 401, 513), 2},
		// 640x480, box 50x50 at (10.5,20.25), scale 2.7: 135x135 about
		// (35.5,45.25): (-32,-22.25)-(103,112.75); both corners move by the
		// overshoot, to (0,0)-(135,135).
		"fractional box": {640, 480, face.Box{X: 10.5, Y: 20.25, Width: 50, Height: 50}, 2.7, image.Rect(0, 0, 136, 136), 2.7},
		// The aspect of the box is kept, not squared: 80x120 at (280,180),
		// scale 2.7 gives 216x324 about (320,240): (212,78)-(428,402).
		"aspect kept": {640, 480, face.Box{X: 280, Y: 180, Width: 80, Height: 120}, 2.7, image.Rect(212, 78, 429, 403), 2.7},
	} {
		got, err := liveness.PADCrop(tc.w, tc.h, tc.box, tc.scale)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got.Rect != tc.want || math.Abs(got.EffectiveScale-tc.effective) > 1e-12 || got.Scale != tc.scale {
			t.Errorf("%s: %v at effective scale %v, want %v at %v", name, got.Rect, got.EffectiveScale, tc.want, tc.effective)
		}
	}
}

func TestPADCropStaysInsideTheImage(t *testing.T) {
	t.Parallel()

	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 2000; i++ {
		w, h := 2+r.IntN(1000), 2+r.IntN(1000)
		box := face.Box{
			X: r.Float64()*float64(w)*1.5 - float64(w)/4, Y: r.Float64()*float64(h)*1.5 - float64(h)/4,
			Width: 1 + r.Float64()*float64(w), Height: 1 + r.Float64()*float64(h),
		}
		scale := 0.5 + r.Float64()*4
		got, err := liveness.PADCrop(w, h, box, scale)
		if err != nil {
			t.Fatalf("%dx%d %+v scale %v: %v", w, h, box, scale, err)
		}
		if got.Rect.Empty() || !got.Rect.In(image.Rect(0, 0, w, h)) || got.EffectiveScale > scale {
			t.Fatalf("%dx%d %+v scale %v: %v at %v", w, h, box, scale, got.Rect, got.EffectiveScale)
		}
	}
}

func TestPADCropRefusesWhatHasNoArea(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		w, h  int
		box   face.Box
		scale float64
	}{
		"no width":      {640, 480, face.Box{X: 1, Y: 1, Height: 10}, 2.7},
		"NaN box":       {640, 480, face.Box{X: math.NaN(), Y: 1, Width: 10, Height: 10}, 2.7},
		"zero scale":    {640, 480, face.Box{X: 1, Y: 1, Width: 10, Height: 10}, 0},
		"tiny image":    {1, 1, face.Box{Width: 1, Height: 1}, 2.7},
		"infinite size": {640, 480, face.Box{Width: math.Inf(1), Height: 10}, 2.7},
	} {
		if _, err := liveness.PADCrop(tc.w, tc.h, tc.box, tc.scale); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func TestSoftmaxAndFusionReproduceUpstreamTestPy(t *testing.T) {
	t.Parallel()

	// exp(0), exp(ln 2), exp(0) = 1, 2, 1 over 4.
	a := liveness.Softmax([3]float64{0, math.Ln2, 0})
	// exp(ln 3), 1, 1 = 3, 1, 1 over 5.
	b := liveness.Softmax([3]float64{math.Log(3), 0, 0})
	for i, want := range [3]float64{0.25, 0.5, 0.25} {
		if !near(a[i], want) {
			t.Errorf("softmax a[%d] = %v, want %v", i, a[i], want)
		}
	}
	for i, want := range [3]float64{0.6, 0.2, 0.2} {
		if !near(b[i], want) {
			t.Errorf("softmax b[%d] = %v, want %v", i, b[i], want)
		}
	}
	// Summed: 0.85, 0.7, 0.45. Upstream labels it class 0 with the score
	// 0.85/2 = 0.425; averaged, the fused values are 0.425, 0.35, 0.225.
	fused, label, err := liveness.FusePAD([][3]float64{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if label != liveness.PADClassAttack0 || label.String() != "attack_0" {
		t.Errorf("label %v, want attack_0", label)
	}
	for i, want := range [3]float64{0.425, 0.35, 0.225} {
		if !near(fused[i], want) {
			t.Errorf("fused[%d] = %v, want %v", i, fused[i], want)
		}
	}

	// A tie goes to the first class, as numpy's argmax does.
	_, tie, err := liveness.FusePAD([][3]float64{{0.4, 0.4, 0.2}})
	if err != nil || tie != liveness.PADClassAttack0 {
		t.Errorf("a tie labelled %v (%v)", tie, err)
	}
	_, bonaFide, err := liveness.FusePAD([][3]float64{{0.1, 0.8, 0.1}, {0.3, 0.4, 0.3}})
	if err != nil || bonaFide != liveness.PADClassBonaFide || bonaFide.String() != "bona_fide" {
		t.Errorf("a bona fide majority labelled %v (%v)", bonaFide, err)
	}
	if liveness.PADClassAttack2.String() != "attack_2" || liveness.PADClass(7).String() != "class_7" {
		t.Error("class names")
	}

	if _, _, err := liveness.FusePAD(nil); err == nil {
		t.Error("fusing nothing was accepted")
	}
	if _, _, err := liveness.FusePAD([][3]float64{{math.NaN(), 0, 0}}); err == nil {
		t.Error("fusing a NaN was accepted")
	}
}

func TestSoftmaxIsStableAndSumsToOne(t *testing.T) {
	t.Parallel()

	for _, logits := range [][3]float64{{1000, 0, -1000}, {-1000, -1000, -1000}, {3.2, -1.1, 0.4}, {0, 0, 0}} {
		p := liveness.Softmax(logits)
		sum := 0.0
		for _, v := range p {
			if math.IsNaN(v) || v < 0 || v > 1 {
				t.Errorf("softmax(%v) = %v", logits, p)
			}
			sum += v
		}
		if math.Abs(sum-1) > 1e-12 {
			t.Errorf("softmax(%v) sums to %v", logits, sum)
		}
	}
	if p := liveness.Softmax([3]float64{1000, 0, -1000}); p[0] != 1 {
		t.Errorf("a dominant logit gives %v", p)
	}
}

func TestTheUpstreamReferenceAndTheCaptureAspect(t *testing.T) {
	t.Parallel()

	ref := liveness.MiniFASNetUpstreamReference()
	if err := ref.Validate(); err != nil {
		t.Fatal(err)
	}
	if ref.Version != "minifasnet-upstream-0.5" || ref.Threshold != 0.5 || ref.ExpectedAspect != 0.75 {
		t.Errorf("reference %+v", ref)
	}
	for name, mutate := range map[string]func(*liveness.PADReferenceSet){
		"no version":        func(r *liveness.PADReferenceSet) { r.Version = "" },
		"negative":          func(r *liveness.PADReferenceSet) { r.Threshold = -0.1 },
		"zero aspect":       func(r *liveness.PADReferenceSet) { r.ExpectedAspect = 0 },
		"negative tolerace": func(r *liveness.PADReferenceSet) { r.AspectTolerance = -1 },
	} {
		r := ref
		mutate(&r)
		if r.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// 480x640 is 3:4; 640x480 is 4:3, 1.3333 - 0.75 = 0.5833 off.
	portrait := liveness.CaptureAspect(480, 640, 0.75, 0.01)
	if portrait.Ratio != 0.75 || portrait.Deviation != 0 || portrait.Tolerance != 0.01 {
		t.Errorf("portrait %+v", portrait)
	}
	landscape := liveness.CaptureAspect(640, 480, 0.75, 0.01)
	if math.Abs(landscape.Deviation-(4.0/3-0.75)) > 1e-12 {
		t.Errorf("landscape %+v", landscape)
	}
	if empty := liveness.CaptureAspect(10, 0, 0.75, 0.01); empty.Ratio != 0 {
		t.Errorf("a frame with no height %+v", empty)
	}
}
