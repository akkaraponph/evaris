# Changelog

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
