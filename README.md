# Evaris

A standalone, open-source Agent & Skill Evaluation Framework written in Go.

Evaris aims to evaluate engineering outcomes through reproducible experiments,
deterministic checks, independent review, and transparent evidence.

## Status

The Go executable supports experiment validation, blind preparation, and external-result import:

```sh
go run ./cmd/evaris validate --experiment testdata/valid/experiment.json
```

Preparation and import are implemented with frozen inputs and integrity checks.
P0 also provides deterministic grading, immutable evaluations, JSON/Markdown
reports, and an offline control demonstration. CI is configured for Linux and
macOS; the remote matrix has not been run in this session. Comparison and
Invaris integration remain future work. See [workflow](docs/workflow.md) and
[configuration status](docs/configuration.md).

## Build and verify P0

```sh
mkdir -p .panda-suite/build
go build -o .panda-suite/build/evaris ./cmd/evaris
.panda-suite/build/evaris --help
go test -race ./...
go vet ./...
```

The five commands are `validate`, `prepare`, `import`, `evaluate`, and `report`.
`evaluate --store DIR --run ID` publishes a fresh immutable evaluation; add
`--allow-commands` only for trusted command graders. `report --store DIR` creates
JSON and Markdown files under the printed report ID. Assessment exits are
0 for PASS, 1 for a known FAIL, and 2 for incomplete evidence or operation errors.

Run the [offline controls](examples/offline/README.md) to observe both ordinary
suites pass while the independent acceptance grader rejects the bad control.
No model API is needed; synthetic controls are not measurements of agent quality.

## Intended scope

The first release is intended to validate experiment definitions, prepare separate
case workspaces, import real results from externally run agents, apply deterministic
graders, and write JSON and Markdown reports. It should work offline without an
LLM API, using JSON configuration and filesystem artifacts.

Later work may add baseline comparisons, regression reporting, and optional
Invaris CLI integration. Panda Suite is a prospective consumer, not a required
dependency. An Invaris verification verdict must remain separate from the Evaris
assessment of whether an agent completed its task.

## Architecture

Use a small Go CLI with hexagonal boundaries and the standard library first.
The following paths describe ownership in the current implementation:

| Area | Path | Responsibility |
| --- | --- | --- |
| Composition | `cmd/evaris/` | Wire dependencies and translate process exit status |
| Domain | `internal/domain/` | Experiment, case, preparation, and run records |
| Application | `internal/application/` | Coordinate validation, preparation, and import |
| Adapters | `internal/adapters/` | CLI input, strict JSON, snapshots, and filesystem evidence |

Dependencies point inward: adapters depend on application and domain code;
application code depends on domain code. Domain code must not depend on CLI,
filesystem, or process execution details. Define small ports at the consuming
boundary when a real external operation requires one; avoid speculative plugin
interfaces and empty packages. Keep wiring in the composition entry point.

Ordinary workspace directories are not security sandboxes. Imported agent claims
are evidence to verify, not proof of success. Keep reviewer oracles out of agent
workspaces and distinguish infrastructure errors from agent task failures.

## Development

Use Go 1.27.4 or newer, preferably the latest stable patch of a supported release.
The minimum is recorded in `go.mod`; no third-party dependencies are declared.
See the official [Go downloads](https://go.dev/dl/) and
[module reference](https://go.dev/doc/modules/gomod-ref).

From the repository root, inspect the module:

```sh
go version
go mod edit -json
go mod verify
```

Run `go build ./...`, `go vet ./...`, and `go test -race ./...` to check the
current implementation. Passing these checks does not establish complete MVP
acceptance; see [P0 verification status](docs/p0-readiness.md).

## Local artifacts

The default example writes private evidence to `testdata/valid/evidence/`.
`.gitignore` excludes that directory, root `evidence/`, `tasks/`, `.evaris/`,
`bin/`, `dist/`, coverage output, local environment files, and `.panda-suite/`.
Source, original fixtures, reviewer control files, documentation, and branding
assets remain versioned. Configuration examples such as `.env.example` stay trackable.

If you configure another evidence or workspace location inside this repository,
add its exact path to `.gitignore` before running evaluations. A custom outputRoot
is not automatically ignored. Keep real responses, transcripts, and private grader
oracles out of commits; the checked-in reviewer fixture is synthetic test data.

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution expectations and
[SECURITY.md](SECURITY.md) for vulnerability reporting.

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
