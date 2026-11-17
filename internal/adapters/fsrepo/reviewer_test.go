package fsrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
)

// reviewerSetup writes reviewer/check.sh, lists it (plus extra entries) in
// reviewerFiles, prepares and imports, and returns what Evaluate needs.
func reviewerSetup(t *testing.T, files []string) (store, runID, script string, err error) {
	t.Helper()
	config, tasks := fixture(t)
	script = filepath.Join(filepath.Dir(config), "reviewer", "check.sh")
	os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o700)
	b, _ := os.ReadFile(config)
	var exp map[string]any
	json.Unmarshal(b, &exp)
	exp["reviewerFiles"] = files
	b, _ = json.Marshal(exp)
	os.WriteFile(config, b, 0o600)
	plan, e := (Repository{}).Load(context.Background(), config)
	if e != nil {
		return "", "", script, e
	}
	p, e := (Repository{}).Prepare(context.Background(), plan, tasks)
	if e != nil {
		return "", "", script, e
	}
	run, e := (Repository{}).Import(context.Background(), plan.OutputRoot, request(t, p, config))
	if e != nil {
		t.Fatal(e)
	}
	return plan.OutputRoot, run.ID, script, nil
}

func TestReviewerFilesFrozen(t *testing.T) {
	store, runID, script, err := reviewerSetup(t, []string{"reviewer/check.sh"})
	if err != nil {
		t.Fatal(err)
	}
	s := application.Service{Repo: Repository{}, Grader: passGrader{}}
	if _, e := s.Evaluate(context.Background(), store, runID, false); e != nil {
		t.Fatalf("unchanged reviewer file: %v", e)
	}
	os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o700)
	_, e := s.Evaluate(context.Background(), store, runID, false)
	if e == nil || !strings.Contains(e.Error(), "EVIDENCE_CORRUPT") || !strings.Contains(e.Error(), "changed since preparation") {
		t.Fatalf("edited reviewer file: %v", e)
	}
	entries, _ := os.ReadDir(filepath.Join(store, "evaluations"))
	if len(entries) != 1 {
		t.Fatalf("evaluations written: %d, want only the one before the edit", len(entries))
	}
	os.Remove(script)
	if _, e := s.Evaluate(context.Background(), store, runID, false); e == nil {
		t.Fatal("missing reviewer file accepted")
	}
}

func TestReviewerFilesValidation(t *testing.T) {
	var many []string
	for i := range domain.MaxReviewerFiles + 1 {
		many = append(many, fmt.Sprintf("reviewer/f%d", i))
	}
	for name, files := range map[string][]string{
		"absent":         {"reviewer/absent.sh"},
		"inside fixture": {"public/input.txt"},
		"too many":       many,
		"duplicate":      {"reviewer/check.sh", "reviewer/check.sh"},
		"escaping":       {"../x.sh"},
	} {
		_, _, _, err := reviewerSetup(t, files)
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
