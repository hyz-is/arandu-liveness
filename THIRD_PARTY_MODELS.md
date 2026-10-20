# Third-party models

The model weights this package runs are **not** in this repository and are not
distributed with it. `cmd/liveness-models` downloads each file from the source
its manifest records and keeps it only when its SHA-256 matches. Whoever
downloads and deploys them accepts their licence. The table below is what was
read before each status was set; it is not legal advice.

| id | source URL | sha256 | code licence | model licence | commercial use | redistribution | status |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `minifasnet_v2` | <https://github.com/yakhyo/face-anti-spoofing/releases/download/weights/MiniFASNetV2.onnx> | `b32929adc2d9c34b9486f8c4c7bc97c1b69bc0ea9befefc380e4faae4e463907` | Apache-2.0 | Apache-2.0 | yes | allowed under Apache-2.0, with its licence and notice | approved |
| `minifasnet_v1se` | <https://github.com/yakhyo/face-anti-spoofing/releases/download/weights/MiniFASNetV1SE.onnx> | `ebab7f90c7833fbccd46d3a555410e78d969db5438e169b6524be444862b3676` | Apache-2.0 | Apache-2.0 | yes | allowed under Apache-2.0, with its licence and notice | approved |

Both are third-party ONNX exports of the weights of
[minivision-ai/Silent-Face-Anti-Spoofing](https://github.com/minivision-ai/Silent-Face-Anti-Spoofing).
The release tag `weights` they are downloaded from is mutable; the SHA-256,
which equals the release asset's published digest, is what pins each file, and
a file that no longer matches is refused.

## Training data, evaluation and open questions

- The training data is undisclosed upstream.
- No ISO/IEC 30107-3 evaluation, iBeta or other laboratory report, or
  measurement on consented attack data is known. APCER and BPCER are unknown.
- What output classes 0 and 2 stand for (print, replay, mask) is not
  documented upstream and is unverified here.

Both were `dev-only` for these reasons until v0.3.0.

## The open-source decision (2026-10-19)

Both models moved from `dev-only` to `approved` on 2026-10-19, by the
maintainer's decision (ADR-0010 of KUAA, the application these engines were
built for): weights under a permissive open-source licence (Apache-2.0, MIT or
BSD) are eligible for production, and no paid licence is used. The decision
resolves none of the questions above. The undisclosed training data and its
consent are a risk the maintainer accepted, recorded in each
`ModelCard.Review`. The missing ISO/IEC 30107-3 evaluation still stands in
each `ModelCard.Evaluation`: `approved` is a statement about the licence and
the provenance, not about how well the models detect attacks, which has to be
measured on consented bona fide and attack data before a PAD signal weighs on
a decision. Non-commercial weights and models trained on CelebA-Spoof stay
excluded, and a unit test refuses any approved manifest whose
`commercial_use` is not `yes`.

A `dev-only` model loads and says so in every `ModelInfo.Status`; with
`Config.RefuseDevOnly` set, `Boot` refuses it before reading a file. No model
this version ships is `dev-only`.

No deepfake or face-swap detector is shipped: none with an acceptable licence,
disclosed and consented training data and pinned provenance has been reviewed.

## Changing a model

A new model or a new revision is a new manifest under `manifests/` and a model
card in `models.go`, with the source pinned, the digest of the exact file, and
the licence fields filled from what the upstream says. It starts as `dev-only`
until its licence and provenance have been reviewed, and moving it to
`approved` is a changelog line and a `ModelCard.Review`. Models that infer race, ethnicity or
gender, and models trained on CelebA-Spoof, are refused by a unit test.
