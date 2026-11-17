package domain

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
)

type Digest string
type Reason string
type Failure struct {
	Code Reason
	Path string
	Err  error
}

func (e *Failure) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Path) }
func (e *Failure) Unwrap() error { return e.Err }

type Experiment struct {
	SchemaVersion string      `json:"schemaVersion"`
	ID            string      `json:"id"`
	Revision      string      `json:"revision"`
	Cases         []Case      `json:"cases"`
	Arms          []Arm       `json:"arms"`
	OutputRoot    string      `json:"outputRoot"`
	Constraints   Constraints `json:"constraints"`
	// Limits optionally raises the snapshot entry bound for this experiment's
	// fixtures, imported trees and grader scratch copies.
	Limits *Limits `json:"limits,omitempty"`
	// ReviewerFiles are files graders rely on (scripts, hidden tests),
	// relative to the experiment file. Prepare records their digests and
	// Evaluate refuses to grade if any changed.
	ReviewerFiles []string `json:"reviewerFiles,omitempty"`
	// Mode is "comparison" (the default when omitted: baseline and specialist
	// arms) or "acceptance" (exactly one arm, no comparison).
	Mode string `json:"mode,omitempty"`
}

// Experiment modes.
const (
	ModeComparison = "comparison"
	ModeAcceptance = "acceptance"
)

// Acceptance reports whether the experiment evaluates a single arm.
func (e Experiment) Acceptance() bool { return e.Mode == ModeAcceptance }

// MaxReviewerFiles bounds Experiment.ReviewerFiles.
const MaxReviewerFiles = 64

// Limits are per-experiment snapshot bounds.
type Limits struct {
	MaxEntries int `json:"maxEntries"`
}

// Snapshot entry bounds: the default, and the largest limits.maxEntries.
const (
	DefaultMaxEntries = 2000
	MaxEntriesCeiling = 200000
)

// MaxEntries is the experiment's effective snapshot entry bound.
func (e Experiment) MaxEntries() int {
	if e.Limits != nil {
		return e.Limits.MaxEntries
	}
	return DefaultMaxEntries
}

type Constraints struct {
	Tools       string `json:"tools"`
	Permissions string `json:"permissions"`
	Budget      string `json:"budget"`
}
type Case struct {
	ID             string   `json:"id"`
	Fixture        string   `json:"fixture"`
	Task           string   `json:"task"`
	Oracle         string   `json:"oracle"`
	SourceRevision string   `json:"sourceRevision"`
	AllowedChanges []string `json:"allowedChanges"`
	// ProtectedPaths are patterns whose changes always fail authorization,
	// even when allowedChanges matches them.
	ProtectedPaths []string `json:"protectedPaths,omitempty"`
	RequiresReview bool     `json:"requiresReview"`
	// Verification is the optional P1 Invaris check; absent means not configured.
	Verification *VerificationConfig `json:"verification,omitempty"`
}

