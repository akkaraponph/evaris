package fsrepo

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
)

const harnessSentinel = `{"name":"HARNESS_SECRET_7"}`

// verifiedFixture adds a verification block to the valid example; edit may change it.
func verifiedFixture(t *testing.T, edit func(c map[string]any)) (config, tasks string) {
	t.Helper()
	config, tasks = fixture(t)
	dir := filepath.Dir(config)
	os.WriteFile(filepath.Join(dir, "reviewer", "invaris.json"), []byte(harnessSentinel), 0600)
	b, _ := os.ReadFile(config)
	var m map[string]any
	json.Unmarshal(b, &m)
	c := m["cases"].([]any)[0].(map[string]any)
	c["verification"] = map[string]any{"harness": "reviewer/invaris.json", "required": true, "timeoutSeconds": 30}
	if edit != nil {
		edit(c)
	}
	b, _ = json.Marshal(m)
	os.WriteFile(config, b, 0600)
	return config, tasks
}

func TestVerificationConfig(t *testing.T) {
	config, _ := verifiedFixture(t, nil)
	p, e := (Repository{}).Load(context.Background(), config)
	if e != nil || string(p.Cases[0].Harness) != harnessSentinel {
		t.Fatalf("valid verification rejected: %v", e)
	}
	v := func(c map[string]any) map[string]any { return c["verification"].(map[string]any) }
	for name, edit := range map[string]func(c map[string]any){
		"harness in fixture":   func(c map[string]any) { v(c)["harness"] = "public/input.txt" },
		"harness is config":    func(c map[string]any) { v(c)["harness"] = "experiment.json" },
		"harness is task":      func(c map[string]any) { v(c)["harness"] = "task.txt" },
		"harness traversal":    func(c map[string]any) { v(c)["harness"] = "../invaris.json" },
		"harness missing":      func(c map[string]any) { v(c)["harness"] = "reviewer/absent.json" },
		"harness in store":     func(c map[string]any) { v(c)["harness"] = "evidence/invaris.json" },
		"timeout zero":         func(c map[string]any) { v(c)["timeoutSeconds"] = 0 },
		"timeout too large":    func(c map[string]any) { v(c)["timeoutSeconds"] = 301 },
		"timeout omitted":      func(c map[string]any) { delete(v(c), "timeoutSeconds") },
		"required omitted":     func(c map[string]any) { delete(v(c), "required") },
		"unknown field":        func(c map[string]any) { v(c)["program"] = "/bin/sh" },
		"null verification":    func(c map[string]any) { c["verification"] = nil },
		"allowed harness name": func(c map[string]any) { c["allowedChanges"] = []any{"result.txt", "invaris.json"} },
		"dependency relative": func(c map[string]any) {
			v(c)["dependencies"] = []any{map[string]any{"name": "node_modules", "source": "rel/dir"}}
		},
		"dependency nested": func(c map[string]any) { v(c)["dependencies"] = []any{map[string]any{"name": "a/b", "source": "/tmp"}} },
		"dependency harness": func(c map[string]any) {
			v(c)["dependencies"] = []any{map[string]any{"name": "invaris.json", "source": "/tmp"}}
		},
		"dependency twice": func(c map[string]any) {
			v(c)["dependencies"] = []any{map[string]any{"name": "node_modules", "source": "/tmp"}, map[string]any{"name": "node_modules", "source": "/var"}}
		},
	} {
		config, _ := verifiedFixture(t, edit)
		if name == "harness in store" {
			os.MkdirAll(filepath.Join(filepath.Dir(config), "evidence"), 0700)
			os.WriteFile(filepath.Join(filepath.Dir(config), "evidence", "invaris.json"), []byte(harnessSentinel), 0600)
		}
		if _, e := (Repository{}).Load(context.Background(), config); e == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	config, _ = verifiedFixture(t, func(c map[string]any) {
		v(c)["dependencies"] = []any{map[string]any{"name": "node_modules", "source": "/tmp"}}
	})
	if _, e := (Repository{}).Load(context.Background(), config); e != nil {
		t.Errorf("valid verification dependency rejected: %v", e)
	}
	config, _ = verifiedFixture(t, nil)
	os.WriteFile(filepath.Join(filepath.Dir(config), "public", "invaris.json"), []byte("{}"), 0600)
	if _, e := (Repository{}).Load(context.Background(), config); e == nil {
		t.Error("fixture invaris.json collision accepted")
	}
	config, _ = verifiedFixture(t, nil)
	check := func([]byte) error { return errors.New("unsupported profile") }
	var f *domain.Failure
	if _, e := (Repository{CheckHarness: check}).Load(context.Background(), config); !errors.As(e, &f) || f.Code != "INVALID_CONFIG" {
		t.Errorf("profile check not applied: %v", e)
	}
}

func TestVerificationHarnessFrozenAndPrivate(t *testing.T) {
	config, tasks := verifiedFixture(t, nil)
	plan, e := (Repository{}).Load(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	p, e := (Repository{}).Prepare(context.Background(), plan, tasks)
	if e != nil {
		t.Fatal(e)
	}
	filepath.WalkDir(tasks, func(path string, d os.DirEntry, _ error) error {
		if !d.IsDir() {
			if b, _ := os.ReadFile(path); strings.Contains(string(b), "HARNESS_SECRET_7") || d.Name() == "invaris.json" {
				t.Errorf("harness leaked into workspace: %s", path)
			}
		}
		return nil
	})
	// Later source edits never change the frozen harness used for evaluation.
	os.WriteFile(filepath.Join(filepath.Dir(config), "reviewer", "invaris.json"), []byte(`{"changed":true}`), 0600)
	run, e := (Repository{}).Import(context.Background(), plan.OutputRoot, request(t, p, config))
	if e != nil {
		t.Fatal(e)
	}
	f, e := (Repository{}).LoadRun(context.Background(), plan.OutputRoot, run.ID)
	if e != nil || string(f.Case.Harness) != harnessSentinel {
		t.Fatalf("frozen harness not used: %v %s", e, f.Case.Harness)
	}
	// The private harness can never be imported as an agent artifact.
	path := request(t, p, config)
	b, _ := os.ReadFile(path)
	var q map[string]any
	json.Unmarshal(b, &q)
	q["armId"] = "specialist"
	q["finalResponse"] = filepath.Join(filepath.Dir(config), "reviewer", "invaris.json")
	b, _ = json.Marshal(q)
	os.WriteFile(path, b, 0600)
	var failure *domain.Failure
	if _, e := (Repository{}).Import(context.Background(), plan.OutputRoot, path); !errors.As(e, &failure) || failure.Code != "PATH_UNSAFE" {
		t.Errorf("imported the private harness as a response: %v", e)
	}
}

type stubVerifier struct{ data []byte }

func (s stubVerifier) Verify(context.Context, application.FrozenRun) (domain.Verification, []application.Blob) {
	d, run, sv := "sha256:x", "0123456789abcdef0123456789abcdef", "invaris.evidence/v1alpha2"
	zero := 0
	ref := domain.FileRef{Path: "evidence/verification/stdout.json", SHA256: domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(s.data))), Bytes: int64(len(s.data))}
	return domain.Verification{Status: "ACCEPT", SchemaVersion: &sv, RunID: &run, ConfigDigest: &d, ExitCode: &zero, Evidence: []domain.FileRef{ref}, Findings: []domain.Finding{}}, []application.Blob{{Path: ref.Path, Data: s.data}}
}

