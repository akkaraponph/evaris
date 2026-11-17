package domain

import "testing"

func TestMatchPattern(t *testing.T) {
	for _, c := range []struct {
		pattern, name string
		want          bool
	}{
		{"src/**", "src/a/b.ts", true},
		{"src/**", "src", true},
		{"src/*.ts", "src/a/b.ts", false},
		{"src/*.ts", "src/b.ts", true},
		{"**", "x", true},
		{"a/**/b", "a/b", true},
		{"a/**/b", "a/x/y/b", true},
		{"a/**/b", "a/x/y/c", false},
		{"**/.env", "app/.env", true},
		{"**/.env", ".env", true},
		{"result.txt", "result.txt", true},
		{"result.txt", "x/result.txt", false},
	} {
		if got := MatchPattern(c.pattern, c.name); got != c.want {
			t.Errorf("MatchPattern(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

func TestValidPattern(t *testing.T) {
	for p, want := range map[string]bool{
		"app/**": true, "**": true, "src/*.ts": true, "a/[bc]/d": true, "result.txt": true,
		"[": false, "a/[/b": false, "a**/b": false, "/abs": false, "../x": false, "a/./b": false, "": false,
	} {
		if got := ValidPattern(p); got != want {
			t.Errorf("ValidPattern(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestGlobCannotAllowHarness(t *testing.T) {
	base := Experiment{SchemaVersion: "evaris.experiment/v1alpha1", ID: "x", Revision: "r", OutputRoot: "evidence",
		Constraints: Constraints{Tools: "t", Permissions: "p", Budget: "b"},
		Arms:        []Arm{{ID: "baseline", Instructions: []Instruction{}}, {ID: "specialist", Instructions: []Instruction{{Path: "i.txt", Revision: "r"}}}}}
	c := Case{ID: "c", Fixture: "f", Task: "t", Oracle: "o", SourceRevision: "r", AllowedChanges: []string{"**"},
		Verification: &VerificationConfig{Harness: "h.json", Required: true, TimeoutSeconds: 30}}
	base.Cases = []Case{c}
	if err := ValidateExperiment(base); err == nil || err.Error() != "PATH_UNSAFE: experiment" {
		t.Fatalf("** with verification: %v", err)
	}
	base.Cases[0].AllowedChanges = []string{"app/**"}
	if err := ValidateExperiment(base); err != nil {
		t.Fatalf("app/** refused: %v", err)
	}
	base.Cases[0].ProtectedPaths = []string{"a/[/b"}
	if err := ValidateExperiment(base); err == nil || err.Error() != "PATH_UNSAFE: experiment" {
		t.Fatalf("malformed protected pattern: %v", err)
	}
}
