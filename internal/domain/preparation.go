package domain

import "time"

type InstructionRef struct {
	Path     string `json:"path"`
	Revision string `json:"revision"`
	SHA256   Digest `json:"sha256"`
}
type PreparedTask struct {
	CaseID         string           `json:"caseId"`
	ArmID          string           `json:"armId"`
	TaskDigest     Digest           `json:"taskDigest"`
	PromptDigest   Digest           `json:"promptDigest"`
	FixtureDigest  Digest           `json:"fixtureDigest"`
	OracleDigest   Digest           `json:"oracleDigest"`
	Instructions   []InstructionRef `json:"instructions"`
	InitialFiles   Manifest         `json:"initialFiles"`
	GeneratedFiles Manifest         `json:"generatedFiles"`
	Workspace      string           `json:"workspace"`
}
type Preparation struct {
	SchemaVersion string         `json:"schemaVersion"`
	ID            string         `json:"id"`
	ExperimentID  string         `json:"experimentId"`
	ConfigDigest  Digest         `json:"configDigest"`
	CreatedAt     time.Time      `json:"createdAt"`
	TasksRoot     string         `json:"tasksRoot"`
	Tasks         []PreparedTask `json:"tasks"`
	// ReviewerFiles are the experiment's reviewer files at preparation, by
	// absolute path, checked again before every evaluation.
	ReviewerFiles []FileRef `json:"reviewerFiles,omitempty"`
	// Mode is "acceptance" for single-arm experiments; omitted otherwise.
	Mode string `json:"mode,omitempty"`
}
