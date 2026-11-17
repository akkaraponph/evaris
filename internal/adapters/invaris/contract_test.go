package invaris

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
)

const harnessTemplate = `{
  "version": "invaris.dev/v1alpha2",
  "name": "evaris-approved-result",
  "trustMode": "trusted-local",
  "networkPolicy": "unrestricted",
  "policies": [{"id": "approved-result", "allowEmpty": false}],
  "guards": [{"id": "result-guard", "policyId": "approved-result", "program": "@GUARD@", "args": []}],
  "gate": {"requiredGuards": ["result-guard"], "requiredProofs": [], "precedence": "refusal-first", "allowWeakEvidence": false}
}
`

func harnessBytes(guard string) []byte {
	return []byte(strings.ReplaceAll(harnessTemplate, "@GUARD@", guard))
}
func digestOf(b []byte) domain.Digest {
	return domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(b)))
}

// doc mirrors the shape the pinned Invaris binary emitted for check runs.
func doc(decision, target string, digest domain.Digest) map[string]any {
	check := map[string]any{"id": "result-guard", "policyId": "approved-result", "providerId": "invaris-command", "status": "PASS", "inspected": 1, "findings": []any{}, "diagnostics": []any{}, "exitCode": 0}
	count := 1
	switch decision {
	case "REJECT":
		check["status"], check["exitCode"] = "FAIL", 1
		check["findings"] = []any{map[string]any{"policyId": "approved-result", "ruleId": "approved-result", "path": "result.txt", "message": ""}}
		check["diagnostics"] = []any{map[string]any{"code": "COMMAND_FAILED", "message": "exit status 1"}}
	case "REFUSE":
		check["status"], check["exitCode"], check["inspected"] = "REFUSE", 2, nil
		check["diagnostics"] = []any{map[string]any{"code": "REFUSED", "message": "invalid or incomplete inspection evidence"}}
		count = 0
	}
	t0, t1 := "2026-11-15T01:05:23.182967Z", "2026-11-15T01:05:23.203964Z"
	return map[string]any{
		"schemaVersion": "invaris.evidence/v1alpha2", "runId": "5081975188247bcc0b9f33a12991f5b5", "action": "check",
		"target": target, "harness": "evaris-approved-result", "configDigest": string(digest), "gitRevision": nil, "gitDirty": nil,
		"environment": map[string]any{"os": "darwin", "arch": "arm64"}, "startedAt": t0, "finishedAt": t1, "durationMs": 21,
		"compatibility": map[string]any{"status": "native", "inputVersion": "invaris.dev/v1alpha2", "warnings": []any{}},
		"policies":      []any{map[string]any{"id": "approved-result", "allowEmpty": false}},
		"gate":          map[string]any{"requiredGuards": []any{"result-guard"}, "requiredProofs": []any{}, "precedence": "refusal-first", "allowWeakEvidence": false},
		"lifecycle":     []any{map[string]any{"id": "result-guard", "kind": "guard", "status": check["status"], "startedAt": t0, "finishedAt": t1}},
		"steps":         []any{}, "checks": []any{check}, "proofs": []any{},
		"observations": []any{map[string]any{"id": "result-guard", "kind": "guard", "program": "/guard", "args": []any{}, "provider": map[string]any{"name": "invaris-command", "version": nil, "toolName": nil, "toolVersion": nil}, "scope": []any{}, "startedAt": t0, "finishedAt": t1, "exitCode": check["exitCode"], "stdout": "{}", "stderr": "", "truncated": false, "diagnostics": []any{}, "executable": map[string]any{"path": "/guard", "sha256": strings.Repeat("a", 64)}, "timeoutSeconds": 30, "envAllowlist": []any{}}},
		"targetCount":  count, "decision": decision, "diagnostics": []any{}, "artifacts": []any{},
	}
}
func encode(m map[string]any) []byte {
	b, _ := json.MarshalIndent(m, "", "  ")
	return append(b, '\n')
}
func exitPtr(n int) *int { return &n }

