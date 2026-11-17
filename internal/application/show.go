package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/akkaraponph/evaris/internal/domain"
)

// showOutputBytes bounds each stream Show prints.
const showOutputBytes = 4096

// Show describes one committed evaluation as text: every criterion's status,
// reason, message and evidence files, and for command-exit criteria and
// verification the retained stdout and stderr, bounded and escaped. criterion
// narrows it to one criterion ("verification" names the verification). It
// reads only; tampered evidence is refused.
func (s Service) Show(ctx context.Context, store, evaluationID, criterion string) (string, error) {
	es, _, err := s.Repo.LoadEvaluations(ctx, store)
	if err != nil {
		return "", err
	}
	var v *domain.Evaluation
	for i := range es {
		if es[i].ID == evaluationID {
			v = &es[i]
		}
	}
	if v == nil {
		return "", &domain.Failure{Code: "UNKNOWN_EVALUATION", Path: evaluationID}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "evaluation %s: case %s, arm %s, run %s\n", v.ID, v.CaseID, v.ArmID, v.RunID)
	fmt.Fprintf(&b, "assessment %s (execution %s, agent outcome %s)\n", v.Assessment, v.Execution, v.AgentOutcome)
	found := false
	header := func(id string, required bool, status string, reason domain.Reason, message string) {
		found = true
		req := "optional"
		if required {
			req = "required"
		}
		fmt.Fprintf(&b, "\ncriterion %s (%s): %s\n", id, req, status)
		if reason != "" {
			fmt.Fprintf(&b, "  reason: %s\n", reason)
		}
		if message != "" {
			fmt.Fprintf(&b, "  message: %s\n", message)
		}
	}
	for _, g := range v.Graders {
		if criterion != "" && g.CriterionID != criterion {
			continue
		}
		header(g.CriterionID, g.Required, g.Status, g.Reason, g.Message)
		for _, ref := range g.Evidence {
			fmt.Fprintf(&b, "  evidence: %s (%s, %d bytes)\n", ref.Path, ref.SHA256, ref.Bytes)
			data, err := s.Repo.ReadEvaluationEvidence(ctx, store, v.ID, ref.Path)
			if err != nil {
				return "", err
			}
			var ev struct {
				Process struct{ Stdout, Stderr []byte }
			}
			if json.Unmarshal(data, &ev) == nil {
				stream(&b, "stdout", ev.Process.Stdout)
				stream(&b, "stderr", ev.Process.Stderr)
			}
		}
	}
	if x := v.Verification; x != nil && (criterion == "" || criterion == "verification") {
		header("verification", x.Required, x.Status, x.Reason, x.Message)
		for _, ref := range x.Evidence {
			fmt.Fprintf(&b, "  evidence: %s (%s, %d bytes)\n", ref.Path, ref.SHA256, ref.Bytes)
			if strings.HasSuffix(ref.Path, "/stdout.json") || strings.HasSuffix(ref.Path, "/stderr.txt") {
				data, err := s.Repo.ReadEvaluationEvidence(ctx, store, v.ID, ref.Path)
				if err != nil {
					return "", err
				}
				stream(&b, ref.Path[strings.LastIndex(ref.Path, "/")+1:], data)
			}
		}
	}
	if !found {
		return "", &domain.Failure{Code: "INVALID_CONFIG", Path: "unknown criterion " + criterion}
	}
	return b.String(), nil
}

// stream prints up to showOutputBytes of data, indented, with bytes that are
// not printable UTF-8 shown as \xNN.
func stream(b *strings.Builder, name string, data []byte) {
	fmt.Fprintf(b, "  %s (%d bytes):\n", name, len(data))
	if len(data) == 0 {
		return
	}
	shown := data
	if len(shown) > showOutputBytes {
		shown = shown[:showOutputBytes]
	}
	var out strings.Builder
	for len(shown) > 0 {
		r, size := utf8.DecodeRune(shown)
		switch {
		case r == '\n' || r == '\t':
			out.WriteRune(r)
		case r == utf8.RuneError && size <= 1, r < 0x20, r == 0x7f:
			fmt.Fprintf(&out, "\\x%02x", shown[0])
		default:
			out.WriteRune(r)
		}
		shown = shown[size:]
	}
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		b.WriteString("    " + line + "\n")
	}
	if len(data) > showOutputBytes {
		fmt.Fprintf(b, "    (truncated, %d bytes total)\n", len(data))
	}
}
