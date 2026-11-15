package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/akkaraponph/evaris/internal/adapters/fsrepo"
	"github.com/akkaraponph/evaris/internal/adapters/graders"
	"github.com/akkaraponph/evaris/internal/adapters/render"
	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	if e := demo(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func demo() error {
	root, e := os.MkdirTemp("", "evaris-demo-")
	if e != nil {
		return e
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		return e
	}
	project := filepath.Join(root, "project")
	source := "examples/offline"
	e = filepath.WalkDir(source, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(source, p)
		target := filepath.Join(project, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		return os.WriteFile(target, b, 0600)
	})
	if e != nil {
		return e
	}
	ctx := context.Background()
	s := application.Service{Repo: fsrepo.Repository{}, Grader: graders.Builtin{}, JSONReport: render.JSON, MarkdownReport: render.Markdown}
	prep, e := s.Prepare(ctx, filepath.Join(project, "experiment.json"), filepath.Join(root, "tasks"))
	if e != nil {
		return e
	}
	store := filepath.Join(project, "evidence")
	for i, task := range prep.Tasks {
		tree := filepath.Join(prep.TasksRoot, task.Workspace)
		control := "good.txt"
		expected := domain.AssessmentStatus("PASS")
		if i == 1 {
			control = "bad.txt"
			expected = "FAIL"
		}
		b, e := os.ReadFile(filepath.Join(project, "controls", control))
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(tree, "result.txt"), b, 0600); e != nil {
			return e
		}
		cmd := exec.Command("go", "test", "./...")
		cmd.Dir = tree
		cmd.Env = append(os.Environ(), "GOPROXY=off", "GOTOOLCHAIN=local")
		if b, e := cmd.CombinedOutput(); e != nil {
			return fmt.Errorf("ordinary suite: %s: %w", b, e)
		}
		response := filepath.Join(root, task.ArmID+".txt")
		os.WriteFile(response, []byte("Synthetic authored control, no model run."), 0600)
		q := domain.ImportRequest{SchemaVersion: "evaris.import/v1alpha1", PreparationID: prep.ID, CaseID: task.CaseID, ArmID: task.ArmID, ConfigDigest: prep.ConfigDigest, FixtureDigest: task.FixtureDigest, SourceRevision: "initial", FinalResponse: response, FinalTree: tree, Transcripts: []string{}, Limitations: []string{"Synthetic control, not agent performance."}, Origin: "synthetic-control"}
		b, _ = json.Marshal(q)
		request := filepath.Join(root, task.ArmID+".json")
		os.WriteFile(request, b, 0600)
		run, e := s.Import(ctx, store, request)
		if e != nil {
			return e
		}
		ev, e := s.Evaluate(ctx, store, run.ID, false)
		if e != nil {
			return e
		}
		if ev.Assessment != expected {
			return fmt.Errorf("control %s: got %s expected %s", control, ev.Assessment, expected)
		}
		fmt.Printf("%s: ordinary tests PASS, independent assessment %s\n", control, ev.Assessment)
	}
	report, e := s.Report(ctx, store)
	if e != nil {
		return e
	}
	fmt.Printf("Synthetic controls verified. Evidence retained at %s\nReport: %s\n", store, filepath.Join(store, "reports", report.ID, "report.md"))
	return nil
}
