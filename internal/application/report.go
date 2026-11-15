package application

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/akkaraponph/evaris/internal/domain"
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
	out = domain.Report{SchemaVersion: "evaris.report/v1alpha1", ID: id, CreatedAt: time.Now().UTC(), EvarisVersion: "dev", EvaluationIDs: []string{}, Cases: []domain.CaseSummary{}, Evidence: []domain.FileRef{}, Limitations: []string{"Configured checks do not establish general semantic correctness.", "Host metadata and origin are operator-reported; no performance improvement is established."}, Recommendations: []string{}}
	latest := map[string]domain.Evaluation{}
	for _, v := range es {
		out.EvaluationIDs = append(out.EvaluationIDs, v.ID)
		old, ok := latest[v.RunID]
		if !ok || v.CreatedAt.After(old.CreatedAt) || v.CreatedAt.Equal(old.CreatedAt) && v.ID > old.ID {
			latest[v.RunID] = v
		}
		b, _ := marshalEvaluation(v)
		out.Evidence = append(out.Evidence, domain.FileRef{Path: "evaluations/" + v.ID + "/evaluation.json", SHA256: domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(b))), Bytes: int64(len(b))})
	}
	for _, p := range ps {
		out.ExperimentID = p.ExperimentID
		out.ConfigDigest = p.ConfigDigest
		for _, task := range p.Tasks {
			row := domain.CaseSummary{PreparationID: p.ID, CaseID: task.CaseID, ArmID: task.ArmID, Execution: "INCOMPLETE", AgentOutcome: "UNKNOWN", Assessment: "INCONCLUSIVE", FailedCriteria: []string{}, MissingEvidence: []string{"run"}}
			for _, run := range runs {
				if run.PreparationID != p.ID || run.CaseID != task.CaseID || run.ArmID != task.ArmID {
					continue
				}
				rid := run.ID
				row.RunID = &rid
				row.MissingEvidence = []string{"evaluation"}
				out.Limitations = append(out.Limitations, "Run "+run.ID+" origin: "+run.Origin)
				if v, ok := latest[run.ID]; ok {
					vid := v.ID
					row.EvaluationID = &vid
					row.Execution = v.Execution
					row.AgentOutcome = v.AgentOutcome
					row.Assessment = v.Assessment
					row.MissingEvidence = []string{}
					for _, g := range v.Graders {
						if g.Status == "FAIL" {
							row.FailedCriteria = append(row.FailedCriteria, g.CriterionID)
						}
						if g.Status == "ERROR" || g.Status == "UNAVAILABLE" {
							row.MissingEvidence = append(row.MissingEvidence, g.CriterionID)
						}
					}
					if v.Review == "PENDING" {
						row.MissingEvidence = append(row.MissingEvidence, "review")
					}
				}
			}
			out.Cases = append(out.Cases, row)
		}
	}
	if len(ps) == 0 {
		return out, &domain.Failure{Code: "EVIDENCE_CORRUPT", Path: "no preparation"}
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
