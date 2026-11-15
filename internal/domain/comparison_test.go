package domain

import (
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

// matched returns a run and evaluation with fully observed, matching conditions.
func matched(id, prep, arm, ctx, assessment, result string) (Run, Evaluation) {
	settings := map[string]any{"temperature": 0, "maxTokens": 1000}
	loaded := []string{}
	instructions := []InstructionRef{}
	if arm == "specialist" {
		loaded = []string{InstructionWorkspacePath(0)}
		instructions = []InstructionRef{{Path: "instruction.txt", Revision: "r1", SHA256: "sha256:i1"}}
	}
	r := Run{ID: "run-" + id, PreparationID: prep, ExperimentID: "example", CaseID: "sample", ArmID: arm, TaskDigest: "sha256:t", FixtureDigest: "sha256:f", OracleDigest: "sha256:o", SourceRevision: "initial", Instructions: instructions, Origin: "external-agent",
		Observed: Observed{Host: ptr("host"), Model: ptr("model-a"), ModelSettings: &settings, Tools: &[]string{"shell", "edit"}, Permissions: ptr("workspace"), Budget: ptr("20 turns"), ContextID: ptr(ctx), FreshContext: ptr(true), LoadedInstructions: &loaded}}
	e := Evaluation{ID: "eval-" + id, RunID: r.ID, PreparationID: prep, CaseID: "sample", ArmID: arm, Assessment: AssessmentStatus(assessment), Graders: []GraderResult{{CriterionID: "result", Required: true, Status: result, GraderVersion: "builtin/1"}, {CriterionID: "source-authorization", Required: true, Status: "PASS", GraderVersion: "builtin/1"}}}
	return r, e
}
func pairOf(c Comparison, dim string) string {
	for _, p := range c.Pairs {
		if p.Dimension == dim {
			return p.Transition
		}
	}
	return "missing"
}
func hasReason(c Comparison, part string) bool {
	for _, r := range c.Reasons {
		if strings.Contains(r, part) {
			return true
		}
	}
	return false
}

func TestCompareMatchedArms(t *testing.T) {
	lr, le := matched("b", "p1", "baseline", "ctx-1", "FAIL", "FAIL")
	rr, re := matched("s", "p1", "specialist", "ctx-2", "PASS", "PASS")
	c, e := Compare([]Evaluation{le}, []Evaluation{re}, []Run{lr, rr}, "arms")
	if e != nil || !c.Comparable || len(c.Reasons) != 0 {
		t.Fatalf("%v %+v", e, c.Reasons)
	}
	if pairOf(c, "assessment") != "improved" || pairOf(c, "criterion:result") != "improved" || pairOf(c, "criterion:source-authorization") != "unchanged" || c.Regressed() || !c.Known() {
		t.Fatalf("%+v", c.Pairs)
	}
	if !strings.Contains(strings.Join(c.Limitations, " "), "not statistically established") {
		t.Fatal("single-trial limitation missing")
	}
	if _, e = Compare(nil, nil, nil, "both"); e == nil {
		t.Fatal("accepted unknown mode")
	}
}

func TestCompareChangedConditions(t *testing.T) {
	for name, edit := range map[string]func(r *Run, e *Evaluation){
		"model":       func(r *Run, _ *Evaluation) { r.Observed.Model = ptr("model-b") },
		"settings":    func(r *Run, _ *Evaluation) { s := map[string]any{"temperature": 1}; r.Observed.ModelSettings = &s },
		"tools":       func(r *Run, _ *Evaluation) { r.Observed.Tools = &[]string{"shell"} },
		"permissions": func(r *Run, _ *Evaluation) { r.Observed.Permissions = ptr("network") },
		"budget":      func(r *Run, _ *Evaluation) { r.Observed.Budget = ptr("5 turns") },
		"host":        func(r *Run, _ *Evaluation) { r.Observed.Host = ptr("other") },
		"task":        func(r *Run, _ *Evaluation) { r.TaskDigest = "sha256:t2" },
		"fixture":     func(r *Run, _ *Evaluation) { r.FixtureDigest = "sha256:f2" },
		"oracle":      func(r *Run, _ *Evaluation) { r.OracleDigest = "sha256:o2" },
		"source":      func(r *Run, _ *Evaluation) { r.SourceRevision = "next" },
		"grader":      func(_ *Run, e *Evaluation) { e.Graders[0].GraderVersion = "builtin/2" },
		"origin":      func(r *Run, _ *Evaluation) { r.Origin = "synthetic-control" },
		"experiment":  func(r *Run, _ *Evaluation) { r.ExperimentID = "other" },
		"preparation": func(r *Run, e *Evaluation) { r.PreparationID, e.PreparationID = "p2", "p2" },
		"verification": func(_ *Run, e *Evaluation) {
			e.Verification = &Verification{Required: true, Status: "ACCEPT"}
		},
	} {
		lr, le := matched("b", "p1", "baseline", "ctx-1", "PASS", "PASS")
		rr, re := matched("s", "p1", "specialist", "ctx-2", "FAIL", "FAIL")
		edit(&rr, &re)
		c, e := Compare([]Evaluation{le}, []Evaluation{re}, []Run{lr, rr}, "arms")
		if e != nil || c.Comparable || len(c.Reasons) == 0 || len(c.Pairs) != 0 || c.Regressed() {
			t.Errorf("%s: comparable=%v reasons=%v pairs=%d", name, c.Comparable, c.Reasons, len(c.Pairs))
		}
	}
	// Set-like tools and key order in settings are normalized, not changes.
	lr, le := matched("b", "p1", "baseline", "ctx-1", "PASS", "PASS")
	rr, re := matched("s", "p1", "specialist", "ctx-2", "PASS", "PASS")
	rr.Observed.Tools = &[]string{"edit", "shell"}
	if c, _ := Compare([]Evaluation{le}, []Evaluation{re}, []Run{lr, rr}, "arms"); !c.Comparable {
		t.Fatalf("tool order treated as a change: %v", c.Reasons)
	}
}

func TestCompareContaminatedBaseline(t *testing.T) {
	for name, edit := range map[string]func(b, s *Run){
		"baseline loaded":          func(b, _ *Run) { b.Observed.LoadedInstructions = &[]string{InstructionWorkspacePath(0)} },
		"baseline loaded unknown":  func(b, _ *Run) { b.Observed.LoadedInstructions = nil },
		"fresh unknown":            func(b, _ *Run) { b.Observed.FreshContext = nil },
		"not fresh":                func(_, s *Run) { s.Observed.FreshContext = ptr(false) },
		"duplicate context":        func(b, s *Run) { s.Observed.ContextID = b.Observed.ContextID },
		"specialist loaded other":  func(_, s *Run) { s.Observed.LoadedInstructions = &[]string{"notes.md"} },
		"specialist loaded none":   func(_, s *Run) { s.Observed.LoadedInstructions = &[]string{} },
		"specialist without instr": func(_, s *Run) { s.Instructions = []InstructionRef{} },
	} {
		lr, le := matched("b", "p1", "baseline", "ctx-1", "PASS", "PASS")
		rr, re := matched("s", "p1", "specialist", "ctx-2", "PASS", "PASS")
		edit(&lr, &rr)
		if c, _ := Compare([]Evaluation{le}, []Evaluation{re}, []Run{lr, rr}, "arms"); c.Comparable {
			t.Errorf("%s: treated as comparable", name)
		}
	}
}

func TestCompareMissingMetadataAndArm(t *testing.T) {
	lr, le := matched("b", "p1", "baseline", "ctx-1", "FAIL", "FAIL")
	rr, re := matched("s", "p1", "specialist", "ctx-2", "PASS", "PASS")
	rr.Observed.Model = nil
	c, _ := Compare([]Evaluation{le}, []Evaluation{re}, []Run{lr, rr}, "arms")
	if c.Comparable || !hasReason(c, "model not observed") || pairOf(c, "assessment") == "improved" {
		t.Fatalf("missing metadata produced a claim: %+v", c)
	}
	c, _ = Compare([]Evaluation{le}, nil, []Run{lr}, "arms")
	if c.Comparable || !hasReason(c, "missing right evaluation") || len(c.Pairs) != 0 {
		t.Fatalf("missing arm produced a claim: %+v", c)
	}
}

func TestRevisionRegression(t *testing.T) {
	lr, le := matched("old", "p1", "specialist", "ctx-1", "PASS", "PASS")
	rr, re := matched("new", "p2", "specialist", "ctx-2", "FAIL", "FAIL")
	rr.Instructions = []InstructionRef{{Path: "instruction.txt", Revision: "r2", SHA256: "sha256:i2"}}
	c, e := Compare([]Evaluation{le}, []Evaluation{re}, []Run{lr, rr}, "revisions")
	if e != nil || !c.Comparable || pairOf(c, "assessment") != "regressed" || !c.Regressed() {
		t.Fatalf("matched regression not observed: %v %+v", e, c)
	}
	// Changing the rubric is a changed condition, not an observed regression.
	rr.OracleDigest = "sha256:o2"
	c, _ = Compare([]Evaluation{le}, []Evaluation{re}, []Run{lr, rr}, "revisions")
	if c.Comparable || c.Regressed() || !hasReason(c, "oracle changed") {
		t.Fatalf("changed rubric reported as regression: %+v", c)
	}
	// Verification harness identity must be known and equal across revisions.
	rr.OracleDigest = lr.OracleDigest
	le.Verification = &Verification{Required: true, Status: "ACCEPT", ConfigDigest: ptr("sha256:h1")}
	re.Verification = &Verification{Required: true, Status: "REJECT", ConfigDigest: ptr("sha256:h2")}
	c, _ = Compare([]Evaluation{le}, []Evaluation{re}, []Run{lr, rr}, "revisions")
	if c.Comparable || !hasReason(c, "verification harness changed") {
		t.Fatalf("changed harness hidden: %+v", c.Reasons)
	}
	re.Verification.ConfigDigest = ptr("sha256:h1")
	c, _ = Compare([]Evaluation{le}, []Evaluation{re}, []Run{lr, rr}, "revisions")
	if !c.Comparable || pairOf(c, "verification") != "regressed" {
		t.Fatalf("verification transition: %+v", c)
	}
}

func TestErrorIsUnknownNotRegression(t *testing.T) {
	lr, le := matched("b", "p1", "baseline", "ctx-1", "PASS", "PASS")
	rr, re := matched("s", "p1", "specialist", "ctx-2", "INCONCLUSIVE", "ERROR")
	c, _ := Compare([]Evaluation{le}, []Evaluation{re}, []Run{lr, rr}, "arms")
	if !c.Comparable || c.Regressed() || pairOf(c, "assessment") != "unknown" || pairOf(c, "criterion:result") != "unknown" || c.Known() {
		t.Fatalf("tool error counted as agent regression: %+v", c.Pairs)
	}
}

func TestReevaluationNotIndependentDomain(t *testing.T) {
	lr, le := matched("b", "p1", "baseline", "ctx-1", "PASS", "PASS")
	rr, re := matched("s", "p1", "specialist", "ctx-2", "PASS", "PASS")
	again := le
	again.ID = "eval-b2"
	c, _ := Compare([]Evaluation{le, again}, []Evaluation{re}, []Run{lr, rr}, "arms")
	if c.Comparable || !hasReason(c, "re-evaluations are not independent trials") {
		t.Fatalf("re-evaluation counted as a trial: %+v", c.Reasons)
	}
	other, oe := matched("b3", "p1", "baseline", "ctx-3", "PASS", "PASS")
	c, _ = Compare([]Evaluation{le, oe}, []Evaluation{re}, []Run{lr, rr, other}, "arms")
	if c.Comparable || !hasReason(c, "multiple trials are ambiguous") {
		t.Fatalf("ambiguous trials paired: %+v", c.Reasons)
	}
	le.RunID = "unknown-run"
	if _, e := Compare([]Evaluation{le}, []Evaluation{re}, []Run{rr}, "arms"); e == nil {
		t.Fatal("evaluation without its run accepted")
	}
}
