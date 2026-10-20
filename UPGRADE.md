# Upgrade Guide

Every release that changes what an installer has to write is described here,
newest first. A release with nothing to change still has a heading, so this file
and [CHANGELOG.md](CHANGELOG.md) describe the same set of versions.

## v0.3.0

Nothing in the code has to change.

1. Release order: this version requires `github.com/hyz-is/arandu-face`
   v0.2.0, so arandu-face v0.2.0 is published first; `go get
   github.com/hyz-is/arandu-liveness@v0.3.0` then brings it and
   `github.com/hyz-is/arandu-onnx` v0.3.0. Read arandu-face's v0.2.0 notes:
   with its `EnableAge`, the nodes need `age/mivolo_v2_age_convt.onnx`.
2. The PAD model files are unchanged (same paths and digests); nothing new
   has to be copied to the nodes for this module.
3. Both PAD models are now `approved`. A configuration with `RefuseDevOnly`
   and `EnablePassive`, which v0.2.0 refused with `ErrDevOnlyRefused`, now
   boots. Evidence records show `Status: "approved"`. That status is about the
   licence and provenance: the models still have no ISO/IEC 30107-3
   evaluation, so a policy that read `dev-only` as "do not weigh this signal"
   needs its own rule on the missing evaluation instead.

## v0.2.0

Nothing to change. Every addition is new API and new Config fields (`Sequence`, `MaxPADFrames`, `ScreenReplay`), each with a default; an installer who sets none of them gets the behaviour of v0.1.0. The new reference sets are versioned `arandu-liveness-sequence-0.1-uncalibrated` and `arandu-liveness-screen-replay-0.1-experimental-uncalibrated`.

## v0.1.0

The first public release. Nothing to upgrade from.

A checkout that imported the module before it was published under its current
path replaces the old import paths with `github.com/hyz-is/arandu-liveness`,
`github.com/hyz-is/arandu-face` and `github.com/hyz-is/arandu-onnx`, and
requires all three at `v0.1.0`. The default reference sets are versioned
`arandu-liveness-challenge-0.1-uncalibrated` and
`arandu-liveness-integrity-0.1-uncalibrated`, and the deepfake reference
`arandu-liveness-deepfake-none-0.1`; a record that compared those strings with
an earlier development name compares them with these. Tests that set the
models directory through an environment variable now set
`LIVENESS_MODELS_DIR`.
