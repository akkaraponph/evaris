package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/akkaraponph/evaris/internal/domain"
	"slices"
	"strings"
	"time"
)

func ID() (string, error) {
	b := make([]byte, 16)
	_, e := rand.Read(b)
	return hex.EncodeToString(b), e
}
func (s Service) Evaluate(ctx context.Context, store, id string, allow bool) (out domain.Evaluation, err error) {
	f, e := s.Repo.LoadRun(ctx, store, id)
	if e != nil {
		return out, e
	}
	f.AllowCommands = allow
	// Never grade with reviewer scripts that changed since preparation.
	if len(f.Preparation.ReviewerFiles) > 0 {
		if e = s.Repo.CheckReviewerFiles(ctx, f.Preparation.ReviewerFiles); e != nil {
			return out, e
		}
	}
	eid, e := ID()
	if e != nil {
		return out, e
	}
	out = domain.Evaluation{SchemaVersion: "evaris.evaluation/v1alpha1", ID: eid, RunID: id, PreparationID: f.Run.PreparationID, ExperimentID: f.Run.ExperimentID, CaseID: f.Run.CaseID, ArmID: f.Run.ArmID, RunDigest: f.RunDigest, OracleDigest: f.Run.OracleDigest, CreatedAt: time.Now().UTC(), Review: "NOT_REQUIRED", Graders: []domain.GraderResult{}, Limitations: append([]string{}, f.Run.Limitations...)}
	if f.Case.Case.RequiresReview {
		out.Review = "PENDING"
	}
	blobs := []Blob{}
	for _, c := range f.Case.Oracle.Criteria {
		r, b := s.grade(ctx, f, c)
		out.Graders = append(out.Graders, r)
		blobs = append(blobs, b...)
	}
	ar, ab := authorization(f)
	out.Graders = append(out.Graders, ar)
	blobs = append(blobs, ab...)
	if f.Case.Case.Verification != nil {
		v, b := s.verify(ctx, f)
		out.Verification = &v
		blobs = append(blobs, b...)
	}
	a := domain.Assess(out.Graders, out.Review, out.Verification)
	out.Execution = a.Execution
	out.AgentOutcome = a.AgentOutcome
	out.Assessment = a.Final
	// Persist cancellation evidence even after grading's context is canceled.
	if e = s.Repo.SaveEvaluation(context.WithoutCancel(ctx), store, out, blobs); e != nil {
		return domain.Evaluation{}, e
	}
	return out, nil
}
func (s Service) grade(ctx context.Context, f FrozenRun, c domain.Criterion) (r domain.GraderResult, b []Blob) {
	bad := func() {
		r = domain.GraderResult{CriterionID: c.ID, Required: c.Required, Status: "ERROR", Reason: "INVALID_GRADER_RESULT", Evidence: []domain.FileRef{}, GraderVersion: "builtin/1"}
		b = nil
	}
	defer func() {
		if recover() != nil {
			bad()
		}
	}()
	if s.Grader == nil {
		bad()
		return
	}
	r, b = s.Grader.Grade(ctx, f, c)
	if r.GraderVersion != "builtin/1" || r.CriterionID != c.ID || r.Status != "PASS" && r.Status != "FAIL" && r.Status != "UNAVAILABLE" && r.Status != "ERROR" {
		bad()
	}
	for _, ref := range r.Evidence {
		found := false
		for _, blob := range b {
			if blob.Path == ref.Path {
				found = true
				if !domain.Relative(blob.Path) || !strings.HasPrefix(blob.Path, "evidence/") || domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(blob.Data))) != ref.SHA256 || int64(len(blob.Data)) != ref.Bytes {
					bad()
					return
				}
			}
		}
		if !found {
			bad()
			return
		}
	}
	r.Required = c.Required
	// Only catalogue messages survive: free text from a grader could carry
	// reviewer-private content or paths that differ between evaluations.
	if !domain.ValidGraderMessage(r.Message) {
		r.Message = ""
	}
	if r.Evidence == nil {
		r.Evidence = []domain.FileRef{}
	}
	return
}

// verify consults the external verifier only with consent, and normalizes any
// invalid verifier output to ERROR so it can never become an ACCEPT.
func (s Service) verify(ctx context.Context, f FrozenRun) (v domain.Verification, b []Blob) {
	required := f.Case.Case.Verification.Required
	base := func(status string, reason domain.Reason) domain.Verification {
		return domain.Verification{Required: required, Status: status, Reason: reason, Evidence: []domain.FileRef{}, Findings: []domain.Finding{}}
	}
	if !f.AllowCommands {
		return base("UNAVAILABLE", "COMMAND_NOT_AUTHORIZED"), nil
	}
	if s.Verifier == nil {
		return base("ERROR", "TOOL_MISSING"), nil
	}
	defer func() {
		if recover() != nil {
			v, b = base("ERROR", "INVALID_GRADER_RESULT"), nil
		}
	}()
	v, b = s.Verifier.Verify(ctx, f)
	v.Required = required
	inputs := f.Case.VerificationInputsDigest()
	v.InputsDigest = &inputs
	if v.Evidence == nil {
		v.Evidence = []domain.FileRef{}
	}
	if v.Findings == nil {
		v.Findings = []domain.Finding{}
	}
	if !domain.VerificationStatuses[v.Status] || v.Status == "NOT_CONFIGURED" || !evidenceMatches(v.Evidence, b, "evidence/verification/") {
		return base("ERROR", "INVALID_GRADER_RESULT"), nil
	}
	if !domain.ValidGraderMessage(v.Message) {
		v.Message = ""
	}
	if v.Status == "ACCEPT" && (v.SchemaVersion == nil || v.RunID == nil || v.ConfigDigest == nil || v.ExitCode == nil || *v.ExitCode != 0) {
		return base("ERROR", "INVALID_GRADER_RESULT"), nil
	}
	return v, b
}

