<p align="center">
  <img src=".github/logo.svg" alt="Arandu" width="180">
</p>

# Arandu Liveness

An Arandu package that measures **presentation-attack, active-challenge and
capture-integrity signals** for a face capture, with the passive PAD models
run through ONNX Runtime by
[`arandu-onnx`](https://github.com/hyz-is/arandu-onnx). It returns
**separate, auditable technical signals**, each with the provenance of the
model behind it when a model is involved. It decides nothing. Thresholds come
back as versioned reference sets beside the measurements, and whether a
capture shows a present, live person is the application's policy, not this
package's.

Liveness, presentation attack detection (PAD) and injection or deepfake
detection are different questions, and each has its own call and result:

| Stage | Call | Model | Result |
| --- | --- | --- | --- |
| 1. Passive PAD | `Module.PassivePAD(ctx, img, box)` | MiniFASNetV2 + MiniFASNetV1SE (**dev-only**) | `PADResult{PerModel []{Model, Scale, EffectiveScale, Crop, Logits, Probabilities}, FusedProbabilities, FusedRealProbability, FusedLabel, FusedLabelProbability, CaptureAspect, ReferenceSet}` |
| 2. Active challenge | `EvaluateChallenge(ch, resp, thresholds)` / `Module.EvaluateChallenge` | none (Go) | `ChallengeResult{Kind, Supported, Observed, Completed, Timing, FrameCount, Monotonicity, Nonce, LandmarkModels, Checks, Thresholds}` |
| 3. Capture integrity | `MeasureInjectionSignals(capture, thresholds)` / `Module.MeasureInjectionSignals` | none (Go) | `InjectionSignals{Nonce, Timing, Duplicates, Geometry, Metadata, Checks, Thresholds}` |
| 4. Deepfake | `Module.Deepfake(ctx, img)` / `DeepfakeUnavailable()` | none available | `DeepfakeSignal{Availability "not_available", Reason, Model nil, Version}` |

**No call detects a face.** The face box (stage 1) and the per-frame
landmarks (stage 2) come from the caller, normally from
[`arandu-face`](https://github.com/hyz-is/arandu-face)'s `Detect`, so the
application composes detection, quality, PAD and challenge explicitly and
records each one's provenance. No function runs two stages for the caller.
`PADResult` and `DeepfakeSignal` hold no boolean at all; a test walks their
types to keep it so.

## What it is not

- **Not a liveness verdict.** It never says a capture is live, spoofed or
  genuine. Signals, never decisions: each result carries a versioned reference
  set beside the measurement, and the application's decision layer applies its
  own policy and records the evidence.
- **Not certified PAD.** The PAD models have no ISO/IEC 30107-3 or laboratory
  evaluation, their training data is undisclosed, and both are `dev-only`.
- **Not a deepfake detector.** No such model is shipped; the stage reports its
  own absence.
- **Not a face detector.** The face box and landmarks come from the caller,
  normally from `arandu-face`.
- **Not a model distribution or a store.** The weights are not in this
  repository (see [THIRD_PARTY_MODELS.md](THIRD_PARTY_MODELS.md)); nothing
  here keeps an image, a frame or a result, or opens a network connection.

## Requirements

| | |
| --- | --- |
| Go | 1.26 or later |
| cgo | `CGO_ENABLED=1` and a C compiler (the ONNX Runtime binding is cgo) |
| ONNX Runtime | the shared library, **1.26 or later**, when passive PAD is enabled; development and CI use **1.30.0**. Installation per operating system is in the [`arandu-onnx` README](https://github.com/hyz-is/arandu-onnx#installing-onnx-runtime). A configuration without `EnablePassive` boots without touching ONNX Runtime |
| Arandu | framework 0.50 or later, `github.com/hyz-is/arandu-onnx` and `github.com/hyz-is/arandu-face` |
| Models | 3.5 MB for both PAD models, fetched once with `cmd/liveness-models`; the face detector comes from `arandu-face` |

## Install

```bash
go get github.com/hyz-is/arandu-liveness@v0.1.0
go run github.com/hyz-is/arandu-liveness/cmd/liveness-models@v0.1.0 -dir /srv/models
```

## Wire it

An Arandu application registers a module explicitly. There is no service
provider, no container and no discovery, so these are the lines to paste into
`bootstrap/app.go` and there are no others.

The imports, with the other module imports:

```go
import (
	onnx "github.com/hyz-is/arandu-onnx"
	face "github.com/hyz-is/arandu-face"
	liveness "github.com/hyz-is/arandu-liveness"
)
```

The construction, in `Build`, before `k.Register`, after the ONNX and face
modules. Every value comes from the application's own typed configuration;
this package reads no environment variable:

```go
	livenessModule, err := liveness.New(liveness.Config{
		ModelsDir:     cfg.Liveness.ModelsDir,
		EnablePassive: cfg.Liveness.Passive,
		RefuseDevOnly: cfg.Liveness.Production,
	}, onnxModule.Runtime())
	if err != nil {
		return App{}, err
	}
```

And the registration, inside the `k.Register(...)` call already there, after
`faceModule`:

```go
		onnxModule,
		faceModule,
		livenessModule,
```

`Boot` initializes the runtime if `arandu-onnx` has not yet and passive PAD is
enabled, and opens the PAD models, verifying each; a missing or altered file
stops the boot, naming the model. With `RefuseDevOnly` set and passive PAD
enabled, the boot stops before any file is read, with `ErrDevOnlyRefused`:
both PAD models are dev-only. `Health` reports whether the module is booted
and, with models open, whether the runtime under them is usable. `Models`
lists the `ModelInfo` of each open model. `Close` closes the sessions.
Registering it adds no route, no table and no background loop.

## Configuration

| Field | Default | What it does |
| --- | --- | --- |
| `ModelsDir` | required with `EnablePassive` | The directory the model files are in |
| `EnablePassive` | `false` | Open the PAD models at Boot, for PassivePAD |
| `RefuseDevOnly` | `false` | Refuse every configured dev-only model at Boot — the production switch |
| `PADModels` | `minifasnet_v2`, `minifasnet_v1se` | The PAD models run and fused, in order |
| `Scales` | 2.7 and 4.0 | Crop scale override per model, above 0 and at most 10; anything else leaves upstream's preprocessing |
| `PADReference` | `MiniFASNetUpstreamReference()` | The reference PassivePAD returns |
| `Challenge` | `DefaultChallengeThresholds()` | The references EvaluateChallenge checks against |
| `Integrity` | `DefaultIntegrityThresholds()` | The references MeasureInjectionSignals checks against |

A field left at its zero value, and a reference set left without a version,
takes its default.

## Measure a capture

```go
img, err := onnx.DecodeImage(upload) // JPEG or PNG, turned upright by EXIF

faces, err := faceModule.Detect(ctx, img)                    // arandu-face: box, landmarks, detector ModelInfo
pad, err := livenessModule.PassivePAD(ctx, img, faces[0].Box) // stage 1: per-model logits, fusion, reference

ch, err := liveness.NewChallenge(liveness.ChallengeTurnLeft, now, 15*time.Second, nil) // issued by the server
// ... the client captures frames and echoes ch.Nonce; each frame goes through faceModule.Detect ...
res, err := livenessModule.EvaluateChallenge(ch, liveness.ChallengeResponse{
	EchoedNonce: echoed, ReceivedAt: receivedAt, Frames: frames, // []{Timestamp, Face}
}) // stage 2

sig, err := livenessModule.MeasureInjectionSignals(liveness.Capture{
	Frames: captured, ExpectedNonce: ch.Nonce, EchoedNonce: echoed, // []{Timestamp, Image, Encoded}
}) // stage 3

df, err := livenessModule.Deepfake(ctx, img) // stage 4: NotAvailable, recorded as such
```

Every model-backed result carries an `onnx.ModelInfo` — ID, Version, SHA256,
Runtime, Precision, Provider and Status — which is what the application
records as evidence beside the signal. A `dev-only` status in that record says
the signal came from a model not cleared for production. Every pure-Go result
carries its reference set and version.

## How each stage works

1. **Passive PAD** — Silent-Face-Anti-Spoofing's MiniFASNet pair, as upstream
   `test.py` runs it:
   - **Crop** (`PADCrop`), upstream `CropImage._get_new_box`: the scale is
     first reduced to what fits, `min((H−1)/box_h, (W−1)/box_w, scale)`; the
     box is enlarged by it about its centre **keeping its aspect** (it is not
     squared); an enlarged box past the left or top edge is moved right or
     down onto it, one past `W−1` or `H−1` moved left or up; the corners are
     truncated to integers and the region is inclusive of both. MiniFASNetV2
     uses scale 2.7, MiniFASNetV1SE 4.0, the values in the upstream file
     names.
   - **Input**: the region resized to 80×80 bilinearly (OpenCV-aligned pixel
     centres, as `cv2.resize`), **BGR, raw 0..255, no `/255` and no mean**
     (upstream's modified `ToTensor`), `input` f32[1,3,80,80].
   - **Output**: `output` f32[1,3] **logits**; `Softmax` turns each model's into
     probabilities (largest logit subtracted first).
   - **Fusion** (`FusePAD`): the models' probabilities summed, the label their
     argmax (first on a tie), and every value divided by the number of models,
     so the label's fused probability is upstream's "score" (sum / 2).
   - **Classes**: index 1 is what upstream calls a real face
     (`PADClassBonaFide`); indices 0 and 2 are both counted as fake upstream,
     and **what each stands for (print, replay, mask) is not documented and is
     unverified here** (`PADClassAttack0`, `PADClassAttack2`).
   - **Capture aspect**: upstream refuses frames that are not 3:4
     (width:height). Here the frame's ratio, its deviation from 0.75 and the
     tolerance come back in `CaptureAspect`; nothing is refused.
   - **Reference**: `MiniFASNetUpstreamReference()`, version
     `minifasnet-upstream-0.5`: threshold 0.5 on the fused bona fide
     probability (which always selects class 1 under upstream's argmax),
     expected aspect 0.75 ± 0.01. Not calibrated on any population.
2. **Active challenge**, pure Go. The server issues a `Challenge{ID, Kind,
   IssuedAt, ExpiresAt, Nonce}` (`NewChallenge` draws a 128-bit ID and a
   256-bit nonce); the client captures frames and echoes the nonce; the
   application detects a face on each frame and passes
   `ChallengeResponse{EchoedNonce, ReceivedAt, Frames []{Timestamp, Face}}`.
   - `YawProxy`: the nose tip's offset from the eyes' midpoint along the eye
     line, in inter-ocular distances (arandu-face's quality signal), positive
     toward the image's right — the subject's left on an **unmirrored** image.
   - `PitchProxy`: the nose tip's position along the eyes-to-mouth axis, as a
     share of it (≈ 0.5 frontal on the ArcFace template); smaller when the head
     tilts back, larger when it tilts forward.
   - Both are geometric proxies, roll-invariant, not angles.
   - **Supported with five-point landmarks**: `turn_left`, `turn_right` (yaw),
     `look_up`, `look_down`, `nod` (pitch; a nod is forward then back, and its
     excursion is the smaller half). **Unsupported**: `blink`, `smile`,
     `open_mouth` — five points carry no eyelid or lip contour; they return
     `Supported: false` with the reason, never an error and never a guess.
   - Measured: the series, the baseline, the peak, the excursion toward the
     requested side, the monotonicity (share of steps larger than
     `StepEpsilon` that go the requested way), the response latency, capture
     and motion durations, frames outside `[IssuedAt, ExpiresAt]`,
     non-increasing timestamps, receipt after expiry, and the nonce binding
     (constant-time comparison).
   - Each is a `Check{Signal, Value, Rule, WithinReference}` against
     `DefaultChallengeThresholds()`, version
     `arandu-liveness-challenge-0.1-uncalibrated`: ≥ 5 frames, motion 0.3–5 s,
     |baseline yaw| ≤ 0.12, yaw excursion ≥ 0.25, pitch excursion ≥ 0.10,
     monotonicity ≥ 0.7, step epsilon 0.005.
   - `Completed` is true only when every check is within its reference. It
     records that the movement matches what was asked under that version —
     **not** that a live person made it: a replayed video of the right
     movement completes a challenge too, which is what the nonce, the timing
     and stage 3 are for.
3. **Capture integrity**, pure Go, as `InjectionSignals`: signals of a
   replayed, injected or edited capture, never a conclusion.
   - Nonce binding: the challenge's nonce against the one echoed in the
     capture metadata.
   - Timing: intervals, mean, standard deviation, min, max, coefficient of
     variation (`FrameTiming`) and non-increasing timestamps. A perfectly
     regular stream is as unusual as a very irregular one.
   - Duplicates: a 64-bit difference hash per frame (`DifferenceHash`, 9×8 cell
     luma averages), exact duplicate pairs (SHA-256 of the pixels) and near
     duplicate pairs anywhere in the capture, consecutive near duplicates and
     the mean consecutive distance (`HashDistance`).
   - Geometry: distinct sizes, size changes, the first frame's aspect, frames
     off the expected aspect.
   - Metadata of frames sent encoded (`ReadJPEGMetadata`, a marker walker that
     stops at the first scan): EXIF presence and IFD0 Make, Model, Software,
     DateTime and Orientation; JFIF, XMP, ICC and Photoshop (APP13) segments;
     the frame header's size and whether it is progressive. Aggregated: mixed
     EXIF presence, editing segments, distinct Software values, and header
     sizes that differ from the decoded size.
   - Reference: `DefaultIntegrityThresholds()`, version
     `arandu-liveness-integrity-0.1-uncalibrated`: jitter CV in [0.002, 0.5],
     max interval 250 ms, near duplicate ≤ 2 bits, consecutive near-duplicate
     ratio ≤ 0.5, aspect 0.75 ± 0.01.
4. **Deepfake and face-swap detection.** No model with an acceptable licence,
   disclosed and consented training data and pinned provenance has been
   reviewed, so none is shipped. The stage still exists:
   `Module.Deepfake` returns `DeepfakeSignal{Availability: "not_available",
   Reason, Model: nil, Version: "arandu-liveness-deepfake-none-0.1"}`, so the
   application records the absence of the signal as evidence instead of
   skipping it silently.

## The models

The model files are not in this repository and are not distributed with it.
Each has a manifest, embedded in the package under `manifests/`, recording its
source, SHA-256, licences, status and contract; `Runtime.Open` verifies every
file against it before ONNX Runtime reads a byte. What the manifest has no
field for — training data, reported accuracy, evaluation, caveats — is in its
`ModelCard` (`LookupModelCard`).

| id | file | sha256 | size | crop scale | status |
| --- | --- | --- | --- | --- | --- |
| `minifasnet_v2` | `liveness/MiniFASNetV2.onnx` | `b32929adc2d9c34b9486f8c4c7bc97c1b69bc0ea9befefc380e4faae4e463907` | 1.7 MB | 2.7 | **dev-only** |
| `minifasnet_v1se` | `liveness/MiniFASNetV1SE.onnx` | `ebab7f90c7833fbccd46d3a555410e78d969db5438e169b6524be444862b3676` | 1.7 MB | 4.0 | **dev-only** |

| | both models |
| --- | --- |
| Source | minivision-ai/Silent-Face-Anti-Spoofing, exported to ONNX by [yakhyo/face-anti-spoofing](https://github.com/yakhyo/face-anti-spoofing/releases/tag/weights), release `weights`. The tag is mutable; the digest, which equals the release asset's published digest, is what pins each file |
| Code licence | Apache-2.0 |
| Model licence | Apache-2.0 |
| Training data | undisclosed upstream |
| Evaluation | none known: no ISO/IEC 30107-3 evaluation, no iBeta or other laboratory report, no measurement on consented attack data. APCER and BPCER are unknown |
| Commercial use | permitted by the licence |
| Status | **dev-only**, for the training-data and evaluation reasons above |

No model here infers race, ethnicity or gender, and none is trained on
CelebA-Spoof (non-commercial); a unit test refuses manifests for them.

### `dev-only` and `RefuseDevOnly`

A `dev-only` model loads, and every `ModelInfo` it produces says
`Status: "dev-only"`, so the evidence the application records shows the signal
came from a model not cleared for production. With `Config.RefuseDevOnly` set —
the production switch — and `EnablePassive` on, `Boot` stops before any file is
read, with `ErrDevOnlyRefused` naming the model: both PAD models are dev-only.
The challenge, integrity and deepfake stages use no model and work either way.

### Download and verify

```bash
go run github.com/hyz-is/arandu-liveness/cmd/liveness-models@v0.1.0 -dir /srv/models
go run github.com/hyz-is/arandu-liveness/cmd/liveness-models@v0.1.0 -dir /srv/models -check
```

A models directory holds each file at the path its manifest's `file` says
(`liveness/MiniFASNetV2.onnx`, `liveness/MiniFASNetV1SE.onnx`), and can be the
same directory `arandu-face` uses. The command downloads each file over HTTPS
from the source its manifest records, follows only HTTPS redirects, hashes
while it writes, and renames the file into place only when the digest matches.
A file already present with the right digest is left alone. `-only` takes a
comma-separated list of ids; `-check` verifies what is there, connects to
nothing, and exits non-zero when a file is missing or altered. **The command is
the only code in this repository that opens a network connection**, and an
application never links it, which is why `arandu.mod.toml` declares
`network = false`.

## What was measured, and what was not

The feature tests run both PAD models on images drawn in Go — a cartoon face
assembled from ellipses (the one arandu-face's tests detect) and
pseudo-random noise. No photograph of a person, no face dataset and no attack
sample is used. The unit tests use synthetic landmark sequences, drawn frames
and handcrafted JPEG segments.

- Both models open against their manifests, and each returns three finite
  logits whose softmax sums to 1 within 1e-9; the fusion equals the mean of
  the two models; the label is its argmax.
- Two runs on one input give bit-identical logits; noise and the cartoon give
  different logits; a sub-image gives the same crops, in the parent's
  coordinates, and the same logits.
- Each crop equals `PADCrop`'s, whose golden values (inside, clamped and
  shifted at each edge, scale reduced to fit, fractional box, aspect kept)
  are worked out by hand from upstream's code.
- The composition test detects the cartoon with arandu-face's YuNet and runs
  PassivePAD on the detected box.
- Concurrent PassivePAD calls from four goroutines give the same logits as a
  sequential call, under the race detector.

Speed on the 480×640 cartoon, CPU provider, ONNX Runtime 1.30.0, on an Apple
M4 Max (`go test -run '^$' -bench . ./tests/Feature/`): `PassivePAD` with both
models takes about 3–5 ms over several runs. The figure is indicative: one
machine, synthetic images, no accelerator.

**The models' APCER and BPCER, their behaviour on any attack instrument
(print, replay, mask, injection), the calibration of every threshold, the
challenge thresholds against real head movement, and any difference between
demographic groups have not been measured.** They must be measured by the
owner on consented bona fide and attack data, under an ISO/IEC 30107-3
protocol, before any of these signals weighs on a decision. The PAD score of
a drawing means nothing either way.

## Concurrency

`PassivePAD` is safe for concurrent use. Calls share the ONNX Runtime
sessions, whose runs are thread-safe; the module's reader-writer lock lets
them proceed together and makes `Close` wait for them. `EvaluateChallenge`,
`MeasureInjectionSignals`, `Deepfake` and the exported helpers are pure.

## Limitations, and what needs evaluation

- **PAD assurance**: none established. The weights are dev-only, their
  training data is undisclosed, and they have no ISO/IEC 30107-3 or
  laboratory evaluation. What classes 0 and 2 mean is unverified.
- The models were built for 3:4 phone frames and an upstream RetinaFace box;
  a YuNet or BlazeFace box differs in size and position, which changes the
  crop. That difference is not measured.
- Five-point landmarks support head turns, tilts and nods only. Blinks,
  smiles and mouth opening need dense landmarks, which no shipped model
  provides. Yaw and pitch are geometric proxies, not angles, and their
  thresholds are uncalibrated.
- Frame timestamps are taken as given, in the server's time base; a client
  clock is not trusted by anything here, and clock skew is the application's
  to handle.
- The integrity signals see crude replay and editing (repeated frames,
  regular synthetic timing, size changes, editing segments). A competent
  virtual-camera or injection attack produces none of them.
- There is no deepfake or face-swap detector.
- Before any of these signals weighs on a decision, measure APCER and BPCER
  on consented bona fide and attack data under an ISO/IEC 30107-3 protocol,
  calibrate every reference set on it, and check the variation across age,
  skin tone, sex, capture device and lighting.

## Security and privacy

Face captures are biometric data, which LGPD, GDPR and similar laws treat as
sensitive personal data. This package holds none of it beyond the call that
measures it: it does not store, log or transmit a frame or a result. Collecting
only what is needed, encrypting the raw capture apart from the normalized
facts, controlling and auditing every access, obtaining a lawful basis or
consent, and deleting on schedule are the application's responsibility.

A challenge's nonce is a secret until the capture is received: the application
issues it, stores it, and compares it through this package, which compares in
constant time. Frame timestamps should come from the server's clock.

These signals are not an anti-spoofing guarantee. An attacker who can inject a
synthetic video stream defeats the integrity signals, and the PAD models are
unevaluated. Report vulnerabilities privately; see [SECURITY.md](SECURITY.md).

## Why there is no table, policy or route

This repository has the shape of the Arandu package skeleton without its demo
entity, like `arandu-onnx` and `arandu-face`. It wraps a technology and owns
no record, so there is nothing for a policy to decide about; a route would
measure any face for anybody who sent one. Captures are measured through the
application's controllers and Grants, challenges and their nonces are issued
and stored by the application, and biometric data is the application's to
minimize, encrypt and audit.

## Tests

```bash
export GOWORK=off CGO_ENABLED=1
test -z "$(gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go'))"
go build ./...
go vet ./...
go test -race -count=1 ./...
```

The model tests read the models from the directory `LIVENESS_MODELS_DIR`
names, or from `../../models` when it is unset, and skip, saying where they
looked, when the files are not there; they skip too when ONNX Runtime is
absent. The composition test also needs arandu-face's YuNet file in the same
directory. No test downloads anything. To run them:

```bash
go run ./cmd/liveness-models -dir /tmp/models
go run github.com/hyz-is/arandu-face/cmd/face-models -dir /tmp/models -only yunet_2026may
LIVENESS_MODELS_DIR=/tmp/models go test -race -count=1 ./...
```

CI installs ONNX Runtime 1.30.0, fetches and verifies both PAD models and
YuNet with the modules' own commands, and runs the suite with nothing skipped.
Every image the tests read is drawn in Go; no photograph of a person and no
attack sample is read or committed.

## Versioning and releases

The module follows [Semantic Versioning](https://semver.org/). While the major
version is `v0`, a minor release may break the API, and every break is named in
[UPGRADE.md](UPGRADE.md); CI compares the exported API with the last tag. A new
model, a new model revision, a changed default reference or a changed reference
version changes what every installer records as evidence, and is always a
changelog line.

A release is a tag `vX.Y.Z` on `main` whose notes are the `## [X.Y.Z] - date`
entry already committed in [CHANGELOG.md](CHANGELOG.md). The release workflow
refuses a `go.mod` that still replaces a module with a local path, re-runs the
gates on the exact tag archive with the models fetched, audits known
vulnerabilities, and publishes those notes. A published version is immutable.

## Licence

The code is MIT; see [LICENSE.md](LICENSE.md). Copyright HYZIS - Serviços
Digitais Ltda.

The model weights are not part of this repository; each is under its own
licence, listed in [THIRD_PARTY_MODELS.md](THIRD_PARTY_MODELS.md). The
upstream preprocessing this code follows is attributed in [NOTICE](NOTICE).