// VerificationConfig names a private, trusted Invaris harness for one case.
type VerificationConfig struct {
	Harness        string `json:"harness"`
	Required       bool   `json:"required"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	// Action is "check" (the default when omitted) or "campaign". For a
	// campaign, Harness names the invaris.campaign/v1alpha1 file.
	Action string `json:"action,omitempty"`
	// Files are the harness files a campaign references, relative to the
	// experiment file and inside the campaign file's directory. Only campaign
	// verification accepts them; they are frozen like the campaign file.
	Files []string `json:"files,omitempty"`
}

// Verification actions.
const (
	VerifyCheck    = "check"
	VerifyCampaign = "campaign"
)

// Campaign reports whether the verification runs an Invaris campaign.
func (v VerificationConfig) Campaign() bool { return v.Action == VerifyCampaign }

// FileRel is f relative to the campaign file's directory.
func (v VerificationConfig) FileRel(f string) string {
	dir := path.Dir(v.Harness)
	if dir == "." {
		return f
	}
	return strings.TrimPrefix(f, dir+"/")
}

// validVerification checks the action and the campaign files.
func validVerification(v *VerificationConfig) bool {
	if v.Action != "" && v.Action != VerifyCheck && v.Action != VerifyCampaign {
		return false
	}
	if v.Files != nil && !v.Campaign() || len(v.Files) > 64 {
		return false
	}
	dir := path.Dir(v.Harness)
	seen := map[string]bool{v.Harness: true}
	for _, f := range v.Files {
		if !Relative(f) || seen[f] || dir != "." && !strings.HasPrefix(f, dir+"/") || v.FileRel(f) == HarnessName {
			return false
		}
		seen[f] = true
	}
	return true
}

// HarnessName is the reserved scratch-root file the frozen harness is written to.
const HarnessName = "invaris.json"

type Arm struct {
	ID           string        `json:"id"`
	Instructions []Instruction `json:"instructions"`
}
type Instruction struct {
	Path     string `json:"path"`
	Revision string `json:"revision"`
}
type Oracle struct {
	SchemaVersion string      `json:"schemaVersion"`
	Criteria      []Criterion `json:"criteria"`
}
type Criterion struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Required bool            `json:"required"`
	Path     string          `json:"path,omitempty"`
	Expected json.RawMessage `json:"expected,omitempty"`
	Command  *Command        `json:"command,omitempty"`
	// Dependencies are directories linked beside a command-exit criterion's
	// scratch copy, such as node_modules. Only command-exit accepts them.
	Dependencies []Dependency `json:"dependencies,omitempty"`
	// SnapshotExclude lists relative paths, such as build output, that the
	// command-exit grader leaves out of its before and after scratch
	// snapshots. Only command-exit criteria accept it.
	SnapshotExclude []string `json:"snapshotExclude,omitempty"`
}

// Dependency links the absolute directory Source as Name in the parent of a
// command's scratch copy, where resolvers that walk up (Node) find it. Its
// contents are not copied or hashed.
type Dependency struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

type Command struct {
	Program        string   `json:"program"`
	Args           []string `json:"args"`
	EnvAllowlist   []string `json:"envAllowlist"`
	TimeoutSeconds int      `json:"timeoutSeconds,omitempty"`
}

// maxPatterns bounds allowedChanges and protectedPaths per case.
const maxPatterns = 256

// ValidID reports whether id is a valid case or arm ID.
func ValidID(id string) bool { return idPattern.MatchString(id) }

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func Relative(p string) bool {
	if p == "" || strings.ContainsAny(p, "\\:\x00") || strings.HasPrefix(p, "/") {
		return false
	}
	for _, s := range strings.Split(p, "/") {
		if s == "" || s == "." || s == ".." {
			return false
		}
	}
	return path.Clean(p) == p
}
func ValidateExperiment(e Experiment) error {
	bad := func(code Reason) error { return &Failure{Code: code, Path: "experiment"} }
	if e.SchemaVersion != "evaris.experiment/v1alpha1" {
		return bad("UNSUPPORTED_SCHEMA")
	}
	if !idPattern.MatchString(e.ID) || strings.TrimSpace(e.Revision) == "" || !Relative(e.OutputRoot) || len(e.Cases) < 1 || len(e.Cases) > 100 || e.Mode != "" && e.Mode != ModeComparison && e.Mode != ModeAcceptance || e.Acceptance() && len(e.Arms) != 1 || !e.Acceptance() && len(e.Arms) != 2 || strings.TrimSpace(e.Constraints.Tools) == "" || strings.TrimSpace(e.Constraints.Permissions) == "" || strings.TrimSpace(e.Constraints.Budget) == "" {
		return bad("INVALID_CONFIG")
	}
	if e.Limits != nil && (e.Limits.MaxEntries < 1 || e.Limits.MaxEntries > MaxEntriesCeiling) {
		return bad("INVALID_CONFIG")
	}
	if len(e.ReviewerFiles) > MaxReviewerFiles {
		return bad("INVALID_CONFIG")
	}
	reviewer := map[string]bool{}
	for _, p := range e.ReviewerFiles {
		if !Relative(p) || reviewer[p] {
			return bad("PATH_UNSAFE")
		}
		reviewer[p] = true
	}
	seen := map[string]bool{}
	for _, c := range e.Cases {
		if seen[c.ID] {
			return bad("DUPLICATE_ID")
		}
		seen[c.ID] = true
		if !idPattern.MatchString(c.ID) || !Relative(c.Fixture) || !Relative(c.Task) || !Relative(c.Oracle) || strings.TrimSpace(c.SourceRevision) == "" || c.AllowedChanges == nil {
			return bad("INVALID_CONFIG")
		}
		if len(c.AllowedChanges) > maxPatterns || len(c.ProtectedPaths) > maxPatterns {
			return bad("INVALID_CONFIG")
		}
		for _, p := range c.AllowedChanges {
			if !ValidPattern(p) {
				return bad("PATH_UNSAFE")
			}
			if c.Verification != nil && MatchPattern(p, HarnessName) {
				return bad("PATH_UNSAFE")
			}
		}
		for _, p := range c.ProtectedPaths {
			if !ValidPattern(p) {
				return bad("PATH_UNSAFE")
			}
		}
		if v := c.Verification; v != nil && (!Relative(v.Harness) || v.TimeoutSeconds < 1 || v.TimeoutSeconds > 300 || !validVerification(v)) {
			return bad("INVALID_CONFIG")
		}
	}
	seen = map[string]bool{}
	for _, a := range e.Arms {
		if seen[a.ID] {
			return bad("DUPLICATE_ID")
		}
		seen[a.ID] = true
		if e.Acceptance() {
			// The single acceptance arm has any valid ID and any instructions.
			if a.Instructions == nil || !idPattern.MatchString(a.ID) {
				return bad("INVALID_CONFIG")
			}
		} else if a.Instructions == nil || a.ID != "baseline" && a.ID != "specialist" || a.ID == "baseline" && len(a.Instructions) != 0 || a.ID == "specialist" && len(a.Instructions) == 0 {
			return bad("INVALID_CONFIG")
		}
		for _, i := range a.Instructions {
			if !Relative(i.Path) || strings.TrimSpace(i.Revision) == "" {
				return bad("INVALID_CONFIG")
			}
		}
	}
	return nil
}
