package feature_test

import (
	"context"
	"image"
	"math"
	"os"
	"path/filepath"
	"testing"

	face "github.com/hyz-is/arandu-face"
	liveness "github.com/hyz-is/arandu-liveness"
	onnx "github.com/hyz-is/arandu-onnx"
)

// These tests run the real MiniFASNet models on images drawn here. They hold
// the shape of the answer -- two models, three classes, probabilities that
// sum to 1, determinism, provenance, the crop each model read -- and never its
// value: a drawing is neither a bona fide presentation nor an attack, and how
// well the models separate the two can only be measured on consented attack
// data under ISO/IEC 30107-3.

func TestPassivePADReturnsEachModelWithItsProvenance(t *testing.T) {
	m := bootModule(t, liveness.Config{})
	ctx := context.Background()
	// A head 100 pixels wide: enlarged four times it still fits the frame,
	// so neither scale is reduced.
	img := cartoonFace(480, 640, 240, 300, 50)
	box := drawnBox(240, 300, 50)

	r, err := m.PassivePAD(ctx, img, box)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.PerModel) != 2 {
		t.Fatalf("%d model outputs, want 2", len(r.PerModel))
	}
	want := []struct {
		id    string
		scale float64
	}{{liveness.ModelMiniFASNetV2, 2.7}, {liveness.ModelMiniFASNetV1SE, 4.0}}
	var mean [3]float64
	for i, out := range r.PerModel {
		manifest, err := liveness.LookupManifest(want[i].id)
		if err != nil {
			t.Fatal(err)
		}
		if out.Model.ID != want[i].id || out.Model.SHA256 != manifest.SHA256 || out.Model.Status != onnx.StatusApproved ||
			out.Model.Version != manifest.Version || out.Model.Runtime == "" {
			t.Errorf("model %d carries %+v", i, out.Model)
		}
		if out.Scale != want[i].scale || out.EffectiveScale != want[i].scale {
			t.Errorf("%s: scale %v effective %v", out.Model.ID, out.Scale, out.EffectiveScale)
		}
		if !out.Crop.In(img.Bounds()) || out.Crop.Empty() {
			t.Errorf("%s: crop %v outside the image", out.Model.ID, out.Crop)
		}
		crop, err := liveness.PADCrop(480, 640, box, want[i].scale)
		if err != nil || crop.Rect != out.Crop {
			t.Errorf("%s: crop %v, PADCrop gives %v (%v)", out.Model.ID, out.Crop, crop.Rect, err)
		}
		sum := 0.0
		for c, p := range out.Probabilities {
			if math.IsNaN(p) || p < 0 || p > 1 || math.IsNaN(out.Logits[c]) {
				t.Errorf("%s: class %d logit %v probability %v", out.Model.ID, c, out.Logits[c], p)
			}
			sum += p
			mean[c] += p / 2
		}
		if math.Abs(sum-1) > 1e-9 {
			t.Errorf("%s: probabilities sum to %v", out.Model.ID, sum)
		}
		if out.Probabilities != liveness.Softmax(out.Logits) {
			t.Errorf("%s: probabilities are not the softmax of the logits", out.Model.ID)
		}
	}
	for c := range mean {
		if math.Abs(r.FusedProbabilities[c]-mean[c]) > 1e-12 {
			t.Errorf("fused class %d = %v, want the mean %v", c, r.FusedProbabilities[c], mean[c])
		}
	}
	if r.FusedRealProbability != r.FusedProbabilities[liveness.PADClassBonaFide] ||
		r.FusedLabelProbability != r.FusedProbabilities[r.FusedLabel] {
		t.Errorf("fused %+v real %v label %v at %v", r.FusedProbabilities, r.FusedRealProbability, r.FusedLabel, r.FusedLabelProbability)
	}
	for c := range r.FusedProbabilities {
		if r.FusedProbabilities[c] > r.FusedProbabilities[r.FusedLabel] {
			t.Errorf("the label %v is not the argmax of %v", r.FusedLabel, r.FusedProbabilities)
		}
	}
	if r.ReferenceSet != liveness.MiniFASNetUpstreamReference() || r.Box != box {
		t.Errorf("reference %+v box %+v", r.ReferenceSet, r.Box)
	}
	if r.CaptureAspect.Ratio != 0.75 || r.CaptureAspect.Deviation != 0 {
		t.Errorf("a 480x640 frame has aspect %+v", r.CaptureAspect)
	}
	t.Logf("cartoon: fused %v, label %v (synthetic image, not a measurement)", r.FusedProbabilities, r.FusedLabel)
}

