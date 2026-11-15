package application

import (
	"context"
	"errors"
	"github.com/akkaraponph/evaris/internal/domain"
	"testing"
)

type reportRepo struct {
	Repository
	missing    bool
	writeError bool
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
