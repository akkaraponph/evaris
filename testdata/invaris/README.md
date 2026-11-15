# Invaris compatibility fixtures

Original fixtures for Evaris's optional Invaris integration. Nothing here is
copied from Invaris.

- `guard/` is a structured command guard that emits
  `invaris.inspection/v1alpha1` for `result.txt`: `approved\n` is clean,
  `rejected\n` is one finding, anything else is refused.
- `harness.json.tmpl` is the restricted check-only harness profile. Tests
  replace `@GUARD@` with the absolute path of the built guard.

The real-binary tests run only when `EVARIS_TEST_INVARIS` names an Invaris
executable built from source revision
`ee92f3c00c1b59b2f576bad05569fb6eafb83097`. See `docs/invaris.md`.

`evidence-{accept,reject,refuse}.json.tmpl` are stdout recorded from the pinned
binary (darwin/arm64) running this guard, with the scratch path, harness digest
and guard path replaced by `@TARGET@`, `@DIGEST@` and `@GUARD@`. Tests use them
to check the validator against real output shapes and to drive a fake
executable. Fake-backed tests are not real compatibility evidence.
