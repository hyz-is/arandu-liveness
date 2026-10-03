package liveness

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"

	face "github.com/hyz-is/arandu-face"
	onnx "github.com/hyz-is/arandu-onnx"
)

// PADClasses is how many classes a MiniFASNet model scores.
const PADClasses = 3

// PADClass is one of the three classes a MiniFASNet model scores.
type PADClass int

// The classes, by the index of the model's output. Only class 1 is
// documented upstream (test.py calls it a real face); what classes 0 and 2
// each stand for is not, and their names say only that upstream counts them
// as attack presentations.
const (
	// PADClassAttack0 is output 0, an attack class whose kind is undocumented.
	PADClassAttack0 PADClass = 0
	// PADClassBonaFide is output 1, the class upstream calls a real face.
	PADClassBonaFide PADClass = 1
	// PADClassAttack2 is output 2, an attack class whose kind is undocumented.
	PADClassAttack2 PADClass = 2
)

// String names the class: "attack_0", "bona_fide" or "attack_2".
func (c PADClass) String() string {
	switch c {
	case PADClassAttack0:
		return "attack_0"
	case PADClassBonaFide:
		return "bona_fide"
	case PADClassAttack2:
		return "attack_2"
	}
	return fmt.Sprintf("class_%d", int(c))
}

// UpstreamCaptureAspect is the width over height upstream test.py requires of
// a frame: 3:4, the portrait video of the phones the models were built for.
const UpstreamCaptureAspect = 0.75

// PADReferenceSet is the versioned reference that comes back beside a
// passive PAD measurement. It is a parameter for the application's policy,
// never a decision.
type PADReferenceSet struct {
	// Version names this set of values. A change to any value is a new
	// version.
	Version string
	// Threshold is the fused bona fide probability the source would call a
	// real face at or above. It is not calibrated on any population.
	Threshold float64
	// ExpectedAspect is the frame width over height the models expect.
	ExpectedAspect float64
	// AspectTolerance is how far a frame's aspect may be from ExpectedAspect
	// and still count as the expected shape.
	AspectTolerance float64
	// Source is where the values come from.
	Source string
}

// MiniFASNetUpstreamReference returns the reference that reproduces upstream
// test.py: both models' softmax outputs averaged, 0.5 on the bona fide class,
// and 3:4 frames. Upstream labels a face by the argmax of the summed outputs,
// which a fused bona fide probability of 0.5 or more always selects. The
// value is the source's, not one calibrated on any population.
func MiniFASNetUpstreamReference() PADReferenceSet {
	return PADReferenceSet{
		Version:         "minifasnet-upstream-0.5",
		Threshold:       0.5,
		ExpectedAspect:  UpstreamCaptureAspect,
		AspectTolerance: 0.01,
		Source:          "minivision-ai/Silent-Face-Anti-Spoofing test.py: summed softmax, argmax, class 1 real, 3:4 frames",
	}
}

// Validate reports a reference set that cannot be returned beside a result.
func (r PADReferenceSet) Validate() error {
	if r.Version == "" {
		return errors.New("liveness: PADReferenceSet.Version is required: a reference nobody can name is one nobody can audit")
	}
	if !finite(r.Threshold) || r.Threshold < 0 || r.Threshold > 1 {
		return fmt.Errorf("liveness: PADReferenceSet.Threshold is %v, and has to be between 0 and 1", r.Threshold)
	}
	if !finite(r.ExpectedAspect) || r.ExpectedAspect <= 0 {
		return fmt.Errorf("liveness: PADReferenceSet.ExpectedAspect is %v, and has to be positive", r.ExpectedAspect)
	}
	if !finite(r.AspectTolerance) || r.AspectTolerance < 0 {
		return fmt.Errorf("liveness: PADReferenceSet.AspectTolerance is %v, and has to be at least 0", r.AspectTolerance)
	}
	return nil
}

// PADCropBox is the region a MiniFASNet model reads, worked out from a face
// box the way upstream CropImage works it out.
type PADCropBox struct {
	// Rect is the region, in the coordinates of the image with its top-left
	// corner at (0, 0); Max is exclusive.
	Rect image.Rectangle
	// Scale is the scale asked for.
	Scale float64
	// EffectiveScale is the scale used: Scale, or less when the enlarged box
	// would not fit in the image.
	EffectiveScale float64
}

