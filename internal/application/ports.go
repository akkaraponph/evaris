package application

import (
	"context"
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
	Load(context.Context, string) (Plan, error)
	Prepare(context.Context, Plan, string) (domain.Preparation, error)
	Import(context.Context, string, string) (domain.Run, error)
	LoadRun(context.Context, string, string) (FrozenRun, error)
	SaveEvaluation(context.Context, string, domain.Evaluation, []Blob) error
	LoadEvaluations(context.Context, string) ([]domain.Evaluation, []domain.Run, error)
	SaveReport(context.Context, string, domain.Report, []byte, []byte) error
	LoadPreparations(context.Context, string) ([]domain.Preparation, error)
	SaveComparison(context.Context, string, domain.Comparison) error
	LoadComparisons(context.Context, string) ([]domain.Comparison, error)
}
type Service struct {
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
}
