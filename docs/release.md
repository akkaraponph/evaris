# Releases, support and reproducible builds

## Supported platforms

| Item | Supported in 0.3.x |
| --- | --- |
| Go toolchain | 1.27.4 or newer (minimum in `go.mod`) |
| Operating systems | Linux and macOS, amd64 and arm64 |
| Windows | Not supported; the process adapter uses Unix process groups and does not build for Windows |
| Invaris (optional) | Only source revision `ee92f3c00c1b59b2f576bad05569fb6eafb83097` |

CI runs build, vet, race tests and the offline acceptance suite on
`ubuntu-latest` and `macos-latest`, plus the real-Invaris job on both. Platforms
not exercised by CI are cross-compiled only.

## Record compatibility

Every stored record carries a schema version, and strict decoding rejects any
other version, unknown fields and duplicate keys:

| Record | Schema version |
| --- | --- |
| Experiment, oracle, import request | `evaris.experiment/v1alpha1`, `evaris.oracle/v1alpha1`, `evaris.import/v1alpha1` |
| Preparation, run, evaluation, report | `evaris.preparation/v1alpha1`, `evaris.run/v1alpha1`, `evaris.evaluation/v1alpha1`, `evaris.report/v1alpha1` |
| Comparison | `evaris.comparison/v1alpha1` |
| Invaris harness / evidence read | `invaris.dev/v1alpha2` / `invaris.evidence/v1alpha2` |

`v1alpha1` records carry no compatibility promise across minor 0.x releases. A
release that changes a schema bumps its version, so older binaries refuse new
records instead of misreading them. Evidence stores are not migrated.
0.3.0 adds one optional field, `snapshotExclude` on `command-exit` criteria,
within `evaris.oracle/v1alpha1`. Strict decoding makes 0.2.0 and earlier refuse
an oracle or preparation that uses it; records without it are unchanged.

## Version string

`evaris --version` prints the version. Release builds set it with
`-ldflags "-X main.version=vX.Y.Z"`. Without it, `go install
github.com/akkaraponph/evaris/cmd/evaris@vX.Y.Z` reports the module version, and
a build from a checkout reports the Go VCS pseudo-version. Reports record the
same value as `evarisVersion`.

## Reproducible release build

From a clean checkout of the release tag, with Go 1.27.4:

```sh
git status --porcelain   # must print nothing
VERSION=v0.3.0
mkdir -p dist
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  GOOS=${target%/*} GOARCH=${target#*/} CGO_ENABLED=0 \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
    -o "dist/evaris-$VERSION-${target%/*}-${target#*/}" ./cmd/evaris
done
(cd dist && shasum -a 256 evaris-* > SHA256SUMS)
```

`dist/` is ignored by Git. The same tag, Go version and flags produce
byte-identical binaries on Linux and macOS hosts, so anyone can rebuild and
compare against `SHA256SUMS`. Go embeds the Git revision, commit time and a
`vcs.modified` flag, so build from a clean Git checkout of the tag: a dirty tree,
a source archive without `.git`, or `-buildvcs=false` produces different bytes.
`go version -m <binary>` shows the embedded build information. Hashes are generated only when release binaries are actually
built; they identify bytes and are not signatures.

## Release checklist

1. Hosted CI is green on the exact release commit, including the `invaris` job.
2. `CHANGELOG.md` has a section for the version, dated on the tag day.
3. README, CONTRIBUTING, SECURITY, AGENTS and this file match the commands.
4. `git status --porcelain` is empty; no private evidence or build output is tracked.
5. Tag the commit (`git tag -a vX.Y.Z`), build as above, and publish the
   binaries with `SHA256SUMS`. Tagging and publishing are explicit maintainer
   actions.

## Installation verification and recovery

After downloading the binary for your OS and architecture, verify its entry in
`SHA256SUMS`, make it executable, and run `evaris --version` and
`evaris validate --experiment testdata/valid/experiment.json` from a source
checkout. Stop publication or installation if a checksum, version, validation,
or required CI check fails.

There are no services or database migrations to deploy. Keep existing evidence
stores intact when replacing a binary. To recover from a later release, reinstall
the previous supported binary and retain stores written with newer schemas
separately; older binaries may reject those records. 0.2.0 is the
earlier release to roll back to; remove `snapshotExclude` from oracles and
prepare again before using it, because it refuses that field. Commands and guards can have operator
controlled side effects that replacing the Evaris binary cannot undo.

Known limitations to state in release notes: bundled runs are synthetic
controls; the v0.2.0 process pilot ran real external agent sessions with
observed host metadata, but its comparisons were NOT_COMPARABLE and its outcomes
come from 3 trials with one host and model, with operator-attested host metadata
and no semantic review, so it supports no agent or
instruction-effectiveness claim; its grading was reproduced by an independent
AI operator session, not by a second person; commands and Invaris guards are not
sandboxed; and only the pinned Invaris revision is supported.
