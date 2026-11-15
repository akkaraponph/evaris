package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/akkaraponph/evaris/internal/domain"
	"strings"
	"time"
)

func ID() (string, error) {
	b := make([]byte, 16)
	_, e := rand.Read(b)
	return hex.EncodeToString(b), e
}
func (s Service) Evaluate(ctx context.Context, store, id string, allow bool) (out domain.Evaluation, err error) {
	f, e := s.Repo.LoadRun(ctx, store, id)
	if e != nil {
		return out, e
	}
	f.AllowCommands = allow
	eid, e := ID()
	if e != nil {
		return out, e
	}
	out = domain.Evaluation{SchemaVersion: "evaris.evaluation/v1alpha1", ID: eid, RunID: id, PreparationID: f.Run.PreparationID, ExperimentID: f.Run.ExperimentID, CaseID: f.Run.CaseID, ArmID: f.Run.ArmID, RunDigest: f.RunDigest, OracleDigest: f.Run.OracleDigest, CreatedAt: time.Now().UTC(), Review: "NOT_REQUIRED", Graders: []domain.GraderResult{}, Limitations: append([]string{}, f.Run.Limitations...)}
	if f.Case.Case.RequiresReview {
		out.Review = "PENDING"
	}
	blobs := []Blob{}
	for _, c := range f.Case.Oracle.Criteria {
		r, b := s.grade(ctx, f, c)
		out.Graders = append(out.Graders, r)
		blobs = append(blobs, b...)
	}
	out.Graders = append(out.Graders, authorization(f))
	a := domain.Assess(out.Graders, out.Review, nil)
	out.Execution = a.Execution
	out.AgentOutcome = a.AgentOutcome
	out.Assessment = a.Final
	// Persist cancellation evidence even after grading's context is canceled.
	if e = s.Repo.SaveEvaluation(context.WithoutCancel(ctx), store, out, blobs); e != nil {
		return domain.Evaluation{}, e
	}
	return out, nil
}
func (s Service) grade(ctx context.Context, f FrozenRun, c domain.Criterion) (r domain.GraderResult, b []Blob) {
	bad := func() {
		r = domain.GraderResult{CriterionID: c.ID, Required: c.Required, Status: "ERROR", Reason: "INVALID_GRADER_RESULT", Evidence: []domain.FileRef{}, GraderVersion: "builtin/1"}
		b = nil
	}
	defer func() {
		if recover() != nil {
			bad()
		}
	}()
	if s.Grader == nil {
		bad()
		return
	}
	r, b = s.Grader.Grade(ctx, f, c)
	if r.GraderVersion != "builtin/1" || r.CriterionID != c.ID || r.Status != "PASS" && r.Status != "FAIL" && r.Status != "UNAVAILABLE" && r.Status != "ERROR" {
		bad()
	}
	for _, ref := range r.Evidence {
		found := false
		for _, blob := range b {
			if blob.Path == ref.Path {
				found = true
				if !domain.Relative(blob.Path) || !strings.HasPrefix(blob.Path, "evidence/") || domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(blob.Data))) != ref.SHA256 || int64(len(blob.Data)) != ref.Bytes {
					bad()
					return
				}
			}
		}
		if !found {
			bad()
			return
		}
	}
	r.Required = c.Required
	r.Message = ""
	if r.Evidence == nil {
		r.Evidence = []domain.FileRef{}
	}
	return
}
func authorization(f FrozenRun) domain.GraderResult {
	r := domain.GraderResult{CriterionID: "source-authorization", Required: true, Status: "PASS", Evidence: []domain.FileRef{}, GraderVersion: "builtin/1"}
	initial := map[string]domain.FileRef{}
	protected := map[string]bool{}
	for _, t := range f.Preparation.Tasks {
		if t.CaseID == f.Run.CaseID && t.ArmID == f.Run.ArmID {
			for _, ref := range append(append(domain.Manifest{}, t.InitialFiles...), t.GeneratedFiles...) {
				initial[ref.Path] = ref
			}
			for _, ref := range t.GeneratedFiles {
				protected[ref.Path] = true
			}
		}
	}
	allowed := map[string]bool{}
	for _, p := range f.Case.Case.AllowedChanges {
		allowed[p] = true
	}
	actual := map[string]domain.FileRef{}
	for _, ref := range f.Run.FinalFiles {
		actual[strings.TrimPrefix(ref.Path, "tree/")] = ref
	}
	keys := map[string]bool{}
	for p := range initial {
		keys[p] = true
	}
	for p := range actual {
		keys[p] = true
	}
	for p := range keys {
		a, ok := initial[p]
		b, present := actual[p]
		if ok != present || a.SHA256 != b.SHA256 || a.Executable != b.Executable {
			if !allowed[p] || protected[p] {
				r.Status = "FAIL"
				r.Reason = "ASSERTION_FAILED"
			}
		}
	}
	return r
}
func operation(e error) error {
	if e == nil {
		return nil
	}
	if _, ok := e.(*domain.Failure); ok {
		return e
	}
	return &domain.Failure{Code: "IO_ERROR", Path: fmt.Sprint("operation"), Err: e}
}
