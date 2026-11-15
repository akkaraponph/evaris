# Offline grader controls

From the repository root, with the Go toolchain already installed:

```sh
GOTOOLCHAIN=local GOPROXY=off go run ./examples/offline
```

If your launcher is older than the module's selected toolchain, provision/select
it first, then run the offline phase with its actual binary:

```sh
EVARIS_GO_BIN="$(go env GOROOT)/bin"
PATH="$EVARIS_GO_BIN:$PATH" GOTOOLCHAIN=local GOPROXY=off go run ./examples/offline
```

The demonstration copies this experiment to a new private temporary directory,
prepares both arms, writes authored good/bad controls, runs each ordinary Go test
suite, imports the resulting files and grades them. Both ordinary suites pass;
only the good control satisfies the independent exact-byte criterion. It prints
the retained evidence and Markdown report paths. A successful demo exits 0 even
though the intentionally bad control has assessment FAIL.

These are synthetic controls, not agent-performance measurements. No model API,
Panda Suite, or Invaris is required. Nothing in the original example is modified.
The full fixture requires a response `result.txt` with `approved` and a newline;
the ordinary supplied test deliberately checks only that the file is nonempty.
Reviewer test code and expected bytes remain outside prepared workspaces.

For actual host runs, use the five commands in docs/workflow.md and import the
real response/tree/transcripts. Keep unobserved metadata null. Evaris never invents
an external host answer. Temporary evidence may contain private bytes; inspect
and remove the printed directory manually when no longer needed.
