// Package invaris integrates the external Invaris CLI through its versioned
// evidence contract. It never imports Invaris packages; it validates a narrow,
// check-only profile, tested against the Invaris versions in Tested.
package invaris

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
)

// Tested lists the Invaris builds the check profile is exercised against in
// CI. Builds before v0.5.0 have no --version and are recorded as legacy.
var Tested = []string{"ee92f3c00c1b59b2f576bad05569fb6eafb83097", "v0.3.0", "v0.4.0"}

// SupportedReleases are the release lines (major.minor) whose --version Evaris
// accepts. Development builds ("dev" or a pseudo-version) are accepted and
// recorded; legacy builds without --version are accepted with no version.
var SupportedReleases = []string{"0.5"}

const (
	harnessVersion  = "invaris.dev/v1alpha2"
	evidenceVersion = "invaris.evidence/v1alpha2"
	maxHarness      = 1 << 20
	maxEvidence     = 4 << 20
)

var (
	runIDPattern  = regexp.MustCompile(`^[0-9a-f]{32}$`)
	gitPattern    = regexp.MustCompile(`^[0-9a-f]{40,64}$`)
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	hexPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func fail(code domain.Reason, p string) error { return &domain.Failure{Code: code, Path: p} }

// Harness is the supported subset of an invaris.dev/v1alpha2 harness. Fields that
// would enable steps, challenges, providers or adapters must be absent or empty.
type Harness struct {
	Version              string            `json:"version"`
	Name                 string            `json:"name"`
	TrustMode            string            `json:"trustMode"`
	NetworkPolicy        string            `json:"networkPolicy"`
	WritablePaths        []string          `json:"writablePaths,omitempty"`
	FailFast             *bool             `json:"failFast,omitempty"`
	AlwaysRunGuards      *bool             `json:"alwaysRunGuards,omitempty"`
	Policies             []Policy          `json:"policies"`
	Gate                 Gate              `json:"gate"`
	RedactEnv            []string          `json:"redactEnv,omitempty"`
	StoreSensitiveOutput *bool             `json:"storeSensitiveOutput,omitempty"`
	Adapters             []json.RawMessage `json:"adapters,omitempty"`
	RequiredAdapters     []json.RawMessage `json:"requiredAdapters,omitempty"`
	Steps                []json.RawMessage `json:"steps,omitempty"`
	Guards               []Guard           `json:"guards"`
	Challenges           []json.RawMessage `json:"challenges,omitempty"`
}
type Policy struct {
	ID         string `json:"id"`
	AllowEmpty bool   `json:"allowEmpty"`
}
type Gate struct {
	RequiredGuards    []string `json:"requiredGuards"`
	RequiredProofs    []string `json:"requiredProofs,omitempty"`
	Precedence        string   `json:"precedence"`
	AllowWeakEvidence bool     `json:"allowWeakEvidence"`
}
type Guard struct {
	ID             string   `json:"id"`
	PolicyID       string   `json:"policyId"`
	Program        string   `json:"program"`
	Args           []string `json:"args,omitempty"`
	EnvAllowlist   []string `json:"envAllowlist,omitempty"`
	TimeoutSeconds *int     `json:"timeoutSeconds,omitempty"`
	MinInspected   *int     `json:"minInspected,omitempty"`
}

// CheckHarness reports whether harness bytes fit the supported profile.
func CheckHarness(b []byte) error {
	_, e := ParseHarness(b)
	return e
}

// ParseHarness strictly decodes and validates the restricted check-only profile:
// explicit policies with allowEmpty false, at least one structured guard, every
// guard required, no proofs, refusal-first precedence and no weak evidence.
func ParseHarness(b []byte) (Harness, error) {
	var h Harness
	if len(b) > maxHarness {
		return h, fail("LIMIT_EXCEEDED", "harness")
	}
	if e := strictDecode(b, &h); e != nil {
		if f, ok := e.(*domain.Failure); ok && f.Code == "EVIDENCE_CORRUPT" {
			return h, fail("INVALID_CONFIG", "harness "+f.Path)
		}
		return h, e
	}
	bad := func(what string) (Harness, error) { return h, fail("INVALID_CONFIG", "harness "+what) }
	if h.Version != harnessVersion {
		return h, fail("UNSUPPORTED_SCHEMA", "harness version")
	}
	if strings.TrimSpace(h.Name) == "" || h.TrustMode != "trusted-local" || h.NetworkPolicy != "unrestricted" {
		return bad("identity")
	}
	if len(h.Steps)+len(h.Challenges)+len(h.Adapters)+len(h.RequiredAdapters) != 0 || len(h.Gate.RequiredProofs) != 0 {
		return bad("unsupported profile")
	}
	if !presentFalse(b, "policies", "allowEmpty") {
		return bad("allowEmpty must be explicit false")
	}
	policies := map[string]bool{}
	for _, p := range h.Policies {
		if strings.TrimSpace(p.ID) == "" || policies[p.ID] || p.AllowEmpty {
			return bad("policy")
		}
		policies[p.ID] = true
	}
	if len(h.Guards) == 0 || len(policies) == 0 {
		return bad("guards")
	}
	guards := map[string]bool{}
	for _, g := range h.Guards {
		if strings.TrimSpace(g.ID) == "" || guards[g.ID] || !policies[g.PolicyID] || strings.TrimSpace(g.Program) == "" {
			return bad("guard")
		}
		if g.TimeoutSeconds != nil && (*g.TimeoutSeconds < 0 || *g.TimeoutSeconds > 300) || g.MinInspected != nil && *g.MinInspected < 1 {
			return bad("guard limits")
		}
		guards[g.ID] = true
	}
	required := map[string]bool{}
	for _, id := range h.Gate.RequiredGuards {
		if !guards[id] || required[id] {
			return bad("gate guard")
		}
		required[id] = true
	}
	if len(required) != len(guards) || h.Gate.Precedence != "refusal-first" || h.Gate.AllowWeakEvidence {
		return bad("gate")
	}
	return h, nil
}

// presentFalse checks that every element of array field carries key explicitly.
func presentFalse(b []byte, array, key string) bool {
	var raw map[string]json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		return false
	}
	var items []map[string]json.RawMessage
	if json.Unmarshal(raw[array], &items) != nil {
		return false
	}
	for _, item := range items {
		if _, ok := item[key]; !ok {
			return false
		}
	}
	return true
}

