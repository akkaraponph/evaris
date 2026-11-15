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
	fmt.Fprintf(&b, "\nSchema: %s; version: %s; config: %s\n", safe(r.SchemaVersion), safe(r.EvarisVersion), safe(string(r.ConfigDigest)))
	b.WriteString("\n## Limitations\n\n")
	for _, s := range r.Limitations {
		fmt.Fprintf(&b, "- %s\n", safe(s))
	}
	b.WriteString("\n## Evidence\n\n")
	for _, ref := range r.Evidence {
		fmt.Fprintf(&b, "- %s (%s)\n", safe(ref.Path), safe(string(ref.SHA256)))
	}
	return []byte(b.String()), nil
}
