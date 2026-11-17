package invaris

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/akkaraponph/evaris/internal/domain"
)

func TestVersionProbe(t *testing.T) {
	zero, two := 0, 2
	for _, tc := range []struct {
		stdout    string
		exit      *int
		version   string
		supported bool
	}{
		{"invaris v0.5.0\n", &zero, "v0.5.0", true},
		{"invaris v0.5.3\n", &zero, "v0.5.3", true},
		{"invaris dev\n", &zero, "dev", true},
		{"invaris v0.4.1-0.20261116080255-c864c43fe5da+dirty\n", &zero, "v0.4.1-0.20261116080255-c864c43fe5da+dirty", true},
		{"invaris v0.9.0\n", &zero, "v0.9.0", false},
		{"invaris v1.0.0\n", &zero, "v1.0.0", false},
		{"", &two, "", true},                  // legacy: unsupported action: --version
		{"usage: invaris\n", &zero, "", true}, // legacy shape, no version line
		{"invaris v0.5.0\n", nil, "", true},   // did not finish
	} {
		v, ok := classifyVersion([]byte(tc.stdout), tc.exit)
		got := ""
		if v != nil {
			got = *v
		}
		if got != tc.version || ok != tc.supported {
			t.Errorf("%q: version %q supported %v, want %q %v", tc.stdout, got, ok, tc.version, tc.supported)
		}
	}
}

func TestUnsupportedInvarisRelease(t *testing.T) {
	harness := harnessBytes("/guard")
	dir := canonical(t)
	exe := filepath.Join(dir, "invaris")
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'invaris v0.9.0'; exit 0; fi\necho called >> " + filepath.Join(dir, "calls") + "\nexit 0\n"
	if err := os.WriteFile(exe, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	v, _ := Client{Executable: exe}.Verify(context.Background(), frozen(t, "approved\n", harness, true))
	if v.Status != "UNAVAILABLE" || v.Reason != "UNSUPPORTED_TOOL_VERSION" || calls(exe) != 0 ||
		v.Message != "verification executable: UNSUPPORTED_TOOL_VERSION; this Invaris release is not supported; see docs/invaris.md" || !domain.ValidGraderMessage(v.Message) {
		t.Fatalf("%+v calls=%d", v, calls(exe))
	}
}

func TestFindingLine(t *testing.T) {
	h, _ := ParseHarness(harnessBytes("/guard"))
	d := digestOf(harnessBytes("/guard"))
	for line, ok := range map[int]bool{3: true, 0: false, -1: false} {
		m := doc("REJECT", "/scratch", d)
		m["checks"].([]any)[0].(map[string]any)["findings"].([]any)[0].(map[string]any)["line"] = line
		o, e := ValidateEvidence(encode(m), exitPtr(1), h, d, "/scratch")
		if (e == nil) != ok {
			t.Fatalf("line %d: err %v", line, e)
		}
		if ok && (o.Findings[0].Line == nil || *o.Findings[0].Line != 3) {
			t.Fatalf("line lost: %+v", o.Findings)
		}
	}
	o, e := ValidateEvidence(encode(doc("REJECT", "/scratch", d)), exitPtr(1), h, d, "/scratch")
	if e != nil || o.Findings[0].Line != nil {
		t.Fatalf("omitted line: %+v %v", o.Findings, e)
	}
}
