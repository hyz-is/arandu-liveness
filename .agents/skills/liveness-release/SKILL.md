---
name: liveness-release
description: Get this Arandu package green, declared and published. Use when the request is to "run the gates", "check it builds", "the tests skip", "the tests fail", "go build says the directory prefix does not contain modules listed in go.work", "update arandu.mod.toml", "add a dependency", "bump the framework", "remove the replace", "tag a release", "publish it", "write the changelog", or before opening a pull request. Covers the four gates, the models the tests need, what the manifest declares and who checks it, the replaces of arandu-onnx and arandu-face, and the immutability of a published module version.
license: MIT
---

# Getting it green and out

## The four gates

```sh
export GOWORK=off CGO_ENABLED=1
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go')
go build ./...
go vet ./...
go test -race ./...
```

`GOWORK=off` because this checkout may sit beside a workspace that does not
list it. `CGO_ENABLED=1` because arandu-onnx's binding is cgo. The model tests
need ONNX Runtime and the models (from `KUAA_MODELS_DIR`, or the KUAA
workspace's `models/`); they skip, saying why, without either. A run where
they skipped has not tested the models: read the skip lines before calling it
green.

`aru doctor` is not a gate: it reads applications, and this is a library.

## What the manifest declares, and who reads it

```toml
name = "hyz-is/liveness"
framework = ">= 0.50"
profiles = ["conventional", "performance"]

[permissions]
network = false
filesystem = true
exec = false
migrations = false
```

`tests/Unit/audit_test.go` compares it with what the code calls. `filesystem`
is the model files, read through arandu-onnx's `Runtime.Open`, which the audit
counts as a read. `network = false` holds because `cmd/liveness-models`, the
download command, is a `main` package no application links;
`TestTheDownloadCommandIsWhereTheNetworkIs` holds that it is the only place a
network call is written.

The `framework` line is a floor, and `go.mod` says it too. Move both together.

## The replaces

`go.mod` replaces `github.com/hyz-is/arandu-onnx` and
`github.com/hyz-is/arandu-face` with `../arandu-onnx` and `../arandu-face`
while those modules are unpublished. Before the first tag here: tag both,
require them at those versions, delete the replaces and their comment, and run
the gates from a checkout without the siblings. The release workflow refuses a
local replace.

## Dependencies

The framework, arandu-onnx and arandu-face (for its Box, Point and Face types
and its RawPlanes), nothing else. A new `require` needs an argument
in the pull request and, if it reaches the network, the filesystem or another
process, a manifest change in the same commit.

## Publishing

A published Go module version is **immutable**. Before tagging: the gates pass
with the model tests run rather than skipped, `arandu.mod.toml` matches the
code, `CHANGELOG.md` has the entry under the version, and `UPGRADE.md` names
every incompatible change. A changed default (a crop scale,
the PAD models fused, a reference set's values) changes every application that
never set it: it is a changelog line and a new threshold version. A manifest's
digest, revision or status changing is a model changing, and is one too.
