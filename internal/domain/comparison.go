package domain

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"time"
)

// Pair is one observed transition for one case and dimension. Left is the
// baseline (arms mode) or the earlier revision (revisions mode).
type Pair struct {
	CaseID     string `json:"caseId"`
	LeftID     string `json:"leftId"`
	RightID    string `json:"rightId"`
	Dimension  string `json:"dimension"`
	Transition string `json:"transition"`
}

// Comparison records whether two evaluation sets were comparable and, only if
// so, their per-dimension transitions. It carries no composite quality score.
type Comparison struct {
	SchemaVersion      string    `json:"schemaVersion"`
	ID                 string    `json:"id"`
	Mode               string    `json:"mode"`
	LeftEvaluationIDs  []string  `json:"leftEvaluationIds"`
	RightEvaluationIDs []string  `json:"rightEvaluationIds"`
	CreatedAt          time.Time `json:"createdAt"`
	Comparable         bool      `json:"comparable"`
	Reasons            []string  `json:"reasons"`
	Pairs              []Pair    `json:"pairs"`
	Limitations        []string  `json:"limitations"`
}

// Transitions are the only valid Pair.Transition values.
var Transitions = map[string]bool{"improved": true, "regressed": true, "unchanged": true, "unknown": true}

// InstructionWorkspacePath is where prepared specialist instruction i appears.
func InstructionWorkspacePath(i int) string { return fmt.Sprintf(".evaris-instructions/%03d.txt", i) }

// Regressed reports an observed comparable regression on any dimension.
func (c Comparison) Regressed() bool {
	for _, p := range c.Pairs {
		if p.Transition == "regressed" {
			return true
		}
	}
	return false
}

// Known reports a comparable result with no unknown transitions.
func (c Comparison) Known() bool {
	if !c.Comparable {
		return false
	}
	for _, p := range c.Pairs {
		if p.Transition == "unknown" {
			return false
		}
	}
	return true
}

type side struct {
	name  string
	evals map[string]Evaluation
	runs  map[string]Run
}

