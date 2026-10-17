---
name: liveness-package
description: Install, wire and use the Arandu Liveness package (Go, Arandu) in an application. Use when the request is to "install arandu-liveness", "run passive PAD", "check a selfie for spoofing", "issue a head-turn challenge", "evaluate a challenge", "detect replayed frames", "measure injection signals", "deepfake detection", "go get github.com/hyz-is/arandu-liveness", "wire it into bootstrap/app.go", "download the liveness models", "the boot fails opening minifasnet_v2", "dev-only model refused", "model digest mismatch", "blink challenge unsupported", or when a project's go.mod already requires github.com/hyz-is/arandu-liveness. Covers the build requirement, the models directory, the wiring and where it goes, the Config fields, each stage's result, and what each error means.
license: MIT
---

# Using Arandu Liveness

A module that measures passive PAD with ONNX models on `arandu-onnx`'s
runtime, and challenge and capture-integrity signals in pure Go. It returns
separate signals — PAD, challenge, injection, and the explicit absence of a
deepfake signal — each with the model behind it when there is one, and
decides nothing. It is registered by hand — there is no service provider, no
container and no discovery.

## Build requirement

`CGO_ENABLED=1`, a C compiler, and the ONNX Runtime shared library (1.26 or
later) on the machine that runs the application, when passive PAD is enabled,
as `arandu-onnx` requires.

## Install and fetch the models

```bash
go get github.com/hyz-is/arandu-liveness@v0.1.0
go run github.com/hyz-is/arandu-liveness/cmd/liveness-models@v0.1.0 -dir /srv/models
```

The command downloads every model over HTTPS from its pinned source and keeps
a file only when its SHA-256 matches; `-check` verifies without connecting,
`-only` takes ids. Run it where the application is deployed or when its image is
built. The library itself never downloads.

## The wiring, and where it goes

All of it is in `bootstrap/app.go`, after the `arandu-onnx` and `arandu-face`
modules.

```go
import (
	onnx "github.com/hyz-is/arandu-onnx"
	liveness "github.com/hyz-is/arandu-liveness"
)
```

In `Build`, before `k.Register`, with every value from the application's typed
configuration (the package reads no environment variable):

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

Inside the `k.Register(...)` call, after `faceModule`:

```go
		livenessModule,
```

## The stages

```go
faces, err := faceModule.Detect(ctx, img)                     // arandu-face: the box and landmarks
pad, err := livenessModule.PassivePAD(ctx, img, faces[0].Box)  // PerModel, Fused*, CaptureAspect, ReferenceSet
ch, err := liveness.NewChallenge(liveness.ChallengeTurnLeft, now, ttl, nil)
res, err := livenessModule.EvaluateChallenge(ch, response)     // Observed, Completed, Timing, Nonce, Checks
sig, err := livenessModule.MeasureInjectionSignals(capture)    // Nonce, Timing, Duplicates, Geometry, Metadata, Checks
df, err := livenessModule.Deepfake(ctx, img)                   // NotAvailable, with the reason

multi, err := livenessModule.PassivePADFrames(ctx, padFrames)  // per-frame PADResult or skip reason, FusedReal stats
seq, err := liveness.NewChallengeSequence(nil, 3, now, ttl, nil) // 2-4 kinds, no repeat, per-step nonce and deadline
sr, err := livenessModule.EvaluateChallengeSequence(seq, seqResp) // Steps, ObservedOrder, OrderInversions, CompletedSteps, Checks
sc, err := livenessModule.MeasureScreenReplay(img, faceRegion)  // experimental, uncalibrated: PeriodicPeakRatio, SpecularHighlightRatio
```

Store the whole `ChallengeSequence` on the server; send the client each
step's `Kind` and `Nonce`, never `seq.Nonce`. A step response is matched to
its step by the step nonce it echoes, not by its position.

Record each result's `ModelInfo` and reference version beside the signal as
evidence, and the `DeepfakeSignal` too: its absence is evidence. The policy
that turns signals into a decision lives in the application. Frames passed to
EvaluateChallenge carry the landmarks of the **unmirrored** image and
timestamps in the server's time base.

## Symptoms to recognise

- **`dev-only model refused`.** `RefuseDevOnly` is set and `EnablePassive`
  too; both PAD models are dev-only. That is the production switch working.
- **Boot fails opening `minifasnet_v2` with "no such file".** The models
  directory is not filled, or `ModelsDir` points elsewhere. Run the command
  with `-check`.
- **`model digest mismatch`.** The file is not the reviewed one. Fetch it again
  with the command; never edit a digest.
- **`stage not enabled`.** PassivePAD without `EnablePassive`.
- **`Supported: false`.** A blink, smile or open-mouth challenge: five-point
  landmarks cannot measure it. Issue a turn, a tilt or a nod.
- **A turn reported the wrong way.** The frames were mirrored (a front-camera
  preview); pass the unmirrored image's landmarks.
- **`ONNX Runtime library unavailable`.** See `arandu-onnx`.
