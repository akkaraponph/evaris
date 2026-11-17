package application

import (
	"context"
	"fmt"
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

type messageGrader struct{ message string }

func (g messageGrader) Grade(_ context.Context, _ FrozenRun, c domain.Criterion) (domain.GraderResult, []Blob) {
	return domain.GraderResult{CriterionID: c.ID, Status: "ERROR", Reason: "LIMIT_EXCEEDED", Message: g.message, Evidence: []domain.FileRef{}, GraderVersion: "builtin/1"}, nil
}

// The evaluation keeps catalogue messages and clears any other grader text,
// which could carry reviewer-private content or scratch paths.
func TestGradeKeepsOnlyCatalogueMessages(t *testing.T) {
	exit := 0
	kept := domain.CommandMessage(domain.StepSnapshotAfter, true, &exit, "LIMIT_EXCEEDED", domain.LimitEntries)
	for msg, want := range map[string]string{
		kept:                              kept,
		"expected value is secret-answer": "",
		kept + " /private/var/folders/x/evaris-grade-123":   "",
		"command: TIMEOUT; see /reviewer/oracle.json":       "",
		"scratch snapshot after the command: SECRET_REASON": "",
	} {
		s := Service{Grader: messageGrader{msg}}
		r, _ := s.grade(context.Background(), FrozenRun{}, domain.Criterion{ID: "scan", Required: true})
		if r.Message != want {
			t.Errorf("message %q became %q, want %q", msg, r.Message, want)
		}
	}
}

func globRun(allowed, protected []string, extra ...string) FrozenRun {
	f := FrozenRun{Run: domain.Run{CaseID: "c", ArmID: "baseline"}, Case: FrozenCase{Case: domain.Case{AllowedChanges: allowed, ProtectedPaths: protected}},
		Preparation: domain.Preparation{Tasks: []domain.PreparedTask{{CaseID: "c", ArmID: "baseline", InitialFiles: domain.Manifest{{Path: "source", SHA256: "same"}}, GeneratedFiles: domain.Manifest{{Path: "TASK.txt", SHA256: "prompt"}}}}}}
	f.Run.FinalFiles = domain.Manifest{{Path: "tree/source", SHA256: "same"}, {Path: "tree/TASK.txt", SHA256: "prompt"}}
	for _, p := range extra {
		f.Run.FinalFiles = append(f.Run.FinalFiles, domain.FileRef{Path: "tree/" + p, SHA256: "new"})
	}
	return f
}

func TestGlobAuthorization(t *testing.T) {
	var files []string
	for i := range 120 {
		files = append(files, fmt.Sprintf("app/pages/p%d.tsx", i))
	}
	if r := authorization(globRun([]string{"app/**"}, nil, files...)); r.Status != "PASS" {
		t.Fatalf("120 generated files: %+v", r)
	}
	if r := authorization(globRun([]string{"app/**"}, []string{"**/.env"}, append(files, "app/.env")...)); r.Status != "FAIL" {
		t.Fatalf("protected .env passed: %+v", r)
	}
	if r := authorization(globRun([]string{"app/**"}, nil, "lib/x.ts")); r.Status != "FAIL" {
		t.Fatalf("change outside the glob passed: %+v", r)
	}
}

func TestGlobStillProtectsGeneratedFiles(t *testing.T) {
	f := globRun([]string{"**"}, nil)
	f.Run.FinalFiles[1].SHA256 = "changed"
	if r := authorization(f); r.Status != "FAIL" {
		t.Fatalf("changed TASK.txt passed under **: %+v", r)
	}
}

type messageVerifier struct{ message string }

func (m messageVerifier) Verify(context.Context, FrozenRun) (domain.Verification, []Blob) {
	return domain.Verification{Status: "ERROR", Reason: "TOOL_MISSING", Message: m.message}, nil
}

func TestVerificationMessageFreeTextCleared(t *testing.T) {
	kept := domain.CommandMessage(domain.StepVerifyExecutable, false, nil, "TOOL_MISSING", "")
	f := FrozenRun{AllowCommands: true, Case: FrozenCase{Case: domain.Case{Verification: &domain.VerificationConfig{Required: true}}}}
	for msg, want := range map[string]string{kept: kept, "harness says secret-answer": ""} {
		v, _ := Service{Verifier: messageVerifier{msg}}.verify(context.Background(), f)
		if v.Message != want {
			t.Errorf("message %q became %q", msg, v.Message)
		}
	}
}
