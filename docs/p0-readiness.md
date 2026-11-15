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

## Not yet established

- GitHub Actions, including the `invaris` job, has not run on hosted runners.
- No real agent has been evaluated. All runs are synthetic controls.
- Compatibility with Invaris revisions other than the pinned one.

Release steps and the support matrix are in [release.md](release.md). Release
readiness is not asserted until hosted CI passes on the release commit. No publication, deployment, or push occurred.

Manifest benchmarks cover 10/100/1,000 files of 128 bytes each and report
allocations and elapsed time. Results are local measurements without a pass target.
Ignored build and verification artifacts live under `.panda-suite/`.
