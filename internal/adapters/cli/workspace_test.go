package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akkaraponph/evaris/internal/adapters/fsrepo"
	"github.com/akkaraponph/evaris/internal/adapters/graders"
	"github.com/akkaraponph/evaris/internal/adapters/render"
	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
)

func TestLocalWorkspaceWorkflow(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	runtime := Runtime{Service: application.Service{Repo: fsrepo.Repository{}, Grader: graders.Builtin{}, JSONReport: render.JSON, MarkdownReport: render.Markdown}}
	run := func(want int, result any, args ...string) {
		t.Helper()
		var out, stderr bytes.Buffer
		code := Run(context.Background(), args, &out, &stderr, runtime)
		if code != want {
			t.Fatalf("%v: code %d, want %d: %s %s", args, code, want, out.String(), stderr.String())
		}
		if result != nil {
			if err := json.Unmarshal(out.Bytes(), result); err != nil {
				t.Fatal(err, out.String())
			}
		}
	}
	run(2, nil, "validate")
	if _, err := os.Stat(".evaris"); !os.IsNotExist(err) {
		t.Fatal("validation created workspace", err)
	}
	run(0, nil, "init")
	run(0, nil, "validate")
	for _, name := range []string{"fixtures", "instructions", "reviewer", "tasks"} {
		info, err := os.Stat(filepath.Join(".evaris", name))
		if err != nil || !info.IsDir() {
			t.Fatal(name, err)
		}
	}
	if _, err := os.Stat(".evaris/evidence"); !os.IsNotExist(err) {
		t.Fatal("init or validate created an invalid evidence store")
	}
	config, err := os.ReadFile(".evaris/experiment.json")
	if err != nil {
		t.Fatal(err)
	}
	run(2, nil, "init")
	after, err := os.ReadFile(".evaris/experiment.json")
	if err != nil || !bytes.Equal(config, after) {
		t.Fatal("init overwrote config", err)
	}
	var first, second domain.Preparation
	run(0, &first, "prepare")
	run(0, &second, "prepare")
	if first.TasksRoot == second.TasksRoot || filepath.Dir(first.TasksRoot) != filepath.Join(root, ".evaris", "tasks") {
		t.Fatal("task roots not fresh/local", first.TasksRoot, second.TasksRoot)
	}
	for _, task := range first.Tasks {
		workspace := filepath.Join(first.TasksRoot, task.Workspace)
		if _, err := os.Stat(filepath.Join(workspace, "TASK.txt")); err != nil {
			t.Fatal(err)
		}
		if err := filepath.WalkDir(workspace, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if strings.Contains(p, "reviewer") || d.Name() == "oracle.json" || d.Name() == "experiment.json" {
				t.Errorf("private input leaked: %s", p)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	var report domain.Report
	run(2, &report, "report")
	if len(report.Cases) != 4 {
		t.Fatal("default report did not read local store", report)
	}
	// Exercise import and evaluate against the default evidence store.
	task := first.Tasks[0]
	tree := filepath.Join(first.TasksRoot, task.Workspace)
	if err := os.WriteFile(filepath.Join(tree, "result.txt"), []byte("hello\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("response.txt", []byte("Synthetic control; no model measurement."), 0600); err != nil {
		t.Fatal(err)
	}
	req := domain.ImportRequest{SchemaVersion: "evaris.import/v1alpha1", PreparationID: first.ID, CaseID: task.CaseID, ArmID: task.ArmID, ConfigDigest: first.ConfigDigest, FixtureDigest: task.FixtureDigest, SourceRevision: "initial", FinalResponse: "response.txt", FinalTree: tree, Transcripts: []string{}, Origin: "synthetic-control", Limitations: []string{"Not a model measurement."}}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("import.json", b, 0600); err != nil {
		t.Fatal(err)
	}
	var imported domain.Run
	run(0, &imported, "import", "--record", "import.json")
	var evaluation domain.Evaluation
	run(0, &evaluation, "evaluate", "--run", imported.ID)
	if evaluation.Assessment != "PASS" {
		t.Fatal(evaluation)
	}
	// Move the workspace to prove explicit config, tasks and store flags win.
	if err := os.Rename(".evaris", "custom"); err != nil {
		t.Fatal(err)
	}
	run(0, nil, "validate", "--experiment", "custom/experiment.json")
	run(0, nil, "prepare", "--experiment", "custom/experiment.json", "--tasks-root", filepath.Join(root, "explicit-tasks"))
	run(2, nil, "report", "--store", filepath.Join(root, "custom", "evidence"))
	if _, err := os.Stat(".evaris"); !os.IsNotExist(err) {
		t.Fatal("explicit flags created default workspace")
	}
}

func TestInitDoesNotOverwriteExistingPath(t *testing.T) {
	for _, kind := range []string{"file", "directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Chdir(root)
			switch kind {
			case "file":
				err = os.WriteFile(".evaris", []byte("keep"), 0600)
			case "directory":
				err = os.Mkdir(".evaris", 0700)
			case "symlink":
				err = os.Symlink("missing", ".evaris")
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(".evaris")
			if err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			code := Run(context.Background(), []string{"init"}, &out, &stderr, Runtime{Service: application.Service{Repo: fsrepo.Repository{}}})
			if code != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), "DEST_EXISTS") {
				t.Fatalf("code %d: %s %s", code, out.String(), stderr.String())
			}
			after, err := os.Lstat(".evaris")
			if err != nil || !os.SameFile(before, after) {
				t.Fatal("existing path changed", err)
			}
		})
	}
}

func TestInitCancellation(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, stderr bytes.Buffer
	if code := Run(ctx, []string{"init"}, &out, &stderr, Runtime{Service: application.Service{Repo: fsrepo.Repository{}}}); code != 2 || out.Len() != 0 {
		t.Fatal(code, out.String())
	}
	if _, err := os.Lstat(".evaris"); !os.IsNotExist(err) {
		t.Fatal("cancelled init left workspace", err)
	}
}
