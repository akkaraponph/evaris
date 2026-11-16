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
	RequiresReview bool     `json:"requiresReview"`
	// Verification is the optional P1 Invaris check; absent means not configured.
	Verification *VerificationConfig `json:"verification,omitempty"`
}

// VerificationConfig names a private, trusted Invaris harness for one case.
type VerificationConfig struct {
	Harness        string `json:"harness"`
	Required       bool   `json:"required"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
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
	// SnapshotExclude lists relative paths, such as build output, that the
	// command-exit grader leaves out of its before and after scratch
	// snapshots. Only command-exit criteria accept it.
	SnapshotExclude []string `json:"snapshotExclude,omitempty"`
}
type Command struct {
	Program        string   `json:"program"`
	Args           []string `json:"args"`
	EnvAllowlist   []string `json:"envAllowlist"`
	TimeoutSeconds int      `json:"timeoutSeconds,omitempty"`
}

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
	if !idPattern.MatchString(e.ID) || strings.TrimSpace(e.Revision) == "" || !Relative(e.OutputRoot) || len(e.Cases) < 1 || len(e.Cases) > 100 || len(e.Arms) != 2 || strings.TrimSpace(e.Constraints.Tools) == "" || strings.TrimSpace(e.Constraints.Permissions) == "" || strings.TrimSpace(e.Constraints.Budget) == "" {
		return bad("INVALID_CONFIG")
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
		for _, p := range c.AllowedChanges {
			if !Relative(p) {
				return bad("PATH_UNSAFE")
			}
			if c.Verification != nil && p == HarnessName {
				return bad("PATH_UNSAFE")
			}
		}
		if v := c.Verification; v != nil && (!Relative(v.Harness) || v.TimeoutSeconds < 1 || v.TimeoutSeconds > 300) {
			return bad("INVALID_CONFIG")
		}
	}
	seen = map[string]bool{}
	for _, a := range e.Arms {
		if seen[a.ID] {
			return bad("DUPLICATE_ID")
		}
		seen[a.ID] = true
		if a.Instructions == nil || a.ID != "baseline" && a.ID != "specialist" || a.ID == "baseline" && len(a.Instructions) != 0 || a.ID == "specialist" && len(a.Instructions) == 0 {
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