// PADCrop is upstream CropImage._get_new_box, in Go.
//
// The scale is first reduced to what fits: min((height−1)/box height,
// (width−1)/box width, scale). The box is enlarged by it about its centre,
// keeping its aspect. An enlarged box past the left or top edge is moved right
// or down onto it; past the right or bottom edge (width−1, height−1), moved
// left or up onto it. The corners are truncated to integers and the region
// read is inclusive of both, as upstream's slice [top:bottom+1, left:right+1].
// The box is in the coordinates of an image of width by height whose
// top-left corner is (0, 0).
func PADCrop(width, height int, box face.Box, scale float64) (PADCropBox, error) {
	if width < 2 || height < 2 {
		return PADCropBox{}, fmt.Errorf("liveness: a %dx%d image is too small to crop", width, height)
	}
	if !finite(box.X, box.Y, box.Width, box.Height) || box.Width <= 0 || box.Height <= 0 {
		return PADCropBox{}, fmt.Errorf("liveness: the face box %+v has no area", box)
	}
	if !finite(scale) || scale <= 0 {
		return PADCropBox{}, fmt.Errorf("liveness: the crop scale %v has to be positive", scale)
	}
	srcW, srcH := float64(width), float64(height)
	effective := math.Min((srcH-1)/box.Height, math.Min((srcW-1)/box.Width, scale))

	newW, newH := box.Width*effective, box.Height*effective
	cx, cy := box.Width/2+box.X, box.Height/2+box.Y
	left, top := cx-newW/2, cy-newH/2
	right, bottom := cx+newW/2, cy+newH/2

	if left < 0 {
		right -= left
		left = 0
	}
	if top < 0 {
		bottom -= top
		top = 0
	}
	if right > srcW-1 {
		left -= right - srcW + 1
		right = srcW - 1
	}
	if bottom > srcH-1 {
		top -= bottom - srcH + 1
		bottom = srcH - 1
	}
	rect := image.Rect(int(math.Trunc(left)), int(math.Trunc(top)), int(math.Trunc(right))+1, int(math.Trunc(bottom))+1)
	rect = rect.Intersect(image.Rect(0, 0, width, height))
	if rect.Empty() {
		return PADCropBox{}, fmt.Errorf("liveness: the face box %+v leaves nothing to crop in a %dx%d image", box, width, height)
	}
	return PADCropBox{Rect: rect, Scale: scale, EffectiveScale: effective}, nil
}

