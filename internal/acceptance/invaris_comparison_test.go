package acceptance

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akkaraponph/evaris/internal/domain"
)

func ptr[T any](v T) *T { return &v }

// controlMetadata is authored for synthetic controls and labeled as such; it is
// not an observation of any agent host.
func controlMetadata(arm, contextID string) domain.Observed {
	settings := map[string]any{"syntheticControl": true}
	loaded := []string{}
	if arm == "specialist" {
		loaded = []string{domain.InstructionWorkspacePath(0)}
	}
	return domain.Observed{Host: ptr("synthetic-control (no agent host)"), Model: ptr("none: synthetic control"), ModelSettings: &settings, Tools: &[]string{}, Permissions: ptr("workspace"), Budget: ptr("none"), ContextID: ptr(contextID), FreshContext: ptr(true), LoadedInstructions: &loaded, AgentClaim: ptr("unknown")}
}

type p1 struct {
	*harness
	invaris string // absolute executable passed to --invaris
	guard   string
}

// verifiedProject copies the offline example and adds a required Invaris check.
// instruction and extraCriterion vary the revision and oracle between projects.
func (h *p1) verifiedProject(name, instruction string, extraCriterion bool) (project, store string) {
	h.t.Helper()
	project = h.project("../../examples/offline", name)
	tmpl, e := os.ReadFile("../../testdata/invaris/harness.json.tmpl")
	if e != nil {
		h.t.Fatal(e)
	}
	os.WriteFile(filepath.Join(project, "reviewer", "invaris.json"), []byte(strings.ReplaceAll(string(tmpl), "@GUARD@", h.guard)), 0600)
	b, _ := os.ReadFile(filepath.Join(project, "experiment.json"))
	var m map[string]any
	json.Unmarshal(b, &m)
	m["cases"].([]any)[0].(map[string]any)["verification"] = map[string]any{"harness": "reviewer/invaris.json", "required": true, "timeoutSeconds": 60}
	if instruction != "" {
		os.WriteFile(filepath.Join(project, "instruction.txt"), []byte(instruction), 0600)
		m["arms"].([]any)[1].(map[string]any)["instructions"].([]any)[0].(map[string]any)["revision"] = "r2"
	}
	b, _ = json.Marshal(m)
	os.WriteFile(filepath.Join(project, "experiment.json"), b, 0600)
	if extraCriterion {
		oracle := filepath.Join(project, "reviewer", "oracle.json")
		b, _ = os.ReadFile(oracle)
		var o map[string]any
		json.Unmarshal(b, &o)
		o["criteria"] = append(o["criteria"].([]any), map[string]any{"id": "result-exists", "kind": "exists", "required": false, "path": "result.txt"})
		b, _ = json.Marshal(o)
		os.WriteFile(oracle, b, 0600)
	}
	return project, filepath.Join(project, "evidence")
}

// trial prepares one project and imports the given controls for both arms.
func (h *p1) trial(project, store, baseline, specialist, tag string) (domain.Preparation, map[string]domain.Run) {
	h.t.Helper()
	var prep domain.Preparation
	h.must(0, nil, "validate", "--experiment", filepath.Join(project, "experiment.json"))
	h.must(0, &prep, "prepare", "--experiment", filepath.Join(project, "experiment.json"), "--tasks-root", filepath.Join(h.root, "tasks-"+tag))
	runs := map[string]domain.Run{}
	for _, task := range prep.Tasks {
		content := map[string]string{"baseline": baseline, "specialist": specialist}[task.ArmID]
		runs[task.ArmID] = h.control(store, prep, task, content, controlMetadata(task.ArmID, tag+"-"+task.ArmID))
	}
	return prep, runs
}
func (h *p1) evaluate(code int, store, runID string, extra ...string) domain.Evaluation {
	h.t.Helper()
	var v domain.Evaluation
	h.must(code, &v, append([]string{"evaluate", "--store", store, "--run", runID}, extra...)...)
	return v
}
func (h *p1) verified(code int, store, runID string) domain.Evaluation {
	return h.evaluate(code, store, runID, "--allow-commands", "--invaris", h.invaris)
}

