# Working on Arandu Liveness

This is an Arandu package that measures presentation-attack, active-challenge
and capture-integrity signals for a face capture, with the passive PAD models
run through ONNX Runtime by `arandu-onnx`. It returns separate technical
signals — passive PAD, a challenge's observed movement, injection and replay
signals, and the explicit absence of a deepfake signal — each with the
`ModelInfo` of the model behind it, when there is one, and the versioned
reference set it was checked against.
It is a Go module somebody `go get`s and registers by hand in their own
`bootstrap/app.go`, which is the whole difference from working in an
application. There is no service provider, no container and no discovery — if a
line of wiring is not written in the installer's repository, it does not happen.

Read `.agents/skills/` before writing code. Each skill is a procedure, and the
one you need is named by the situation you are in.

## The rules

These come from the engines' design, not from taste. They were written for
KUAA, the identity-verification application these engines were built for, and
they hold for every installer. A change that breaks one is not
merged.

1. **Signals, never decisions.** Every stage is its own exported call with its
   own result. Nothing here returns "live", "real", "genuine", "spoof",
   "approved" or "fraud", and no function runs several stages for the caller
   (there is no `CheckLiveness`). Liveness, PAD and injection/deepfake are
   separate results. Thresholds travel with the results as versioned
   reference sets (`PADReferenceSet`, `ChallengeThresholds`,
   `SequenceThresholds`, `IntegrityThresholds`,
   `ScreenReplayReferenceSet`); what they mean for a case is the policy
   engine's business, in the application. `ChallengeResult.Completed` and
   `Check.WithinReference` say a measurement met a versioned reference, never
   what the subject is. `tests/Unit/decision_test.go` refuses the names that
   would break this, and holds that `PADResult`, `PADFramesResult`,
   `ScreenReplaySignals` and `DeepfakeSignal` carry no boolean at all, and
   `ChallengeSequenceResult` none outside the types the name rule sanctions.
2. **No hidden detection.** The face box and the landmarks come from the
   caller, normally from arandu-face's `Detect`. Nothing here detects a face.
3. **No Python.** Not as a runtime, not in a gate, not in a test, not to
   generate a fixture. Models arrive as already-exported ONNX files; test
   images, landmark sequences and JPEG segments are made in Go.
4. **Model provenance is mandatory.** Every model is opened through its
   embedded manifest, which `Runtime.Open` verifies. A new model is a manifest
   with a pinned source, a digest and its licences, plus a `ModelCard` for
   what the manifest has no field for (training data, reported accuracy, PAD
   evaluation), reviewed before its status is `approved`. `dev-only` models are
   refused by `Boot` when `Config.RefuseDevOnly` is set. `approved` is a
   licence and provenance status: under the maintainer's open-source decision
   (2026-10-19), permissive weights (Apache-2.0, MIT, BSD) qualify, with what
   is unknown about their data recorded in `ModelCard.Review` as an accepted
   risk. It is not a PAD performance claim: until an ISO/IEC 30107-3
   evaluation on consented data exists, the card's `Evaluation` says so, and
   no PAD signal is fit to weigh on a decision before it is measured.
5. **No sensitive or non-commercial model.** No model that infers race,
   ethnicity or gender (sensitive data under LGPD art. 11), no non-commercial
   recognizer, and nothing trained on CelebA-Spoof. `tests/Unit/models_test.go`
   holds the list.
6. **No silent absence.** A stage without a cleared model still exists and
   says so (`DeepfakeSignal{Availability: NotAvailable}`), and an unsupported
   challenge says `Supported: false` with its reason.
7. **The library does not touch the network.** `cmd/liveness-models` is the
   one program that downloads, and an application never links it. The library
   reads model files from `Config.ModelsDir`.
8. **Configuration is typed.** `Config` comes from the application. This
   package reads no environment variable; only the tests read
   `LIVENESS_MODELS_DIR`.
9. **Fixtures are synthetic.** No photograph of a person, no face dataset, no
   attack sample, no name or number in a test, an example or a log line.

## The gates

Nothing is finished until all four exit zero.

