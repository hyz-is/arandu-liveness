<p align="center">
  <img src=".github/logo.svg" alt="Arandu" width="180">
</p>

# Arandu Liveness

An Arandu package that measures **presentation-attack, active-challenge and
capture-integrity signals** for a face capture, with the passive PAD models
run through ONNX Runtime by
[`arandu-onnx`](https://github.com/kuaa-io/arandu-onnx). It returns
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
[`arandu-face`](https://github.com/kuaa-io/arandu-face)'s `Detect`, so the
application composes detection, quality, PAD and challenge explicitly and
records each one's provenance. No function runs two stages for the caller.
`PADResult` and `DeepfakeSignal` hold no boolean at all; a test walks their
types to keep it so.

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
     `kuaa-liveness-challenge-0.1-uncalibrated`: ≥ 5 frames, motion 0.3–5 s,
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
     `kuaa-liveness-integrity-0.1-uncalibrated`: jitter CV in [0.002, 0.5],
     max interval 250 ms, near duplicate ≤ 2 bits, consecutive near-duplicate
     ratio ≤ 0.5, aspect 0.75 ± 0.01.
4. **Deepfake and face-swap detection.** No model with an acceptable licence,
   disclosed and consented training data and pinned provenance has been
   reviewed, so none is shipped. The stage still exists:
   `Module.Deepfake` returns `DeepfakeSignal{Availability: "not_available",
   Reason, Model: nil, Version: "kuaa-liveness-deepfake-none-0.1"}`, so the
   application records the absence of the signal as evidence instead of
   skipping it silently.

## Build requirement

Those of `arandu-onnx`: `CGO_ENABLED=1`, a C compiler, and the ONNX Runtime
shared library, 1.26 or later, on the machine that runs the application, when
passive PAD is enabled. Development and the tests use 1.30.0. A configuration
without `EnablePassive` boots without touching ONNX Runtime.

## Install

```bash
go get github.com/kuaa-io/arandu-liveness
```

Until `arandu-onnx` and `arandu-face` are tagged, `go.mod` resolves them from
the sibling checkouts:

```
replace (
	github.com/kuaa-io/arandu-face => ../arandu-face
	github.com/kuaa-io/arandu-onnx => ../arandu-onnx
)
```

Those lines are removed, and both modules required at their tags, once they
are published. The release workflow refuses to publish this one while they
are there.

## The models

The model files are not in this repository. Each has a manifest, embedded in
the package under `manifests/`, recording its source, SHA-256, licences,
status and contract; `Runtime.Open` verifies every file against it before
ONNX Runtime reads a byte. What the manifest has no field for — training
data, reported accuracy, evaluation, caveats — is in its `ModelCard`
(`LookupModelCard`).

| id | model | source | sha256 | size | status |
| --- | --- | --- | --- | --- | --- |
| `minifasnet_v2` | MiniFASNetV2, 2.7_80x80 | yakhyo release `weights` | `b32929adc2d9…` | 1.7 MB | **dev-only** |
| `minifasnet_v1se` | MiniFASNetV1SE, 4_0_0_80x80 | yakhyo release `weights` | `ebab7f90c783…` | 1.7 MB | **dev-only** |

- **Code and weights**: Apache-2.0, minivision-ai/Silent-Face-Anti-Spoofing;
  the ONNX files are a third-party export by yakhyo/face-anti-spoofing
  (Apache-2.0). The release tag is mutable; the digest, which equals the
  release asset's published digest, is what pins each file.
- **Training data**: undisclosed upstream.
- **Evaluation**: none known — no ISO/IEC 30107-3 evaluation, no iBeta or
  other laboratory report, no measurement on consented attack data. APCER and
  BPCER are unknown.
- Both are **dev-only** for those reasons, and `RefuseDevOnly` refuses them.

No model here infers race, ethnicity or gender, and none is trained on
CelebA-Spoof (non-commercial); a test refuses them.

A models directory holds each file at the path its manifest's `file` says
(`liveness/MiniFASNetV2.onnx`, `liveness/MiniFASNetV1SE.onnx`).
`cmd/liveness-models` fills one:

```bash
go run github.com/kuaa-io/arandu-liveness/cmd/liveness-models -dir /srv/models
go run github.com/kuaa-io/arandu-liveness/cmd/liveness-models -dir /srv/models -check
```

It downloads each file over HTTPS from the source its manifest records,
follows only HTTPS redirects, hashes while it writes, and keeps the file only
when the digest matches. `-only` takes a comma-separated list of ids; `-check`
verifies what is there and connects to nothing. **The command is the only code
here that opens a network connection.** The library an application links
reads files from the directory it is given, which is why `arandu.mod.toml`
declares `network = false`.

## Wire it

An Arandu application registers a module explicitly. There is no service
provider, no container and no discovery, so these are the lines to paste into
`bootstrap/app.go` and there are no others.

The imports, with the other module imports:

```go
import (
	onnx "github.com/kuaa-io/arandu-onnx"
	face "github.com/kuaa-io/arandu-face"
	liveness "github.com/kuaa-io/arandu-liveness"
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

On an Apple M4 Max, CPU provider, ONNX Runtime 1.30.0, on the 480×640 cartoon
(`go test -run '^$' -bench . ./tests/Feature/`): PassivePAD with both models
≈ 4.8 ms.

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

## Why there is no table, policy or route

This repository has the shape of the Arandu package skeleton without its demo
entity, like `arandu-onnx` and `arandu-face`. It wraps a technology and owns
no record, so there is nothing for a policy to decide about; a route would
measure any face for anybody who sent one. Captures are measured through the
application's controllers and Grants, challenges and their nonces are issued
and stored by the application, and biometric data is the application's to
minimize, encrypt and audit.

## Limitations

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

## Gates

```sh
export GOWORK=off CGO_ENABLED=1
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go')
go build ./...
go vet ./...
go test -race ./...
```

The model tests read the models from `KUAA_MODELS_DIR`, or from the KUAA
workspace's `models/` two levels above this checkout, and skip, saying where
they looked, when the files are not there; they skip too when ONNX Runtime is
absent. The composition test also needs arandu-face's YuNet file. A skip is a
reason to fetch the models, not a green result. No test downloads anything.

## Licence

MIT. See [LICENSE.md](LICENSE.md). The model files, which this repository does
not contain, are under their own licences, listed above and in their
manifests.
