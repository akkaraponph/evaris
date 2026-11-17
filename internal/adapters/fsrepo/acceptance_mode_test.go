package fsrepo

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akkaraponph/evaris/internal/adapters/render"
	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
)

// acceptanceConfig rewrites the valid experiment into acceptance mode with the
// given arms.
func acceptanceConfig(t *testing.T, arms []map[string]any) (config, tasks string) {
	t.Helper()
	config, tasks = fixture(t)
	b, _ := os.ReadFile(config)
	var exp map[string]any
	json.Unmarshal(b, &exp)
	exp["mode"] = "acceptance"
	exp["arms"] = arms
	b, _ = json.Marshal(exp)
	os.WriteFile(config, b, 0o600)
	return config, tasks
}

func TestAcceptanceModeOneArm(t *testing.T) {
	config, tasks := acceptanceConfig(t, []map[string]any{{"id": "candidate", "instructions": []any{map[string]any{"path": "instruction.txt", "revision": "initial"}}}})
	plan, e := (Repository{}).Load(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	p, e := (Repository{}).Prepare(context.Background(), plan, tasks)
	if e != nil || p.Mode != domain.ModeAcceptance || len(p.Tasks) != 1 || p.Tasks[0].ArmID != "candidate" {
		t.Fatalf("prepare %+v %v", p, e)
	}
	os.WriteFile(filepath.Join(p.TasksRoot, p.Tasks[0].Workspace, "result.txt"), []byte("approved\n"), 0o600)
	run, e := (Repository{}).Import(context.Background(), plan.OutputRoot, request(t, p, config))
	if e != nil {
		t.Fatal(e)
	}
	s := application.Service{Repo: Repository{}, Grader: passGrader{}, JSONReport: func(r domain.Report) ([]byte, error) { return jsonBytes(r) }, MarkdownReport: render.Markdown}
	if _, e := s.Evaluate(context.Background(), plan.OutputRoot, run.ID, false); e != nil {
		t.Fatal(e)
	}
	r, e := s.Report(context.Background(), plan.OutputRoot)
	if e != nil {
		t.Fatal(e)
	}
	if r.Mode != domain.ModeAcceptance || r.Comparison != nil || len(r.Cases) != 1 || r.Cases[0].Assessment == "INCONCLUSIVE" {
		t.Fatalf("report %+v", r)
	}
	for _, l := range append(r.Limitations, r.Recommendations...) {
		if strings.Contains(l, "comparison") {
			t.Fatalf("comparison noise: %q", l)
		}
	}
	md, _ := render.Markdown(r)
	if !strings.Contains(string(md), "Not applicable: acceptance mode.") {
		t.Fatalf("markdown:\n%s", md)
	}
	if _, e := s.Compare(context.Background(), plan.OutputRoot, plan.OutputRoot, "revisions"); e == nil || !strings.Contains(e.Error(), "acceptance mode has no arms to compare") {
		t.Fatalf("compare: %v", e)
	}
}

func TestAcceptanceModeRejectsTwoArms(t *testing.T) {
	config, _ := acceptanceConfig(t, []map[string]any{{"id": "a", "instructions": []any{}}, {"id": "b", "instructions": []any{}}})
	if _, e := (Repository{}).Load(context.Background(), config); e == nil {
		t.Fatal("two arms accepted in acceptance mode")
	}
	config, _ = fixture(t)
	b, _ := os.ReadFile(config)
	var exp map[string]any
	json.Unmarshal(b, &exp)
	exp["mode"] = "solo"
	b, _ = json.Marshal(exp)
	os.WriteFile(config, b, 0o600)
	if _, e := (Repository{}).Load(context.Background(), config); e == nil {
		t.Fatal("unknown mode accepted")
	}
}
