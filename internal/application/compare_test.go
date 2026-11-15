package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/akkaraponph/evaris/internal/domain"
)

func p[T any](v T) *T { return &v }

type storeData struct {
	evals []domain.Evaluation
	runs  []domain.Run
	preps []domain.Preparation
}
type compareRepo struct {
	Repository
	stores map[string]*storeData
	saved  *[]domain.Comparison
}

func (r compareRepo) LoadEvaluations(_ context.Context, s string) ([]domain.Evaluation, []domain.Run, error) {
	return r.stores[s].evals, r.stores[s].runs, nil
}
func (r compareRepo) LoadPreparations(_ context.Context, s string) ([]domain.Preparation, error) {
	return r.stores[s].preps, nil
}
func (r compareRepo) SaveComparison(_ context.Context, _ string, c domain.Comparison) error {
	*r.saved = append(*r.saved, c)
	return nil
}
func (r compareRepo) LoadComparisons(context.Context, string) ([]domain.Comparison, error) {
	return *r.saved, nil
}
func (r compareRepo) SaveReport(context.Context, string, domain.Report, []byte, []byte) error {
	return nil
}

// trial adds one preparation with both arms evaluated under matched conditions.
func trial(d *storeData, prep string, baseline, specialist string, at time.Time) {
	d.preps = append(d.preps, domain.Preparation{ID: prep, ExperimentID: "example", Tasks: []domain.PreparedTask{{CaseID: "sample", ArmID: "baseline"}, {CaseID: "sample", ArmID: "specialist"}}})
	for _, arm := range []string{"baseline", "specialist"} {
		loaded, instr, result := []string{}, []domain.InstructionRef{}, baseline
		if arm == "specialist" {
			loaded, instr, result = []string{domain.InstructionWorkspacePath(0)}, []domain.InstructionRef{{Path: "i.txt", Revision: "r1", SHA256: "sha256:i"}}, specialist
		}
		settings := map[string]any{"temperature": 0}
		run := domain.Run{ID: prep + "-" + arm, PreparationID: prep, ExperimentID: "example", CaseID: "sample", ArmID: arm, TaskDigest: "t", FixtureDigest: "f", OracleDigest: "o", SourceRevision: "initial", Instructions: instr, Origin: "synthetic-control",
			Observed: domain.Observed{Host: p("h"), Model: p("m"), ModelSettings: &settings, Tools: &[]string{}, Permissions: p("w"), Budget: p("b"), ContextID: p(prep + "-" + arm), FreshContext: p(true), LoadedInstructions: &loaded}}
		d.runs = append(d.runs, run)
		d.evals = append(d.evals, domain.Evaluation{ID: run.ID + "-eval", RunID: run.ID, PreparationID: prep, CaseID: "sample", ArmID: arm, CreatedAt: at, Assessment: domain.AssessmentStatus(result), Graders: []domain.GraderResult{{CriterionID: "result", Required: true, Status: result, GraderVersion: "builtin/1"}}})
	}
}
func compareService(stores map[string]*storeData) (Service, *[]domain.Comparison) {
	saved := &[]domain.Comparison{}
	return Service{Repo: compareRepo{stores: stores, saved: saved}, JSONReport: func(domain.Report) ([]byte, error) { return nil, nil }, MarkdownReport: func(domain.Report) ([]byte, error) { return nil, nil }}, saved
}

func TestCompareArmsService(t *testing.T) {
	d := &storeData{}
	trial(d, "p1", "FAIL", "PASS", time.Now())
	s, saved := compareService(map[string]*storeData{"/store": d})
	c, e := s.Compare(context.Background(), "/store", "/store", "arms")
	if e != nil || !c.Comparable || len(*saved) != 1 || c.ID == "" || c.Regressed() {
		t.Fatalf("%+v %v", c, e)
	}
	if _, e = s.Compare(context.Background(), "/store", "/other", "arms"); e == nil {
		t.Fatal("arms mode across stores accepted")
	}
	if _, e = s.Compare(context.Background(), "/store", "/store", "trials"); e == nil {
		t.Fatal("unknown mode accepted")
	}
}

