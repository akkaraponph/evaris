package application

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/akkaraponph/evaris/internal/domain"
)

type runRepo struct {
	Repository
	f     FrozenRun
	saved *domain.Evaluation
}

func (r runRepo) LoadRun(context.Context, string, string) (FrozenRun, error) { return r.f, nil }
func (r runRepo) SaveEvaluation(_ context.Context, _ string, v domain.Evaluation, _ []Blob) error {
	*r.saved = v
	return nil
}

type statusGrader string

func (g statusGrader) Grade(_ context.Context, _ FrozenRun, c domain.Criterion) (domain.GraderResult, []Blob) {
	return domain.GraderResult{CriterionID: c.ID, Status: string(g), Evidence: []domain.FileRef{}, GraderVersion: "builtin/1"}, nil
}

type fakeVerifier struct {
	v     domain.Verification
	blobs []Blob
	panic bool
	calls *int
}

func (f fakeVerifier) Verify(context.Context, FrozenRun) (domain.Verification, []Blob) {
	*f.calls++
	if f.panic {
		panic("verifier crashed")
	}
	return f.v, f.blobs
}

func verifiedRun() FrozenRun {
	return FrozenRun{Run: domain.Run{ID: "run", CaseID: "c", ArmID: "baseline"}, Case: FrozenCase{Case: domain.Case{ID: "c", AllowedChanges: []string{}, Verification: &domain.VerificationConfig{Harness: "h.json", Required: true, TimeoutSeconds: 5}}, Oracle: domain.Oracle{Criteria: []domain.Criterion{{ID: "result", Kind: "exact", Required: true}}}}, Preparation: domain.Preparation{Tasks: []domain.PreparedTask{{CaseID: "c", ArmID: "baseline", InitialFiles: domain.Manifest{}, GeneratedFiles: domain.Manifest{}}}}}
}
func accepted() (domain.Verification, []Blob) {
	data := []byte(`{"decision":"ACCEPT"}`)
	s, run, d := "invaris.evidence/v1alpha2", "0123456789abcdef0123456789abcdef", "sha256:x"
	zero := 0
	return domain.Verification{Status: "ACCEPT", SchemaVersion: &s, RunID: &run, ConfigDigest: &d, ExitCode: &zero, Evidence: []domain.FileRef{{Path: "evidence/verification/stdout.json", SHA256: domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(data))), Bytes: int64(len(data))}}, Findings: []domain.Finding{}}, []Blob{{Path: "evidence/verification/stdout.json", Data: data}}
}
func evaluateWith(t *testing.T, grader string, verifier Verifier, allow bool) domain.Evaluation {
	t.Helper()
	var saved domain.Evaluation
	s := Service{Repo: runRepo{f: verifiedRun(), saved: &saved}, Grader: statusGrader(grader), Verifier: verifier}
	v, e := s.Evaluate(context.Background(), "store", "run", allow)
	if e != nil || saved.ID != v.ID {
		t.Fatal(e)
	}
	return v
}

func TestInvarisAcceptNotTaskSuccess(t *testing.T) {
	calls := 0
	v, b := accepted()
	e := evaluateWith(t, "FAIL", fakeVerifier{v: v, blobs: b, calls: &calls}, true)
	if e.Verification == nil || e.Verification.Status != "ACCEPT" || e.Assessment != "FAIL" || e.AgentOutcome != "VIOLATED" {
		t.Fatalf("ACCEPT masked a failed criterion: %+v", e)
	}
	if e = evaluateWith(t, "PASS", fakeVerifier{v: v, blobs: b, calls: &calls}, true); e.Assessment != "PASS" || e.Verification.Status != "ACCEPT" {
		t.Fatalf("%+v", e)
	}
}

func TestVerifierNormalization(t *testing.T) {
	good, blobs := accepted()
	forged := good
	forged.Evidence = []domain.FileRef{{Path: "evidence/verification/stdout.json", SHA256: "sha256:00", Bytes: 1}}
	noExit := good
	noExit.ExitCode = nil
	bad := good
	bad.Status = "PASS"
	outside := good
	outside.Evidence = []domain.FileRef{{Path: "tree/result.txt", SHA256: good.Evidence[0].SHA256, Bytes: good.Evidence[0].Bytes}}
	for name, tc := range map[string]struct {
		verifier Verifier
		allow    bool
		status   string
		reason   domain.Reason
		calls    int
	}{
		"no consent":      {fakeVerifier{v: good, blobs: blobs, calls: new(int)}, false, "UNAVAILABLE", "COMMAND_NOT_AUTHORIZED", 0},
		"no verifier":     {nil, true, "ERROR", "TOOL_MISSING", -1},
		"panic":           {fakeVerifier{panic: true, calls: new(int)}, true, "ERROR", "INVALID_GRADER_RESULT", 1},
		"invalid status":  {fakeVerifier{v: bad, blobs: blobs, calls: new(int)}, true, "ERROR", "INVALID_GRADER_RESULT", 1},
		"forged evidence": {fakeVerifier{v: forged, blobs: blobs, calls: new(int)}, true, "ERROR", "INVALID_GRADER_RESULT", 1},
		"outside prefix":  {fakeVerifier{v: outside, blobs: []Blob{{Path: "tree/result.txt", Data: blobs[0].Data}}, calls: new(int)}, true, "ERROR", "INVALID_GRADER_RESULT", 1},
		"accept no exit":  {fakeVerifier{v: noExit, blobs: blobs, calls: new(int)}, true, "ERROR", "INVALID_GRADER_RESULT", 1},
	} {
		e := evaluateWith(t, "PASS", tc.verifier, tc.allow)
		if e.Verification == nil || e.Verification.Status != tc.status || e.Verification.Reason != tc.reason || e.Assessment == "PASS" || !e.Verification.Required {
			t.Errorf("%s: %+v", name, e.Verification)
		}
		if f, ok := tc.verifier.(fakeVerifier); ok && *f.calls != tc.calls {
			t.Errorf("%s: verifier calls %d", name, *f.calls)
		}
	}
}

func TestNoVerificationConfiguredNoLookup(t *testing.T) {
	calls := 0
	var saved domain.Evaluation
	f := verifiedRun()
	f.Case.Case.Verification = nil
	s := Service{Repo: runRepo{f: f, saved: &saved}, Grader: statusGrader("PASS"), Verifier: fakeVerifier{calls: &calls}}
	v, e := s.Evaluate(context.Background(), "store", "run", true)
	if e != nil || v.Verification != nil || calls != 0 || v.Assessment != "PASS" {
		t.Fatalf("%+v %v calls=%d", v, e, calls)
	}
}
