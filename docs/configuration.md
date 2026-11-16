# Experiment configuration

The current executable supports:

```sh
go run ./cmd/evaris validate --experiment testdata/valid/experiment.json
```

The example defines one case and baseline/specialist arms. Paths resolve relative
to the experiment file. Validation is read-only. A valid request prints JSON with
`valid`, `experimentId`, and the SHA-256 of the exact configuration bytes. Errors
use stderr and exit status 2 (the `go run` wrapper itself may return status 1).

JSON rejects duplicate keys, unknown fields, trailing values, omitted required
fields, non-nullable nulls, invalid UTF-8, inputs above 1 MiB and nesting above 32.
The domain requires the experiment and oracle v1alpha1 versions. The implementation plan is maintained locally under
`.panda-suite/plans/evaris-master-plan.md`; it is not distributed in source archives.

Preparation and import are available; see [workflow.md](workflow.md). Configuration validation includes fixture traversal, cross-case private-input
exclusion, criterion IDs, unchanged-file references and command environment/timeout
checks. Directory isolation is not a security sandbox. See the plan for release
gates and verification limitations.

Cases may add an optional `verification` object (P1) naming a private Invaris
harness, `required` and `timeoutSeconds` (1–300). Validation rejects a harness
inside a fixture or the store, a fixture containing `invaris.json`, and harnesses
outside the supported check-only profile. See [invaris.md](invaris.md).

## Command criteria

A `command-exit` criterion runs its command in a scratch copy of the imported
tree and records a manifest of that copy before and after the run. The manifest
walks at most 2000 entries, so a command that builds into the copy (a `.next`
or `dist` directory) can exceed it. List such output in the optional
`snapshotExclude` array of the criterion: unique relative paths, at most 64,
skipped with everything below them in both manifests. Other criterion kinds
refuse the field.

```json
{"id": "credential-scan", "kind": "command-exit", "required": true, "expected": 0,
 "command": {"program": "/usr/local/bin/scan", "args": ["."], "envAllowlist": ["PATH"]},
 "snapshotExclude": [".next"]}
```

When a command criterion ends in `ERROR`, its `message` in `evaluation.json`
says which step failed and how: for example `scratch snapshot after the command
(command exited 0): LIMIT_EXCEEDED: entries: more than 2000 under the snapshot
root`. The reason is the specific code (`LIMIT_EXCEEDED`, `PATH_UNSAFE`,
`TIMEOUT`), not a generic `IO_ERROR`.
