---
name: liveness-module
description: Change this Arandu package's liveness, PAD, challenge or capture-integrity stages. Use when the request is to "add a PAD model", "tune the PAD threshold", "change the crop", "add a blink challenge", "support smile", "add a deepfake detector", "detect injection", "add an integrity signal", "return whether the face is live", "add an is-real flag", "check liveness in one call", "detect the face inside PassivePAD", or when a change touches pad.go, challenge.go, integrity.go, jpegmeta.go, deepfake.go, check.go, models.go, config.go, module.go, manifests/ or cmd/liveness-models. Covers what may and may not change, where each constant comes from, and the tests that hold each property.
license: MIT
---

# Changing a stage

## Refused outright

- **A decision.** No `IsLive`, `Real`, `Genuine`, `Spoofed`, `Approved`,
  `Fraud` field or function, and no boolean of any name on `PADResult` or
  `DeepfakeSignal`. Each stage returns its measurement, its `ModelInfo` when a
  model ran, and the versioned reference set it used.
  `tests/Unit/decision_test.go` refuses the names and walks the two types.
- **A function that hides stages.** No `CheckLiveness(img)`. PassivePAD,
  EvaluateChallenge, MeasureInjectionSignals and Deepfake stay separate calls,
  so each signal can be recorded and audited on its own.
- **Hidden detection.** PassivePAD takes a box and EvaluateChallenge takes
  faces; the caller detects with arandu-face. Do not import a detector here.
- **A silent absence.** A stage without a cleared model returns
  `NotAvailable` with a reason; a challenge five landmarks cannot measure
  returns `Supported: false` with a reason.
- **A model without a manifest and a card,** or a PAD model marked `approved`
  without an ISO/IEC 30107-3 evaluation on consented data.
- **A sensitive or non-commercial model.** Nothing that infers race,
  ethnicity or gender, nothing trained on CelebA-Spoof.
  `tests/Unit/models_test.go` holds the list.
- **A download in the library or in a test.** `cmd/liveness-models` is the
  only network code, and the tests run it in `-check` mode only.
- **A real face or an attack sample in a fixture.** Tests draw their images in
  Go; landmark sequences and JPEG segments are built in the test.
- **Python,** anywhere, including "just to compare against upstream".
- **A route.** Captures are measured through the application's controllers.

## Where the constants come from

- Crop: minivision-ai/Silent-Face-Anti-Spoofing `src/generate_patches.py`,
  `CropImage._get_new_box`: scale reduced to `min((H−1)/h, (W−1)/w, scale)`,
  aspect kept, shift onto the edges, `int()` truncation, inclusive slice.
  yakhyo's `onnx_inference.py` clamps without shifting; upstream is followed.
- Scales: the upstream file names, `2.7_80x80_MiniFASNetV2` and
  `4_0_0_80x80_MiniFASNetV1SE`.
- Input: `cv2.resize` to 80×80 (INTER_LINEAR), BGR, upstream's modified
  `ToTensor` that does not divide by 255, no mean.
- Fusion: upstream `test.py`: softmax per model, sum, argmax, `p[label]/2`;
  class 1 is "Real Face". The 3:4 frame check is `check_image`.
- Proxies: `YawProxy` is arandu-face's quality yaw proxy; `PitchProxy` is the
  nose tip along the eyes-to-mouth axis.
- Every challenge and integrity threshold is a starting value, uncalibrated,
  and its version says so.

A change to any of them names its source in the commit message.

## Thresholds

Every reference is versioned: `PADReferenceSet.Version`,
`ChallengeThresholds.Version`, `IntegrityThresholds.Version`,
`DeepfakeReferenceVersion`. Changing a value is a new version string, a
changelog line, and never a boolean added to a result.

## Adding a model

1. Pin the source; take the digest from the Git LFS object id, the release
   asset digest or the Hugging Face linked etag, and check it against the file
   you have.
2. Write `manifests/<id>.json`: id, version (naming the revision or release),
   file (the path under the models directory), sha256, `source_url`, licences,
   `status`, inputs and outputs as the graph declares them, precision.
3. Add its `ModelCard` in models.go: source, training data, reported accuracy,
   evaluation, caveats.
4. Extend `tests/Unit/models_test.go`, and a feature test that runs it on a
   drawn image.

A deepfake detector that passes this review replaces `DeepfakeUnavailable` in
`Module.Deepfake`, gets a new `DeepfakeReferenceVersion` and an availability
value of its own.

## Golden values

Unit tests compute their expected values by hand, in a comment beside the
assertion, never by running the code under test.

## Concurrency

`PassivePAD` holds the module's read lock for the whole call; `Close` the
write lock. Sessions are shared. Any change here runs `go test -race ./...`
with the library and the models present, and reads the skip lines.