func TestHarnessProfile(t *testing.T) {
	if _, e := ParseHarness(harnessBytes("/guard")); e != nil {
		t.Fatal(e)
	}
	for name, edit := range map[string]func(string) string{
		"version": func(s string) string { return strings.Replace(s, "v1alpha2", "v1alpha3", 1) },
		"steps": func(s string) string {
			return strings.Replace(s, `"guards"`, `"steps": [{"id":"s","program":"x"}], "guards"`, 1)
		},
		"challenges": func(s string) string {
			return strings.Replace(s, `"guards"`, `"challenges": [{"id":"c"}], "guards"`, 1)
		},
		"provider guard": func(s string) string { return strings.Replace(s, `"args": []`, `"args": [], "adapterId": "a"`, 1) },
		"adapters": func(s string) string {
			return strings.Replace(s, `"guards"`, `"adapters": [{"id":"a","program":"x"}], "guards"`, 1)
		},
		"failure-first": func(s string) string { return strings.Replace(s, "refusal-first", "failure-first", 1) },
		"weak evidence": func(s string) string {
			return strings.Replace(s, `"allowWeakEvidence": false`, `"allowWeakEvidence": true`, 1)
		},
		"allowEmpty omitted": func(s string) string { return strings.Replace(s, `, "allowEmpty": false`, ``, 1) },
		"allowEmpty true":    func(s string) string { return strings.Replace(s, `"allowEmpty": false`, `"allowEmpty": true`, 1) },
		"guard not required": func(s string) string {
			return strings.Replace(s, `"requiredGuards": ["result-guard"]`, `"requiredGuards": []`, 1)
		},
		"required proofs": func(s string) string { return strings.Replace(s, `"requiredProofs": []`, `"requiredProofs": ["p"]`, 1) },
		"unknown policy": func(s string) string {
			return strings.Replace(s, `"policyId": "approved-result"`, `"policyId": "other"`, 1)
		},
		"no guards": func(s string) string {
			return strings.Replace(s, `[{"id": "result-guard", "policyId": "approved-result", "program": "@GUARD@", "args": []}]`, `[]`, 1)
		},
		"duplicate key": func(s string) string {
			return strings.Replace(s, `"name": "evaris-approved-result",`, `"name": "a", "name": "b",`, 1)
		},
		"unknown field": func(s string) string { return strings.Replace(s, `"name":`, `"extra": 1, "name":`, 1) },
		"null gate":     func(s string) string { return s[:strings.Index(s, `"gate"`)] + `"gate": null}` },
		"trailing":      func(s string) string { return s + "{}" },
	} {
		if _, e := ParseHarness([]byte(edit(harnessTemplate))); e == nil {
			t.Errorf("%s: accepted unsupported harness", name)
		}
	}
}

func TestValidateEvidenceDecisions(t *testing.T) {
	h, _ := ParseHarness(harnessBytes("/guard"))
	d := digestOf(harnessBytes("/guard"))
	for decision, code := range map[string]int{"ACCEPT": 0, "REJECT": 1, "REFUSE": 2} {
		o, e := ValidateEvidence(encode(doc(decision, "/scratch", d)), exitPtr(code), h, d, "/scratch")
		if e != nil || o.Decision != decision {
			t.Fatalf("%s: %+v %v", decision, o, e)
		}
		if decision == "REJECT" && (len(o.Findings) != 1 || o.Findings[0].Path != "result.txt") {
			t.Fatalf("findings lost: %+v", o.Findings)
		}
	}
}

