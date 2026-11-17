# Comparing arms and instruction revisions (P1)

Comparison needs a comparison-mode experiment; an [acceptance-mode](configuration.md#acceptance-mode) store is refused.

`compare` checks whether two sets of evaluations ran under matched conditions
and, only if they did, records per-dimension transitions. It never computes a
quality score, a statistical effect, or a causal claim.

```sh
evaris compare --left /abs/evidence --right /abs/evidence --mode arms
evaris compare --left /abs/old-evidence --right /abs/new-evidence --mode revisions
```

| Mode | Left | Right | Stores |
| --- | --- | --- | --- |
| `arms` | baseline | specialist | the same store (`--left` equals `--right`) |
| `revisions` | earlier revision, same case and arm | later revision | two stores |

Each store must contain exactly one committed preparation. Several preparations
are separate trials; Evaris refuses to choose a pairing among them. For each run
the newest evaluation is used. Older evaluations of the same run are re-gradings
of the same output, not independent trials, and are listed as limitations.

The result is a new immutable `evaris.comparison/v1alpha1` record under
`comparisons/<id>/` in the left store. Re-running creates a new record.

## Comparability conditions

A pair is comparable only when all of these hold. Every failure is listed in
`reasons`, `comparable` is false, and no transitions are recorded.

- Same experiment ID, case, task, fixture and oracle digests, source revision
  and origin.
- Same criteria, grader versions and required flags.
- Same verification configuration. In revisions mode, both harness digests must
  be known and equal.
- Observed and equal host, model, permissions and budget; model settings equal
  after JSON key normalization; tools equal as a set. Null means not observed,
  which blocks comparison. Evaris never estimates a missing value.
- Fresh context observed as true for both runs, with distinct context IDs across
  every run in the comparison.
- Baseline loaded instructions observed as `[]`. Specialist loaded instructions
  equal exactly the prepared files (`.evaris-instructions/000.txt`, ...).
- Arms mode: baseline and specialist from the same preparation. Revisions mode:
  the same arm. Only the declared instruction bytes and revision may differ.
- Each side has an evaluated run for every prepared case and arm. A missing arm
  is incomplete, not an improvement.

## Transitions

Dimensions are `assessment`, each `criterion:<id>` (including
`source-authorization`), and `verification` when configured. Only completed
outcomes are ranked: PASS/FAIL for assessment and criteria, ACCEPT/REJECT for
verification. `FAIL→PASS` is `improved`, `PASS→FAIL` is `regressed`, equal is
`unchanged`, and anything involving INCONCLUSIVE, ERROR, UNAVAILABLE or REFUSE is
`unknown`. A tool failure is never an agent regression.

## Exit codes

| Exit | Meaning |
| --- | --- |
| 0 | Comparable and every transition known, with no regression |
| 1 | Comparable with at least one observed regression |
| 2 | Not comparable, some transition unknown, invalid input, or an operation error |

Exit 1 reports an observed transition in one trial, not a statistically
established regression. Corrupt evidence aborts with exit 2 and no record.

## Reports

`report` embeds the newest committed comparison whose left and right evaluation
IDs together equal exactly the evaluations the report selects. Otherwise
`comparison` is null and a limitation says so. A revisions comparison lives in
the left store and names evaluations from another store, so a single-store
report never embeds it.

## Synthetic controls

Authored control runs (`origin: synthetic-control`) exercise the comparison
logic. Their metadata is supplied by the test author, and the comparison carries
a limitation that they measure grader behavior only, not agent performance.
