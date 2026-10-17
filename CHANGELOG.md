# Changelog

Everything worth knowing about a release of Arandu Liveness is recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
the versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

A published module version is immutable: Go serves it from the proxy forever, so
a release is corrected by another release and never by moving a tag.

## [Unreleased]

Everything here is additive: no exported symbol changed, and an installer
who sets none of the new Config fields gets their defaults.

### Added

- `NewChallengeSequence`, `EvaluateChallengeSequence` and
  `Module.EvaluateChallengeSequence`: a server-drawn sequence of 2 to 4
  (`MinSequenceSteps`, `MaxSequenceSteps`) supported challenge kinds, drawn
  uniformly by rejection sampling with no kind following itself
  (`DefaultSequenceKinds` when none are given), each step a `Challenge` with
  a cumulative deadline and a nonce derived by `DeriveStepNonce`
  (`SequenceNonceDerivation`, `arandu-liveness-step-nonce-sha256-v1`).
  `ChallengeSequence.Validate` recomputes every step nonce. The evaluation
  returns every step's `ChallengeResult` (`SequenceStepResult`) and
  `ChallengeSequenceResult{ExpectedOrder, ExpectedKinds, ObservedOrder,
  ObservedKinds, OrderInversions, StepCount, AnsweredSteps, MissingSteps,
  CompletedSteps, UnboundResponses, DuplicateResponses, Timing, Checks,
  Thresholds}`, with `SequenceResponse`, `SequenceTiming` and
  `SequenceThresholds` (`DefaultSequenceThresholds()`,
  `arandu-liveness-sequence-0.1-uncalibrated`).
- `Module.PassivePADFrames`: PassivePAD on several frames (`PADFrame`), with
  per-frame results or skip reasons (`PADFrameResult`, `PADSkipReason`:
  `no_image`, `image_too_large`, `box_outside_image`, `over_frame_limit`) and
  `PADFramesResult{Submitted, Evaluated, Skipped, SkippedBy, FrameLimit,
  FusedReal, LabelCounts, Span, ReferenceSet}`. `SummarizePADSeries` and
  `PADSeriesStats` (mean, median, min, max, variance, standard deviation,
  mean and largest consecutive change); `EvenlySpacedFrames` picks the frames
  within the limit. No boolean on the result.
- `MeasureScreenReplay` and `Module.MeasureScreenReplay`: an experimental
  screen-replay signal in pure Go, a Hann-windowed 2D FFT periodicity measure
  (`PeriodicPeakRatio`, `BandEnergyRatio`, `PeakFrequency`,
  `PeakOrientation`) and a specular highlight ratio, as
  `ScreenReplaySignals`, with `ScreenReplayMethod`, `MinScreenReplaySize`,
  `MaxScreenReplaySize` and `ScreenReplayReferenceSet`
  (`DefaultScreenReplayReference()`,
  `arandu-liveness-screen-replay-0.1-experimental-uncalibrated`, no
  threshold). No boolean on the result.
- `Config.Sequence`, `Config.MaxPADFrames` (default `DefaultMaxPADFrames`,
  8, at most `MaxFrames`) and `Config.ScreenReplay`, validated by
  `Config.Validate` and filled by `WithDefaults`.

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
