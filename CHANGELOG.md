# Changelog

## 0.5.0 - 2026-11-19

Fixes found by evaluating a real NestJS and Next.js remaster with Evaris 0.4.0
and Invaris 0.5.0.

- Fixed: an `allowedChanges` or `protectedPaths` entry always matches the
  identical path. 0.4.0 read literal paths as patterns, so a listed Next.js
  route such as `app/products/[id]/page.tsx` no longer matched itself and an
  unchanged experiment failed source authorization on a correct delivery.
- A failed `source-authorization` criterion now has a catalogue message and
  writes `evidence/source-authorization.json`, listing each unauthorized path,
  how it changed and which rule it broke. Before, it failed with no message or
  evidence.
- With verification, `allowedChanges: ["**"]` is accepted when `protectedPaths`
  also covers `invaris.json`, so the deny-list form works with Invaris. Pattern
  errors name the case, field and pattern instead of `PATH_UNSAFE: experiment`.
- `verification.dependencies` links directories such as `node_modules` into the
  verification scratch root, so an Invaris campaign whose harnesses declare
  workspace dependencies can run against the imported tree. Before, such a
  campaign was refused with `workspace_prepare_failed`.

- Campaign verification checks the complete frozen experiment and repeat set,
  recomputes the native decision, and shows each experiment verdict in reports.
  The real-binary CI matrix now includes tagged Invaris 0.5.0.
- Comparisons check verifier executable and input digests. Use
  `--left-preparation` and `--right-preparation` to pair explicit trials.
- Import can derive observed Claude Code session metadata and confirmed
  instruction reads from a retained stream JSON transcript.

## 0.4.0 - 2026-11-17

- `allowedChanges` entries are patterns: `*`, `?` and `[...]` match within a
  segment and a whole `**` segment matches any depth, so `app/**` authorizes a
  generated tree. The new optional `protectedPaths` (same syntax) always fails
  authorization, even where `allowedChanges` matches. Generated files stay
  protected. Plain paths keep their meaning.
- `limits.maxEntries` (1 to 200000) raises the 2000-entry snapshot bound for an
  experiment's fixtures, imported trees and command scratch copies.
- `command-exit` criteria accept `dependencies`: absolute directories such as
  `node_modules`, linked beside the scratch copy so Node resolves them without
  copying. A missing source is `ERROR` with `TOOL_MISSING` and a catalogue
  message.
- Invaris verification failures carry a catalogue `message` naming the failing
  step (harness, executable, scratch, command or evidence) instead of a bare
  `IO_ERROR`.
- Reports move to `evaris.report/v1alpha2`: each case row lists every
  criterion's status, reason and message, and report.md adds a Criteria table.
- `evaris show --evaluation ID [--criterion ID]` prints one evaluation per
  criterion, including bounded command stdout and stderr, and refuses changed
  evidence.
- Import accepts `finalSource: {"kind": "git", "repository", "revision"}` in
  place of `finalTree`: Evaris exports the commit with `git archive`, adds the
  prepared generated files, and imports it. Runs that record the optional
  `deliveredRevision` (resolved automatically for git imports) use
  `evaris.run/v1alpha2`; other runs stay `evaris.run/v1alpha1`. 0.3.0 refuses
  v1alpha2 runs.
- `reviewerFiles` on the experiment freezes reviewer scripts and hidden tests:
  `prepare` records their digests and `evaluate` refuses to grade when one
  changed.
- `"mode": "acceptance"` evaluates a single arm with any ID: reports carry
  `mode`, no comparison and no comparison limitations, and `compare` refuses
  the store. Preparations record the mode.
- Invaris support widens from `ee92f3c` to v0.3.0, v0.4.0 and 0.5.x. Evaris
  probes `invaris --version` before `check`, records it as `invarisVersion`, and
  refuses an untested release with `UNAVAILABLE` / `UNSUPPORTED_TOOL_VERSION`.
  Findings accept an optional `line`. CI runs the real-Invaris job for each
  tested build, and both repositories publish the same compatibility table.
- Verification can run an Invaris campaign: `"action": "campaign"` with the
  campaign file as `harness` and its harness files in `files`, frozen at
  preparation. The campaign decision gates the case like a `check` decision.
  Every experiment in the report must have run a frozen harness file with its
  frozen digest; a harness read from the agent's tree is `EVIDENCE_CORRUPT`.