func TestCompareAmbiguousTrials(t *testing.T) {
	d := &storeData{}
	trial(d, "p1", "PASS", "PASS", time.Now())
	trial(d, "p2", "PASS", "FAIL", time.Now())
	s, saved := compareService(map[string]*storeData{"/store": d})
	c, e := s.Compare(context.Background(), "/store", "/store", "arms")
	if e != nil || c.Comparable || len(c.Pairs) != 0 || len(*saved) != 1 || !strings.Contains(strings.Join(c.Reasons, " "), "exactly one is required") {
		t.Fatalf("two preparations were paired: %+v %v", c, e)
	}
}

func TestReevaluationNotIndependent(t *testing.T) {
	d := &storeData{}
	now := time.Now()
	trial(d, "p1", "PASS", "PASS", now)
	// A later re-grading of the same specialist run changes the selected evaluation only.
	again := d.evals[1]
	again.ID, again.CreatedAt = "p1-specialist-eval2", now.Add(time.Second)
	again.Assessment, again.Graders = "FAIL", []domain.GraderResult{{CriterionID: "result", Required: true, Status: "FAIL", GraderVersion: "builtin/1"}}
	d.evals = append(d.evals, again)
	s, _ := compareService(map[string]*storeData{"/store": d})
	c, e := s.Compare(context.Background(), "/store", "/store", "arms")
	if e != nil || !c.Comparable || len(c.RightEvaluationIDs) != 1 || c.RightEvaluationIDs[0] != again.ID {
		t.Fatalf("selection: %+v %v", c, e)
	}
	if !strings.Contains(strings.Join(c.Limitations, " "), "not independent trials") {
		t.Fatal("re-evaluation not disclosed")
	}
}

func TestCompareRevisionsService(t *testing.T) {
	old, next := &storeData{}, &storeData{}
	trial(old, "p1", "PASS", "PASS", time.Now())
	trial(next, "p2", "PASS", "FAIL", time.Now())
	s, _ := compareService(map[string]*storeData{"/old": old, "/new": next})
	c, e := s.Compare(context.Background(), "/old", "/new", "revisions")
	if e != nil || !c.Comparable || !c.Regressed() {
		t.Fatalf("%+v %v", c, e)
	}
	next.evals = next.evals[:1] // specialist evaluation missing
	c, _ = s.Compare(context.Background(), "/old", "/new", "revisions")
	if c.Comparable || c.Regressed() || !strings.Contains(strings.Join(c.Reasons, " "), "no evaluated run") {
		t.Fatalf("missing evaluation: %+v", c)
	}
}

func TestReportComparisonSelection(t *testing.T) {
	d := &storeData{}
	trial(d, "p1", "FAIL", "PASS", time.Now())
	s, saved := compareService(map[string]*storeData{"/store": d})
	r, e := s.Report(context.Background(), "/store")
	if e != nil || r.Comparison != nil {
		t.Fatal("embedded a comparison that does not exist", e)
	}
	c, _ := s.Compare(context.Background(), "/store", "/store", "arms")
	stale := c
	stale.ID, stale.LeftEvaluationIDs = "stale", []string{"other-eval"}
	stale.CreatedAt = c.CreatedAt.Add(time.Hour)
	*saved = append(*saved, stale)
	r, e = s.Report(context.Background(), "/store")
	if e != nil || r.Comparison == nil || r.Comparison.ID != c.ID {
		t.Fatalf("selected %+v", r.Comparison)
	}
	// A newer evaluation changes the selection; the old comparison no longer matches.
	again := d.evals[0]
	again.ID, again.CreatedAt = "p1-baseline-eval2", again.CreatedAt.Add(time.Minute)
	d.evals = append(d.evals, again)
	r, _ = s.Report(context.Background(), "/store")
	if r.Comparison != nil || !strings.Contains(strings.Join(r.Limitations, " "), "comparison is null") {
		t.Fatal("embedded a comparison over superseded evaluations")
	}
}
