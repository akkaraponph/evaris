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
