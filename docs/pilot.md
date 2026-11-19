# Running a real external-agent evaluation

This runbook strings together the commands in [workflow.md](workflow.md) for an
evaluation of a real agent. Evaris never starts the agent: you run it, then
import what you actually observed. Bundled controls are synthetic and measure
nothing about an agent. Commands below run from the repository root.

## 0. Build and choose locations

```sh
mkdir -p .panda-suite/build
go build -o .panda-suite/build/evaris ./cmd/evaris
.panda-suite/build/evaris --version
```

Pick one private root outside the repository (`PILOT_ROOT`, an absolute path)
for the experiment, evidence stores, task workspaces and import requests. Do not
commit any of it. A store is the experiment's `outputRoot` resolved against the
experiment file, so use one copy of the experiment directory per independent trial,
for example `$PILOT_ROOT/trial-1/` holding `experiment.json`, its fixtures, tasks
and oracles, with the store at `trial-1/evidence` and task workspaces at
`trial-1/ws`. The tasks root must be a new directory, not an existing one such as
`tasks/`. For a store with multiple preparations, pass both preparation IDs to `compare` using the [explicit pairing flags](comparison.md).
Below, `STORE` means `$PILOT_ROOT/trial-1/evidence`.

## 1. Freeze before running

Before the first `prepare`, write down and keep (privately, with file hashes):
task, fixture, reviewer oracle and any reviewer scripts or hidden tests, the
instruction treatment, model and host settings, tools, permissions, budget,
number of trials and exclusion rules. Decide the number of trials before seeing
any outcome. Keep reviewer oracles and hidden tests out of the fixture and of
every agent workspace. Evaris freezes the experiment, task, oracle and
instruction bytes. List the files a command criterion reads (scripts, hidden
tests) in the experiment's `reviewerFiles`: `prepare` records their SHA-256 and
`evaluate` refuses to grade if any changed (see
[configuration](configuration.md#reviewer-files)).

```sh
.panda-suite/build/evaris validate --experiment "$PILOT_ROOT/experiment/experiment.json"
```

`outputRoot` in the experiment resolves relative to the experiment file, so use a
copy per trial store or set it deliberately.

## 2. Prepare

```sh
.panda-suite/build/evaris prepare --experiment "$PILOT_ROOT/experiment/experiment.json" \
  --tasks-root "$PILOT_ROOT/trial-1/ws"
```

The tasks root must be new and its parent must exist without symlinks. Save the
printed record: you need `id` (preparation ID), `configDigest`, each task's
`fixtureDigest` and `workspace`.

## 3. Run each arm externally

For every case and arm, in a fresh agent session whose working directory is only
that workspace (`<tasks-root>/<preparation id>/<case>/<arm>`):

- Give the agent the contents of `TASK.txt`. The specialist workspace also holds
  `.evaris-instructions/`; tell the host to use it only if that is your protocol.
- Isolate the host from your own global instructions, memory and skills, or
  record truthfully what it loaded. A baseline that loads any instruction makes
  the arms not comparable.
- Do not let the agent read the evidence store, experiment directory, reviewer
  files or another arm's workspace. Directory separation is not a sandbox; use
  host permissions.
- Save the agent's final response verbatim to a file, keep the workspace exactly
  as it ended, and export the transcript if the host provides one.
- Do not rerun a failed arm inside the same trial. Log crashes, timeouts and
  deviations; failed and incomplete runs stay in the evidence.

Before the first trial, run a smoke session that attempts every action your
protocol allows or forbids (edit and create inside the workspace, write and read
outside it, permitted and forbidden shell commands) and check the files on disk.
Lessons from the v0.2.0 pilot with the Claude Code CLI (`claude -p`), version
2.1.251:

- `--permission-mode dontAsk` denies every tool that is not pre-allowed, including
  Edit and Write; a run then ends with an untouched tree. `acceptEdits` together
  with `--restricted` allowed edits inside the working directory only.
- `--restricted` also allows the host's per-session scratchpad, which is outside
  the workspace (under `/tmp` by default). Set `CLAUDE_CODE_TMPDIR` to an
  arm-private directory and declare it in the protocol, or arms that write scratch
  files will breach a "workspace only" exclusion rule.
- `--output-format stream-json --verbose` gives an init event with the model,
  tools, permission mode, version, session ID, skills, plugins and memory paths:
  together with the wrapper's recorded budget and start/finish times, enough to
  fill the `observed` fields from evidence (`loadedInstructions` comes from later
  tool calls, not the init event).
- Telling the specialist to read `.evaris-instructions/` does not guarantee it
  does. Under `acceptEdits` the host also ran read-only shell commands such as
  `cat`, and specialists often read the instruction file that way. Decide before
  the first trial which tool calls count as loading (for example any call whose
  result contains the file), derive `loadedInstructions` from the transcript by
  that rule, and expect Evaris to report a mismatch as not comparable.
- Code the agent runs through permitted commands (for example `go test`) is not
  confined by the host's file-tool restrictions.

## 4. Import

Write an import request outside every agent workspace (schema in
[workflow.md](workflow.md)). Fill `observed` only from evidence such as host
version output, session ID, transcript and timestamps. Use `null` for anything
you did not observe and `[]` for an observed empty list. Take
`loadedInstructions` from the transcript, never from what was prepared. Use
`origin: external-agent`.

```sh
.panda-suite/build/evaris import --store "$STORE" --record "$PILOT_ROOT/imports/trial-1-baseline.json"
```

For Claude Code, set the import request's
[`claudeTranscript`](workflow.md#import-claude-code-stream-metadata) field
to extract observed host details and confirm instruction reads from the stream.
Keep the launch settings and record how the baseline supplied an observed
empty instruction list.

The output's `id` is the run ID (also the directory name under `STORE/runs/`; its `run.json` names the case and arm). Duplicate imports for one preparation, case and
arm are refused (exit 2).

## 5. Evaluate, report, compare

```sh
.panda-suite/build/evaris evaluate --store "$STORE" --run RUN_ID --allow-commands
.panda-suite/build/evaris report   --store "$STORE"
.panda-suite/build/evaris compare  --left "$STORE" --right "$STORE" --mode arms
```

`compare --left X --right X` with the same store on both sides is the arms mode:
it pairs baseline and specialist from that store's single preparation. Evaluating a
run again creates a new evaluation and keeps the old one; the report uses the newest
per run and `compare` lists older ones as re-gradings, not independent trials.

`--allow-commands` runs trusted reviewer commands unsandboxed; omit it and
required command criteria stay UNAVAILABLE. Exit codes: `evaluate`/`report` 0 =
PASS, 1 = known requirement failure, 2 = incomplete evidence or error;
`compare` 0 = comparable with no regression, 1 = comparable with a regression,
2 = not comparable, unknown transition or error. With any null observed field,
`compare` exits 2 and lists the reasons; that is a legitimate outcome, not a bug.
A tool error (ERROR/UNAVAILABLE) is not an agent failure; report them separately.

## 6. Reproduce and publish

A second operator should receive the stores, the reviewer files and this
runbook. `evaluate` checks the reviewer files against the digests recorded at
preparation and stops on a mismatch; for a store prepared without
`reviewerFiles`, verify recorded SHA-256 values by hand first (for example
`shasum -a 256 -c MANIFEST`). An oracle that names
reviewer files by absolute path does not relocate: a copied store still grades with the files at that path, so
verify the hashes there. Then re-run `evaluate` and `report` and
compare per-criterion outcomes (IDs differ because every evaluation is new).
Publish only a manually reviewed summary: no oracle, hidden tests, transcripts,
secrets or local paths. A handful of trials supports descriptive statements only,
never a claim of effectiveness. See [comparison.md](comparison.md) and
[security.md](security.md).