// Compare checks matched conditions before pairing. Arms mode pairs baseline
// (left) with specialist (right) per case; revisions mode pairs the same case and
// arm across two revisions. Any changed or unobserved condition, missing
// counterpart, ambiguous trial or reused context makes the result not comparable
// with explicit reasons, and then no transitions are emitted.
func Compare(left, right []Evaluation, runs []Run, mode string) (Comparison, error) {
	c := Comparison{SchemaVersion: "evaris.comparison/v1alpha1", Mode: mode, LeftEvaluationIDs: []string{}, RightEvaluationIDs: []string{}, Reasons: []string{}, Pairs: []Pair{}, Limitations: []string{}}
	if mode != "arms" && mode != "revisions" {
		return c, &Failure{Code: "INVALID_CONFIG", Path: "mode"}
	}
	byID := map[string]Run{}
	for _, r := range runs {
		byID[r.ID] = r
	}
	reason := func(format string, a ...any) { c.Reasons = append(c.Reasons, fmt.Sprintf(format, a...)) }
	build := func(name string, es []Evaluation, ids *[]string) (side, error) {
		s := side{name: name, evals: map[string]Evaluation{}, runs: map[string]Run{}}
		for _, e := range es {
			r, ok := byID[e.RunID]
			if !ok || r.CaseID != e.CaseID || r.ArmID != e.ArmID || r.PreparationID != e.PreparationID {
				return s, &Failure{Code: "EVIDENCE_CORRUPT", Path: "comparison run " + e.RunID}
			}
			*ids = append(*ids, e.ID)
			key := e.CaseID
			if mode == "revisions" {
				key += "/" + e.ArmID
			} else if want := map[string]string{"left": "baseline", "right": "specialist"}[name]; e.ArmID != want {
				reason("%s side contains arm %s; arms mode compares baseline (left) with specialist (right)", name, e.ArmID)
				continue
			}
			if prev, dup := s.evals[key]; dup {
				if prev.RunID == e.RunID {
					reason("%s side has several evaluations of run %s; re-evaluations are not independent trials", name, e.RunID)
				} else {
					reason("%s side has several runs for %s; multiple trials are ambiguous and are not paired", name, key)
				}
				continue
			}
			s.evals[key], s.runs[key] = e, r
		}
		return s, nil
	}
	l, err := build("left", left, &c.LeftEvaluationIDs)
	if err != nil {
		return c, err
	}
	r, err := build("right", right, &c.RightEvaluationIDs)
	if err != nil {
		return c, err
	}
	sort.Strings(c.LeftEvaluationIDs)
	sort.Strings(c.RightEvaluationIDs)
	keys := map[string]bool{}
	for k := range l.evals {
		keys[k] = true
	}
	for k := range r.evals {
		keys[k] = true
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	if len(ordered) == 0 {
		reason("no evaluations to compare")
	}
	contexts := map[string]string{}
	synthetic := false
	pairs := []Pair{}
	for _, k := range ordered {
		le, lok := l.evals[k]
		re, rok := r.evals[k]
		if !lok || !rok {
			missing := "left"
			if lok {
				missing = "right"
			}
			reason("%s: missing %s evaluation; an absent arm is incomplete, not an improvement", k, missing)
			continue
		}
		lr, rr := l.runs[k], r.runs[k]
		for _, why := range conditions(lr, rr, le, re, mode) {
			reason("%s: %s", k, why)
		}
		for _, run := range []Run{lr, rr} {
			synthetic = synthetic || run.Origin == "synthetic-control"
			if id := run.Observed.ContextID; id != nil {
				if other, seen := contexts[*id]; seen && other != run.ID {
					reason("%s: context ID reused by runs %s and %s; contexts are not independent", k, other, run.ID)
				}
				contexts[*id] = run.ID
			}
		}
		pairs = append(pairs, transitions(lr.CaseID, le, re)...)
	}
	if len(c.Reasons) == 0 {
		c.Comparable = true
		c.Pairs = pairs
	}
	sort.Strings(c.Reasons)
	c.Reasons = slices.Compact(c.Reasons)
	c.Limitations = append(c.Limitations, "One trial per case and arm: transitions are observations, not statistically established effects.")
	if synthetic {
		c.Limitations = append(c.Limitations, "Synthetic-control runs establish grader behavior only, not agent or model performance.")
	}
	c.Limitations = append(c.Limitations, "Observed metadata is operator-reported and not attested by Evaris.")
	return c, nil
}

// conditions lists every changed or unobserved comparability condition.
func conditions(l, r Run, le, re Evaluation, mode string) []string {
	out := []string{}
	add := func(cond bool, why string) {
		if cond {
			out = append(out, why)
		}
	}
	add(l.ExperimentID != r.ExperimentID, "experiment changed")
	add(l.CaseID != r.CaseID, "case changed")
	add(l.TaskDigest != r.TaskDigest, "task changed")
	add(l.FixtureDigest != r.FixtureDigest, "fixture changed")
	add(l.OracleDigest != r.OracleDigest, "oracle changed")
	add(l.SourceRevision != r.SourceRevision, "source revision changed")
	add(l.Origin != r.Origin, "origin differs")
	add(!sameGraders(le.Graders, re.Graders), "grader or criteria changed")
	switch {
	case (le.Verification == nil) != (re.Verification == nil):
		out = append(out, "verification configuration changed")
	case le.Verification != nil:
		add(le.Verification.Required != re.Verification.Required, "verification requirement changed")
		a, b := le.Verification, re.Verification
		finished := func(v *Verification) bool {
			return v.Status == "ACCEPT" || v.Status == "REJECT" || v.Status == "REFUSE"
		}
		if finished(a) && finished(b) {
			add(a.ConfigDigest == nil || b.ConfigDigest == nil, "verification harness identity unknown")
			add(a.ConfigDigest != nil && b.ConfigDigest != nil && *a.ConfigDigest != *b.ConfigDigest, "verification harness changed")
			add(a.InputsDigest == nil || b.InputsDigest == nil, "verification inputs identity unknown")
			add(a.InputsDigest != nil && b.InputsDigest != nil && *a.InputsDigest != *b.InputsDigest, "verification inputs changed")
			add(a.Executable == nil || b.Executable == nil, "verification executable identity unknown")
			add(a.Executable != nil && b.Executable != nil && a.Executable.SHA256 != b.Executable.SHA256, "verification executable changed")
		}
	}
	lo, ro := l.Observed, r.Observed
	text := func(name string, a, b *string) {
		add(a == nil || b == nil, name+" not observed")
		add(a != nil && b != nil && *a != *b, name+" changed")
	}
	text("host", lo.Host, ro.Host)
	text("model", lo.Model, ro.Model)
	text("permissions", lo.Permissions, ro.Permissions)
	text("budget", lo.Budget, ro.Budget)
	add(lo.ModelSettings == nil || ro.ModelSettings == nil, "model settings not observed")
	if lo.ModelSettings != nil && ro.ModelSettings != nil {
		a, _ := json.Marshal(*lo.ModelSettings)
		b, _ := json.Marshal(*ro.ModelSettings)
		add(string(a) != string(b), "model settings changed")
	}
	add(lo.Tools == nil || ro.Tools == nil, "tools not observed")
	if lo.Tools != nil && ro.Tools != nil {
		add(!sameSet(*lo.Tools, *ro.Tools), "tools changed")
	}
	add(lo.ContextID == nil || ro.ContextID == nil, "context ID not observed")
	add(lo.FreshContext == nil || ro.FreshContext == nil || !*lo.FreshContext || !*ro.FreshContext, "fresh context not observed")
	if mode == "arms" {
		add(l.ArmID != "baseline" || r.ArmID != "specialist", "arms mode requires baseline (left) and specialist (right)")
		add(l.PreparationID != r.PreparationID, "arms come from different preparations")
	} else {
		add(l.ArmID != r.ArmID, "arm changed")
	}
	for _, run := range []Run{l, r} {
		if why := instructionTreatment(run); why != "" {
			out = append(out, why)
		}
	}
	return out
}

// instructionTreatment checks that observed loaded instructions match the arm's
// declared treatment: none for baseline, exactly the prepared files for specialist.
func instructionTreatment(r Run) string {
	loaded := r.Observed.LoadedInstructions
	if loaded == nil {
		return r.ArmID + " loaded instructions not observed"
	}
	if r.ArmID == "baseline" {
		if len(*loaded) != 0 || len(r.Instructions) != 0 {
			return "baseline contaminated: instructions loaded"
		}
		return ""
	}
	want := []string{}
	for i := range r.Instructions {
		want = append(want, InstructionWorkspacePath(i))
	}
	if len(want) == 0 || !sameSet(*loaded, want) {
		return "specialist loaded instructions do not match the declared treatment"
	}
	return ""
}

func sameGraders(a, b []GraderResult) bool {
	key := func(gs []GraderResult) []string {
		out := []string{}
		for _, g := range gs {
			out = append(out, fmt.Sprintf("%s|%s|%t", g.CriterionID, g.GraderVersion, g.Required))
		}
		sort.Strings(out)
		return out
	}
	return slices.Equal(key(a), key(b))
}

// sameSet compares set-like lists independent of order; duplicates differ.
func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	sort.Strings(x)
	sort.Strings(y)
	return slices.Equal(x, y)
}

