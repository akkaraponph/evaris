# Preparation and external-result import

Run from the repository root with Go 1.27.4 or newer:

```sh
go build -o /tmp/evaris ./cmd/evaris
/tmp/evaris validate --experiment testdata/valid/experiment.json
/tmp/evaris prepare --experiment testdata/valid/experiment.json --tasks-root /absolute/new/task-root
```

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
Use origin synthetic-control for authored controls, not external-agent. Evaris
cannot attest the truth of host metadata or origin labels.

```sh
/tmp/evaris import --store /absolute/path/to/evidence --record /absolute/path/to/import.json
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
