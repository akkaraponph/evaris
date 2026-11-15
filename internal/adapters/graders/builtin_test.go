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