func TestInvarisRejectsEvidence(t *testing.T) {
	h, _ := ParseHarness(harnessBytes("/guard"))
	d := digestOf(harnessBytes("/guard"))
	check := func(m map[string]any) map[string]any { return m["checks"].([]any)[0].(map[string]any) }
	cases := map[string]struct {
		decision string
		exit     int
		edit     func(map[string]any)
		raw      func([]byte) []byte
		code     domain.Reason
	}{
		"schema mismatch":   {"ACCEPT", 0, func(m map[string]any) { m["schemaVersion"] = "invaris.evidence/v1alpha3" }, nil, "UNSUPPORTED_SCHEMA"},
		"missing field":     {"ACCEPT", 0, func(m map[string]any) { delete(m, "lifecycle") }, nil, "EVIDENCE_CORRUPT"},
		"null decision":     {"ACCEPT", 0, func(m map[string]any) { m["decision"] = nil }, nil, "EVIDENCE_CORRUPT"},
		"null configDigest": {"ACCEPT", 0, func(m map[string]any) { m["configDigest"] = nil }, nil, "EVIDENCE_CORRUPT"},
		"unknown field":     {"ACCEPT", 0, func(m map[string]any) { m["extra"] = true }, nil, "EVIDENCE_CORRUPT"},
		"malformed":         {"ACCEPT", 0, nil, func([]byte) []byte { return []byte("not json") }, "EVIDENCE_CORRUPT"},
		"truncated":         {"ACCEPT", 0, nil, func(b []byte) []byte { return b[:len(b)/2] }, "EVIDENCE_CORRUPT"},
		"trailing":          {"ACCEPT", 0, nil, func(b []byte) []byte { return append(b, []byte("{}")...) }, "EVIDENCE_CORRUPT"},
		"duplicate key": {"ACCEPT", 0, nil, func(b []byte) []byte {
			return []byte(strings.Replace(string(b), `"action": "check",`, `"action": "check", "action": "check",`, 1))
		}, "EVIDENCE_CORRUPT"},
		"duplicate check ids": {"ACCEPT", 0, func(m map[string]any) { m["checks"] = append(m["checks"].([]any), check(m)) }, nil, "EVIDENCE_CORRUPT"},
		"exit/decision":       {"ACCEPT", 1, nil, nil, "EVIDENCE_CORRUPT"},
		"refuse with exit 0":  {"REFUSE", 0, nil, nil, "EVIDENCE_CORRUPT"},
		"config digest":       {"ACCEPT", 0, func(m map[string]any) { m["configDigest"] = "sha256:" + strings.Repeat("0", 64) }, nil, "EVIDENCE_CORRUPT"},
		"target":              {"ACCEPT", 0, func(m map[string]any) { m["target"] = "/elsewhere" }, nil, "EVIDENCE_CORRUPT"},
		"harness name":        {"ACCEPT", 0, func(m map[string]any) { m["harness"] = "other" }, nil, "EVIDENCE_CORRUPT"},
		"action":              {"ACCEPT", 0, func(m map[string]any) { m["action"] = "challenge" }, nil, "EVIDENCE_CORRUPT"},
		"gate precedence":     {"ACCEPT", 0, func(m map[string]any) { m["gate"].(map[string]any)["precedence"] = "failure-first" }, nil, "EVIDENCE_CORRUPT"},
		"gate guards":         {"ACCEPT", 0, func(m map[string]any) { m["gate"].(map[string]any)["requiredGuards"] = []any{} }, nil, "EVIDENCE_CORRUPT"},
		"policy allowEmpty":   {"ACCEPT", 0, func(m map[string]any) { m["policies"].([]any)[0].(map[string]any)["allowEmpty"] = true }, nil, "EVIDENCE_CORRUPT"},
		"zero targets":        {"ACCEPT", 0, func(m map[string]any) { m["targetCount"] = 0 }, nil, "EVIDENCE_CORRUPT"},
		"null targets":        {"ACCEPT", 0, func(m map[string]any) { m["targetCount"] = nil }, nil, "EVIDENCE_CORRUPT"},
		"zero inspected":      {"ACCEPT", 0, func(m map[string]any) { check(m)["inspected"] = 0 }, nil, "EVIDENCE_CORRUPT"},
		"pass with findings": {"ACCEPT", 0, func(m map[string]any) {
			check(m)["findings"] = []any{map[string]any{"policyId": "approved-result", "ruleId": "r", "path": "p", "message": ""}}
		}, nil, "EVIDENCE_CORRUPT"},
		"proof present": {"ACCEPT", 0, func(m map[string]any) { m["proofs"] = []any{map[string]any{"id": "p"}} }, nil, "EVIDENCE_CORRUPT"},
		"step present":  {"ACCEPT", 0, func(m map[string]any) { m["steps"] = []any{check(m)} }, nil, "EVIDENCE_CORRUPT"},
		"artifact present": {"ACCEPT", 0, func(m map[string]any) {
			m["artifacts"] = []any{map[string]any{"path": "a", "sha256": strings.Repeat("a", 64), "bytes": 1}}
		}, nil, "EVIDENCE_CORRUPT"},
		"legacy compatibility": {"ACCEPT", 0, func(m map[string]any) { m["compatibility"].(map[string]any)["status"] = "legacy" }, nil, "EVIDENCE_CORRUPT"},
		"run id":               {"ACCEPT", 0, func(m map[string]any) { m["runId"] = "XYZ" }, nil, "EVIDENCE_CORRUPT"},
		"times reversed":       {"ACCEPT", 0, func(m map[string]any) { m["startedAt"], m["finishedAt"] = m["finishedAt"], m["startedAt"] }, nil, "EVIDENCE_CORRUPT"},
		"lifecycle mismatch":   {"ACCEPT", 0, func(m map[string]any) { m["lifecycle"].([]any)[0].(map[string]any)["status"] = "FAIL" }, nil, "EVIDENCE_CORRUPT"},
		"finding policy":       {"REJECT", 1, func(m map[string]any) { check(m)["findings"].([]any)[0].(map[string]any)["policyId"] = "other" }, nil, "EVIDENCE_CORRUPT"},
		"decision vs results":  {"ACCEPT", 0, func(m map[string]any) { m["decision"] = "REJECT" }, nil, "EVIDENCE_CORRUPT"},
		"decision lies":        {"REJECT", 0, func(m map[string]any) { m["decision"] = "ACCEPT" }, nil, "EVIDENCE_CORRUPT"},
		"refusal hidden":       {"REFUSE", 0, func(m map[string]any) { m["decision"] = "ACCEPT"; m["targetCount"] = 1 }, nil, "EVIDENCE_CORRUPT"},
		"unknown check status": {"ACCEPT", 0, func(m map[string]any) {
			check(m)["status"] = "SKIP"
			m["lifecycle"].([]any)[0].(map[string]any)["status"] = "SKIP"
		}, nil, "EVIDENCE_CORRUPT"},
		"foreign observation":   {"ACCEPT", 0, func(m map[string]any) { m["observations"].([]any)[0].(map[string]any)["id"] = "other" }, nil, "EVIDENCE_CORRUPT"},
		"wrong provider":        {"ACCEPT", 0, func(m map[string]any) { check(m)["providerId"] = "provider" }, nil, "EVIDENCE_CORRUPT"},
		"exit code wrong type":  {"ACCEPT", 0, func(m map[string]any) { check(m)["exitCode"] = "0" }, nil, "EVIDENCE_CORRUPT"},
		"diagnostic empty code": {"ACCEPT", 0, func(m map[string]any) { m["diagnostics"] = []any{map[string]any{"code": "", "message": ""}} }, nil, "EVIDENCE_CORRUPT"},
	}
	for name, tc := range cases {
		m := doc(tc.decision, "/scratch", d)
		if tc.edit != nil {
			tc.edit(m)
		}
		b := encode(m)
		if tc.raw != nil {
			b = tc.raw(b)
		}
		_, e := ValidateEvidence(b, exitPtr(tc.exit), h, d, "/scratch")
		f, ok := e.(*domain.Failure)
		if !ok || f.Code != tc.code {
			t.Errorf("%s: got %v, want %s", name, e, tc.code)
		}
	}
	if _, e := ValidateEvidence(encode(doc("ACCEPT", "/scratch", d)), nil, h, d, "/scratch"); e == nil {
		t.Error("accepted evidence without an exit code")
	}
	big := make([]byte, maxEvidence+1)
	if _, e := ValidateEvidence(big, exitPtr(0), h, d, "/scratch"); e == nil {
		t.Error("accepted oversized evidence")
	}
}

