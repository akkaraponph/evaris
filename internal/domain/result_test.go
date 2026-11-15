package domain

import "testing"

func TestAssessPrecedence(t *testing.T) {
	for _, tc := range []struct {
		g         []GraderResult
		review    ReviewStatus
		final     AssessmentStatus
		execution ExecutionState
	}{{[]GraderResult{{Required: true, Status: "PASS"}}, "NOT_REQUIRED", "PASS", "COMPLETE"}, {[]GraderResult{{Required: true, Status: "FAIL"}, {Required: true, Status: "ERROR"}}, "NOT_REQUIRED", "FAIL", "ERROR"}, {[]GraderResult{{Required: true, Status: "UNAVAILABLE"}}, "NOT_REQUIRED", "INCONCLUSIVE", "INCOMPLETE"}, {[]GraderResult{{Required: true, Status: "PASS"}}, "PENDING", "INCONCLUSIVE", "COMPLETE"}} {
		a := Assess(tc.g, tc.review, nil)
		if a.Final != tc.final || a.Execution != tc.execution {
			t.Fatal(a)
		}
	}
}

func TestAssessVerification(t *testing.T) {
	pass := []GraderResult{{Required: true, Status: "PASS"}}
	for _, tc := range []struct {
		name      string
		g         []GraderResult
		v         Verification
		final     AssessmentStatus
		execution ExecutionState
	}{
		{"accept", pass, Verification{Required: true, Status: "ACCEPT"}, "PASS", "COMPLETE"},
		{"accept-not-task-success", []GraderResult{{Required: true, Status: "FAIL"}}, Verification{Required: true, Status: "ACCEPT"}, "FAIL", "COMPLETE"},
		{"accept-missing-assertion", []GraderResult{{Required: true, Status: "UNAVAILABLE"}}, Verification{Required: true, Status: "ACCEPT"}, "INCONCLUSIVE", "INCOMPLETE"},
		{"reject", pass, Verification{Required: true, Status: "REJECT"}, "FAIL", "COMPLETE"},
		{"refuse", pass, Verification{Required: true, Status: "REFUSE"}, "INCONCLUSIVE", "INCOMPLETE"},
		{"error", pass, Verification{Required: true, Status: "ERROR", Reason: "TOOL_MISSING"}, "INCONCLUSIVE", "ERROR"},
		{"unauthorized", pass, Verification{Required: true, Status: "UNAVAILABLE"}, "INCONCLUSIVE", "INCOMPLETE"},
		{"canceled", pass, Verification{Required: true, Status: "ERROR", Reason: "CANCELED"}, "INCONCLUSIVE", "CANCELED"},
		{"unknown-status", pass, Verification{Required: true, Status: "PASS"}, "INCONCLUSIVE", "ERROR"},
		{"optional-reject-visible", pass, Verification{Required: false, Status: "REJECT"}, "PASS", "COMPLETE"},
		{"optional-error-visible", pass, Verification{Required: false, Status: "ERROR"}, "PASS", "ERROR"},
	} {
		v := tc.v
		if a := Assess(tc.g, "NOT_REQUIRED", &v); a.Final != tc.final || a.Execution != tc.execution {
			t.Errorf("%s: %+v", tc.name, a)
		}
	}
}
