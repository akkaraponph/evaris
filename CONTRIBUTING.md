# Contributing to Evaris

Evaris is in early (0.x) development. The CLI provides `validate`, `prepare`,
`import`, `evaluate`, `report` and `compare`, with optional Invaris
verification. Discuss substantial behavior, configuration contracts, or new
dependencies in an issue before implementation.

## Local setup

Clone the repository and use Go 1.27.4 or newer, preferably the latest stable patch
of a supported release. Run these commands from the repository root:

```sh
go version
go mod edit -json
go mod verify
```

No dependency installation or external account is currently required.

## Implementation conventions

- Follow the hexagonal dependency direction described in the README. Keep domain
  rules independent of I/O; wire adapters at the CLI entry point.
- Prefer the standard library. Explain the need, maintenance cost, and license
  of any proposed dependency.
- Introduce packages and interfaces for concrete responsibilities as they arise.
- Keep configuration and evidence contracts explicitly versioned. Preserve the
  distinction between execution state, grader results, and agent task outcomes.
- Never fabricate agent runs, transcripts, model metadata, or verification results.
- Keep reviewer expectations separate from task inputs. Do not commit credentials,
  private run data, or sensitive logs.

## Validation and pull requests

Format changed Go files with `gofmt -w <file.go>` and run:

```sh
test -z "$(gofmt -l cmd internal examples)"
go build ./...
go vet ./...
go test -race ./...
GOTOOLCHAIN=local GOPROXY=off go test ./internal/acceptance -count=1
```

Real-Invaris tests run only with `EVARIS_TEST_INVARIS` set to a binary built
from a tested Invaris release; see [docs/invaris.md](docs/invaris.md). CI builds
`main` only: it runs for pull requests to `main` and pushes to `main`, so run
these checks locally on other branches. Release steps
are in [docs/release.md](docs/release.md).

Add tests for observable behavior and relevant failure cases, including malformed
inputs, path escapes, incomplete evidence, and interrupted writes where applicable.
For documentation-only changes, check links, command accuracy, and consistency
with the actual implementation instead.

A pull request should describe the problem, resulting behavior, verification
commands and results, and any remaining limitations. Update documentation when
behavior or contracts change. Do not report an empty test suite as successful
evaluation of an agent.

## Licensing

Contributions are submitted under the repository's Apache License, Version 2.0,
as described in `LICENSE`. Preserve existing attribution. Before copying source
or fixtures from another project, check its license and retain any required
notices. Add a `NOTICE` file when applicable attribution obligations require it.
