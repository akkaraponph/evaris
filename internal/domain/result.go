package domain

import "time"

type ExecutionState string
type TaskOutcome string
type ReviewStatus string
type AssessmentStatus string
type GraderResult struct {
	CriterionID   string    `json:"criterionId"`
	Required      bool      `json:"required"`
	Status        string    `json:"status"`
	Reason        Reason    `json:"reason"`
	Message       string    `json:"message"`
	Evidence      []FileRef `json:"evidence"`
	GraderVersion string    `json:"graderVersion"`
}
type Verification struct {
	Required bool   `json:"required"`
	Status   string `json:"status"`
}
type Assessment struct {
	Execution    ExecutionState
	AgentOutcome TaskOutcome
	Review       ReviewStatus
	Final        AssessmentStatus
}

func Assess(gs []GraderResult, review ReviewStatus, v *Verification) Assessment {
	a := Assessment{"COMPLETE", "SATISFIED", review, "PASS"}
	missing := len(gs) == 0
	failed := false
	for _, g := range gs {
		if g.Required && g.Status == "FAIL" {
			failed = true
		}
		if g.Required && g.Status != "PASS" && g.Status != "FAIL" {
			missing = true
		}
		if g.Status == "UNAVAILABLE" && a.Execution == "COMPLETE" {
			a.Execution = "INCOMPLETE"
		}
		if g.Status == "ERROR" && a.Execution != "CANCELED" {
			a.Execution = "ERROR"
		}
		if g.Reason == "CANCELED" {
			a.Execution = "CANCELED"
		}
	}
	if v != nil && v.Required {
		if v.Status == "REJECT" {
			failed = true
		} else if v.Status != "ACCEPT" {
			missing = true
		}
	}
	if missing || review == "PENDING" {
		a.AgentOutcome = "UNKNOWN"
		a.Final = "INCONCLUSIVE"
	}
	if failed {
		a.AgentOutcome = "VIOLATED"
		a.Final = "FAIL"
	}
	return a
}

type Evaluation struct {
	SchemaVersion string           `json:"schemaVersion"`
	ID            string           `json:"id"`
	RunID         string           `json:"runId"`
	PreparationID string           `json:"preparationId"`
	ExperimentID  string           `json:"experimentId"`
	CaseID        string           `json:"caseId"`
	ArmID         string           `json:"armId"`
	RunDigest     Digest           `json:"runDigest"`
	OracleDigest  Digest           `json:"oracleDigest"`
	CreatedAt     time.Time        `json:"createdAt"`
	Execution     ExecutionState   `json:"execution"`
	AgentOutcome  TaskOutcome      `json:"agentOutcome"`
	Graders       []GraderResult   `json:"graders"`
	Review        ReviewStatus     `json:"review"`
	Verification  *Verification    `json:"verification"`
	Assessment    AssessmentStatus `json:"assessment"`
	Limitations   []string         `json:"limitations"`
}
type CaseSummary struct {
	PreparationID   string           `json:"preparationId"`
	CaseID          string           `json:"caseId"`
	ArmID           string           `json:"armId"`
	RunID           *string          `json:"runId"`
	EvaluationID    *string          `json:"evaluationId"`
	Execution       ExecutionState   `json:"execution"`
	AgentOutcome    TaskOutcome      `json:"agentOutcome"`
	Assessment      AssessmentStatus `json:"assessment"`
	FailedCriteria  []string         `json:"failedCriteria"`
	MissingEvidence []string         `json:"missingEvidence"`
}
type Report struct {
	SchemaVersion   string        `json:"schemaVersion"`
	ID              string        `json:"id"`
	ExperimentID    string        `json:"experimentId"`
	ConfigDigest    Digest        `json:"configDigest"`
	CreatedAt       time.Time     `json:"createdAt"`
	EvarisVersion   string        `json:"evarisVersion"`
	EvaluationIDs   []string      `json:"evaluationIds"`
	Cases           []CaseSummary `json:"cases"`
	Comparison      any           `json:"comparison"`
	Evidence        []FileRef     `json:"evidence"`
	Limitations     []string      `json:"limitations"`
	Recommendations []string      `json:"recommendations"`
}
