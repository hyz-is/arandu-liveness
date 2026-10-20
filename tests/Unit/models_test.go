package unit_test

import (
	"net/url"
	"strings"
	"testing"

	liveness "github.com/hyz-is/arandu-liveness"
	onnx "github.com/hyz-is/arandu-onnx"
)

func TestEveryShippedManifestRecordsItsProvenance(t *testing.T) {
	t.Parallel()

	manifests, err := liveness.Manifests()
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]struct {
		file    string
		version string
	}{
		liveness.ModelMiniFASNetV2:   {"liveness/MiniFASNetV2.onnx", "2.7_80x80-yakhyo-weights"},
		liveness.ModelMiniFASNetV1SE: {"liveness/MiniFASNetV1SE.onnx", "4_0_0_80x80-yakhyo-weights"},
	}
	if len(manifests) != len(expected) {
		t.Fatalf("%d manifests, want %d", len(manifests), len(expected))
	}
	for _, m := range manifests {
		w, ok := expected[m.ID]
		if !ok {
			t.Errorf("unexpected manifest %s", m.ID)
			continue
		}
		// Both are approved for their Apache-2.0 weights; their undisclosed
		// training data is an accepted risk, and they still have no PAD
		// evaluation, which their cards say.
		if m.Status != onnx.StatusApproved || m.CommercialUse != onnx.CommercialUseYes || m.File != w.file ||
			m.Version != w.version || m.Precision != onnx.PrecisionFP32 || m.License != "Apache-2.0" || m.ModelLicense != "Apache-2.0" {
			t.Errorf("%s: status %s, commercial use %s, file %s, version %s, precision %s, licences %s/%s",
				m.ID, m.Status, m.CommercialUse, m.File, m.Version, m.Precision, m.License, m.ModelLicense)
		}
		if len(m.Inputs) != 1 || m.Inputs[0].Name != "input" || len(m.Outputs) != 1 || m.Outputs[0].Name != "output" {
			t.Errorf("%s: contract %+v -> %+v", m.ID, m.Inputs, m.Outputs)
		}
		if got := m.Inputs[0].Shape; len(got) != 4 || got[1] != 3 || got[2] != liveness.PADInputSize || got[3] != liveness.PADInputSize {
			t.Errorf("%s: input shape %v", m.ID, got)
		}
		if got := m.Outputs[0].Shape; len(got) != 2 || got[1] != liveness.PADClasses {
			t.Errorf("%s: output shape %v", m.ID, got)
		}
		u, err := url.Parse(m.SourceURL)
		if err != nil || u.Scheme != "https" || u.Host != "github.com" ||
			!strings.HasPrefix(u.Path, "/yakhyo/face-anti-spoofing/releases/download/weights/") {
			t.Errorf("%s: source_url %s is not the reviewed release asset", m.ID, m.SourceURL)
		}
		if _, err := liveness.LookupModelCard(m.ID); err != nil {
			t.Errorf("%s has no model card: %v", m.ID, err)
		}
	}
}

func TestTheManifestsRecordTheReviewedDigests(t *testing.T) {
	t.Parallel()

	// The digests the model review recorded, which are also the GitHub
	// release asset digests of yakhyo/face-anti-spoofing's "weights" release.
	for id, digest := range map[string]string{
		liveness.ModelMiniFASNetV2:   "b32929adc2d9c34b9486f8c4c7bc97c1b69bc0ea9befefc380e4faae4e463907",
		liveness.ModelMiniFASNetV1SE: "ebab7f90c7833fbccd46d3a555410e78d969db5438e169b6524be444862b3676",
	} {
		m, err := liveness.LookupManifest(id)
		if err != nil {
			t.Fatal(err)
		}
		if m.SHA256 != digest {
			t.Errorf("%s: sha256 %s, want %s", id, m.SHA256, digest)
		}
	}
	if _, err := liveness.LookupManifest("no-such-model"); err == nil {
		t.Error("an unknown id was found")
	}
}

