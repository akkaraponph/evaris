package fsrepo

import (
	"bytes"
	"context"
	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
	"path/filepath"
	"regexp"
	"strings"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func validManifest(m domain.Manifest) bool {
	if m == nil {
		return false
	}
	seen := map[string]bool{}
	for _, f := range m {
		if !domain.Relative(f.Path) || !digestPattern.MatchString(string(f.SHA256)) || f.Bytes < 0 || f.Bytes > 10<<20 || seen[f.Path] {
			return false
		}
		seen[f.Path] = true
	}
	return true
}
func validatePreparation(p domain.Preparation) error {
	if !recordID.MatchString(p.ID) || p.CreatedAt.IsZero() || p.ExperimentID == "" || !digestPattern.MatchString(string(p.ConfigDigest)) || !filepath.IsAbs(p.TasksRoot) || len(p.Tasks) == 0 || len(p.Tasks) > 200 {
		return fail("EVIDENCE_CORRUPT", "preparation")
	}
	seen := map[string]bool{}
	for _, t := range p.Tasks {
		key := t.CaseID + "/" + t.ArmID
		if seen[key] || t.CaseID == "" || (t.ArmID != "baseline" && t.ArmID != "specialist") || t.Workspace != p.ID+"/"+key || !validManifest(t.InitialFiles) || !validManifest(t.GeneratedFiles) || t.Instructions == nil {
			return fail("EVIDENCE_CORRUPT", "prepared task")
		}
		seen[key] = true
		for _, d := range []domain.Digest{t.TaskDigest, t.PromptDigest, t.OracleDigest, t.FixtureDigest} {
			if !digestPattern.MatchString(string(d)) {
				return fail("EVIDENCE_CORRUPT", "prepared digest")
			}
		}
		if manifestDigest(t.InitialFiles) != t.FixtureDigest {
			return fail("EVIDENCE_CORRUPT", "fixture digest")
		}
	}
	return nil
}
func validateRun(ctx context.Context, root string, r domain.Run, p domain.Preparation) error {
	if r.ImportedAt.IsZero() || r.Limitations == nil || r.Transcripts == nil || r.Instructions == nil || !validManifest(r.FinalFiles) || r.Origin != "external-agent" && r.Origin != "synthetic-control" || p.ConfigDigest != r.ConfigDigest || p.ExperimentID != r.ExperimentID {
		return fail("EVIDENCE_CORRUPT", "run fields")
	}
	var task *domain.PreparedTask
	for i := range p.Tasks {
		if p.Tasks[i].CaseID == r.CaseID && p.Tasks[i].ArmID == r.ArmID {
			task = &p.Tasks[i]
		}
	}
	if task == nil {
		return fail("EVIDENCE_CORRUPT", "run task")
	}
	a, _ := jsonBytes(task.Instructions)
	b, _ := jsonBytes(r.Instructions)
	if !bytes.Equal(a, b) {
		return fail("EVIDENCE_CORRUPT", "instructions")
	}
	actual, e := Snapshot(ctx, filepath.Join(root, "tree"))
	if e != nil {
		return e
	}
	want := domain.Manifest{}
	for _, f := range r.FinalFiles {
		if !strings.HasPrefix(f.Path, "tree/") {
			return fail("EVIDENCE_CORRUPT", "final path")
		}
		f.Path = strings.TrimPrefix(f.Path, "tree/")
		want = append(want, f)
	}
	if manifestDigest(actual) != manifestDigest(want) {
		return fail("EVIDENCE_CORRUPT", "final manifest")
	}
	refs := append([]domain.FileRef{r.FinalResponse}, r.Transcripts...)
	if r.FinalResponse.Path != "response.txt" {
		return fail("EVIDENCE_CORRUPT", "response path")
	}
	if r.Diff != nil {
		if r.Diff.Path != "diff.txt" {
			return fail("EVIDENCE_CORRUPT", "diff path")
		}
		refs = append(refs, *r.Diff)
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		if !domain.Relative(ref.Path) || seen[ref.Path] {
			return fail("EVIDENCE_CORRUPT", "reference")
		}
		seen[ref.Path] = true
		b, e := readFile(filepath.Join(root, ref.Path))
		if e != nil || digest(b) != ref.SHA256 || int64(len(b)) != ref.Bytes {
			return fail("EVIDENCE_CORRUPT", "referenced bytes")
		}
	}
	for _, ref := range r.Transcripts {
		if !strings.HasPrefix(ref.Path, "transcripts/") {
			return fail("EVIDENCE_CORRUPT", "transcript path")
		}
	}
	return nil
}

// evaluationRefs lists every evidence reference an evaluation record owns.
func evaluationRefs(v domain.Evaluation) []domain.FileRef {
	refs := []domain.FileRef{}
	for _, g := range v.Graders {
		refs = append(refs, g.Evidence...)
	}
	if v.Verification != nil {
		refs = append(refs, v.Verification.Evidence...)
	}
	return refs
}
func validateEvaluation(v domain.Evaluation, f application.FrozenRun) error {
	if !recordID.MatchString(v.ID) || v.CreatedAt.IsZero() || v.RunID != f.Run.ID || v.PreparationID != f.Run.PreparationID || v.ExperimentID != f.Run.ExperimentID || v.CaseID != f.Run.CaseID || v.ArmID != f.Run.ArmID || v.RunDigest != f.RunDigest || v.OracleDigest != f.Run.OracleDigest || (v.Verification == nil) != (f.Case.Case.Verification == nil) {
		return fail("EVIDENCE_CORRUPT", "evaluation identity")
	}
	review := domain.ReviewStatus("NOT_REQUIRED")
	if f.Case.Case.RequiresReview {
		review = "PENDING"
	}
	if v.Review != review {
		return fail("EVIDENCE_CORRUPT", "review")
	}
	required := map[string]bool{"source-authorization": true}
	for _, c := range f.Case.Oracle.Criteria {
		required[c.ID] = c.Required
	}
	if len(v.Graders) != len(required) {
		return fail("EVIDENCE_CORRUPT", "criterion count")
	}
	seen := map[string]bool{}
	for _, g := range v.Graders {
		req, ok := required[g.CriterionID]
		if !ok || seen[g.CriterionID] || req != g.Required || g.GraderVersion != "builtin/1" || g.Evidence == nil || g.Status != "PASS" && g.Status != "FAIL" && g.Status != "ERROR" && g.Status != "UNAVAILABLE" {
			return fail("EVIDENCE_CORRUPT", "criterion")
		}
		seen[g.CriterionID] = true
		for _, ref := range g.Evidence {
			if !domain.Relative(ref.Path) || !strings.HasPrefix(ref.Path, "evidence/") || !digestPattern.MatchString(string(ref.SHA256)) || ref.Bytes < 0 {
				return fail("EVIDENCE_CORRUPT", "grader evidence")
			}
		}
	}
	if x := v.Verification; x != nil {
		if x.Required != f.Case.Case.Verification.Required || !domain.VerificationStatuses[x.Status] || x.Status == "NOT_CONFIGURED" || x.Evidence == nil || x.Findings == nil {
			return fail("EVIDENCE_CORRUPT", "verification")
		}
		for _, ref := range x.Evidence {
			if !domain.Relative(ref.Path) || !strings.HasPrefix(ref.Path, "evidence/verification/") || !digestPattern.MatchString(string(ref.SHA256)) || ref.Bytes < 0 {
				return fail("EVIDENCE_CORRUPT", "verification evidence")
			}
		}
		if x.Executable != nil && (!filepath.IsAbs(x.Executable.Path) || !digestPattern.MatchString(string(x.Executable.SHA256))) {
			return fail("EVIDENCE_CORRUPT", "verification executable")
		}
	}
	a := domain.Assess(v.Graders, v.Review, v.Verification)
	if a.Execution != v.Execution || a.AgentOutcome != v.AgentOutcome || a.Final != v.Assessment {
		return fail("EVIDENCE_CORRUPT", "assessment")
	}
	return nil
}
