package application

import (
	"context"
	"github.com/akkaraponph/evaris/internal/domain"
	"testing"
)

type invalidGrader struct{ panicNow bool }

func (g invalidGrader) Grade(context.Context, FrozenRun, domain.Criterion) (domain.GraderResult, []Blob) {
	if g.panicNow {
		panic("private text must not escape")
	}
	return domain.GraderResult{CriterionID: "x", Status: "PASS", GraderVersion: "invalid"}, nil
}
func TestGraderInvalidOrPanic(t *testing.T) {
	for _, p := range []bool{false, true} {
		s := Service{Grader: invalidGrader{p}}
		r, b := s.grade(context.Background(), FrozenRun{}, domain.Criterion{ID: "x", Required: true})
		if r.Status != "ERROR" || r.Reason != "INVALID_GRADER_RESULT" || len(b) != 0 || r.Message != "" {
			t.Fatal(r)
		}
	}
}
func TestUnauthorizedChanges(t *testing.T) {
	for _, mode := range []string{"modified", "deleted", "added", "mode", "protected"} {
		f := FrozenRun{Run: domain.Run{CaseID: "c", ArmID: "baseline"}, Case: FrozenCase{Case: domain.Case{AllowedChanges: []string{"TASK.txt"}}}, Preparation: domain.Preparation{Tasks: []domain.PreparedTask{{CaseID: "c", ArmID: "baseline", InitialFiles: domain.Manifest{{Path: "source", SHA256: "same"}}, GeneratedFiles: domain.Manifest{{Path: "TASK.txt", SHA256: "prompt"}}}}}}
		f.Run.FinalFiles = domain.Manifest{{Path: "tree/source", SHA256: "same"}, {Path: "tree/TASK.txt", SHA256: "prompt"}}
		switch mode {
		case "modified":
			f.Run.FinalFiles[0].SHA256 = "changed"
		case "deleted":
			f.Run.FinalFiles = f.Run.FinalFiles[1:]
		case "added":
			f.Run.FinalFiles = append(f.Run.FinalFiles, domain.FileRef{Path: "tree/unauthorized", SHA256: "x"})
		case "mode":
			f.Run.FinalFiles[0].Executable = true
		case "protected":
			f.Run.FinalFiles[1].SHA256 = "changed"
		}
		if r := authorization(f); r.Status != "FAIL" {
			t.Fatalf("%s: %+v", mode, r)
		}
	}
}
