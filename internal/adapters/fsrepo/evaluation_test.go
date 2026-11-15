package fsrepo

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
)

type passGrader struct{}

func (passGrader) Grade(_ context.Context, _ application.FrozenRun, c domain.Criterion) (domain.GraderResult, []application.Blob) {
	return domain.GraderResult{CriterionID: c.ID, Status: "PASS", Evidence: []domain.FileRef{}, GraderVersion: "builtin/1"}, nil
}

func evaluated(t *testing.T) (store string, run domain.Run, s application.Service) {
	t.Helper()
	config, store, p := prepared(t)
	run, e := (Repository{}).Import(context.Background(), store, request(t, p, config))
	if e != nil {
		t.Fatal(e)
	}
	s = application.Service{Repo: Repository{}, Grader: passGrader{}, JSONReport: func(r domain.Report) ([]byte, error) { return jsonBytes(r) }, MarkdownReport: func(domain.Report) ([]byte, error) { return []byte("report\n"), nil }}
	return store, run, s
}
func TestRepeatedEvaluationRetainsBoth(t *testing.T) {
	store, run, s := evaluated(t)
	first, e := s.Evaluate(context.Background(), store, run.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(filepath.Join(store, "evaluations", first.ID, "evaluation.json"))
	second, e := s.Evaluate(context.Background(), store, run.ID, false)
	if e != nil || second.ID == first.ID {
		t.Fatal("re-evaluation reused an ID", e)
	}
	after, _ := os.ReadFile(filepath.Join(store, "evaluations", first.ID, "evaluation.json"))
	es, _, e := (Repository{}).LoadEvaluations(context.Background(), store)
	if e != nil || len(es) != 2 || !bytes.Equal(before, after) {
		t.Fatalf("retained=%d err=%v changed=%v", len(es), e, !bytes.Equal(before, after))
	}
}
func TestReportCorruptEvidence(t *testing.T) {
	for _, target := range []string{"evaluation", "run"} {
		t.Run(target, func(t *testing.T) {
			store, run, s := evaluated(t)
			v, e := s.Evaluate(context.Background(), store, run.ID, false)
			if e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(store, "runs", run.ID, "response.txt")
			if target == "evaluation" {
				path = filepath.Join(store, "evaluations", v.ID, "evaluation.json")
			}
			b, _ := os.ReadFile(path)
			os.WriteFile(path, bytes.Replace(b, []byte("a"), []byte("b"), 1), 0600)
			r, e := s.Report(context.Background(), store)
			if e == nil || r.ID != "" {
				t.Fatalf("rendered a report over corrupt %s", target)
			}
			if entries, _ := os.ReadDir(filepath.Join(store, "reports")); len(entries) != 0 {
				t.Fatal("published report")
			}
		})
	}
}

func TestComparisonPublication(t *testing.T) {
	store, run, s := evaluated(t)
	v, e := s.Evaluate(context.Background(), store, run.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	c := domain.Comparison{SchemaVersion: "evaris.comparison/v1alpha1", ID: "0123456789abcdef0123456789abcdef", Mode: "arms", LeftEvaluationIDs: []string{v.ID}, RightEvaluationIDs: []string{}, CreatedAt: v.CreatedAt, Reasons: []string{"missing right"}, Pairs: []domain.Pair{}, Limitations: []string{}}
	if e = (Repository{}).SaveComparison(context.Background(), store, c); e != nil {
		t.Fatal(e)
	}
	if e = (Repository{}).SaveComparison(context.Background(), store, c); e == nil {
		t.Fatal("overwrote a comparison")
	}
	claim := c
	claim.ID = "0123456789abcdef0123456789abcd01"
	claim.Pairs = []domain.Pair{{CaseID: "sample", LeftID: v.ID, RightID: v.ID, Dimension: "assessment", Transition: "improved"}}
	if e = (Repository{}).SaveComparison(context.Background(), store, claim); e == nil {
		t.Fatal("saved transitions on a non-comparable result")
	}
	cs, e := (Repository{}).LoadComparisons(context.Background(), store)
	if e != nil || len(cs) != 1 || cs[0].ID != c.ID {
		t.Fatalf("%v %v", cs, e)
	}
	path := filepath.Join(store, "comparisons", c.ID, "comparison.json")
	b, _ := os.ReadFile(path)
	os.WriteFile(path, bytes.Replace(b, []byte(`"comparable": false`), []byte(`"comparable": true`), 1), 0600)
	if _, e = (Repository{}).LoadComparisons(context.Background(), store); e == nil {
		t.Fatal("accepted a tampered comparison")
	}
}
