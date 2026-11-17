# Experiment configuration

Initialize a local workspace in the current project directory:

```sh
evaris init
evaris validate
```

`init` creates `.evaris/experiment.json`, a sample fixture under `fixtures/`,
`task.txt`, specialist instructions under `instructions/`, a private oracle under
`reviewer/`, and an empty `tasks/` directory. The sample is an editable starting
point, not a real agent measurement. Existing `.evaris` paths are never overwritten.
Preparation creates `evidence/` as a committed store; initialization deliberately
does not create an empty evidence store.

`validate` and `prepare` default to `.evaris/experiment.json`. `import`,
`evaluate`, and `report` default to `.evaris/evidence`. Defaults are relative to
the current working directory; commands do not search parent directories.
Explicit `--experiment`, `--store`, and `--tasks-root` flags retain their existing
behavior. If you change `outputRoot`, supply the corresponding absolute `--store`
for later commands. `compare` still requires explicit `--left` and `--right` stores.

All configuration paths resolve relative to the experiment file, including
fixtures, task text, instructions, private oracles, and `outputRoot`. There is no
automatic migration of existing experiments. Validation remains read-only and
never creates a workspace.

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
harness, `required` and `timeoutSeconds` (1–300), and optionally `"action":
"campaign"` with the campaign's harness `files`. Validation rejects a harness
inside a fixture or the store, a fixture containing `invaris.json`, and `check`
harnesses outside the supported profile. See [invaris.md](invaris.md).

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

A command criterion can also declare `dependencies`, directories such as
`node_modules` that the scratch copy needs but that are not part of the
imported tree. Each entry has a single-segment `name` and an absolute `source`
directory outside the fixture and the evidence store, at most 16 entries. The
grader links each one as `<name>` in the parent of the scratch copy, where
resolvers that walk up the tree (Node) find it, so it never appears in the
copy's manifests. The source must exist when the criterion is graded;
otherwise the criterion is `ERROR` with `TOOL_MISSING`. Its contents are
neither copied nor hashed, so two evaluations can see different dependency
versions, and a command can write through the link. The evidence records each
source path.

```json
"dependencies": [{"name": "node_modules", "source": "/home/me/app/node_modules"}]
```

When a command criterion ends in `ERROR`, its `message` in `evaluation.json`
says which step failed and how, for example `scratch snapshot after the command
(command exited 0): LIMIT_EXCEEDED (entries); list generated output such as
.next in snapshotExclude`. The reason is the specific code (`LIMIT_EXCEEDED`,
`PATH_UNSAFE`, `TIMEOUT`), not a generic `IO_ERROR`.

Messages come from a fixed catalogue: the failing step, the command's exit
code, the reason code, the kind of limit and a fixed hint. They never contain
paths, command text, output or oracle values, so they cannot expose
reviewer-private content and are the same each time a run is evaluated. Any
other grader text is cleared before the evaluation is stored. `report.md` does
not show messages yet; read them from `evaluation.json`.

## Allowed and protected paths

Each case lists `allowedChanges`: the paths the agent may add, change or
delete. Every entry is a pattern, so a job that creates many files does not
have to list each one:

- a plain path such as `result.txt` matches only that path;
- `*`, `?` and `[...]` match within one path segment, as in Go's `path.Match`
  (`src/*.ts` matches `src/a.ts`, not `src/x/a.ts`);
- a whole `**` segment matches zero or more segments (`app/**` matches
  everything under `app`, and `**/.env` matches `.env` at any depth).

The optional `protectedPaths` uses the same syntax. A change there always fails
source authorization, even when `allowedChanges` matches it, which turns
`allowedChanges: ["**"]` into a deny-list. Generated files (`TASK.txt` and the
frozen instructions) stay protected whatever the patterns say. When a case has
`verification`, no `allowedChanges` pattern may match `invaris.json`. Each list
holds at most 256 entries; a malformed pattern is `PATH_UNSAFE`.

```json
"allowedChanges": ["app/**", "package.json"],
"protectedPaths": ["**/.env", "app/auth/**"]
```

## Limits

Snapshots of fixtures, imported trees and command scratch copies walk at most
2000 entries by default. Set `limits.maxEntries` on the experiment (1 to
200000) to raise that bound for a Node-sized project:

```json
"limits": {"maxEntries": 20000}
```

The per-file (10 MiB) and per-tree (100 MiB) bounds are unchanged. Stored
records are re-read with the 200000 ceiling, because their contents are pinned
by recorded manifests.

## Reviewer files

List the files graders rely on but that Evaris does not otherwise freeze, such
as scripts a `command-exit` criterion runs or hidden tests it reads, in the
experiment's optional `reviewerFiles` (paths relative to the experiment file,
at most 64):

```json
"reviewerFiles": ["reviewer/check-routes.sh", "reviewer/hidden.test.js"]
```

`prepare` records each file's absolute path and SHA-256 in the preparation.
Each must be a regular file of at most 10 MiB outside every fixture, the
evidence store and the tasks root. Before grading, `evaluate` hashes them again
and refuses with `EVIDENCE_CORRUPT` (`reviewer file … changed since
preparation`) if any changed, disappeared or is no longer a regular file; no
evaluation is written. Restore the file, or prepare again, to continue.

## Acceptance mode

By default an experiment compares a `baseline` arm (no instructions) with a
`specialist` arm. To check one configuration against the oracle without a
comparison, set `"mode": "acceptance"` and declare exactly one arm, with any
valid ID and any instructions:

```json
"mode": "acceptance",
"arms": [{"id": "candidate", "instructions": [{"path": "instruction.txt", "revision": "initial"}]}]
```

Reports of an acceptance experiment have `mode: "acceptance"`, one row per
case, no comparison and no comparison limitation, and report.md says
`Not applicable: acceptance mode` under Comparison. `evaris compare` refuses an
acceptance store. An omitted `mode` means `comparison`.
