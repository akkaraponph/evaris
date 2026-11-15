# Evaris

Go CLI with hexagonal boundaries. Runtime minimum lives in go.mod. Domain rules
live in internal/domain, lifecycle orchestration in internal/application, and I/O
in internal/adapters. cmd/evaris wires concrete adapters. No third-party modules.

Run from the repository root:

```sh
go build ./...
go vet ./...
go test -race ./...
GOTOOLCHAIN=local GOPROXY=off go test ./internal/acceptance -count=1
```

Format Go changes with gofmt. Add discriminating tests for behavior changes.
Keep reviewer oracles out of public fixtures and task workspaces. Preserve exact
input/evidence hashes, unknown metadata, and explicit failure states. A passing
supplied suite does not establish all task requirements. Synthetic controls are
not real model measurements. No automatic agent execution or Invaris integration
exists in P0. Trusted command grading requires --allow-commands and is not sandboxed.

Track source, original examples and durable docs. Keep private runs/evidence,
secrets and temporary .panda-suite artifacts out of commits. Custom output locations
need exact ignore rules. Do not introduce a missing RTK.md import.
