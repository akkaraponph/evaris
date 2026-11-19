package application

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/akkaraponph/evaris/internal/domain"
)

type InstructionBytes struct {
	Ref  domain.InstructionRef
	Data []byte
}
type FrozenCase struct {
	Files        domain.Manifest
	Case         domain.Case
	Oracle       domain.Oracle
	Task         []byte
	Instructions []InstructionBytes
	// Harness holds the frozen private Invaris harness bytes when configured.
	Harness []byte
	// HarnessFiles holds a campaign's frozen referenced harness files, with
	// paths relative to the campaign file's directory.
	HarnessFiles []Blob
}

// VerificationInputsDigest identifies the verifier action and private files.
func (f FrozenCase) VerificationInputsDigest() domain.Digest {
	v := f.Case.Verification
	action := domain.VerifyCheck
	if v.Campaign() {
		action = domain.VerifyCampaign
	}
	files := make([]domain.FileRef, 0, len(f.HarnessFiles)+1)
	digest := func(name string, data []byte) domain.FileRef {
		return domain.FileRef{Path: name, SHA256: domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(data)))}
	}
	files = append(files, digest(v.Harness, f.Harness))
	for _, file := range f.HarnessFiles {
		files = append(files, digest(file.Path, file.Data))
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	data, _ := json.Marshal(struct {
		Action         string
		Files          []domain.FileRef
		TimeoutSeconds int
		Dependencies   []domain.Dependency
	}{action, files, v.TimeoutSeconds, v.Dependencies})
	return domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(data)))
}

type Plan struct {
	Experiment   domain.Experiment
	ConfigDigest domain.Digest
	ConfigBytes  []byte
	ConfigPath   string
	ConfigDir    string
	OutputRoot   string
	Cases        []FrozenCase
}
type Repository interface {
	Init(context.Context, string) (string, error)
	Load(context.Context, string) (Plan, error)
	Prepare(context.Context, Plan, string) (domain.Preparation, error)
	Import(context.Context, string, string) (domain.Run, error)
	LoadRun(context.Context, string, string) (FrozenRun, error)
	SaveEvaluation(context.Context, string, domain.Evaluation, []Blob) error
	LoadEvaluations(context.Context, string) ([]domain.Evaluation, []domain.Run, error)
	// ReadEvaluationEvidence returns one evidence file of a committed,
	// validated evaluation after checking its recorded digest.
	ReadEvaluationEvidence(ctx context.Context, store, evaluationID, path string) ([]byte, error)
	// CheckReviewerFiles fails when a reviewer file recorded at preparation
	// changed, disappeared or changed type.
	CheckReviewerFiles(ctx context.Context, refs []domain.FileRef) error
	SaveReport(context.Context, string, domain.Report, []byte, []byte) error
	LoadPreparations(context.Context, string) ([]domain.Preparation, error)
	SaveComparison(context.Context, string, domain.Comparison) error
	LoadComparisons(context.Context, string) ([]domain.Comparison, error)
}
type Service struct {
	// Version is recorded in reports as evarisVersion; empty means "dev".
	Version        string
	Repo           Repository
	Grader         Grader
	Verifier       Verifier
	JSONReport     func(domain.Report) ([]byte, error)
	MarkdownReport func(domain.Report) ([]byte, error)
}
type Blob struct {
	Path string
	Data []byte
}
type Grader interface {
	Grade(context.Context, FrozenRun, domain.Criterion) (domain.GraderResult, []Blob)
}

// Verifier runs an optional external verifier against a frozen run. It is only
// consulted for cases that configure verification and only with command consent.
type Verifier interface {
	Verify(context.Context, FrozenRun) (domain.Verification, []Blob)
}

func (s Service) Validate(ctx context.Context, p string) (Plan, error) { return s.Repo.Load(ctx, p) }

type FrozenRun struct {
	Run           domain.Run
	RunDigest     domain.Digest
	Preparation   domain.Preparation
	Case          FrozenCase
	SnapshotRoot  string
	AllowCommands bool
	// MaxEntries is the experiment's snapshot entry bound for grader copies.
	MaxEntries int
}
