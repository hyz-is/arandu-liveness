# Security policy

## Supported versions

The latest minor release of Arandu Liveness receives security fixes. Older
minor releases do not: a published Go module version is immutable, so a fix is
a new release rather than a moved tag.

## Reporting a vulnerability

Report it privately, through GitHub's advisory form:

<https://github.com/kuaa-io/arandu-liveness/security/advisories/new>

Do not open a public issue and do not describe the problem in a pull request.
A report that arrives in public is a report every reader of this repository can
act on before there is a release to upgrade to.

Include what you need to reproduce it: the version, the configuration, and an
image that triggers it -- a synthetic one, never a photograph of a real person.

Expect an acknowledgement within a few days. If the report is confirmed, the
fix, the release and the advisory are published together, and you are credited
unless you ask not to be.

## What is in scope

Anything that lets a model run that its manifest did not admit, or lets an
image cost more than it should. In particular:

- a way to open a model whose file does not match its manifest's SHA-256;
- a way to run a dev-only model while `Config.RefuseDevOnly` is set;
- `cmd/liveness-models` keeping a file whose digest does not match, following
  a redirect off HTTPS, or writing outside the directory it was given;
- an image that crashes the process, reads out of bounds, or costs memory or
  time out of proportion to its size;
- a race that lets a call run on a module that was closed;
- a nonce comparison that leaks timing, or a challenge evaluation that reads
  a capture as bound to a nonce it did not echo;
- a JPEG whose markers make `ReadJPEGMetadata` read out of bounds or loop.

## What is not

- A vulnerability in ONNX Runtime or in the PAD models themselves.
  Report it to those projects.
- What an application decides from the signals. This package returns
  measurements and the models behind them.
- An attack the PAD models, the challenge or the integrity signals do not
  see. Their limits are documented in the README; a bypass is a finding for
  the application's evaluation, not a vulnerability here.