- The Invaris verification scratch snapshots use the experiment's
  `limits.maxEntries`.

## 0.3.0 - 2026-11-16

Workspace initialization, npm distribution, clearer command and import failures,
and command criteria that tolerate build output. Records keep their v1alpha1
schema versions; the one new field is optional.

- `evaris init` creates a local workspace, and common commands use local defaults.
- The `evaris-cli` npm package distributes native Linux and macOS binaries.
- `command-exit` criteria accept an optional `snapshotExclude` list of relative
  paths, such as `.next`, that the scratch manifests skip. A build that leaves
  more than 2000 entries no longer turns a passing command into an `ERROR`.
- A command criterion that ends in `ERROR` now carries the specific reason
  (`LIMIT_EXCEEDED`, `PATH_UNSAFE`) instead of `IO_ERROR`, and a `message` in
  `evaluation.json` that names the failing step, the command's exit code, the
  kind of limit and a fix. Messages come from a fixed catalogue with no paths,
  command text or oracle values; any other grader text is still cleared.
  `report.md` does not show them yet.
- `INVALID_CONFIG` for `agentClaim` lists the accepted values, and
  `REVISION_MISMATCH` on import says which identity field differs and what it
  must be. `docs/workflow.md` documents both fields.
- Compatibility: an oracle that uses `snapshotExclude`, and a preparation frozen
  from it, are refused by 0.2.0 and earlier as an unknown field. Oracles without
  it, and every other record, are unchanged. Invaris support is still only the
  pinned revision `ee92f3c`.

## 0.2.0 - 2026-11-15

- Runbook for real external-agent evaluations (`docs/pilot.md`); documented
  commands use one binary path.
- A process-validation pilot (kept privately with its preregistrations) of real
  Claude Code CLI sessions with `observed` fields taken from the host transcript
  and the run wrapper (operator-attested, not verified by Evaris). The final
  revision ran 12 sessions in 3 trials: one case separated the arms descriptively (baseline failed one stated rule in 2 of 3 trials), but
  every comparison was NOT_COMPARABLE because specialists read the instruction
  file through a shell command that the preregistered loading rule did not
  count. Not evidence of agent or instruction effectiveness.
- Runbook lessons for the Claude Code CLI host: permission mode, scratchpad
  location, observed-metadata sources and instruction-loading detection.
- Independent reproduction: a fresh AI operator session, given a copy of
  the retained pilot records (stores, reviewer files, preregistration and the
  outcomes to check against), a handoff note and the runbook, reproduced all
  12 evaluations and all 3 comparisons. No second human operator reproduced the
  grading, and the summary was reviewed by an independent AI session.
- No Go source, command or stored-schema changes since 0.1.0.

## 0.1.0 - 2026-11-15

First release.

- Validate versioned experiments, prepare blind baseline/specialist workspaces,
  and import externally produced outputs with frozen provenance.
- Grade deterministic artifacts and source authorization; trusted command checks
  require explicit consent and retain bounded execution evidence.
- Preserve evaluations and generate JSON/Markdown reports with missing evidence
  and pending semantic review shown explicitly.
- Add original offline positive/negative controls and Linux/macOS CI checks.
- Add optional Invaris `check` verification through the external CLI, pinned to
  Invaris source `ee92f3c00c1b59b2f576bad05569fb6eafb83097`, with strict
  evidence validation. Native ACCEPT/REJECT/REFUSE is recorded separately from
  the Evaris assessment; ACCEPT never replaces a failed criterion.
- Add `compare` for baseline/specialist arms and instruction revisions. Changed
  or unobserved conditions produce NOT_COMPARABLE reasons instead of claims;
  reports embed a comparison only when it covers exactly their evaluations.
- Recommendations in reports name failed and missing gates.
- `evaris --version`; release builds set the version with
  `-ldflags "-X main.version=..."`, and reports record it as `evarisVersion`.
- Supported platforms, record compatibility and reproducible builds are
  documented in `docs/release.md`.
- Isolate output-limit verification from the short timeout controls, retaining
  an exact 4 MiB bound assertion on slow race-enabled CI runners.

Known limitations: no real agent has been evaluated (bundled runs are synthetic
controls); command graders and Invaris guards are not sandboxed; Windows is not
supported; only Invaris revision `ee92f3c` is supported.