// Evidence is the inspected invaris.evidence/v1alpha2 shape. Fields tagged
// nullable may be JSON null; all others are required and non-null.
type Evidence struct {
	SchemaVersion string            `json:"schemaVersion"`
	RunID         string            `json:"runId"`
	Action        string            `json:"action"`
	Target        string            `json:"target"`
	Harness       string            `json:"harness"`
	ConfigDigest  *string           `json:"configDigest,nullable"`
	GitRevision   *string           `json:"gitRevision,nullable"`
	GitDirty      *bool             `json:"gitDirty,nullable"`
	Environment   environment       `json:"environment"`
	StartedAt     string            `json:"startedAt"`
	FinishedAt    string            `json:"finishedAt"`
	DurationMs    int64             `json:"durationMs"`
	Compatibility compatibility     `json:"compatibility"`
	Policies      []Policy          `json:"policies"`
	Gate          Gate              `json:"gate"`
	Lifecycle     []event           `json:"lifecycle"`
	Steps         []check           `json:"steps"`
	Checks        []check           `json:"checks"`
	Proofs        []json.RawMessage `json:"proofs"`
	Observations  []observation     `json:"observations"`
	TargetCount   *int64            `json:"targetCount,nullable"`
	Decision      string            `json:"decision"`
	Diagnostics   []diagnostic      `json:"diagnostics"`
	Artifacts     []json.RawMessage `json:"artifacts"`
}
type environment struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}
type compatibility struct {
	Status       string   `json:"status"`
	InputVersion string   `json:"inputVersion"`
	Warnings     []string `json:"warnings"`
}
type event struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	Status     string  `json:"status"`
	StartedAt  *string `json:"startedAt,nullable"`
	FinishedAt *string `json:"finishedAt,nullable"`
}
type check struct {
	ID          string       `json:"id"`
	PolicyID    string       `json:"policyId"`
	ProviderID  string       `json:"providerId"`
	Status      string       `json:"status"`
	Inspected   *int64       `json:"inspected,nullable"`
	Findings    []finding    `json:"findings"`
	Diagnostics []diagnostic `json:"diagnostics"`
	ExitCode    *int64       `json:"exitCode,nullable"`
}
type finding struct {
	PolicyID string `json:"policyId"`
	RuleID   string `json:"ruleId"`
	Path     string `json:"path"`
	Message  string `json:"message"`
	// Line is optional (Invaris 0.5 structured guards); at least 1 when set.
	Line *int `json:"line,omitempty"`
}
type diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type observation struct {
	ID             string       `json:"id"`
	Kind           string       `json:"kind"`
	Program        string       `json:"program"`
	Args           []string     `json:"args"`
	Provider       provider     `json:"provider"`
	Scope          []string     `json:"scope"`
	StartedAt      *string      `json:"startedAt,nullable"`
	FinishedAt     *string      `json:"finishedAt,nullable"`
	ExitCode       *int64       `json:"exitCode,nullable"`
	Stdout         string       `json:"stdout"`
	Stderr         string       `json:"stderr"`
	Truncated      bool         `json:"truncated"`
	Diagnostics    []diagnostic `json:"diagnostics"`
	Executable     *executable  `json:"executable,nullable"`
	TimeoutSeconds *int64       `json:"timeoutSeconds,nullable"`
	EnvAllowlist   []string     `json:"envAllowlist"`
}
type provider struct {
	Name        string  `json:"name"`
	Version     *string `json:"version,nullable"`
	ToolName    *string `json:"toolName,nullable"`
	ToolVersion *string `json:"toolVersion,nullable"`
}
type executable struct {
	Path   string  `json:"path"`
	SHA256 *string `json:"sha256,nullable"`
}