func TestVerificationEvidencePersisted(t *testing.T) {
	config, tasks := verifiedFixture(t, nil)
	plan, _ := (Repository{}).Load(context.Background(), config)
	p, e := (Repository{}).Prepare(context.Background(), plan, tasks)
	if e != nil {
		t.Fatal(e)
	}
	run, e := (Repository{}).Import(context.Background(), plan.OutputRoot, request(t, p, config))
	if e != nil {
		t.Fatal(e)
	}
	s := application.Service{Repo: Repository{}, Grader: passGrader{}, Verifier: stubVerifier{[]byte(`{"decision":"ACCEPT"}`)}}
	v, e := s.Evaluate(context.Background(), plan.OutputRoot, run.ID, true)
	if e != nil || v.Verification == nil || v.Verification.Status != "ACCEPT" {
		t.Fatalf("%+v %v", v, e)
	}
	es, _, e := (Repository{}).LoadEvaluations(context.Background(), plan.OutputRoot)
	if e != nil || len(es) != 1 || es[0].Verification == nil || es[0].Verification.Status != "ACCEPT" {
		t.Fatalf("verification not round-tripped: %v", e)
	}
	// Dropping the configured verification from a record is corruption.
	stripped := v
	stripped.ID = "0123456789abcdef0123456789abcd00"
	stripped.Verification = nil
	if e = (Repository{}).SaveEvaluation(context.Background(), plan.OutputRoot, stripped, nil); e == nil {
		t.Fatal("saved an evaluation without its configured verification")
	}
	os.WriteFile(filepath.Join(plan.OutputRoot, "evaluations", v.ID, "evidence", "verification", "stdout.json"), []byte(`{"decision":"REJECT"}`), 0600)
	if _, _, e = (Repository{}).LoadEvaluations(context.Background(), plan.OutputRoot); e == nil {
		t.Fatal("accepted tampered verifier evidence")
	}
}
