# Preparation and external-result import

For a local project, start with an installed Evaris binary:

```sh
evaris init
# Edit .evaris/experiment.json and its inputs for your evaluation.
evaris validate
evaris prepare
# Run the external agent separately, then author an import request.
evaris import --record /absolute/path/to/import.json
evaris evaluate --run RUN_ID
evaris report
```

Run these commands from the same project directory. Initialization refuses an
existing `.evaris` path. Preparation chooses a new directory under `.evaris/tasks/`
for each invocation and preserves earlier workspaces. Its JSON output identifies
the exact tasksRoot and task workspace paths. Supply `--tasks-root` to use another
new absolute task root. The default evidence store is `.evaris/evidence`.

The entire `.evaris` directory contains private configuration and evidence;
give the external agent only its individual prepared task workspace. Reviewer
oracles and the experiment config are not copied into task workspaces. Keep
`.evaris/` out of version control (this repository already ignores it); copy
reusable examples to a tracked example directory deliberately. Existing experiment
files and explicit paths continue to work without initialization.

## Running the repository examples

Run from the repository root with Go 1.27.4 or newer:

```sh
mkdir -p .panda-suite/build
go build -o .panda-suite/build/evaris ./cmd/evaris
.panda-suite/build/evaris validate --experiment testdata/valid/experiment.json
.panda-suite/build/evaris prepare --experiment testdata/valid/experiment.json --tasks-root /absolute/new/task-root
```

The commands below use this binary path from the repository root; adjust it if
you built elsewhere. For a real external-agent evaluation, follow the
[pilot runbook](pilot.md).

Replace `/absolute/new/task-root` with a new absolute destination whose parent
exists and contains no symlink components. The example's private evidence store
is `testdata/valid/evidence`, as specified by outputRoot in the configuration.
Use a copied experiment for real evaluations; keep private evidence untracked.

Preparation prints a committed preparation record with a random ID and tasks.
Each task gives its relative workspace under tasksRoot, case/arm IDs and fixture
hash. Baseline receives the common TASK.txt; specialist also receives frozen
instruction files. Never give an agent the private store, experiment configuration,
reviewer directory, or import request. Directory separation is not a security
sandbox; use your host's permissions to enforce reviewer confidentiality.

Run the external host in a fresh context per arm, following TASK.txt. Evaris does
not start the agent. Save its actual final response to a file, retain the complete
final workspace, and record only metadata you actually observed.

Create a JSON import request outside agent workspaces. Required fields:

```json
{
  "schemaVersion": "evaris.import/v1alpha1",
  "preparationId": "<preparation id>",
  "caseId": "sample",
  "armId": "baseline",
  "configDigest": "<preparation configDigest>",
  "fixtureDigest": "<task fixtureDigest>",
  "sourceRevision": "initial",
  "finalResponse": "response.txt",
  "finalTree": "/absolute/new/task-root/<preparation id>/sample/baseline",
  "transcripts": [],
  "diff": null,
  "observed": {
    "host": null,
    "model": null,
    "modelSettings": null,
    "tools": null,
    "permissions": null,
    "budget": null,
    "contextId": null,
    "freshContext": null,
    "startedAt": null,
    "finishedAt": null,
    "loadedInstructions": null,
    "agentClaim": null
  },
  "limitations": [],
  "origin": "external-agent"
}
```

Replace placeholders with the actual preparation output. Relative input paths
resolve against the import request directory. All observed keys are required;
null means unavailable, while an observed empty instruction/tool list is [].
`agentClaim` is null or one of `completed`, `not-completed` and `unknown`: what
the agent said about its own work, not whether the work is correct.
`sourceRevision` is the case's starting revision from the experiment file, not
the commit the agent delivered; a mismatch fails with `REVISION_MISMATCH` and
names the expected value. Record the delivered commit in the optional
`deliveredRevision`.

### Import from a Git repository

