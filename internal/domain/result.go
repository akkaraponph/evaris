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

// Finding is an external verifier finding. Paths are reported, never followed.
type Finding struct {
	PolicyID string `json:"policyId"`
	RuleID   string `json:"ruleId"`
	Path     string `json:"path"`
	// Line is the 1-based line Invaris reported, when it did.
	Line    *int   `json:"line,omitempty"`
	Message string `json:"message"`
}

// Verification is an external verifier signal kept separate from Evaris grading.
// Status ACCEPT/REJECT/REFUSE is the validated native decision; ERROR means the
// tool or its evidence failed; UNAVAILABLE means it was not authorized to run.
type Verification struct {
	Required      bool      `json:"required"`
	Status        string    `json:"status"`
	SchemaVersion *string   `json:"schemaVersion"`
	ExitCode      *int      `json:"exitCode"`
	RunID         *string   `json:"runId"`
	ConfigDigest  *string   `json:"configDigest"`
	Executable    *FileRef  `json:"executable"`
	Evidence      []FileRef `json:"evidence"`
	Reason        Reason    `json:"reason"`
	// Message is a catalogue message (see ValidGraderMessage) naming the
	// failing step; omitted when there is none.
	Message  string    `json:"message,omitempty"`
	Findings []Finding `json:"findings"`
}

// VerificationStatuses are the only valid Verification.Status values.
var VerificationStatuses = map[string]bool{"NOT_CONFIGURED": true, "ACCEPT": true, "REJECT": true, "REFUSE": true, "ERROR": true, "UNAVAILABLE": true}

type Assessment struct {
	Execution    ExecutionState
	AgentOutcome TaskOutcome
	Review       ReviewStatus
	Final        AssessmentStatus
}

// Assess aggregates required gates. A known FAIL (or required verifier REJECT)
// dominates; otherwise any missing, unavailable, errored or refused required
// evidence, or pending review, is inconclusive. A verifier ACCEPT never supplies
// a missing task assertion.
func Assess(gs []GraderResult, review ReviewStatus, v *Verification) Assessment {
	a := Assessment{"COMPLETE", "SATISFIED", review, "PASS"}
	missing := len(gs) == 0
	failed, errored, incomplete, canceled := false, false, false, false
	for _, g := range gs {
		if g.Required && g.Status == "FAIL" {
			failed = true
		}
		if g.Required && g.Status != "PASS" && g.Status != "FAIL" {
			missing = true
		}
		incomplete = incomplete || g.Status == "UNAVAILABLE"
		errored = errored || g.Status == "ERROR"
		canceled = canceled || g.Reason == "CANCELED"
	}
	if v != nil {
		errored = errored || v.Status == "ERROR" || !VerificationStatuses[v.Status]
		incomplete = incomplete || v.Status == "REFUSE" || v.Status == "UNAVAILABLE"
		canceled = canceled || v.Reason == "CANCELED"
		if v.Required {
			if v.Status == "REJECT" {
				failed = true
			} else if v.Status != "ACCEPT" {
				missing = true
			}
		}
	}
	switch {
	case canceled:
		a.Execution = "CANCELED"
	case errored:
		a.Execution = "ERROR"
	case incomplete:
		a.Execution = "INCOMPLETE"
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
	// Criteria lists every grader result of the chosen evaluation, plus
	// "verification" when configured, in evaluation order. Empty without one.
	Criteria []CriterionSummary `json:"criteria"`
}

// CriterionSummary is one criterion's result in a report. Message is the
// evaluation's catalogue message, empty when there is none.
type CriterionSummary struct {
	ID       string `json:"id"`
	Required bool   `json:"required"`
	Status   string `json:"status"`
	Reason   Reason `json:"reason"`
	Message  string `json:"message"`
}

// ReportSchemaVersion is the report record version this build writes.
const ReportSchemaVersion = "evaris.report/v1alpha2"

type Report struct {
	SchemaVersion string `json:"schemaVersion"`
	// Mode is "comparison" or "acceptance"; acceptance reports carry no comparison.
	Mode            string        `json:"mode"`
	ID              string        `json:"id"`
	ExperimentID    string        `json:"experimentId"`
	ConfigDigest    Digest        `json:"configDigest"`
	CreatedAt       time.Time     `json:"createdAt"`
	EvarisVersion   string        `json:"evarisVersion"`
	EvaluationIDs   []string      `json:"evaluationIds"`
	Cases           []CaseSummary `json:"cases"`
	Comparison      *Comparison   `json:"comparison"`
	Evidence        []FileRef     `json:"evidence"`
	Limitations     []string      `json:"limitations"`
	Recommendations []string      `json:"recommendations"`
}