func TestPassivePADIsDeterministicAndReadsThePixels(t *testing.T) {
	m := bootModule(t, liveness.Config{})
	ctx := context.Background()
	img := cartoonFace(480, 640, 240, 300, 95)
	box := drawnBox(240, 300, 95)

	first, err := m.PassivePAD(ctx, img, box)
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.PassivePAD(ctx, img, box)
	if err != nil {
		t.Fatal(err)
	}
	for i := range first.PerModel {
		if first.PerModel[i].Logits != again.PerModel[i].Logits {
			t.Errorf("model %d: two runs on one input gave %v and %v", i, first.PerModel[i].Logits, again.PerModel[i].Logits)
		}
	}

	other, err := m.PassivePAD(ctx, noFaceScenes()["noise"], face.Box{X: 260, Y: 180, Width: 120, Height: 150})
	if err != nil {
		t.Fatal(err)
	}
	if other.PerModel[0].Logits == first.PerModel[0].Logits {
		t.Error("noise and the cartoon gave identical logits: the pixels are not reaching the model")
	}

	// A sub-image keeps the parent's coordinates. Holding every pixel either
	// crop reads, it gives the same crops, in the parent's coordinates, and
	// the same logits.
	big := cartoonFace(800, 900, 400, 450, 60)
	bigBox := drawnBox(400, 450, 60)
	whole, err := m.PassivePAD(ctx, big, bigBox)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := m.PassivePAD(ctx, big.SubImage(image.Rect(50, 40, 800, 900)), bigBox)
	if err != nil {
		t.Fatal(err)
	}
	for i := range whole.PerModel {
		if whole.PerModel[i].Crop != sub.PerModel[i].Crop || whole.PerModel[i].Logits != sub.PerModel[i].Logits {
			t.Errorf("model %d: the image read %v %v, its sub-image %v %v", i,
				whole.PerModel[i].Crop, whole.PerModel[i].Logits, sub.PerModel[i].Crop, sub.PerModel[i].Logits)
		}
	}
}

func TestPassivePADWithOneModelAndAnotherScale(t *testing.T) {
	m := bootModule(t, liveness.Config{
		PADModels: []string{liveness.ModelMiniFASNetV1SE},
		Scales:    map[string]float64{liveness.ModelMiniFASNetV1SE: 3},
	})
	img := cartoonFace(480, 640, 240, 300, 95)
	r, err := m.PassivePAD(context.Background(), img, drawnBox(240, 300, 95))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.PerModel) != 1 || r.PerModel[0].Model.ID != liveness.ModelMiniFASNetV1SE || r.PerModel[0].Scale != 3 {
		t.Fatalf("outputs %+v", r.PerModel)
	}
	if r.FusedProbabilities != r.PerModel[0].Probabilities {
		t.Errorf("one model's fusion %v is not its probabilities %v", r.FusedProbabilities, r.PerModel[0].Probabilities)
	}
	// 4:3 is not the 3:4 the models expect, and the result says so.
	land, err := m.PassivePAD(context.Background(), cartoonFace(640, 480, 320, 240, 80), drawnBox(320, 240, 80))
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(land.CaptureAspect.Deviation-(4.0/3-0.75)) > 1e-12 {
		t.Errorf("a 640x480 frame has aspect %+v", land.CaptureAspect)
	}
}

func TestPassivePADRefusesABoxItCannotCrop(t *testing.T) {
	m := bootModule(t, liveness.Config{})
	ctx := context.Background()
	img := cartoonFace(480, 640, 240, 300, 95)
	for name, box := range map[string]face.Box{
		"no area":     {X: 10, Y: 10},
		"outside":     {X: 5000, Y: 5000, Width: 10, Height: 10},
		"NaN":         {X: math.NaN(), Y: 1, Width: 10, Height: 10},
		"negative":    {X: 10, Y: 10, Width: -5, Height: 10},
		"far left up": {X: -500, Y: -500, Width: 100, Height: 100},
	} {
		if _, err := m.PassivePAD(ctx, img, box); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := m.PassivePAD(ctx, image.NewRGBA(image.Rect(0, 0, 0, 0)), drawnBox(0, 0, 10)); err == nil {
		t.Error("an empty image was accepted")
	}
	// A box that crosses the edge is cropped the way upstream does it.
	if _, err := m.PassivePAD(ctx, img, face.Box{X: -40, Y: -30, Width: 160, Height: 200}); err != nil {
		t.Errorf("a box across the corner: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := m.PassivePAD(cancelled, img, drawnBox(240, 300, 95)); err == nil {
		t.Error("a cancelled context ran")
	}
}

// TestTheStagesComposeExplicitly runs the composition an application writes:
// arandu-face's Detect finds the box, and PassivePAD reads it. Nothing in
// this package detects; the test needs the YuNet file and skips without it.
func TestTheStagesComposeExplicitly(t *testing.T) {
	m := bootModule(t, liveness.Config{})
	yunet, err := face.LookupManifest(face.ModelYuNet)
	if err != nil {
		t.Fatal(err)
	}
	dir := modelsRoot(t)
	if _, err := os.Stat(filepath.Join(dir, yunet.File)); err != nil {
		t.Skipf("the YuNet file is not in %s: %v", dir, err)
	}
	detector, err := face.New(face.Config{ModelsDir: dir, RefuseDevOnly: true}, newRuntime(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := detector.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = detector.Close(ctx) })

	img := cartoonFace(480, 640, 240, 300, 95)
	faces, err := detector.Detect(ctx, img)
	if err != nil || len(faces) != 1 {
		t.Fatalf("Detect = %d faces, %v", len(faces), err)
	}
	r, err := m.PassivePAD(ctx, img, faces[0].Box)
	if err != nil {
		t.Fatal(err)
	}
	if r.Box != faces[0].Box || len(r.PerModel) != 2 {
		t.Errorf("PassivePAD on the detected box: %+v", r)
	}
}
