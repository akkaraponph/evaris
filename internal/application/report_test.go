package application

import (
	"context"
	"errors"
	"github.com/akkaraponph/evaris/internal/domain"
	"strings"
	"testing"
)

type reportRepo struct {
	Repository
	missing     bool
	writeError  bool
	comparisons []domain.Comparison
}

func (r reportRepo) LoadEvaluations(context.Context, string) ([]domain.Evaluation, []domain.Run, error) {
	es := []domain.Evaluation{}
	runs := []domain.Run{}
	for _, arm := range []string{"baseline", "specialist"} {
		if r.missing && arm == "specialist" {
			continue
		}
		id := arm
		runs = append(runs, domain.Run{ID: id, PreparationID: "prep", CaseID: "case", ArmID: arm, Origin: "synthetic-control"})
		es = append(es, domain.Evaluation{ID: id, RunID: id, Execution: "COMPLETE", AgentOutcome: "SATISFIED", Assessment: "PASS", Review: "NOT_REQUIRED", Graders: []domain.GraderResult{}})
	}
	return es, runs, nil
}
func (r reportRepo) LoadPreparations(context.Context, string) ([]domain.Preparation, error) {
	return []domain.Preparation{{ID: "prep", ExperimentID: "test", Tasks: []domain.PreparedTask{{CaseID: "case", ArmID: "baseline"}, {CaseID: "case", ArmID: "specialist"}}}}, nil
}
func (r reportRepo) LoadComparisons(context.Context, string) ([]domain.Comparison, error) {
	return r.comparisons, nil
}
func (r reportRepo) SaveReport(context.Context, string, domain.Report, []byte, []byte) error {
	if r.writeError {
		return errors.New("publication failed")
	}
	return nil
}
func TestReportComplete(t *testing.T) {
	s := Service{Repo: reportRepo{}, JSONReport: func(domain.Report) ([]byte, error) { return []byte("{}"), nil }, MarkdownReport: func(domain.Report) ([]byte, error) { return []byte("report"), nil }}
	r, e := s.Report(context.Background(), "store")
	if e != nil || len(r.Cases) != 2 {
		t.Fatal(r, e)
	}
	for _, row := range r.Cases {
		if row.Assessment != "PASS" {
			t.Fatal(row)
		}
	}
}
func TestReportIncompleteArms(t *testing.T) {
	s := Service{Repo: reportRepo{missing: true}, JSONReport: func(domain.Report) ([]byte, error) { return nil, nil }, MarkdownReport: func(domain.Report) ([]byte, error) { return nil, nil }}
	r, e := s.Report(context.Background(), "store")
	if e != nil || r.Cases[1].Assessment != "INCONCLUSIVE" || r.Cases[1].RunID != nil {
		t.Fatal(r, e)
	}
}
func TestReportWriteFailure(t *testing.T) {
	s := Service{Repo: reportRepo{writeError: true}, JSONReport: func(domain.Report) ([]byte, error) { return nil, nil }, MarkdownReport: func(domain.Report) ([]byte, error) { return nil, nil }}
	r, e := s.Report(context.Background(), "store")
	if e == nil || r.ID != "" {
		t.Fatal("announced unpublished report")
	}
}

type failingReportRepo struct{ reportRepo }

func (r failingReportRepo) LoadEvaluations(ctx context.Context, s string) ([]domain.Evaluation, []domain.Run, error) {
	es, runs, e := r.reportRepo.LoadEvaluations(ctx, s)
	es[1].Assessment, es[1].AgentOutcome = "FAIL", "VIOLATED"
	es[1].Graders = []domain.GraderResult{{CriterionID: "result", Required: true, Status: "FAIL"}, {CriterionID: "source-authorization", Required: true, Status: "PASS"}}
	return es, runs, e
}
func TestReportKnownFailure(t *testing.T) {
	s := Service{Repo: failingReportRepo{}, JSONReport: func(domain.Report) ([]byte, error) { return nil, nil }, MarkdownReport: func(domain.Report) ([]byte, error) { return nil, nil }}
	r, e := s.Report(context.Background(), "store")
	if e != nil {
		t.Fatal(e)
	}
	row := r.Cases[1]
	if row.Assessment != "FAIL" || len(row.FailedCriteria) != 1 || row.FailedCriteria[0] != "result" {
		t.Fatalf("%+v", row)
	}
	if len(r.Recommendations) != 1 || !strings.Contains(r.Recommendations[0], "result") || strings.Contains(strings.ToLower(r.Recommendations[0]), "improv") {
		t.Fatalf("recommendations must name only the failed gate: %v", r.Recommendations)
	}
}

type verifiedReportRepo struct {
	reportRepo
	status string
}

func (r verifiedReportRepo) LoadEvaluations(ctx context.Context, s string) ([]domain.Evaluation, []domain.Run, error) {
	es, runs, e := r.reportRepo.LoadEvaluations(ctx, s)
	es[0].Verification = &domain.Verification{Required: true, Status: r.status}
	return es, runs, e
}
func TestReportNamesVerificationGate(t *testing.T) {
	for status, want := range map[string]string{"REJECT": "failed", "REFUSE": "missing", "ERROR": "missing"} {
		s := Service{Repo: verifiedReportRepo{status: status}, JSONReport: func(domain.Report) ([]byte, error) { return nil, nil }, MarkdownReport: func(domain.Report) ([]byte, error) { return nil, nil }}
		r, e := s.Report(context.Background(), "store")
		if e != nil {
			t.Fatal(e)
		}
		row := r.Cases[0]
		list := row.MissingEvidence
		if want == "failed" {
			list = row.FailedCriteria
		}
		if len(list) != 1 || list[0] != "verification" || !strings.Contains(strings.Join(r.Recommendations, " "), "verification") {
			t.Errorf("%s: row %+v recommendations %v", status, row, r.Recommendations)
		}
	}
}
