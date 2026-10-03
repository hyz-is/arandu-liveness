---
name: liveness-vault-notes
description: Produce the KUAA vault notes that record this module, when the checkout sits inside the KUAA workspace. Use when the request is to "write the note", "record this in the vault", "update the journal", "open a gap", "document the module", "which folder does this note go in", "the doc audit is failing", or when a change to this package closed or opened a defect somebody else has to know about. Covers how to tell you are in the workspace, who writes, where a module note lives, the frontmatter, and the instrument that checks it.
license: MIT
---

# Recording this module in the KUAA vault

## First: are you in the workspace?

This applies only when this checkout sits at `modules/arandu-liveness` inside the
KUAA workspace:

```sh
ls ../../MOC-kuaa.md ../../45-modules/ ../../plans/cmd/check-docs/ 2>/dev/null
```

If those are absent, stop. Do not create a vault or imitate one inside this
repository.

## Second: are you the one who writes?

**Workers do not write vault notes.** The main session holds
`lock:orchestration` and is the only writer of the vault and `plans/`. A worker
produces the note's **content**, names the file it belongs in, and hands it
over in its handoff.

## Where a note about this module goes

`45-modules/MOD-arandu-liveness.md`, and nowhere else. A defect whose fix lands in
this repository is a `GAP-` note in `70-gaps/` with this module's layer; one
whose fix lands in `arandu-onnx`, the framework or the app carries that layer
instead. A defect's layer is where its fix has to happen. Search `70-gaps/`
for an open gap on PAD evaluation or model licences before opening another.

The note answers, in order: what `arandu.mod.toml` declares; how the app wires
it (`liveness.New(cfg, onnxModule.Runtime())`, registered after the ONNX and face modules in
`bootstrap/app.go`); the stages and their results, kept separate (PAD, challenge,
integrity, deepfake absence); the models it runs, with source, digest and
status; what was measured, with the command; and what is open — above all,
that APCER, BPCER and every threshold must be measured on consented bona fide
and attack data under ISO/IEC 30107-3.

## Frontmatter

The twelve keys of `00-meta/DOC-documentation-standard.md`, and no others:

```yaml
id: "MOD-arandu-liveness"
type: "module"
layer: "liveness"
front: "F4-biometric-engine"
status: "tested"
closure: "open"
owner: "orchestration"
evidence: "modules/arandu-liveness@<sha>; <test count> testes -race"
source: "<task id>"
aliases: ["arandu-liveness", "liveness engine"]
updated: "<YYYY-MM-DD>"
tags: ["type/module", "closure/open"]
```

Structure, keys and titles in English; prose in Brazilian Portuguese.
`declared` is not `implemented`, and `implemented` is not `tested`: `evidence`
names something a reader can open or run.

## The check

From the workspace root:

```sh
GOWORK=off go run ./plans/cmd/check-docs/main.go -root .
```

The instrument outranks this file. If they disagree, this file is what needs
correcting.