// transitions compares completed outcomes only; anything else is unknown.
func transitions(caseID string, l, r Evaluation) []Pair {
	pair := func(dim, a, b, good, bad string) Pair {
		t := "unknown"
		switch {
		case (a == good || a == bad) && a == b:
			t = "unchanged"
		case a == bad && b == good:
			t = "improved"
		case a == good && b == bad:
			t = "regressed"
		}
		return Pair{CaseID: caseID, LeftID: l.ID, RightID: r.ID, Dimension: dim, Transition: t}
	}
	out := []Pair{pair("assessment", string(l.Assessment), string(r.Assessment), "PASS", "FAIL")}
	status := func(gs []GraderResult) map[string]string {
		m := map[string]string{}
		for _, g := range gs {
			m[g.CriterionID] = g.Status
		}
		return m
	}
	ls, rs := status(l.Graders), status(r.Graders)
	ids := []string{}
	for id := range ls {
		ids = append(ids, id)
	}
	for id := range rs {
		if _, ok := ls[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		out = append(out, pair("criterion:"+id, ls[id], rs[id], "PASS", "FAIL"))
	}
	if l.Verification != nil || r.Verification != nil {
		a, b := "", ""
		if l.Verification != nil {
			a = l.Verification.Status
		}
		if r.Verification != nil {
			b = r.Verification.Status
		}
		out = append(out, pair("verification", a, b, "ACCEPT", "REJECT"))
	}
	return out
}