// Outcome is the validated native result of one check invocation.
type Outcome struct {
	Decision      string
	SchemaVersion string
	RunID         string
	ConfigDigest  string
	Findings      []domain.Finding
}

// ValidateEvidence validates raw stdout from `invaris check` against the frozen
// harness, its digest, the scratch target and the process exit code. Any
// mismatch is an error: unknown or inconsistent evidence never becomes ACCEPT.
func ValidateEvidence(b []byte, exit *int, h Harness, harnessDigest domain.Digest, target string) (Outcome, error) {
	var o Outcome
	if len(b) > maxEvidence {
		return o, fail("LIMIT_EXCEEDED", "evidence")
	}
	if probe := (struct {
		SchemaVersion string `json:"schemaVersion"`
	}{}); json.Unmarshal(b, &probe) == nil && probe.SchemaVersion != "" && probe.SchemaVersion != evidenceVersion {
		return o, fail("UNSUPPORTED_SCHEMA", probe.SchemaVersion)
	}
	var ev Evidence
	if e := strictDecode(b, &ev); e != nil {
		return o, e
	}
	corrupt := func(what string) (Outcome, error) { return o, fail("EVIDENCE_CORRUPT", what) }
	if ev.SchemaVersion != evidenceVersion {
		return o, fail("UNSUPPORTED_SCHEMA", "evidence")
	}
	if !runIDPattern.MatchString(ev.RunID) || ev.Action != "check" || ev.Target != target || ev.Harness != h.Name {
		return corrupt("identity")
	}
	if ev.ConfigDigest == nil || !digestPattern.MatchString(*ev.ConfigDigest) || domain.Digest(*ev.ConfigDigest) != harnessDigest {
		return corrupt("configDigest")
	}
	if ev.GitRevision != nil && !gitPattern.MatchString(*ev.GitRevision) || ev.Environment.OS == "" || ev.Environment.Arch == "" || ev.DurationMs < 0 {
		return corrupt("context")
	}
	if !ordered(&ev.StartedAt, &ev.FinishedAt, false) {
		return corrupt("times")
	}
	if ev.Compatibility.Status != "native" || ev.Compatibility.InputVersion != harnessVersion {
		return corrupt("compatibility")
	}
	if len(ev.Steps)+len(ev.Proofs)+len(ev.Artifacts) != 0 {
		return corrupt("unsupported evidence profile")
	}
	if !samePolicies(ev.Policies, h.Policies) || !sameStrings(ev.Gate.RequiredGuards, h.Gate.RequiredGuards) || len(ev.Gate.RequiredProofs) != 0 || ev.Gate.Precedence != h.Gate.Precedence || ev.Gate.AllowWeakEvidence != h.Gate.AllowWeakEvidence {
		return corrupt("policies or gate")
	}
	for _, d := range ev.Diagnostics {
		if d.Code == "" {
			return corrupt("diagnostic")
		}
	}
	// One check and one lifecycle event per declared guard, in declaration order.
	if len(ev.Checks) != len(h.Guards) || len(ev.Lifecycle) != len(h.Guards) {
		return corrupt("check count")
	}
	seen := map[string]bool{}
	o.Findings = []domain.Finding{}
	refused, failed := false, false
	for i, g := range h.Guards {
		c, l := ev.Checks[i], ev.Lifecycle[i]
		if seen[c.ID] || c.ID != g.ID || c.PolicyID != g.PolicyID || c.ProviderID != "invaris-command" || l.ID != g.ID || l.Kind != "guard" || l.Status != c.Status || !ordered(l.StartedAt, l.FinishedAt, true) {
			return corrupt("check " + g.ID)
		}
		seen[c.ID] = true
		for _, d := range c.Diagnostics {
			if d.Code == "" {
				return corrupt("check diagnostic")
			}
		}
		for _, f := range c.Findings {
			if f.PolicyID != g.PolicyID {
				return corrupt("finding policy")
			}
			if f.Line != nil && *f.Line < 1 {
				return corrupt("finding line")
			}
			o.Findings = append(o.Findings, domain.Finding{PolicyID: f.PolicyID, RuleID: f.RuleID, Path: f.Path, Message: f.Message, Line: f.Line})
		}
		switch c.Status {
		case "PASS":
			if c.Inspected == nil || *c.Inspected < 1 || len(c.Findings) != 0 || c.ExitCode == nil || *c.ExitCode != 0 {
				return corrupt("passing check " + g.ID)
			}
		case "FAIL":
			if len(c.Findings) == 0 && (c.ExitCode == nil || *c.ExitCode == 0) {
				return corrupt("failing check " + g.ID)
			}
			failed = true
		case "REFUSE", "NOT_RUN":
			refused = true
		default:
			return corrupt("check status")
		}
	}
	for _, x := range ev.Observations {
		if !seen[x.ID] || x.Kind != "guard" || !ordered(x.StartedAt, x.FinishedAt, true) || x.Executable != nil && x.Executable.SHA256 != nil && !hexPattern.MatchString(*x.Executable.SHA256) {
			return corrupt("observation")
		}
	}
	// Recompute the restricted refusal-first required-gate decision.
	want := "ACCEPT"
	switch {
	case refused:
		want = "REFUSE"
	case failed:
		want = "REJECT"
	case ev.TargetCount == nil || *ev.TargetCount < 1:
		want = "REFUSE"
	}
	if ev.Decision != want {
		return corrupt("decision")
	}
	code := map[string]int{"ACCEPT": 0, "REJECT": 1, "REFUSE": 2}[want]
	if exit == nil || *exit != code {
		return corrupt("exit code")
	}
	o.Decision, o.SchemaVersion, o.RunID, o.ConfigDigest = ev.Decision, ev.SchemaVersion, ev.RunID, *ev.ConfigDigest
	return o, nil
}

