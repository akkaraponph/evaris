package render

import (
	"fmt"
	"github.com/akkaraponph/evaris/internal/domain"
	"strings"
)

func safe(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./ :", r) {
			b.WriteRune(r)
		} else {
			fmt.Fprintf(&b, "&#%d;", r)
		}
	}
	return b.String()
}
func Markdown(r domain.Report) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# Evaris report\n\nExperiment: %s\n\n| Case | Arm | Execution | Agent outcome | Assessment | Failed criteria | Missing evidence |\n| --- | --- | --- | --- | --- | --- | --- |\n", safe(r.ExperimentID))
	for _, c := range r.Cases {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n", safe(c.CaseID), safe(c.ArmID), safe(string(c.Execution)), safe(string(c.AgentOutcome)), safe(string(c.Assessment)), safe(strings.Join(c.FailedCriteria, ", ")), safe(strings.Join(c.MissingEvidence, ", ")))
	}
	b.WriteString("\n## Criteria\n")
	any := false
	for _, c := range r.Cases {
		if len(c.Criteria) == 0 {
			continue
		}
		any = true
		fmt.Fprintf(&b, "\n### %s / %s\n\n| Criterion | Required | Status | Reason | Message |\n| --- | --- | --- | --- | --- |\n", safe(c.CaseID), safe(c.ArmID))
		for _, g := range c.Criteria {
			fmt.Fprintf(&b, "| %s | %t | %s | %s | %s |\n", safe(g.ID), g.Required, safe(g.Status), safe(string(g.Reason)), safe(g.Message))
		}
		for _, g := range c.Criteria {
			if len(g.Campaign) == 0 {
				continue
			}
			b.WriteString("\n| Campaign experiment | Kind | Stability | Verdict |\n| --- | --- | --- | --- |\n")
			for _, item := range g.Campaign {
				fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", safe(item.ID), safe(item.Kind), safe(item.Stability), safe(item.Verdict))
			}
		}
	}
	if !any {
		b.WriteString("\nNo evaluated cases.\n")
	}
	fmt.Fprintf(&b, "\nSchema: %s; version: %s; config: %s\n", safe(r.SchemaVersion), safe(r.EvarisVersion), safe(string(r.ConfigDigest)))
	b.WriteString("\n## Limitations\n\n")
	for _, s := range r.Limitations {
		fmt.Fprintf(&b, "- %s\n", safe(s))
	}
	b.WriteString("\n## Comparison\n\n")
	if r.Mode == domain.ModeAcceptance {
		b.WriteString("Not applicable: acceptance mode.\n")
	} else if c := r.Comparison; c == nil {
		b.WriteString("None.\n")
	} else {
		fmt.Fprintf(&b, "Mode: %s; comparable: %t; ID: %s\n\n", safe(c.Mode), c.Comparable, safe(c.ID))
		for _, s := range c.Reasons {
			fmt.Fprintf(&b, "- Not comparable: %s\n", safe(s))
		}
		if len(c.Pairs) > 0 {
			b.WriteString("\n| Case | Dimension | Transition | Left evaluation | Right evaluation |\n| --- | --- | --- | --- | --- |\n")
			for _, p := range c.Pairs {
				fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", safe(p.CaseID), safe(p.Dimension), safe(p.Transition), safe(p.LeftID), safe(p.RightID))
			}
		}
		for _, s := range c.Limitations {
			fmt.Fprintf(&b, "- %s\n", safe(s))
		}
	}
	b.WriteString("\n## Recommendations\n\n")
	for _, s := range r.Recommendations {
		fmt.Fprintf(&b, "- %s\n", safe(s))
	}
	b.WriteString("\n## Evidence\n\n")
	for _, ref := range r.Evidence {
		fmt.Fprintf(&b, "- %s (%s)\n", safe(ref.Path), safe(string(ref.SHA256)))
	}
	return []byte(b.String()), nil
}
