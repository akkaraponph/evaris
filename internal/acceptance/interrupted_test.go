package acceptance

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/akkaraponph/evaris/internal/adapters/fsrepo"
	"github.com/akkaraponph/evaris/internal/adapters/graders"
	"github.com/akkaraponph/evaris/internal/adapters/render"
	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
)

func TestWorkflowInterruptedAndCorrupt(t *testing.T) {
	h := newHarness(t)
	project := h.project("../../examples/offline", "project")
	var prep domain.Preparation
	h.must(0, &prep, "prepare", "--experiment", filepath.Join(project, "experiment.json"), "--tasks-root", filepath.Join(h.root, "tasks"))
	store := filepath.Join(project, "evidence")
	baseline := h.control(store, prep, prep.Tasks[0], "approved\n", domain.Observed{})

	// An uncommitted evaluation claiming PASS must be ignored, never counted.
	fake := filepath.Join(store, "evaluations", "0123456789abcdef0123456789abcdef")
	os.MkdirAll(fake, 0700)
	os.WriteFile(filepath.Join(fake, "evaluation.json"), []byte(`{"runId":"`+baseline.ID+`","assessment":"PASS"}`), 0600)

	// Cancellation during grading is persisted as CANCELED, never PASS.
	specialist := h.control(store, prep, prep.Tasks[1], "approved\n", domain.Observed{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := application.Service{Repo: fsrepo.Repository{}, Grader: cancelingGrader{cancel}, JSONReport: render.JSON, MarkdownReport: render.Markdown}
	v, e := s.Evaluate(ctx, store, specialist.ID, false)
	if e != nil || v.Execution != "CANCELED" || v.Assessment == "PASS" {
		t.Fatalf("canceled evaluation: %+v %v", v, e)
	}

	var report domain.Report
	h.must(2, &report, "report", "--store", store)
	for _, row := range report.Cases {
		if row.Assessment == "PASS" {
			t.Fatalf("false success: %+v", row)
		}
		if row.ArmID == "baseline" && row.EvaluationID != nil {
			t.Fatal("uncommitted evaluation was adopted")
		}
	}

	// A truncated committed record is corruption: exit 2, nothing on stdout.
	path := filepath.Join(store, "runs", baseline.ID, "run.json")
	b, _ := os.ReadFile(path)
	os.WriteFile(path, b[:len(b)/2], 0600)
	if code, out := h.run(nil, "evaluate", "--store", store, "--run", baseline.ID); code != 2 || len(out) != 0 {
		t.Fatalf("evaluate over truncated run: %d %s", code, out)
	}
	if code, out := h.run(nil, "report", "--store", store); code != 2 || len(out) != 0 {
		t.Fatalf("report over truncated run: %d %s", code, out)
	}
}

// cancelingGrader interrupts the evaluation after the run is loaded, mid-grading.
type cancelingGrader struct{ cancel context.CancelFunc }

func (g cancelingGrader) Grade(ctx context.Context, f application.FrozenRun, c domain.Criterion) (domain.GraderResult, []application.Blob) {
	g.cancel()
	return graders.Builtin{}.Grade(ctx, f, c)
}