When the agent worked in a real repository instead of the prepared workspace,
replace `finalTree` with `finalSource`; exactly one of the two is required:

```json
"finalSource": {"kind": "git", "repository": "/absolute/path/to/repo", "revision": "agent-branch"},
"deliveredRevision": "<optional full commit SHA>"
```

Evaris resolves `revision` to a commit with `git rev-parse`, exports that
commit's tree with `git archive`, adds the prepared task's generated files
(TASK.txt and frozen instructions) byte-identical from the prepared workspace,
and imports the result like a final tree. The run (`evaris.run/v1alpha2`)
records the resolved commit as `deliveredRevision`, the `finalSource`, and the
identity of the `git` executable. A supplied `deliveredRevision` must equal the
resolved full SHA. The commit must not contain symbolic links, submodules or
its own copy of a generated file (`PATH_UNSAFE`). Files marked `export-ignore`
in `.gitattributes` are not exported, so they are absent from the imported
tree. The commit's tree should equal the fixture plus the agent's changes, so
that source authorization compares like with like. `git` must be on PATH
(`TOOL_MISSING` otherwise); `GIT_*` variables are not passed to it.
Use origin synthetic-control for authored controls, not external-agent. Evaris
cannot attest the truth of host metadata or origin labels.

```sh
.panda-suite/build/evaris import --store /absolute/path/to/evidence --record /absolute/path/to/import.json
```

Import copies supplied bytes; it never executes imported text/scripts. A forbidden
source edit is retained for later grading rather than hidden by import rejection.
One import is allowed per preparation/case/arm. Retry independent trials with a
new preparation and new task root. Prior runs are immutable; duplicate imports
exit 2 without a new stdout record. Unknown case, stale identities, linked/private
inputs and missing response also fail. Evaluate an imported run and generate reports:

```sh
.panda-suite/build/evaris evaluate --store /absolute/path/to/evidence --run RUN_ID
.panda-suite/build/evaris report --store /absolute/path/to/evidence
```

To read one evaluation criterion by criterion, including a command's retained
stdout and stderr (up to 4096 bytes each, control bytes shown as `\xNN`):

```sh
.panda-suite/build/evaris show --store /absolute/path/to/evidence --evaluation EVALUATION_ID [--criterion ID]
```

`show` prints text, reads only, and refuses an evaluation whose evidence changed.
Use `--criterion verification` for the Invaris verification.

Replace RUN_ID with the committed import output. Evaluation/report exit 1 means a
known requirement failure; exit 2 means incomplete evidence or an operation error.
Required command graders stay UNAVAILABLE unless --allow-commands is supplied.
A pending human review keeps assessment INCONCLUSIVE. Every evaluation and report
gets a fresh ID; repeated grading never overwrites a previous result. The report
selects the newest evaluation per run while retaining the earlier evaluation IDs.
JSON and Markdown live under reports/<report ID>/.

Keep COMMITTED markers and all referenced evidence. Readers check schema,
preparation linkage, manifests and stored bytes. Hashes detect changed bytes but
are not signatures or proof of who ran an agent. Crash leftovers without a marker
are not accepted; inspect before manually removing only the failed operation's
owned directories. No automatic deletion of completed workspaces is performed.

## Optional verification and comparison (P1)

Cases may configure an Invaris check; evaluate them with
`--allow-commands --invaris /abs/invaris`. See [invaris.md](invaris.md).
Without `--invaris`, a configured verification is recorded as `ERROR`/
`TOOL_MISSING`, never silently skipped.

After evaluating both arms of one preparation, compare them:

```sh
.panda-suite/build/evaris compare --left /absolute/path/to/evidence --right /absolute/path/to/evidence --mode arms
```

For two instruction revisions, prepare and evaluate each revision in its own
store, then pass both stores with `--mode revisions`. Comparison requires fully
observed host metadata; null metadata blocks it with explicit reasons. See
[comparison.md](comparison.md).