// The real pinned output is the source of the fixture shape; keep it decodable.
func TestFixtureMatchesRecordedShape(t *testing.T) {
	b, e := os.ReadFile("../../../testdata/invaris/harness.json.tmpl")
	if e != nil {
		t.Fatal(e)
	}
	if string(b) != harnessTemplate {
		t.Fatal("testdata harness template and test template diverged")
	}
}

// TestRecordedEvidence validates outputs recorded from the pinned binary.
func TestRecordedEvidence(t *testing.T) {
	harness := harnessBytes("/guard")
	h, _ := ParseHarness(harness)
	d := digestOf(harness)
	for name, want := range map[string]struct {
		decision string
		exit     int
	}{"accept": {"ACCEPT", 0}, "reject": {"REJECT", 1}, "refuse": {"REFUSE", 2}} {
		b, e := os.ReadFile("../../../testdata/invaris/evidence-" + name + ".json.tmpl")
		if e != nil {
			t.Fatal(e)
		}
		b = []byte(strings.NewReplacer("@TARGET@", "/scratch", "@DIGEST@", string(d), "@GUARD@", "/guard").Replace(string(b)))
		o, e := ValidateEvidence(b, exitPtr(want.exit), h, d, "/scratch")
		if e != nil || o.Decision != want.decision {
			t.Errorf("%s: %+v %v", name, o, e)
		}
	}
}

