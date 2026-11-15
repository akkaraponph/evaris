package render

import (
	"bytes"
	"github.com/akkaraponph/evaris/internal/domain"
	"testing"
)

func TestStableRendering(t *testing.T) {
	r := domain.Report{ExperimentID: "<script>[x](secret)", Cases: []domain.CaseSummary{{Execution: "ERROR", AgentOutcome: "VIOLATED", Assessment: "FAIL"}}, Limitations: []string{"<img src=x>"}}
	a, _ := JSON(r)
	b, _ := JSON(r)
	if !bytes.Equal(a, b) {
		t.Fatal("unstable JSON")
	}
	a, _ = Markdown(r)
	b, _ = Markdown(r)
	if !bytes.Equal(a, b) || bytes.Contains(a, []byte("<script>")) || bytes.Contains(a, []byte("[x](secret)")) || !bytes.Contains(a, []byte("ERROR")) || !bytes.Contains(a, []byte("VIOLATED")) {
		t.Fatal("unsafe/incomplete Markdown")
	}
}

func TestReportEscapesUntrustedText(t *testing.T) {
	hostile := "<script>alert(1)</script>|[link](http://x)\n# heading`code`"
	r := domain.Report{ExperimentID: hostile, Cases: []domain.CaseSummary{{CaseID: hostile, ArmID: "baseline", FailedCriteria: []string{hostile}, MissingEvidence: []string{}}}, Limitations: []string{hostile}, Recommendations: []string{hostile}, Evidence: []domain.FileRef{{Path: hostile}}}
	md, e := Markdown(r)
	if e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{"<script", "](", "\n# heading", "`", "|[link"} {
		if bytes.Contains(md, []byte(bad)) {
			t.Fatalf("unescaped %q in %s", bad, md)
		}
	}
	if !bytes.Contains(md, []byte("Recommendations")) {
		t.Fatal("recommendations omitted from Markdown")
	}
}
func TestReportNoSecretExcerpts(t *testing.T) {
	// Reports carry IDs, criteria and references only; response/oracle bytes never appear.
	r := domain.Report{ExperimentID: "example", Cases: []domain.CaseSummary{{CaseID: "c", ArmID: "baseline", FailedCriteria: []string{"result"}, MissingEvidence: []string{}}}, Evidence: []domain.FileRef{{Path: "evaluations/x/evaluation.json", SHA256: "sha256:00"}}, Limitations: []string{}, Recommendations: []string{}}
	for _, render := range []func(domain.Report) ([]byte, error){JSON, Markdown} {
		b, e := render(r)
		if e != nil || bytes.Contains(b, []byte("ORACLE_SECRET_42")) || bytes.Contains(b, []byte("approved")) {
			t.Fatalf("secret excerpt or error: %v %s", e, b)
		}
	}
}

func TestComparisonRendering(t *testing.T) {
	c := &domain.Comparison{Mode: "arms", Comparable: true, Reasons: []string{}, Pairs: []domain.Pair{{CaseID: "sample", Dimension: "assessment", Transition: "regressed", LeftID: "l", RightID: "r"}}, Limitations: []string{"<b>single trial</b>"}}
	md, _ := Markdown(domain.Report{Comparison: c})
	j, _ := JSON(domain.Report{Comparison: c})
	if !bytes.Contains(md, []byte("| sample | assessment | regressed | l | r |")) || bytes.Contains(md, []byte("<b>")) || !bytes.Contains(j, []byte(`"transition": "regressed"`)) {
		t.Fatalf("comparison missing or unsafe:\n%s", md)
	}
	none, _ := Markdown(domain.Report{})
	if !bytes.Contains(none, []byte("## Comparison\n\nNone.")) {
		t.Fatal("absent comparison not stated")
	}
}
