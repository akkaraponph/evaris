package graders

import (
	"context"
	"encoding/json"
	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltinCriteria(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "result"), []byte("approved\n"), 0600)
	f := application.FrozenRun{SnapshotRoot: dir}
	for _, tc := range []struct{ kind, expected, status string }{{"exists", "", "PASS"}, {"exact", `"approved\n"`, "PASS"}, {"exact", `"approved"`, "FAIL"}} {
		r, _ := (Builtin{}).Grade(context.Background(), f, domain.Criterion{ID: "x", Kind: tc.kind, Path: "result", Expected: json.RawMessage(tc.expected)})
		if r.Status != tc.status {
			t.Fatal(r)
		}
	}
}
func TestJSONEquality(t *testing.T) {
	a, e := strictJSON([]byte(`{"n":1.0,"list":[1,2]}`))
	if e != nil {
		t.Fatal(e)
	}
	b, _ := strictJSON([]byte(`{"list":[1,2],"n":1e0}`))
	if !equal(a, b) {
		t.Fatal("numeric/key equality")
	}
	if _, e = strictJSON([]byte(`{"n":1,"n":2}`)); e == nil {
		t.Fatal("duplicate accepted")
	}
}
func TestCommandConsent(t *testing.T) {
	r, _ := (Builtin{}).Grade(context.Background(), application.FrozenRun{}, domain.Criterion{ID: "cmd", Kind: "command-exit"})
	if r.Status != "UNAVAILABLE" || r.Reason != "COMMAND_NOT_AUTHORIZED" {
		t.Fatal(r)
	}
}

func TestCommandCannotMutateEvidence(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "source"), []byte("original"), 0600)
	f := application.FrozenRun{SnapshotRoot: root, AllowCommands: true}
	r, b := (Builtin{}).Grade(context.Background(), f, domain.Criterion{ID: "command", Kind: "command-exit", Expected: json.RawMessage("0"), Command: &domain.Command{Program: "/bin/sh", Args: []string{"-c", "echo changed > source"}}})
	if r.Status != "PASS" || len(b) != 1 {
		t.Fatalf("%+v", r)
	}
	actual, _ := os.ReadFile(filepath.Join(root, "source"))
	if string(actual) != "original" {
		t.Fatal("changed imported evidence")
	}
}

// An ordinary exit-0 suite passes both controls; only the independent exact gate separates them.
func TestGreenSuiteHiddenViolation(t *testing.T) {
	suite := domain.Criterion{ID: "ordinary-suite", Kind: "command-exit", Required: true, Expected: json.RawMessage("0"), Command: &domain.Command{Program: "/bin/sh", Args: []string{"-c", "test -s result.txt"}, EnvAllowlist: []string{}}}
	exact := domain.Criterion{ID: "result", Kind: "exact", Required: true, Path: "result.txt", Expected: json.RawMessage(`"approved\n"`)}
	for _, tc := range []struct {
		content, exact string
	}{{"approved\n", "PASS"}, {"rejected\n", "FAIL"}} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "result.txt"), []byte(tc.content), 0600)
		f := application.FrozenRun{SnapshotRoot: dir, AllowCommands: true}
		s, _ := (Builtin{}).Grade(context.Background(), f, suite)
		x, _ := (Builtin{}).Grade(context.Background(), f, exact)
		if s.Status != "PASS" || x.Status != tc.exact {
			t.Fatalf("%q: suite=%s exact=%s", tc.content, s.Status, x.Status)
		}
		a := domain.Assess([]domain.GraderResult{s, x}, "NOT_REQUIRED", nil)
		if tc.exact == "FAIL" && a.Final != "FAIL" || tc.exact == "PASS" && a.Final != "PASS" {
			t.Fatalf("green suite hid violation: %+v", a)
		}
	}
}

