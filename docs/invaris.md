# Optional Invaris verification (P1)

Evaris can run an already installed Invaris CLI as a separate verifier for a
case. Evaris never imports Invaris code, downloads it, or installs it. Without a
configured `verification` block, Evaris performs no lookup and starts no
subprocess; P0 workflows are unchanged.

## Tested compatibility

Evaris runs the Invaris `check` action only. This table is kept identical in
Invaris `docs/ecosystem.md`:

| Evaris | Invaris build | `invaris --version` | Harness / evidence read | Status |
| --- | --- | --- | --- | --- |
| 0.4.0 | `ee92f3c` (pre-v0.2.0) | none (legacy) | `invaris.dev/v1alpha2` / `invaris.evidence/v1alpha2` | Tested in Evaris CI |
| 0.4.0 | v0.3.0 | none (legacy) | same | Tested in Evaris CI |
| 0.4.0 | v0.4.0 | none (legacy) | same | Tested in Evaris CI |
| 0.4.0 | 0.5.x | `invaris v0.5.Z` | same; findings may carry `line` | Accepted; tested locally against a development build of the 0.5 changes, added to CI once v0.5.0 is tagged |
| 0.4.0 | development build | `invaris dev` or a Go pseudo-version | same | Accepted and recorded |
| 0.4.0 | any other release | `invaris vX.Y.Z` | not tested | `UNAVAILABLE`, `UNSUPPORTED_TOOL_VERSION` |
| 0.3.0 and earlier | `ee92f3c` only | not checked | same, without `line` | Rejects findings that carry `line` |

Before `check`, Evaris runs `<invaris> --version` and records the output as
`invarisVersion` in `evidence/verification/process.json` (null for a legacy
build), with the executable path and SHA-256. Newer harness or evidence versions
fail closed as `ERROR`.

## Configuration

Add an optional `verification` object to a case:

```json
"verification": {"harness": "reviewer/invaris.json", "required": true, "timeoutSeconds": 60}
```

All three fields are required. `harness` is a private path relative to the
experiment file. It must sit outside every fixture and the evidence store, and
must not be the configuration, a task or an instruction file. The fixture must
not contain `invaris.json`, and no `allowedChanges` pattern may match it. Prepare
freezes the harness bytes with the other private inputs; later edits to the
source file do not affect existing stores. `timeoutSeconds` is 1–300.

The harness must match a restricted profile:

- `version` `invaris.dev/v1alpha2`, `trustMode` `trusted-local`.
- At least one policy, each with an explicit `"allowEmpty": false`.
- At least one structured command guard with a declared `policyId`.
- `gate.requiredGuards` lists every guard; `requiredProofs` empty;
  `precedence` `refusal-first`; `allowWeakEvidence` false.
- No `steps`, `challenges`, `adapters`, `requiredAdapters`, or provider guards.

`validate` and `prepare` reject other harnesses. Invaris mutation, challenge
and campaign features remain Invaris features; Evaris does not reimplement them.

## Evaluation

```sh
evaris evaluate --store /abs/evidence --run RUN_ID --allow-commands --invaris /abs/path/to/invaris
```

Evaris copies the imported final tree into a new scratch directory, writes the
frozen harness there as `invaris.json`, and runs
`<invaris> check <scratch>/invaris.json` from that directory. Only `PATH` is
inherited from the environment. Stdout is limited to 4 MiB and stderr to 64 KiB.

| Situation | `verification.status` | Reason |
| --- | --- | --- |
| No `--allow-commands` | `UNAVAILABLE` | `COMMAND_NOT_AUTHORIZED` (nothing runs) |
| No `--invaris`, or not an executable file | `ERROR` | `TOOL_MISSING` |
| Imported tree contains `invaris.json` | `ERROR` | `PATH_UNSAFE` (nothing runs) |
| Timeout, cancellation, output limit | `ERROR` | `TIMEOUT`, `CANCELED`, `LIMIT_EXCEEDED` |
| Exit 2 with no report | `ERROR` | `OUTPUT_MISSING` |
| Malformed, unsupported or inconsistent evidence | `ERROR` | `EVIDENCE_CORRUPT` or `UNSUPPORTED_SCHEMA` |
| Valid evidence | `ACCEPT`, `REJECT`, or `REFUSE` | native decision |

Evidence validation is strict: duplicate keys, unknown fields, missing fields
and wrong nulls are refused. The report must name the scratch target, the
harness name and the SHA-256 of the frozen harness bytes, use native
compatibility, and contain one check and lifecycle event per guard in order, with
no steps, proofs or artifacts. Evaris recomputes the check-only decision (any
refused or missing required check gives REFUSE, else any FAIL gives REJECT, else
ACCEPT with a positive target count) and requires it to match both the native
decision and the exit code (0, 1, 2). A mismatch is `ERROR`, never `ACCEPT`.

## Separate verdicts

`verification.status` records the validated native Invaris decision. The Evaris
`assessment` is computed separately:

- A required `REJECT` fails the evaluation.
- A required `REFUSE`, `ERROR` or `UNAVAILABLE` makes it `INCONCLUSIVE`.
- `ACCEPT` never replaces a missing or failed Evaris criterion. A failed exact
  criterion with Invaris `ACCEPT` is still `FAIL`.
- An optional verifier result is recorded but does not change the assessment.

Raw stdout, stderr and a process record (executable hash, exit, timings, scratch
manifests before and after) are kept privately under
`evaluations/<id>/evidence/verification/`. Findings keep paths as reported;
Evaris never reads the files they name. Re-evaluating creates a new evaluation.

## Trust boundary

Harness guard commands are trusted and not sandboxed. A guard whose `program` is
relative to the scratch directory runs code from the imported, agent-produced
tree; prefer absolute trusted executables. Scratch manifests detect persistent
changes inside the scratch copy only, not restored changes, external writes,
network access or detached descendants. Evaris checks the evidence is internally
consistent; it cannot prove Invaris or a guard reported honestly.

## Real-binary tests

Build a binary from the pinned revision outside this module, then:

```sh
EVARIS_TEST_INVARIS=/abs/invaris go test ./internal/adapters/invaris -run TestRealInvaris -count=1
EVARIS_TEST_INVARIS=/abs/invaris go test ./internal/acceptance -run TestP1OfflineIntegration -count=1
```

Without `EVARIS_TEST_INVARIS` these tests skip and real compatibility is not
established. Tests that use fake executables check Evaris's handling only.

## Failure messages

A verification that ends in `ERROR` carries a catalogue `message` in
`evaluation.json`, naming the failing step (`verification harness`,
`verification executable`, `verification scratch`, `verification command` or
`verification evidence`), the exit code when Invaris ran, the reason code and a
fixed hint, for example `verification evidence (command exited 0):
UNSUPPORTED_SCHEMA; this Invaris version is not supported; see docs/invaris.md`.
Like grader messages, it never contains paths, harness text or Invaris output;
read the retained `evidence/verification/` files for those.