func setupP1(t *testing.T, invaris func(h *harness, harnessDigest func(string) string) string) *p1 {
	h := &p1{harness: newHarness(t)}
	h.guard = filepath.Join(h.root, "guard")
	if b, e := exec.Command("go", "build", "-o", h.guard, "../../testdata/invaris/guard").CombinedOutput(); e != nil {
		t.Fatalf("build guard: %v %s", e, b)
	}
	digest := func(guard string) string {
		tmpl, _ := os.ReadFile("../../testdata/invaris/harness.json.tmpl")
		return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(strings.ReplaceAll(string(tmpl), "@GUARD@", guard))))
	}
	h.invaris = invaris(h.harness, digest)
	return h
}

// fakeInvaris replays evidence recorded from the pinned binary, choosing the
// decision from result.txt like the original guard. It is not compatibility evidence.
func fakeInvaris(body string) func(h *harness, digest func(string) string) string {
	return func(h *harness, digest func(string) string) string {
		dir, _ := filepath.Abs("../../testdata/invaris")
		guard := filepath.Join(h.root, "guard")
		script := fmt.Sprintf(`#!/bin/sh
d=$(dirname "$2")
case "$(cat "$d/result.txt" 2>/dev/null)" in
approved) n=accept; code=0 ;;
rejected) n=reject; code=1 ;;
*) n=refuse; code=2 ;;
esac
%s
sed -e "s#@TARGET@#$d#g" -e "s#@DIGEST@#%s#g" -e "s#@GUARD@#%s#g" "%s/evidence-$n.json.tmpl"
exit $code
`, body, digest(guard), guard, dir)
		path := filepath.Join(h.root, "fake-invaris-"+fmt.Sprint(len(body)))
		os.WriteFile(path, []byte(script), 0700)
		return path
	}
}

