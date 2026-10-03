# Changelog

Everything worth knowing about a release of Arandu Liveness is recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
the versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

A published module version is immutable: Go serves it from the proxy forever, so
a release is corrected by another release and never by moving a tag.

## [Unreleased]

## [0.1.0] - 2026-10-03

The first public release, as `github.com/hyz-is/arandu-liveness`. It requires
`github.com/hyz-is/arandu-onnx` v0.1.0, `github.com/hyz-is/arandu-face` v0.1.0
and, for passive PAD, ONNX Runtime 1.26 or later (CI and release run 1.30.0).
Both PAD models are `dev-only`; see `THIRD_PARTY_MODELS.md`.

### Added

- `Module`: `foundation.Module` with no route, plus `Bootable` (opens the PAD
  models when `EnablePassive` is set, verified against their manifests; with
  `RefuseDevOnly`, refuses dev-only models before reading any file; boots
  without ONNX Runtime when no model is enabled), `Health`, `Models` and
  `Closable`. `New(Config, *onnx.Runtime)`.
- `Module.PassivePAD`: MiniFASNetV2 (scale 2.7) and MiniFASNetV1SE (scale 4.0)
  on a caller-supplied face box, returning `PADResult{Box, PerModel,
  FusedProbabilities, FusedRealProbability, FusedLabel, FusedLabelProbability,
  CaptureAspect, ReferenceSet}` with no boolean. Both models are dev-only.
- `PADCrop` (upstream `CropImage._get_new_box`), `Softmax`, `FusePAD`,
  `CaptureAspect`, `PADClass`, and the reference set
  `MiniFASNetUpstreamReference()` (`minifasnet-upstream-0.5`).
- `EvaluateChallenge` and `Module.EvaluateChallenge`: `turn_left`,
  `turn_right`, `look_up`, `look_down` and `nod` measured from five-point
  landmarks with `YawProxy` and `PitchProxy`; `blink`, `smile` and
  `open_mouth` reported unsupported. Timing, expiry, nonce binding,
  monotonicity and excursion checked against `DefaultChallengeThresholds()`
  (`arandu-liveness-challenge-0.1-uncalibrated`). `NewChallenge`, `BindNonce`.
- `MeasureInjectionSignals` and `Module.MeasureInjectionSignals`: nonce
  binding, frame timing and jitter (`FrameTiming`), exact and near duplicate
  frames (`DifferenceHash`, `HashDistance`), size and aspect consistency, and
  JPEG metadata (`ReadJPEGMetadata`), checked against
  `DefaultIntegrityThresholds()` (`arandu-liveness-integrity-0.1-uncalibrated`).
- `Module.Deepfake` and `DeepfakeUnavailable`: a `DeepfakeSignal` that reports
  `NotAvailable` with its reason, since no deepfake detector is cleared.
- `Config`: `ModelsDir`, `EnablePassive`, `RefuseDevOnly`, `PADModels`,
  `Scales`, `PADReference`, `Challenge`, `Integrity`, with `Validate` and
  `WithDefaults`.
- Manifests for minifasnet_v2 and minifasnet_v1se, and their `ModelCard`s.
  `Manifests`, `LookupManifest`, `LookupModelCard`, `DefaultScales`.
- `cmd/liveness-models`: downloads the model files over HTTPS and verifies
  their digests; `-check` verifies without connecting.
- `NOTICE`, attributing the upstream preprocessing the stages follow, and
  `THIRD_PARTY_MODELS.md`, listing each model's source, digest, licences,
  training data, evaluation and status. The weights are not distributed with
  the module.
