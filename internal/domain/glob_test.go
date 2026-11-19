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
		// Literal paths with glob characters still match themselves
		// (Next.js dynamic routes), and keep their glob meaning otherwise.
		{"app/(site)/products/[id]/page.tsx", "app/(site)/products/[id]/page.tsx", true},
		{"app/[...slug]/page.tsx", "app/[...slug]/page.tsx", true},
		{"app/[id]/page.tsx", "app/i/page.tsx", true},
		{"app/[id]/page.tsx", "app/[id]/other.tsx", false},
		{"docs/what?.md", "docs/what?.md", true},
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
	if err := ValidateExperiment(base); err == nil || err.Error() != `PATH_UNSAFE: case c allowedChanges "**" matches invaris.json; add invaris.json to protectedPaths` {
		t.Fatalf("** with verification: %v", err)
	}
	// The deny-list form works with verification once invaris.json is protected.
	base.Cases[0].ProtectedPaths = []string{"invaris.json", "LICENSE"}
	if err := ValidateExperiment(base); err != nil {
		t.Fatalf("** with protected invaris.json refused: %v", err)
	}
	base.Cases[0].ProtectedPaths = nil
	base.Cases[0].AllowedChanges = []string{"app/**"}
	if err := ValidateExperiment(base); err != nil {
		t.Fatalf("app/** refused: %v", err)
	}
	base.Cases[0].ProtectedPaths = []string{"a/[/b"}
	if err := ValidateExperiment(base); err == nil || err.Error() != `PATH_UNSAFE: case c protectedPaths "a/[/b" is not a valid pattern` {
		t.Fatalf("malformed protected pattern: %v", err)
	}
}

func TestVerificationActionValidation(t *testing.T) {
	ok := func(v VerificationConfig) bool { return validVerification(&v) }
	for name, tc := range map[string]struct {
		v    VerificationConfig
		want bool
	}{
		"check default":         {VerificationConfig{Harness: "r/h.json"}, true},
		"campaign with files":   {VerificationConfig{Harness: "r/c.json", Action: "campaign", Files: []string{"r/m.json", "r/sub/g.json"}}, true},
		"unknown action":        {VerificationConfig{Harness: "r/h.json", Action: "mutate"}, false},
		"files on check":        {VerificationConfig{Harness: "r/h.json", Files: []string{"r/m.json"}}, false},
		"file outside dir":      {VerificationConfig{Harness: "r/c.json", Action: "campaign", Files: []string{"other/m.json"}}, false},
		"file is the campaign":  {VerificationConfig{Harness: "r/c.json", Action: "campaign", Files: []string{"r/c.json"}}, false},
		"file named invaris":    {VerificationConfig{Harness: "r/c.json", Action: "campaign", Files: []string{"r/invaris.json"}}, false},
		"duplicate file":        {VerificationConfig{Harness: "r/c.json", Action: "campaign", Files: []string{"r/m.json", "r/m.json"}}, false},
		"root campaign any dir": {VerificationConfig{Harness: "c.json", Action: "campaign", Files: []string{"x/m.json"}}, true},
	} {
		if got := ok(tc.v); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}
