package liveness

import (
	"context"
	"errors"
	"image"

	onnx "github.com/hyz-is/arandu-onnx"
)

// Availability is whether a signal could be measured at all.
type Availability string

// NotAvailable is a signal no cleared model or method can measure.
const NotAvailable Availability = "not_available"

// DeepfakeReferenceVersion names the statement Deepfake returns. A change to
// the reason, or a model becoming available, is a new version.
const DeepfakeReferenceVersion = "kuaa-liveness-deepfake-none-0.1"

// deepfakeReason is why there is no deepfake signal.
const deepfakeReason = "no deepfake, face-swap or face-reenactment detection model has been reviewed with an " +
	"acceptable licence, disclosed and consented training data, and pinned provenance; none is shipped, so " +
	"this signal is absent rather than measured. InjectionSignals and the challenge measure other parts of " +
	"the attack surface and do not stand in for it"

// DeepfakeSignal is the result of the deepfake and face-swap detection stage.
// Today it always reports NotAvailable, so that the application records the
// absence of the signal as evidence instead of skipping the stage silently.
type DeepfakeSignal struct {
	// Availability is NotAvailable.
	Availability Availability
	// Reason says why the signal is not available.
	Reason string
	// Model is the model that measured it; nil, because none did.
	Model *onnx.ModelInfo
	// Version names the statement, so that a record can say which one it
	// holds.
	Version string
}

// DeepfakeUnavailable returns the DeepfakeSignal that records that no
// deepfake detector is available, and why.
func DeepfakeUnavailable() DeepfakeSignal {
	return DeepfakeSignal{Availability: NotAvailable, Reason: deepfakeReason, Version: DeepfakeReferenceVersion}
}

// Deepfake is the deepfake and face-swap detection stage for one frame. It
// runs nothing and returns DeepfakeUnavailable: the stage exists so that a
// capture's record holds its explicit absence. It refuses a done context and
// an empty image, as a stage that measured something would.
func (m *Module) Deepfake(ctx context.Context, img image.Image) (DeepfakeSignal, error) {
	if err := ctx.Err(); err != nil {
		return DeepfakeSignal{}, err
	}
	if img == nil || img.Bounds().Empty() {
		return DeepfakeSignal{}, errors.New("liveness: Deepfake of an empty image")
	}
	return DeepfakeUnavailable(), nil
}
