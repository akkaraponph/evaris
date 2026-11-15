# P0 implementation and verification

P0 implements five commands: validate, prepare, import, evaluate, and report.
The offline control demonstrates ordinary tests passing for both outputs while
an independent exact-byte requirement rejects the bad output. These are authored
controls, not real model-performance observations.

Verified locally on macOS/arm64 with the selected Go 1.27.4 toolchain:

- Build and vet.
- Race-enabled unit, filesystem, process, renderer and CLI acceptance tests.
- Read-only configuration and oracle validation, duplicate/null/unknown JSON cases.
- Exclusive transactions, write/sync/close faults, missing markers, root replacement,
  copy independence, links and size boundaries.
- Blind preparation, import, evidence/schema/identity tampering and duplicate imports.
- Grader aggregation, source authorization, invalid/panicking graders, command consent,
  timeout/crash/pipe-wait failures, bounded streams, environment isolation and scratch copies.
- JSON/Markdown output, incomplete arms, failed publication, retained evaluations and
  corrupt-evidence refusal.
- Offline application workflow with GOPROXY=off and GOTOOLCHAIN=local after selecting
  the already provisioned Go 1.27.4 binary. This prevents Go module downloads; it
  does not assert an OS network sandbox.
- Linux/amd64 cross-build; this is compilation evidence, not Linux runtime testing.

GitHub Actions is configured for Linux/macOS with a read-only token and pinned
checkout/setup-go revisions. Remote CI has not been run during this work. Linux
runtime/process tests and clean hosted-runner evidence remain pending, so release
readiness is not asserted. No model run, publication, deployment, or push occurred.

Manifest benchmark procedures cover 10/100/1,000 files of 128 bytes each and report
allocations and elapsed time. Results are local measurements without a pass target.
Ignored build and verification artifacts live under .panda-suite/. The local master
plan and execution ledger live under .panda-suite/plans/ and are not distributed.