func ordered(a, b *string, nullable bool) bool {
	if a == nil || b == nil {
		return nullable && a == nil && b == nil || nullable && a != nil && b == nil
	}
	x, ea := time.Parse(time.RFC3339Nano, *a)
	y, eb := time.Parse(time.RFC3339Nano, *b)
	return ea == nil && eb == nil && !y.Before(x)
}
func samePolicies(a, b []Policy) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// strictDecode rejects invalid UTF-8, duplicate keys, excessive depth, trailing
// input, missing required fields, nulls outside nullable fields and unknown fields.
func strictDecode(b []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		if depth > 32 {
			return nil, fail("LIMIT_EXCEEDED", "JSON depth")
		}
		t, e := d.Token()
		if e != nil {
			return nil, fail("EVIDENCE_CORRUPT", "JSON syntax")
		}
		switch t {
		case json.Delim('{'):
			m := map[string]any{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return nil, fail("EVIDENCE_CORRUPT", "JSON syntax")
				}
				key := k.(string)
				if _, ok := m[key]; ok {
					return nil, fail("EVIDENCE_CORRUPT", "duplicate key "+key)
				}
				if m[key], e = read(depth + 1); e != nil {
					return nil, e
				}
			}
			if _, e = d.Token(); e != nil {
				return nil, fail("EVIDENCE_CORRUPT", "JSON syntax")
			}
			return m, nil
		case json.Delim('['):
			a := []any{}
			for d.More() {
				v, e := read(depth + 1)
				if e != nil {
					return nil, e
				}
				a = append(a, v)
			}
			if _, e = d.Token(); e != nil {
				return nil, fail("EVIDENCE_CORRUPT", "JSON syntax")
			}
			return a, nil
		}
		return t, nil
	}
	v, e := read(0)
	if e != nil {
		return e
	}
	if _, ok := v.(map[string]any); !ok {
		return fail("EVIDENCE_CORRUPT", "JSON object")
	}
	if _, e = d.Token(); e != io.EOF {
		return fail("EVIDENCE_CORRUPT", "trailing JSON")
	}
	if e = presence(v, reflect.TypeOf(out).Elem(), "$"); e != nil {
		return e
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if dec.Decode(out) != nil {
		return fail("EVIDENCE_CORRUPT", "JSON types or unknown field")
	}
	return nil
}
func presence(v any, t reflect.Type, at string) error {
	if t == reflect.TypeOf(json.RawMessage{}) {
		return nil
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if v == nil {
		return fail("EVIDENCE_CORRUPT", "null "+at)
	}
	switch t.Kind() {
	case reflect.Struct:
		m, ok := v.(map[string]any)
		if !ok {
			return fail("EVIDENCE_CORRUPT", "object "+at)
		}
		for i := 0; i < t.NumField(); i++ {
			tag := strings.Split(t.Field(i).Tag.Get("json"), ",")
			x, ok := m[tag[0]]
			optional, nullable := false, false
			for _, opt := range tag[1:] {
				optional = optional || opt == "omitempty"
				nullable = nullable || opt == "nullable"
			}
			if !ok {
				if optional {
					continue
				}
				return fail("EVIDENCE_CORRUPT", "missing "+at+"."+tag[0])
			}
			if x == nil && nullable {
				continue
			}
			if e := presence(x, t.Field(i).Type, at+"."+tag[0]); e != nil {
				return e
			}
		}
	case reflect.Slice:
		a, ok := v.([]any)
		if !ok {
			return fail("EVIDENCE_CORRUPT", "array "+at)
		}
		for _, x := range a {
			if e := presence(x, t.Elem(), at+"[]"); e != nil {
				return e
			}
		}
	}
	return nil
}

// Campaign report versions Evaris reads.
var campaignVersions = []string{"invaris.evidence/v1alpha4", "invaris.evidence/v1alpha5"}

// ValidateCampaign reads stdout from `invaris campaign`. Unlike check
// evidence, which is decoded strictly in full, a campaign report is large and
// versioned by Invaris, so only the fields Evaris relies on are checked: the
// schema version, the action, the run ID, the decision with a matching exit
// code (0 ACCEPT, 1 REJECT, 2 REFUSE), the digest of the frozen campaign
// file, and that every experiment ran a frozen harness file with its digest.
// A harness the reviewer did not freeze would otherwise be read from the
// delivered tree. The full report is kept as evidence.
func ValidateCampaign(b []byte, exit *int, campaignDigest domain.Digest, harnesses []application.Blob) (Outcome, error) {
	var o Outcome
	if len(b) > maxEvidence {
		return o, fail("LIMIT_EXCEEDED", "evidence")
	}
	var r struct {
		SchemaVersion        string `json:"schemaVersion"`
		RunID                string `json:"runId"`
		Action               string `json:"action"`
		CampaignConfigDigest string `json:"campaignConfigDigest"`
		Decision             string `json:"decision"`
		Experiments          []struct {
			Harness       string `json:"harness"`
			HarnessDigest string `json:"harnessDigest"`
		} `json:"experiments"`
	}
	if json.Unmarshal(b, &r) != nil {
		return o, fail("EVIDENCE_CORRUPT", "campaign JSON")
	}
	if !slices.Contains(campaignVersions, r.SchemaVersion) {
		return o, fail("UNSUPPORTED_SCHEMA", "campaign "+r.SchemaVersion)
	}
	if r.Action != "campaign" || !runIDPattern.MatchString(r.RunID) {
		return o, fail("EVIDENCE_CORRUPT", "campaign identity")
	}
	if domain.Digest(r.CampaignConfigDigest) != campaignDigest {
		return o, fail("EVIDENCE_CORRUPT", "campaignConfigDigest")
	}
	frozen := map[string]domain.Digest{}
	for _, h := range harnesses {
		frozen[h.Path] = domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(h.Data)))
	}
	if len(r.Experiments) == 0 {
		return o, fail("EVIDENCE_CORRUPT", "campaign experiments")
	}
	for _, x := range r.Experiments {
		if d, ok := frozen[x.Harness]; !ok || domain.Digest(x.HarnessDigest) != d {
			return o, fail("EVIDENCE_CORRUPT", "campaign harness is not a frozen verification file")
		}
	}
	want := map[string]int{"ACCEPT": 0, "REJECT": 1, "REFUSE": 2}
	code, ok := want[r.Decision]
	if !ok || exit == nil || *exit != code {
		return o, fail("EVIDENCE_CORRUPT", "campaign decision and exit code")
	}
	return Outcome{Decision: r.Decision, SchemaVersion: r.SchemaVersion, RunID: r.RunID, ConfigDigest: r.CampaignConfigDigest, Findings: []domain.Finding{}}, nil
}