func TestValidateCampaign(t *testing.T) {
	d := digestOf([]byte("campaign"))
	frozen := []application.Blob{{Path: "killed.json", Data: []byte("harness")}}
	good := []any{map[string]any{"harness": "killed.json", "harnessDigest": string(digestOf([]byte("harness")))}}
	report := func(version, decision string, digest domain.Digest, experiments []any) []byte {
		return encode(map[string]any{"schemaVersion": version, "runId": "5081975188247bcc0b9f33a12991f5b5", "action": "campaign", "campaignConfigDigest": string(digest), "decision": decision, "experiments": experiments})
	}
	for _, tc := range []struct {
		name, version, decision string
		digest                  domain.Digest
		experiments             []any
		exit                    *int
		reason                  domain.Reason
	}{
		{"accept v4", "invaris.evidence/v1alpha4", "ACCEPT", d, good, exitPtr(0), ""},
		{"reject v5", "invaris.evidence/v1alpha5", "REJECT", d, good, exitPtr(1), ""},
		{"refuse", "invaris.evidence/v1alpha4", "REFUSE", d, good, exitPtr(2), ""},
		{"exit mismatch", "invaris.evidence/v1alpha4", "ACCEPT", d, good, exitPtr(1), "EVIDENCE_CORRUPT"},
		{"digest mismatch", "invaris.evidence/v1alpha4", "ACCEPT", digestOf([]byte("other")), good, exitPtr(0), "EVIDENCE_CORRUPT"},
		{"unknown schema", "invaris.evidence/v1alpha9", "ACCEPT", d, good, exitPtr(0), "UNSUPPORTED_SCHEMA"},
		{"check report", "invaris.evidence/v1alpha2", "ACCEPT", d, good, exitPtr(0), "UNSUPPORTED_SCHEMA"},
		{"no experiments", "invaris.evidence/v1alpha4", "ACCEPT", d, []any{}, exitPtr(0), "EVIDENCE_CORRUPT"},
		{"unfrozen harness", "invaris.evidence/v1alpha4", "ACCEPT", d, []any{map[string]any{"harness": "other.json", "harnessDigest": string(digestOf([]byte("harness")))}}, exitPtr(0), "EVIDENCE_CORRUPT"},
		{"harness digest mismatch", "invaris.evidence/v1alpha4", "ACCEPT", d, []any{map[string]any{"harness": "killed.json", "harnessDigest": string(digestOf([]byte("agent")))}}, exitPtr(0), "EVIDENCE_CORRUPT"},
	} {
		o, e := ValidateCampaign(report(tc.version, tc.decision, tc.digest, tc.experiments), tc.exit, d, frozen)
		if tc.reason == "" {
			if e != nil || o.Decision != tc.decision {
				t.Errorf("%s: %+v %v", tc.name, o, e)
			}
			continue
		}
		if e == nil || reason(e, "") != tc.reason {
			t.Errorf("%s: err %v, want %s", tc.name, e, tc.reason)
		}
	}
}
