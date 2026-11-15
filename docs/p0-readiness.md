# Implementation and verification status

P0 implements five commands: validate, prepare, import, evaluate, and report.
P1 adds `compare` and optional Invaris verification. The offline controls show
ordinary tests passing for both outputs while an independent exact-byte
requirement rejects the bad output. These are authored controls, not real
model-performance observations.

## Verified locally (Go 1.27.4)

On macOS (darwin/arm64) and in a Linux container (golang:1.27.4, linux/arm64,
network disabled):

- gofmt, build, vet, and race-enabled unit, filesystem, process, renderer, CLI
  and acceptance tests.
- Configuration and oracle validation; duplicate, null and unknown JSON cases.
- Exclusive transactions, write/sync/close faults, missing markers, copy
  independence, links and size boundaries.
- Blind preparation, import, tampering and duplicate-import refusal.
- Grader aggregation, source authorization, command consent, timeout, crash,
  bounded streams, environment isolation and scratch copies.
- JSON/Markdown reports, incomplete arms, failed publication, retained
  evaluations, corrupt evidence, interrupted grading.
- Invaris evidence contract: strict decoding, harness profile, and ACCEPT,
  REJECT, REFUSE, timeout, cancellation, output limit, malformed output and
  missing executable handling.
- Comparison: matched arms, each changed or unobserved condition, contaminated
  baseline, missing arm, ambiguous trials, re-evaluations, revision regression
  and changed rubric.
- Real Invaris built from source `ee92f3c00c1b59b2f576bad05569fb6eafb83097`:
  ACCEPT, REJECT and REFUSE contract test and the full P1 offline journey passed
  on both platforms.

The offline phase uses `GOPROXY=off` and `GOTOOLCHAIN=local`, so no module is
downloaded; the Linux container also had networking disabled.

Linux/amd64 was cross-compiled only, without an amd64 runtime test. The
container ran as root, which can hide permission-dependent failures.

## Hosted release verification

The [Go checks workflow](https://github.com/akkaraponph/evaris/actions/workflows/ci.yml)
runs build, vet, race tests, offline acceptance, and pinned real-Invaris
integration on Linux and macOS. Verify the run's commit SHA against the release
tag; local passes and runs from older revisions do not satisfy the release gate.

The initial release candidate `a794cb7` passed Linux verification and real-Invaris
integration on both hosted platforms. Its macOS race run exposed an output-limit
control reaching its one-second deadline before emitting 4 MiB. The control now
uses a separate five-second budget and checks the exact retained byte bound;
the timeout controls retain their one-second deadlines.

## Not yet established

- No agent or instruction-file effectiveness. The bundled offline runs are
  synthetic controls. The v0.2.0 pilot evaluated real Claude Code CLI sessions
  with observed host metadata. Earlier revisions hit a ceiling or excluded arms
  for host-configuration reasons; in the final revision one case separated the
  arms descriptively, but every comparison was NOT_COMPARABLE because the
  preregistered rule counted only one way of reading the instruction file. Its
  results are descriptive only.
- Reproduction of pilot grading by a second person. An independent AI operator
  session reproduced all 12 pilot evaluations and 3 comparisons from the
  retained evidence.
- Compatibility with Invaris revisions other than the pinned one.

Release steps and the support matrix are in [release.md](release.md). Release
readiness requires hosted CI to pass on the exact release commit. Published
versions and their checksums are listed on the repository's
[releases page](https://github.com/akkaraponph/evaris/releases).

Manifest benchmarks cover 10/100/1,000 files of 128 bytes each and report
allocations and elapsed time. Results are local measurements without a pass target.
Ignored build and verification artifacts live under `.panda-suite/`.