```sh
export GOWORK=off CGO_ENABLED=1
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go')
go build ./...
go vet ./...
go test -race ./...
```

`CGO_ENABLED=1` is required: arandu-onnx's binding is cgo. The model tests
need the ONNX Runtime shared library and the model files, from
`LIVENESS_MODELS_DIR` or `../../models`; where either is absent
they skip saying why, and the skip is a reason to fix the machine, not a green
result for the models. On a shared workstation, `GOFLAGS=-p=2` keeps the
build from competing with everything else for memory.

`GOWORK=off` matters because this checkout may sit beside a Go workspace that
does not list it.

`go.mod` requires `github.com/hyz-is/arandu-onnx` and
`github.com/hyz-is/arandu-face` at published tags, with no replace, so the
checkout builds on its own; the release workflow refuses a local replace. When
this module requires an arandu-face version not yet tagged, arandu-face is
released first (see the release skill). Local runs and gate logs go in the
git-ignored `tmp/`.

`aru doctor` reads applications, not libraries. That is why
`tests/Unit/audit_test.go` compares `arandu.mod.toml` with what the code calls.

## What this repository holds

```
module.go     registration: Name, Routes (none), Boot, Health, Models, Close
config.go     what the application passes in, and the defaults
models.go     the embedded manifests, the scales and the model cards
pad.go        PassivePAD, PADCrop, Softmax, FusePAD, CaptureAspect, the reference
challenge.go  Challenge, YawProxy, PitchProxy, EvaluateChallenge, BindNonce
sequence.go   ChallengeSequence, NewChallengeSequence, DeriveStepNonce, EvaluateChallengeSequence
padframes.go  PassivePADFrames, SummarizePADSeries, EvenlySpacedFrames
screenreplay.go  MeasureScreenReplay: FFT periodicity and specular highlights, experimental
integrity.go  MeasureInjectionSignals, FrameTiming, DifferenceHash
jpegmeta.go   ReadJPEGMetadata: JPEG markers and EXIF IFD0
deepfake.go   DeepfakeSignal, NotAvailable
check.go      Check: a signal against its reference
manifests/    one manifest per model
cmd/liveness-models/  downloads and verifies the model files
NOTICE                 the upstream preprocessing the stages follow, attributed
THIRD_PARTY_MODELS.md  the source, digest, licences and status of every model
```

Tests live under `tests/Unit` (crop geometry, softmax and fusion, challenges,
integrity signals, JPEG metadata, manifests, config, the audit, the
no-decision rule) and `tests/Feature` (the module, the command's check mode,
and every test that runs a model).

## What does not exist here, on purpose

| A change reaches for | What is here instead |
| --- | --- |
| an `IsLive`, `Genuine`, `Spoof` or `Approved` field | the measurement and the reference set beside it. The policy engine decides |
| one call that detects, checks quality, runs PAD and the challenge | separate calls, each with its own result; detection is arandu-face's |
| a deepfake check that quietly returns nothing | `DeepfakeSignal{NotAvailable, Reason}` |
| a blink or smile challenge on five landmarks | `Supported: false` with the reason |
| a colour or flash reflection challenge | nothing: the technique is the subject of third-party patents |
| a model file in the repository | its manifest; the file is fetched by `cmd/liveness-models` |
| a download in the library or a test | `cmd/liveness-models`, run once where the application is deployed |
| loading a model by path alone | `Runtime.Open(manifest, ModelsDir)` |
| a route that reads a selfie | nothing. Captures are measured through the application's controllers |
| OpenCV, a Python reference, a photograph or attack sample as fixture | the Go implementations and images drawn in Go |

## Writing code

Everything in the source is in English: identifiers, doc comments, internal
comments, error messages, log messages, and the names and messages of tests.

Every exported symbol carries a doc comment, and the comment documents the
symbol and nothing else. A date, an issue number, a version in progress or the
name of another repository does not belong there.

Tests go under `tests/`, in a capitalized category directory declaring a
lowercase external package: `tests/Unit` holds `package unit_test`,
`tests/Feature` holds `package feature_test`. Both import the package by its
module path, which is what makes them see exactly what a caller sees.