// TestTheScalesAreTheOnesTheModelsWereTrainedWith pins the crop scales to the
// upstream file names: 2.7_80x80_MiniFASNetV2 and 4_0_0_80x80_MiniFASNetV1SE.
func TestTheScalesAreTheOnesTheModelsWereTrainedWith(t *testing.T) {
	t.Parallel()

	scales := liveness.DefaultScales()
	if len(scales) != 2 || scales[liveness.ModelMiniFASNetV2] != 2.7 || scales[liveness.ModelMiniFASNetV1SE] != 4.0 {
		t.Errorf("DefaultScales() = %v", scales)
	}
	scales[liveness.ModelMiniFASNetV2] = 1
	if liveness.DefaultScales()[liveness.ModelMiniFASNetV2] != 2.7 {
		t.Error("DefaultScales shares its map with its callers")
	}
}

// TestNoSensitiveOrNonCommercialModelIsShipped holds the exclusions: no model
// that infers race, ethnicity or gender (sensitive data under LGPD art. 11),
// none of the non-commercial recognizers, and no model trained on
// CelebA-Spoof, whose licence is non-commercial.
func TestNoSensitiveOrNonCommercialModelIsShipped(t *testing.T) {
	t.Parallel()

	manifests, err := liveness.Manifests()
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"fairface", "race", "ethnic", "arcface", "insightface", "buffalo", "genderage", "edgeface", "gender", "celeba"}
	for _, m := range manifests {
		card, err := liveness.LookupModelCard(m.ID)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(strings.Join([]string{m.ID, m.File, m.SourceURL, m.Redistribution, card.Source, card.TrainingData}, " "))
		for _, word := range forbidden {
			if strings.Contains(text, word) {
				t.Errorf("%s mentions %q", m.ID, word)
			}
		}
		for _, out := range m.Outputs {
			for _, word := range forbidden {
				if strings.Contains(strings.ToLower(out.Name), word) {
					t.Errorf("%s has the output %s", m.ID, out.Name)
				}
			}
		}
	}
}

func TestTheModelCardsRecordWhatTheManifestCannot(t *testing.T) {
	t.Parallel()

	for _, id := range []string{liveness.ModelMiniFASNetV2, liveness.ModelMiniFASNetV1SE} {
		card, err := liveness.LookupModelCard(id)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(card.TrainingData, "Undisclosed") {
			t.Errorf("%s: the card does not say the training data is undisclosed: %q", id, card.TrainingData)
		}
		if !strings.Contains(card.Evaluation, "ISO/IEC 30107-3") || !strings.Contains(card.Evaluation, "iBeta") ||
			!strings.HasPrefix(card.Evaluation, "None known") {
			t.Errorf("%s: the card does not record the missing PAD evaluation: %q", id, card.Evaluation)
		}
		if !strings.Contains(card.Notes, "unverified") || !strings.Contains(card.Notes, "3:4") {
			t.Errorf("%s: the card does not record the class caveat and the capture aspect: %q", id, card.Notes)
		}
		if !strings.Contains(card.Source, "minivision-ai/Silent-Face-Anti-Spoofing") || !strings.Contains(card.Source, "yakhyo") {
			t.Errorf("%s: source %q", id, card.Source)
		}
	}
	if _, err := liveness.LookupModelCard("fairface"); err == nil {
		t.Error("a card for an unshipped model was found")
	}
}

// TestOnlyCommercialModelsAreApproved holds the licence rule the statuses
// follow, and what an approval of a PAD model does not claim: a model is
// approved only when its weights' licence allows commercial use, its card
// says who approved it and which risks were accepted, and the card still
// says that no PAD evaluation exists until one does.
func TestOnlyCommercialModelsAreApproved(t *testing.T) {
	t.Parallel()

	manifests, err := liveness.Manifests()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range manifests {
		if m.Status != onnx.StatusApproved {
			continue
		}
		if m.CommercialUse != onnx.CommercialUseYes {
			t.Errorf("%s is approved with commercial_use %q", m.ID, m.CommercialUse)
		}
		card, err := liveness.LookupModelCard(m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(card.Review, "ADR-0010") || !strings.Contains(card.Review, "accepted") ||
			!strings.Contains(card.Review, "ISO/IEC 30107-3") {
			t.Errorf("%s: the review does not name the decision, the accepted risk and the missing evaluation: %q", m.ID, card.Review)
		}
		if strings.Contains(strings.ToLower(card.TrainingData+card.Review+card.Notes), "dev-only") {
			t.Errorf("%s is approved and its card still calls it dev-only", m.ID)
		}
	}
}
