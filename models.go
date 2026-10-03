package liveness

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	onnx "github.com/hyz-is/arandu-onnx"
)

// The identifiers of the models this package ships manifests for.
const (
	// ModelMiniFASNetV2 is MiniFASNetV2, the 2.7_80x80 passive presentation
	// attack detector of Silent-Face-Anti-Spoofing.
	ModelMiniFASNetV2 = "minifasnet_v2"
	// ModelMiniFASNetV1SE is MiniFASNetV1SE, the 4_0_0_80x80 passive
	// presentation attack detector of Silent-Face-Anti-Spoofing.
	ModelMiniFASNetV1SE = "minifasnet_v1se"
)

// The crop scales each model was trained with, as the upstream file names
// state them (2.7_80x80_MiniFASNetV2, 4_0_0_80x80_MiniFASNetV1SE).
const (
	// ScaleMiniFASNetV2 is the factor MiniFASNetV2's face box is enlarged by.
	ScaleMiniFASNetV2 = 2.7
	// ScaleMiniFASNetV1SE is the factor MiniFASNetV1SE's face box is enlarged
	// by.
	ScaleMiniFASNetV1SE = 4.0
)

// PADInputSize is the side of the square crop both MiniFASNet models read.
const PADInputSize = 80

// DefaultScales returns the crop scale of every shipped model, by identifier.
func DefaultScales() map[string]float64 {
	return map[string]float64{
		ModelMiniFASNetV2:   ScaleMiniFASNetV2,
		ModelMiniFASNetV1SE: ScaleMiniFASNetV1SE,
	}
}

//go:embed manifests/*.json
var manifestFiles embed.FS

// Manifests returns the manifest of every model this package can run, sorted
// by identifier. Each one records where its file was obtained, its SHA-256,
// its licences and its status, and Runtime.Open verifies the file against it.
func Manifests() ([]onnx.Manifest, error) {
	entries, err := fs.ReadDir(manifestFiles, "manifests")
	if err != nil {
		return nil, fmt.Errorf("liveness: listing the embedded manifests: %w", err)
	}
	out := make([]onnx.Manifest, 0, len(entries))
	for _, entry := range entries {
		data, err := manifestFiles.ReadFile(path.Join("manifests", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("liveness: reading the embedded manifest %s: %w", entry.Name(), err)
		}
		m, err := onnx.LoadManifest(data)
		if err != nil {
			return nil, fmt.Errorf("liveness: embedded manifest %s: %w", entry.Name(), err)
		}
		if want := strings.TrimSuffix(entry.Name(), ".json"); m.ID != want {
			return nil, fmt.Errorf("liveness: embedded manifest %s carries the id %q", entry.Name(), m.ID)
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// LookupManifest returns the manifest of one model by its identifier.
func LookupManifest(id string) (onnx.Manifest, error) {
	all, err := Manifests()
	if err != nil {
		return onnx.Manifest{}, err
	}
	for _, m := range all {
		if m.ID == id {
			return m, nil
		}
	}
	return onnx.Manifest{}, fmt.Errorf("liveness: no model is called %q", id)
}

// ModelCard is what the manifest has no field for: the model's training data,
// what its authors report about it, how far it has been evaluated, and the
// caveats a reviewer recorded.
type ModelCard struct {
	// ID is the manifest's model identifier.
	ID string
	// Source is where the model comes from, upstream of the file.
	Source string
	// TrainingData is what is known of the data the weights were trained on,
	// and under which terms.
	TrainingData string
	// Reported is what the model's authors report about its accuracy, quoted
	// as metadata; nothing here measured it.
	Reported string
	// Evaluation is the independent evaluation the model has, against
	// ISO/IEC 30107-3 or a laboratory such as iBeta, or the statement that it
	// has none.
	Evaluation string
	// Notes are the reviewer's caveats.
	Notes string
}

// minifasnetTraining is what is known of both MiniFASNet models' data.
const minifasnetTraining = "Undisclosed. The upstream repository publishes code and weights under Apache-2.0 " +
	"and does not publish or describe the dataset the released weights were trained on, nor the consent " +
	"under which it was collected. The ONNX file is a third-party export by yakhyo/face-anti-spoofing; " +
	"the release tag it comes from is not immutable, and the digest, which equals the release asset's " +
	"published digest, is what pins it."

// minifasnetEvaluation is the independent evaluation both models have.
const minifasnetEvaluation = "None known. No ISO/IEC 30107-3 evaluation, no iBeta or other laboratory " +
	"report, and no measurement on consented attack data exists for these weights; their APCER and " +
	"BPCER are unknown."

// minifasnetClasses is what is known of the three output classes.
const minifasnetClasses = "Three logits, softmax outside the graph. Upstream test.py calls class 1 a real " +
	"face and every other class fake; what classes 0 and 2 each stand for (print, replay, mask) is not " +
	"documented upstream and is unverified here."

// cards are the model cards, by identifier.
var cards = map[string]ModelCard{
	ModelMiniFASNetV2: {
		ID: ModelMiniFASNetV2,
		Source: "minivision-ai/Silent-Face-Anti-Spoofing (Apache-2.0), 2.7_80x80_MiniFASNetV2.pth, exported to " +
			"ONNX by yakhyo/face-anti-spoofing (Apache-2.0), release \"weights\"",
		TrainingData: minifasnetTraining,
		Reported:     "The upstream repository reports no APCER/BPCER on a public protocol for these weights; nothing was measured here.",
		Evaluation:   minifasnetEvaluation,
		Notes: "Input: the face box enlarged 2.7 times about its centre as upstream CropImage does, resized to 80x80, " +
			"BGR raw 0..255 with no mean or scale. " + minifasnetClasses + " Upstream expects 3:4 (width:height) frames.",
	},
	ModelMiniFASNetV1SE: {
		ID: ModelMiniFASNetV1SE,
		Source: "minivision-ai/Silent-Face-Anti-Spoofing (Apache-2.0), 4_0_0_80x80_MiniFASNetV1SE.pth, exported to " +
			"ONNX by yakhyo/face-anti-spoofing (Apache-2.0), release \"weights\"",
		TrainingData: minifasnetTraining,
		Reported:     "The upstream repository reports no APCER/BPCER on a public protocol for these weights; nothing was measured here.",
		Evaluation:   minifasnetEvaluation,
		Notes: "Input: the face box enlarged 4.0 times about its centre as upstream CropImage does, resized to 80x80, " +
			"BGR raw 0..255 with no mean or scale. " + minifasnetClasses + " Upstream expects 3:4 (width:height) frames.",
	},
}

// LookupModelCard returns the model card of one model by its identifier.
func LookupModelCard(id string) (ModelCard, error) {
	card, ok := cards[id]
	if !ok {
		return ModelCard{}, fmt.Errorf("liveness: no model card for %q", id)
	}
	return card, nil
}
