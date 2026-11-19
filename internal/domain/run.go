package domain

import "time"

type FileRef struct {
	Path       string `json:"path"`
	SHA256     Digest `json:"sha256"`
	Bytes      int64  `json:"bytes"`
	Executable bool   `json:"executable"`
}
type Manifest []FileRef

type Observed struct {
	Host               *string         `json:"host"`
	Model              *string         `json:"model"`
	ModelSettings      *map[string]any `json:"modelSettings"`
	Tools              *[]string       `json:"tools"`
	Permissions        *string         `json:"permissions"`
	Budget             *string         `json:"budget"`
	ContextID          *string         `json:"contextId"`
	FreshContext       *bool           `json:"freshContext"`
	StartedAt          *time.Time      `json:"startedAt"`
	FinishedAt         *time.Time      `json:"finishedAt"`
	LoadedInstructions *[]string       `json:"loadedInstructions"`
	AgentClaim         *string         `json:"agentClaim"`
}
type ImportRequest struct {
	SchemaVersion  string `json:"schemaVersion"`
	PreparationID  string `json:"preparationId"`
	CaseID         string `json:"caseId"`
	ArmID          string `json:"armId"`
	ConfigDigest   Digest `json:"configDigest"`
	FixtureDigest  Digest `json:"fixtureDigest"`
	SourceRevision string `json:"sourceRevision"`
	FinalResponse  string `json:"finalResponse"`
	// Exactly one of FinalTree and FinalSource names the delivered work.
	FinalTree   string       `json:"finalTree,omitempty"`
	FinalSource *FinalSource `json:"finalSource,omitempty"`
	// DeliveredRevision optionally names the commit the agent delivered. For
	// a git FinalSource it must equal the resolved commit.
	DeliveredRevision *string  `json:"deliveredRevision,omitempty"`
	Transcripts       []string `json:"transcripts"`
	// ClaudeTranscript optionally derives observed metadata and is retained.
	ClaudeTranscript string   `json:"claudeTranscript,omitempty"`
	Diff             *string  `json:"diff"`
	Observed         Observed `json:"observed"`
	Limitations      []string `json:"limitations"`
	Origin           string   `json:"origin"`
}

// FinalSource imports the delivered work from a Git commit. Kind is "git";
// Repository is the repository directory; Revision any commit-ish.
type FinalSource struct {
	Kind       string `json:"kind"`
	Repository string `json:"repository"`
	Revision   string `json:"revision"`
}

// Run schema versions: v1alpha2 records a delivered revision or final source.
const (
	RunSchemaV1 = "evaris.run/v1alpha1"
	RunSchemaV2 = "evaris.run/v1alpha2"
)

type Run struct {
	SchemaVersion  string           `json:"schemaVersion"`
	ID             string           `json:"id"`
	PreparationID  string           `json:"preparationId"`
	ExperimentID   string           `json:"experimentId"`
	CaseID         string           `json:"caseId"`
	ArmID          string           `json:"armId"`
	ConfigDigest   Digest           `json:"configDigest"`
	FixtureDigest  Digest           `json:"fixtureDigest"`
	TaskDigest     Digest           `json:"taskDigest"`
	PromptDigest   Digest           `json:"promptDigest"`
	OracleDigest   Digest           `json:"oracleDigest"`
	Instructions   []InstructionRef `json:"instructions"`
	SourceRevision string           `json:"sourceRevision"`
	ImportedAt     time.Time        `json:"importedAt"`
	FinalFiles     Manifest         `json:"finalFiles"`
	FinalResponse  FileRef          `json:"finalResponse"`
	Diff           *FileRef         `json:"diff"`
	Transcripts    []FileRef        `json:"transcripts"`
	Observed       Observed         `json:"observed"`
	Origin         string           `json:"origin"`
	Limitations    []string         `json:"limitations"`
	// v1alpha2 only: the delivered commit (resolved SHA for git imports),
	// the git source it came from, and the git executable used.
	DeliveredRevision *string      `json:"deliveredRevision,omitempty"`
	FinalSource       *FinalSource `json:"finalSource,omitempty"`
	GitExecutable     *FileRef     `json:"gitExecutable,omitempty"`
}
