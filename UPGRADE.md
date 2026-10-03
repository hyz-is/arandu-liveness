# Upgrade Guide

Every release that changes what an installer has to write is described here,
newest first. A release with nothing to change still has a heading, so this file
and [CHANGELOG.md](CHANGELOG.md) describe the same set of versions.

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