// A build that leaves thousands of files must not turn a passing command into
// a bare IO_ERROR: the reason names the limit, the message says the command
// finished, and snapshotExclude keeps the output out of the snapshots.
func TestCommandSnapshotLimitIsExplainedAndExcludable(t *testing.T) {
	build := "mkdir -p .next && i=0; while [ $i -le 2000 ]; do : > .next/f$i; i=$((i+1)); done"
	c := domain.Criterion{ID: "scan", Kind: "command-exit", Required: true, Expected: json.RawMessage("0"),
		Command: &domain.Command{Program: "/bin/sh", Args: []string{"-c", build}, EnvAllowlist: []string{}}}
	f := application.FrozenRun{SnapshotRoot: t.TempDir(), AllowCommands: true}

	r, _ := (Builtin{}).Grade(context.Background(), f, c)
	want := "scratch snapshot after the command (command exited 0): LIMIT_EXCEEDED (entries); list generated output such as .next in snapshotExclude"
	if r.Status != "ERROR" || r.Reason != "LIMIT_EXCEEDED" || r.Message != want || !domain.ValidGraderMessage(r.Message) {
		t.Fatalf("result %+v", r)
	}

	c.SnapshotExclude = []string{".next"}
	if r, _ = (Builtin{}).Grade(context.Background(), f, c); r.Status != "PASS" || r.Message != "" {
		t.Fatalf("excluded build output: %+v", r)
	}
}

func TestCommandFailureMessages(t *testing.T) {
	f := application.FrozenRun{SnapshotRoot: t.TempDir(), AllowCommands: true}
	r, _ := (Builtin{}).Grade(context.Background(), f, domain.Criterion{ID: "slow", Kind: "command-exit", Expected: json.RawMessage("0"),
		Command: &domain.Command{Program: "/bin/sh", Args: []string{"-c", "sleep 5"}, EnvAllowlist: []string{}, TimeoutSeconds: 1}})
	if r.Reason != "TIMEOUT" || r.Message != "command: TIMEOUT; raise timeoutSeconds or shorten the command" || !domain.ValidGraderMessage(r.Message) {
		t.Fatalf("result %+v", r)
	}
}

func dependencyCriterion(script, source string) domain.Criterion {
	return domain.Criterion{ID: "deps", Kind: "command-exit", Required: true, Expected: json.RawMessage("0"),
		Command:      &domain.Command{Program: "/bin/sh", Args: []string{"-c", script}, EnvAllowlist: []string{}},
		Dependencies: []domain.Dependency{{Name: "node_modules", Source: source}}}
}

func TestCommandDependencyResolves(t *testing.T) {
	deps := t.TempDir()
	if err := os.MkdirAll(filepath.Join(deps, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deps, "pkg", "index.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := application.FrozenRun{SnapshotRoot: t.TempDir(), AllowCommands: true}
	r, blobs := (Builtin{}).Grade(context.Background(), f, dependencyCriterion("test -f ../node_modules/pkg/index.js", deps))
	if r.Status != "PASS" {
		t.Fatalf("result %+v", r)
	}
	var ev struct {
		Before, After domain.Manifest
		Dependencies  []domain.Dependency
	}
	if err := json.Unmarshal(blobs[0].Data, &ev); err != nil {
		t.Fatal(err)
	}
	for _, m := range []domain.Manifest{ev.Before, ev.After} {
		for _, ref := range m {
			if ref.Path == "node_modules" || len(ref.Path) > 12 && ref.Path[:13] == "node_modules/" {
				t.Fatalf("dependency entered the manifest: %s", ref.Path)
			}
		}
	}
	if len(ev.Dependencies) != 1 || ev.Dependencies[0].Source != deps {
		t.Fatalf("evidence dependencies %+v", ev.Dependencies)
	}
}

func TestCommandDependencyMissing(t *testing.T) {
	f := application.FrozenRun{SnapshotRoot: t.TempDir(), AllowCommands: true}
	r, _ := (Builtin{}).Grade(context.Background(), f, dependencyCriterion("true", filepath.Join(t.TempDir(), "absent")))
	if r.Status != "ERROR" || r.Reason != "TOOL_MISSING" || r.Message != "link dependencies: TOOL_MISSING; a declared dependency directory does not exist on this host" {
		t.Fatalf("result %+v", r)
	}
}

func TestCommandRaisedLimit(t *testing.T) {
	build := "mkdir -p out && i=0; while [ $i -le 2500 ]; do : > out/f$i; i=$((i+1)); done"
	c := domain.Criterion{ID: "b", Kind: "command-exit", Required: true, Expected: json.RawMessage("0"),
		Command: &domain.Command{Program: "/bin/sh", Args: []string{"-c", build}, EnvAllowlist: []string{}}}
	f := application.FrozenRun{SnapshotRoot: t.TempDir(), AllowCommands: true, MaxEntries: 3000}
	if r, _ := (Builtin{}).Grade(context.Background(), f, c); r.Status != "PASS" {
		t.Fatalf("raised limit: %+v", r)
	}
}
