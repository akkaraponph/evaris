package application

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/akkaraponph/evaris/internal/domain"
	"strings"
	"time"
)

// Report rendering is injected as pure functions, keeping application imports inward.

func (s Service) Report(ctx context.Context, store string) (out domain.Report, err error) {
	es, runs, e := s.Repo.LoadEvaluations(ctx, store)
	if e != nil {
		return out, e
	}
	ps, e := s.Repo.LoadPreparations(ctx, store)
	if e != nil {
		return out, e
	}
	id, e := ID()
	if e != nil {
		return out, e
	}
	out = domain.Report{SchemaVersion: domain.ReportSchemaVersion, ID: id, CreatedAt: time.Now().UTC(), EvarisVersion: s.version(), EvaluationIDs: []string{}, Cases: []domain.CaseSummary{}, Evidence: []domain.FileRef{}, Limitations: []string{"Configured checks do not establish general semantic correctness.", "Host metadata and origin are operator-reported; no performance improvement is established."}, Recommendations: []string{}}
	out.Mode = domain.ModeComparison
	for _, p := range ps {
		if p.Mode == domain.ModeAcceptance {
			out.Mode = domain.ModeAcceptance
		}
	}
	chosen, _ := latest(es)
	for _, v := range es {
		out.EvaluationIDs = append(out.EvaluationIDs, v.ID)
		b, _ := marshalEvaluation(v)
		out.Evidence = append(out.Evidence, domain.FileRef{Path: "evaluations/" + v.ID + "/evaluation.json", SHA256: domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(b))), Bytes: int64(len(b))})
	}
	for _, p := range ps {
		out.ExperimentID = p.ExperimentID
		out.ConfigDigest = p.ConfigDigest
		for _, task := range p.Tasks {
			row := domain.CaseSummary{PreparationID: p.ID, CaseID: task.CaseID, ArmID: task.ArmID, Execution: "INCOMPLETE", AgentOutcome: "UNKNOWN", Assessment: "INCONCLUSIVE", FailedCriteria: []string{}, MissingEvidence: []string{"run"}, Criteria: []domain.CriterionSummary{}}
			for _, run := range runs {
				if run.PreparationID != p.ID || run.CaseID != task.CaseID || run.ArmID != task.ArmID {
					continue
				}
				rid := run.ID
				row.RunID = &rid
				row.MissingEvidence = []string{"evaluation"}
				out.Limitations = append(out.Limitations, "Run "+run.ID+" origin: "+run.Origin)
				if v, ok := chosen[run.ID]; ok {
					vid := v.ID
					row.EvaluationID = &vid
					row.Execution = v.Execution
					row.AgentOutcome = v.AgentOutcome
					row.Assessment = v.Assessment
					row.MissingEvidence = []string{}
					for _, g := range v.Graders {
						row.Criteria = append(row.Criteria, domain.CriterionSummary{ID: g.CriterionID, Required: g.Required, Status: g.Status, Reason: g.Reason, Message: g.Message})
						if g.Status == "FAIL" {
							row.FailedCriteria = append(row.FailedCriteria, g.CriterionID)
						}
						if g.Status == "ERROR" || g.Status == "UNAVAILABLE" {
							row.MissingEvidence = append(row.MissingEvidence, g.CriterionID)
						}
					}
					if x := v.Verification; x != nil {
						row.Criteria = append(row.Criteria, domain.CriterionSummary{ID: "verification", Required: x.Required, Status: x.Status, Reason: x.Reason, Message: x.Message})
						switch x.Status {
						case "REJECT":
							row.FailedCriteria = append(row.FailedCriteria, "verification")
						case "ERROR", "UNAVAILABLE", "REFUSE":
							row.MissingEvidence = append(row.MissingEvidence, "verification")
						}
					}
					if v.Review == "PENDING" {
						row.MissingEvidence = append(row.MissingEvidence, "review")
					}
				}
			}
			// Recommendations name only failed or missing gates; never claim improvement.
			where := row.CaseID + "/" + row.ArmID + " (preparation " + row.PreparationID + ")"
			if len(row.FailedCriteria) > 0 {
				out.Recommendations = append(out.Recommendations, "Inspect failed gates for "+where+": "+strings.Join(row.FailedCriteria, ", "))
			}
			if len(row.MissingEvidence) > 0 {
				out.Recommendations = append(out.Recommendations, "Supply or re-run missing evidence for "+where+": "+strings.Join(row.MissingEvidence, ", "))
			}
			out.Cases = append(out.Cases, row)
		}
	}
	if len(ps) == 0 {
		return out, &domain.Failure{Code: "EVIDENCE_CORRUPT", Path: "no preparation"}
	}
	// Embed only the newest committed comparison over exactly the selected
	// evaluations. Acceptance experiments have nothing to compare.
	var cs []domain.Comparison
	if out.Mode != domain.ModeAcceptance {
		cs, e = s.Repo.LoadComparisons(ctx, store)
	}
	if e != nil {
		return out, e
	}
	selected := map[string]bool{}
	for _, v := range chosen {
		selected[v.ID] = true
	}
	for i := range cs {
		c := cs[i]
		ids := append(append([]string{}, c.LeftEvaluationIDs...), c.RightEvaluationIDs...)
		match := len(ids) == len(selected)
		for _, id := range ids {
			match = match && selected[id]
		}
		if match && (out.Comparison == nil || c.CreatedAt.After(out.Comparison.CreatedAt) || c.CreatedAt.Equal(out.Comparison.CreatedAt) && c.ID > out.Comparison.ID) {
			out.Comparison = &c
		}
	}
	if out.Comparison == nil && out.Mode != domain.ModeAcceptance {
		out.Limitations = append(out.Limitations, "No committed comparison covers exactly the selected evaluations; comparison is null.")
	}
	if s.JSONReport == nil || s.MarkdownReport == nil {
		return out, &domain.Failure{Code: "IO_ERROR", Path: "renderer not configured"}
	}
	j, e := s.JSONReport(out)
	if e != nil {
		return out, e
	}
	m, e := s.MarkdownReport(out)
	if e != nil {
		return out, e
	}
	if e = s.Repo.SaveReport(ctx, store, out, j, m); e != nil {
		return domain.Report{}, e
	}
	return out, nil
}

func marshalEvaluation(v domain.Evaluation) ([]byte, error) {
	b, e := json.MarshalIndent(v, "", "  ")
	return append(b, '\n'), e
}

func (s Service) version() string {
	if s.Version == "" {
		return "dev"
	}
	return s.Version
}