// Softmax turns a model's logits into probabilities that sum to 1. The
// largest logit is subtracted first, so large logits do not overflow.
func Softmax(logits [PADClasses]float64) [PADClasses]float64 {
	top := math.Max(logits[0], math.Max(logits[1], logits[2]))
	var out [PADClasses]float64
	sum := 0.0
	for i, l := range logits {
		out[i] = math.Exp(l - top)
		sum += out[i]
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}

// FusePAD is upstream test.py's fusion: the models' probabilities summed,
// the label their argmax (the first on a tie, as numpy's), and every value
// divided by the number of models, so that the label's fused probability is
// upstream's "score" (the sum over two models, halved).
func FusePAD(perModel [][PADClasses]float64) ([PADClasses]float64, PADClass, error) {
	var fused [PADClasses]float64
	if len(perModel) == 0 {
		return fused, 0, errors.New("liveness: FusePAD needs at least one model's probabilities")
	}
	for i, p := range perModel {
		if !finite(p[0], p[1], p[2]) {
			return fused, 0, fmt.Errorf("liveness: model %d's probabilities %v are not finite", i, p)
		}
		for c := range fused {
			fused[c] += p[c]
		}
	}
	label := 0
	for c := 1; c < PADClasses; c++ {
		if fused[c] > fused[label] {
			label = c
		}
	}
	n := float64(len(perModel))
	for c := range fused {
		fused[c] /= n
	}
	return fused, PADClass(label), nil
}

// CaptureAspectSignal is a frame's shape held against the shape the PAD
// models expect.
type CaptureAspectSignal struct {
	// Width and Height are the frame's size in pixels.
	Width, Height int
	// Ratio is Width over Height.
	Ratio float64
	// Expected is the ratio the reference expects.
	Expected float64
	// Deviation is Ratio minus Expected.
	Deviation float64
	// Tolerance is the deviation the reference allows either way.
	Tolerance float64
}

// CaptureAspect measures a frame's width over height against the expected
// ratio.
func CaptureAspect(width, height int, expected, tolerance float64) CaptureAspectSignal {
	s := CaptureAspectSignal{Width: width, Height: height, Expected: expected, Tolerance: tolerance}
	if height > 0 {
		s.Ratio = float64(width) / float64(height)
		s.Deviation = s.Ratio - expected
	}
	return s
}

// PADModelOutput is what one model said about one face.
type PADModelOutput struct {
	// Model identifies the model, file and runtime that produced it.
	Model onnx.ModelInfo
	// Scale is the crop scale asked for.
	Scale float64
	// EffectiveScale is the crop scale used, smaller than Scale when the
	// enlarged box would not fit in the frame.
	EffectiveScale float64
	// Crop is the region the model read, in the coordinates of the image as
	// given.
	Crop image.Rectangle
	// Logits are the model's raw outputs, by class.
	Logits [PADClasses]float64
	// Probabilities are the softmax of Logits; they sum to 1.
	Probabilities [PADClasses]float64
}

// PADResult is what PassivePAD returns: each model's output, their fusion
// and the reference beside it. It holds no verdict.
type PADResult struct {
	// Box is the face box the crops were made from.
	Box face.Box
	// PerModel is each model's output, in the order the module opened them.
	PerModel []PADModelOutput
	// FusedProbabilities are the models' probabilities averaged, by class.
	FusedProbabilities [PADClasses]float64
	// FusedRealProbability is FusedProbabilities of PADClassBonaFide.
	FusedRealProbability float64
	// FusedLabel is the class with the highest fused probability, as
	// upstream labels it; a model output, not a conclusion.
	FusedLabel PADClass
	// FusedLabelProbability is FusedProbabilities of FusedLabel, upstream's
	// "score".
	FusedLabelProbability float64
	// CaptureAspect is the frame's shape against the shape the models
	// expect.
	CaptureAspect CaptureAspectSignal
	// ReferenceSet is the reference the application's policy reads the fused
	// probability against, with its version.
	ReferenceSet PADReferenceSet
}

// PassivePAD runs every configured MiniFASNet model on one face in one frame
// and returns each model's output, their fusion and the reference set. It
// needs Config.EnablePassive.
//
// The face box comes from the caller, normally from arandu-face's Detect on
// the same image: this call detects nothing. For each model the box is
// enlarged by the model's scale as PADCrop does, the region is resized to
// 80×80 bilinearly and given as raw BGR 0..255, and the model's three logits
// are turned into probabilities by Softmax. FusePAD averages them.
func (m *Module) PassivePAD(ctx context.Context, img image.Image, box face.Box) (PADResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := m.usable(); err != nil {
		return PADResult{}, err
	}
	if len(m.pad) == 0 {
		return PADResult{}, fmt.Errorf("%w: PassivePAD needs Config.EnablePassive", ErrNotEnabled)
	}
	b := img.Bounds()
	if b.Empty() {
		return PADResult{}, errors.New("liveness: PassivePAD of an empty image")
	}
	if int64(b.Dx())*int64(b.Dy()) > onnx.MaxImagePixels {
		return PADResult{}, fmt.Errorf("%w: %dx%d is more than %d pixels", onnx.ErrImageTooLarge, b.Dx(), b.Dy(), onnx.MaxImagePixels)
	}
	frame := face.Box{X: float64(b.Min.X), Y: float64(b.Min.Y), Width: float64(b.Dx()), Height: float64(b.Dy())}
	if !finite(box.X, box.Y, box.Width, box.Height) || box.Area() <= 0 || face.IoU(box, frame) <= 0 {
		return PADResult{}, fmt.Errorf("liveness: the face box %+v has no area inside the image %v", box, b)
	}
	local := face.Box{X: box.X - frame.X, Y: box.Y - frame.Y, Width: box.Width, Height: box.Height}

	result := PADResult{
		Box:           box,
		CaptureAspect: CaptureAspect(b.Dx(), b.Dy(), m.cfg.PADReference.ExpectedAspect, m.cfg.PADReference.AspectTolerance),
		ReferenceSet:  m.cfg.PADReference,
	}
	probabilities := make([][PADClasses]float64, 0, len(m.pad))
	for _, s := range m.pad {
		out, err := m.runPAD(ctx, s, img, local)
		if err != nil {
			return PADResult{}, err
		}
		result.PerModel = append(result.PerModel, out)
		probabilities = append(probabilities, out.Probabilities)
	}
	fused, label, err := FusePAD(probabilities)
	if err != nil {
		return PADResult{}, err
	}
	result.FusedProbabilities = fused
	result.FusedRealProbability = fused[PADClassBonaFide]
	result.FusedLabel = label
	result.FusedLabelProbability = fused[label]
	return result, nil
}

// runPAD runs one model on the crop its scale gives.
func (m *Module) runPAD(ctx context.Context, s *onnx.Session, img image.Image, local face.Box) (PADModelOutput, error) {
	b := img.Bounds()
	id := s.Model().ID
	scale := m.cfg.Scales[id]
	crop, err := PADCrop(b.Dx(), b.Dy(), local, scale)
	if err != nil {
		return PADModelOutput{}, err
	}
	region := crop.Rect.Add(b.Min)
	cropped, err := onnx.Crop(img, region)
	if err != nil {
		return PADModelOutput{}, err
	}
	resized, err := onnx.Resize(cropped, PADInputSize, PADInputSize)
	if err != nil {
		return PADModelOutput{}, err
	}
	result, err := s.Run(ctx, map[string]onnx.Tensor{"input": face.RawPlanes(resized, true)})
	if err != nil {
		return PADModelOutput{}, err
	}
	raw := result.Outputs["output"].Float32
	if len(raw) != PADClasses {
		return PADModelOutput{}, fmt.Errorf("liveness: %s returned %d values, want %d", id, len(raw), PADClasses)
	}
	var logits [PADClasses]float64
	for i, v := range raw {
		logits[i] = float64(v)
		if !finite(logits[i]) {
			return PADModelOutput{}, fmt.Errorf("liveness: %s returned the logit %v", id, v)
		}
	}
	return PADModelOutput{
		Model:          result.Model,
		Scale:          scale,
		EffectiveScale: crop.EffectiveScale,
		Crop:           region,
		Logits:         logits,
		Probabilities:  Softmax(logits),
	}, nil
}
