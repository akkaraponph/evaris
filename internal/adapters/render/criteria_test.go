package render

import (
	"strings"
	"testing"

	"github.com/akkaraponph/evaris/internal/domain"
)

func TestReportCriteriaTable(t *testing.T) {
	r := domain.Report{SchemaVersion: domain.ReportSchemaVersion, ExperimentID: "x", Cases: []domain.CaseSummary{
		{CaseID: "c", ArmID: "baseline", Criteria: []domain.CriterionSummary{
			{ID: "result", Required: true, Status: "PASS"},
			{ID: "build", Required: true, Status: "ERROR", Reason: "LIMIT_EXCEEDED", Message: "scratch snapshot after the command (command exited 0): LIMIT_EXCEEDED (entries)"},
		}},
		{CaseID: "c", ArmID: "specialist", Criteria: []domain.CriterionSummary{}},
	}}
	b, err := Markdown(r)
	if err != nil {
		t.Fatal(err)
	}
	md := string(b)
	for _, want := range []string{
		"## Criteria",
		"### c / baseline",
		"| Criterion | Required | Status | Reason | Message |",
		"| result | true | PASS |  |  |",
		"| build | true | ERROR | LIMIT_EXCEEDED | scratch snapshot after the command &#40;command exited 0&#41;: LIMIT_EXCEEDED &#40;entries&#41; |",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %q in:\n%s", want, md)
		}
	}
	if strings.Contains(md, "### c / specialist") {
		t.Fatal("rendered a heading for a case without criteria")
	}
}
