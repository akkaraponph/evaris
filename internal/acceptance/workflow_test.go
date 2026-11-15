package acceptance

import (
	"bytes"
	"encoding/json"
	"github.com/akkaraponph/evaris/internal/domain"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOfflineWorkflow(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	binary := filepath.Join(root, "evaris")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/evaris")
	if b, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, b)
	}
	project := filepath.Join(root, "project")
	src := "../../examples/offline"
	e = filepath.WalkDir(src, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(src, p)
		dest := filepath.Join(project, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		return os.WriteFile(dest, b, 0600)
	})
	if e != nil {
		t.Fatal(e)
	}
	run := func(code int, out any, args ...string) {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		b, e := cmd.Output()
		got := 0
		if e != nil {
			if x, ok := e.(*exec.ExitError); ok {
				got = x.ExitCode()
			} else {
				t.Fatal(e)
			}
		}
		if got != code {
			t.Fatalf("%v exit=%d want=%d: %s %s", args, got, code, stderr.String(), b)
		}
		if out != nil {
			if e = json.Unmarshal(b, out); e != nil {
				t.Fatalf("JSON: %s %v", b, e)
			}
		}
	}
	run(0, nil, "validate", "--experiment", filepath.Join(project, "experiment.json"))
	var prep domain.Preparation
	run(0, &prep, "prepare", "--experiment", filepath.Join(project, "experiment.json"), "--tasks-root", filepath.Join(root, "tasks"))
	store := filepath.Join(project, "evidence")
	var badID string
	for i, task := range prep.Tasks {
		tree := filepath.Join(prep.TasksRoot, task.Workspace)
		control := "good.txt"
		expected := 0
		if i == 1 {
			control = "bad.txt"
			expected = 1
		}
		b, _ := os.ReadFile(filepath.Join(project, "controls", control))
		os.WriteFile(filepath.Join(tree, "result.txt"), b, 0600)
		ordinary := exec.Command("go", "test", "./...")
		ordinary.Dir = tree
		ordinary.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off")
		if b, e := ordinary.CombinedOutput(); e != nil {
			t.Fatalf("ordinary test: %v %s", e, b)
		}
		response := filepath.Join(root, task.ArmID+".txt")
		os.WriteFile(response, []byte("Synthetic control; no model run"), 0600)
		q := domain.ImportRequest{SchemaVersion: "evaris.import/v1alpha1", PreparationID: prep.ID, CaseID: task.CaseID, ArmID: task.ArmID, ConfigDigest: prep.ConfigDigest, FixtureDigest: task.FixtureDigest, SourceRevision: "initial", FinalResponse: response, FinalTree: tree, Transcripts: []string{}, Origin: "synthetic-control", Limitations: []string{"Synthetic control, not model performance."}}
		request := filepath.Join(root, task.ArmID+".json")
		b, _ = json.Marshal(q)
		os.WriteFile(request, b, 0600)
		var record domain.Run
		run(0, &record, "import", "--store", store, "--record", request)
		var ev domain.Evaluation
		run(expected, &ev, "evaluate", "--store", store, "--run", record.ID)
		if i == 0 && ev.Assessment != "PASS" || i == 1 && ev.Assessment != "FAIL" {
			t.Fatal(ev)
		}
		old, _ := os.ReadFile(filepath.Join(store, "evaluations", ev.ID, "evaluation.json"))
		var again domain.Evaluation
		run(expected, &again, "evaluate", "--store", store, "--run", record.ID)
		now, _ := os.ReadFile(filepath.Join(store, "evaluations", ev.ID, "evaluation.json"))
		if ev.ID == again.ID || !bytes.Equal(old, now) {
			t.Fatal("overwrote evaluation")
		}
		if i == 0 {
			var incomplete domain.Report
			run(2, &incomplete, "report", "--store", store)
			if len(incomplete.Cases) != 2 {
				t.Fatal("missing arm hidden")
			}
		}
		if i == 1 {
			badID = record.ID
		}
	}
	var report domain.Report
	run(1, &report, "report", "--store", store)
	if len(report.Cases) != 2 || len(report.EvaluationIDs) != 4 {
		t.Fatal(report)
	}
	md, e := os.ReadFile(filepath.Join(store, "reports", report.ID, "report.md"))
	if e != nil || !bytes.Contains(md, []byte("FAIL")) {
		t.Fatal("missing Markdown failure")
	}
	os.WriteFile(filepath.Join(store, "runs", badID, "response.txt"), []byte("tampered"), 0600)
	run(2, nil, "report", "--store", store)
}
