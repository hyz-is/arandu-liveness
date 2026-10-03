// Package liveness is an Arandu module that measures presentation-attack,
// active-challenge and capture-integrity signals for a face capture, and
// returns each as a separate technical signal with the provenance of the model
// behind it, when a model is involved.
//
// Each stage is its own call: PassivePAD runs the MiniFASNet models on one
// frame and one face box, EvaluateChallenge measures a head-movement
// challenge from per-frame landmarks, MeasureInjectionSignals measures the
// timing, duplication, geometry, metadata and nonce binding of a capture, and
// Deepfake records that no deepfake detector is available. The face box and
// the landmarks come from the caller, normally from arandu-face's Detect: no
// call here detects a face for the caller, and none decides anything.
// Thresholds travel with the results as versioned reference sets, and what
// they mean for a case is the application's policy.
//
// An application builds it once in bootstrap/app.go, after arandu-onnx, and
// registers it there. There is no service provider, no container and no
// discovery.
package liveness
