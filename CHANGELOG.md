# Changelog

## 0.3.0 - not yet released

Clearer command and import failures, and command criteria that tolerate build
output. Records keep their v1alpha1 schema versions; the one new field is
optional.

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