// p1Workflow runs the full P1 journey; the verifier is real or a recorded fake.
func p1Workflow(t *testing.T, h *p1) {
	// Trial 1: baseline writes the bad control, specialist the good one.
	project, store := h.verifiedProject("rev1", "", false)
	_, runs := h.trial(project, store, "rejected\n", "approved\n", "rev1")

	// Missing Invaris and malformed evidence are distinct non-pass outcomes; they
	// are re-gradings of the same run, retained but superseded by the final one.
	missing := h.evaluate(2, store, runs["specialist"].ID, "--allow-commands")
	if missing.Assessment != "INCONCLUSIVE" || missing.Execution != "ERROR" || missing.Verification == nil || missing.Verification.Status != "ERROR" || missing.Verification.Reason != "TOOL_MISSING" {
		t.Fatalf("missing tool: %+v", missing.Verification)
	}
	unauthorized := h.evaluate(2, store, runs["specialist"].ID, "--invaris", h.invaris)
	if unauthorized.Verification.Status != "UNAVAILABLE" || unauthorized.Assessment != "INCONCLUSIVE" {
		t.Fatalf("ran without consent: %+v", unauthorized.Verification)
	}
	broken := filepath.Join(h.root, "broken-invaris")
	os.WriteFile(broken, []byte("#!/bin/sh\necho '{\"schemaVersion\":\"invaris.evidence/v1alpha2\",\"decision\":\"ACCEPT\"}'\nexit 0\n"), 0700)
	var malformed domain.Evaluation
	h.must(2, &malformed, "evaluate", "--store", store, "--run", runs["specialist"].ID, "--allow-commands", "--invaris", broken)
	if malformed.Verification.Status != "ERROR" || malformed.Verification.Reason != "EVIDENCE_CORRUPT" || malformed.Assessment != "INCONCLUSIVE" || len(malformed.Verification.Evidence) != 3 {
		t.Fatalf("malformed evidence: %+v", malformed.Verification)
	}
	for _, ref := range malformed.Verification.Evidence {
		if _, e := os.Stat(filepath.Join(store, "evaluations", malformed.ID, ref.Path)); e != nil {
			t.Fatalf("evidence reference not retained: %s", ref.Path)
		}
	}

	bad := h.verified(1, store, runs["baseline"].ID)
	good := h.verified(0, store, runs["specialist"].ID)
	if bad.Verification.Status != "REJECT" || bad.Assessment != "FAIL" || good.Verification.Status != "ACCEPT" || good.Assessment != "PASS" {
		t.Fatalf("verified outcomes: bad=%+v good=%+v", bad.Verification, good.Verification)
	}

	var arms domain.Comparison
	h.must(0, &arms, "compare", "--left", store, "--right", store, "--mode", "arms")
	if !arms.Comparable || !strings.Contains(strings.Join(arms.Limitations, " "), "Synthetic-control") {
		t.Fatalf("arms comparison: %+v", arms)
	}
	transitions := map[string]string{}
	for _, p := range arms.Pairs {
		transitions[p.Dimension] = p.Transition
	}
	if transitions["assessment"] != "improved" || transitions["verification"] != "improved" || transitions["criterion:result"] != "improved" {
		t.Fatalf("transitions: %v", transitions)
	}
	if !strings.Contains(strings.Join(arms.Limitations, " "), "not independent trials") {
		t.Fatal("re-evaluations not disclosed")
	}

	var report domain.Report
	h.must(1, &report, "report", "--store", store)
	if report.Comparison == nil || report.Comparison.ID != arms.ID {
		t.Fatalf("report comparison: %+v", report.Comparison)
	}
	md, e := os.ReadFile(filepath.Join(store, "reports", report.ID, "report.md"))
	if e != nil || !strings.Contains(string(md), "| sample | assessment | improved |") {
		t.Fatalf("Markdown comparison missing: %v", e)
	}

	// Trial 2, new instruction revision: specialist now writes the bad control.
	project2, store2 := h.verifiedProject("rev2", "Revised instruction: keep result.txt minimal.\n", false)
	_, runs2 := h.trial(project2, store2, "rejected\n", "rejected\n", "rev2")
	h.verified(1, store2, runs2["baseline"].ID)
	h.verified(1, store2, runs2["specialist"].ID)
	var revisions domain.Comparison
	h.must(1, &revisions, "compare", "--left", store, "--right", store2, "--mode", "revisions")
	regressed := false
	for _, p := range revisions.Pairs {
		regressed = regressed || p.Dimension == "assessment" && p.Transition == "regressed"
	}
	if !revisions.Comparable || !regressed {
		t.Fatalf("matched revision regression not observed: %+v", revisions)
	}

	// Trial 3 changes the oracle: a changed rubric blocks the regression claim.
	project3, store3 := h.verifiedProject("rev3", "Revised instruction: keep result.txt minimal.\n", true)
	_, runs3 := h.trial(project3, store3, "rejected\n", "rejected\n", "rev3")
	h.verified(1, store3, runs3["baseline"].ID)
	h.verified(1, store3, runs3["specialist"].ID)
	var changed domain.Comparison
	h.must(2, &changed, "compare", "--left", store, "--right", store3, "--mode", "revisions")
	if changed.Comparable || len(changed.Pairs) != 0 || !strings.Contains(strings.Join(changed.Reasons, " "), "oracle changed") {
		t.Fatalf("changed oracle compared: %+v", changed)
	}

	// Refusal: an unrecognized control makes the guard refuse; Evaris stays inconclusive.
	project4, store4 := h.verifiedProject("refuse", "", false)
	_, runs4 := h.trial(project4, store4, "unrecognized\n", "approved\n", "refuse")
	refused := h.verified(1, store4, runs4["baseline"].ID)
	if refused.Verification.Status != "REFUSE" || refused.Verification.Reason != "VERIFIER_REFUSED" {
		t.Fatalf("refusal: %+v", refused.Verification)
	}
	// The exact criterion independently fails; REFUSE alone would be INCONCLUSIVE.
	if refused.Assessment != "FAIL" {
		t.Fatalf("refusal hid the independent failure: %s", refused.Assessment)
	}
}

// TestP1FakeVerifierWorkflow always runs. It replays recorded pinned-binary
// output through a fake executable: it checks Evaris's handling, not Invaris.
func TestP1FakeVerifierWorkflow(t *testing.T) {
	p1Workflow(t, setupP1(t, fakeInvaris("")))
}

// TestP1OfflineIntegration needs a separately provisioned Invaris binary built
// from the pinned revision; otherwise real compatibility is not established.
func TestP1OfflineIntegration(t *testing.T) {
	exe := os.Getenv("EVARIS_TEST_INVARIS")
	if exe == "" {
		t.Skip("EVARIS_TEST_INVARIS unset: real Invaris integration NOT verified (blocks the P1 release gate)")
	}
	p1Workflow(t, setupP1(t, func(*harness, func(string) string) string { return exe }))
}