// evidenceMatches requires every reference to name exactly one supplied blob
// under prefix with matching bytes and digest.
func evidenceMatches(refs []domain.FileRef, blobs []Blob, prefix string) bool {
	for _, ref := range refs {
		found := 0
		for _, blob := range blobs {
			if blob.Path != ref.Path {
				continue
			}
			found++
			if !domain.Relative(blob.Path) || !strings.HasPrefix(blob.Path, prefix) || domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(blob.Data))) != ref.SHA256 || int64(len(blob.Data)) != ref.Bytes {
				return false
			}
		}
		if found != 1 {
			return false
		}
	}
	return len(refs) == len(blobs)
}

// AuthorizationEvidencePath is the private evidence file a failed
// source-authorization criterion writes. It names each unauthorized path, how
// it changed and which rule it broke; the catalogue message cannot carry paths.
const AuthorizationEvidencePath = "evidence/source-authorization.json"

// maxAuthorizationViolations bounds the listed paths; Omitted counts the rest.
const maxAuthorizationViolations = 500

// AuthorizationViolation is one unauthorized path. Change is added, removed,
// modified or mode; Rule is not-allowed (no allowedChanges pattern matches),
// protected (a protectedPaths pattern matches) or generated (an Evaris
// generated file such as TASK.txt changed).
type AuthorizationViolation struct {
	Path   string `json:"path"`
	Change string `json:"change"`
	Rule   string `json:"rule"`
}

// AuthorizationEvidence is the content of AuthorizationEvidencePath.
type AuthorizationEvidence struct {
	Violations []AuthorizationViolation `json:"violations"`
	Omitted    int                      `json:"omitted"`
}

func authorization(f FrozenRun) (domain.GraderResult, []Blob) {
	r := domain.GraderResult{CriterionID: "source-authorization", Required: true, Status: "PASS", Evidence: []domain.FileRef{}, GraderVersion: "builtin/1"}
	initial := map[string]domain.FileRef{}
	protected := map[string]bool{}
	for _, t := range f.Preparation.Tasks {
		if t.CaseID == f.Run.CaseID && t.ArmID == f.Run.ArmID {
			for _, ref := range append(append(domain.Manifest{}, t.InitialFiles...), t.GeneratedFiles...) {
				initial[ref.Path] = ref
			}
			for _, ref := range t.GeneratedFiles {
				protected[ref.Path] = true
			}
		}
	}
	actual := map[string]domain.FileRef{}
	for _, ref := range f.Run.FinalFiles {
		actual[strings.TrimPrefix(ref.Path, "tree/")] = ref
	}
	keys := map[string]bool{}
	for p := range initial {
		keys[p] = true
	}
	for p := range actual {
		keys[p] = true
	}
	var violations []AuthorizationViolation
	for p := range keys {
		a, ok := initial[p]
		b, present := actual[p]
		if ok != present || a.SHA256 != b.SHA256 || a.Executable != b.Executable {
			// A change passes only when an allowedChanges pattern matches it,
			// no protectedPaths pattern does, and it is not a generated file.
			rule := ""
			switch {
			case protected[p]:
				rule = "generated"
			case domain.MatchAny(f.Case.Case.ProtectedPaths, p):
				rule = "protected"
			case !domain.MatchAny(f.Case.Case.AllowedChanges, p):
				rule = "not-allowed"
			}
			if rule == "" {
				continue
			}
			change := "modified"
			switch {
			case !ok:
				change = "added"
			case !present:
				change = "removed"
			case a.SHA256 == b.SHA256:
				change = "mode"
			}
			violations = append(violations, AuthorizationViolation{Path: p, Change: change, Rule: rule})
		}
	}
	if len(violations) == 0 {
		return r, nil
	}
	r.Status = "FAIL"
	r.Reason = "ASSERTION_FAILED"
	r.Message = domain.CommandMessage(domain.StepSourceAuthorization, false, nil, "ASSERTION_FAILED", "")
	slices.SortFunc(violations, func(x, y AuthorizationViolation) int { return strings.Compare(x.Path, y.Path) })
	ev := AuthorizationEvidence{Violations: violations}
	if len(violations) > maxAuthorizationViolations {
		ev.Violations, ev.Omitted = violations[:maxAuthorizationViolations], len(violations)-maxAuthorizationViolations
	}
	data, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		return r, nil
	}
	data = append(data, '\n')
	r.Evidence = []domain.FileRef{{Path: AuthorizationEvidencePath, SHA256: domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(data))), Bytes: int64(len(data))}}
	return r, []Blob{{Path: AuthorizationEvidencePath, Data: data}}
}

// AsFailure gives every operation error a stable reason code for diagnostics.
func AsFailure(e error) error {
	if e == nil {
		return nil
	}
	var f *domain.Failure
	if errors.As(e, &f) {
		return e
	}
	switch {
	case errors.Is(e, context.Canceled):
		return &domain.Failure{Code: "CANCELED", Path: "operation", Err: e}
	case errors.Is(e, context.DeadlineExceeded):
		return &domain.Failure{Code: "TIMEOUT", Path: "operation", Err: e}
	}
	return &domain.Failure{Code: "IO_ERROR", Path: "operation", Err: e}
}
