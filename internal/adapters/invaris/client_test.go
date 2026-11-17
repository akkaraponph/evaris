package invaris

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
)

func canonical(t *testing.T) string {
	t.Helper()
	d, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return d
}

// frozen builds a run whose snapshot holds result.txt with content (none if empty).
func frozen(t *testing.T, content string, harness []byte, allow bool) application.FrozenRun {
	t.Helper()
	tree := canonical(t)
	if content != "" {
		os.WriteFile(filepath.Join(tree, "result.txt"), []byte(content), 0600)
	}
	return application.FrozenRun{SnapshotRoot: tree, AllowCommands: allow, Case: application.FrozenCase{Case: domain.Case{ID: "sample", Verification: &domain.VerificationConfig{Harness: "reviewer/invaris.json", Required: true, TimeoutSeconds: 5}}, Harness: harness}}
}

// fake writes an executable that emits template with @TARGET@ replaced by the
// harness directory, records each invocation in a marker, and exits with code.
func fake(t *testing.T, template []byte, body string) string {
	t.Helper()
	dir := canonical(t)
	tmpl := filepath.Join(dir, "template.json")
	os.WriteFile(tmpl, template, 0600)
	// Like Invaris before 0.5, the fake rejects --version at once and is not
	// counted as a call.
	script := "#!/bin/sh\n[ \"$1\" = --version ] && { echo 'unsupported action: --version' >&2; exit 2; }\necho called >> " + filepath.Join(dir, "calls") + "\nd=$(dirname \"$2\")\n" + strings.ReplaceAll(body, "@TEMPLATE@", tmpl) + "\n"
	path := filepath.Join(dir, "invaris")
	if e := os.WriteFile(path, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	return path
}
func calls(exe string) int {
	b, _ := os.ReadFile(filepath.Join(filepath.Dir(exe), "calls"))
	return strings.Count(string(b), "called")
}
func templateFor(decision string, harness []byte) []byte {
	return encode(doc(decision, "@TARGET@", digestOf(harness)))
}

func TestInvarisContract(t *testing.T) {
	harness := harnessBytes("/guard")
	for decision, code := range map[string]string{"ACCEPT": "0", "REJECT": "1", "REFUSE": "2"} {
		exe := fake(t, templateFor(decision, harness), `sed "s#@TARGET@#$d#g" @TEMPLATE@; exit `+code)
		v, blobs := Client{Executable: exe}.Verify(context.Background(), frozen(t, "approved\n", harness, true))
		if v.Status != decision || v.ExitCode == nil || v.RunID == nil || v.ConfigDigest == nil || v.SchemaVersion == nil || *v.SchemaVersion != "invaris.evidence/v1alpha2" {
			t.Fatalf("%s: %+v", decision, v)
		}
		if len(v.Evidence) != 3 || len(blobs) != 3 || v.Executable == nil || v.Executable.Path != exe {
			t.Fatalf("%s: evidence not retained: %+v", decision, v.Evidence)
		}
		var raw map[string]any
		if e := json.Unmarshal(blobs[0].Data, &raw); e != nil || raw["decision"] != decision {
			t.Fatalf("%s: raw stdout not retained verbatim", decision)
		}
		if decision == "REJECT" && len(v.Findings) != 1 || decision != "REJECT" && len(v.Findings) != 0 {
			t.Fatalf("%s: findings %+v", decision, v.Findings)
		}
	}
}

func TestInvarisClientFailures(t *testing.T) {
	harness := harnessBytes("/guard")
	accept := templateFor("ACCEPT", harness)
	for name, tc := range map[string]struct {
		body    string
		ctx     func() (context.Context, context.CancelFunc)
		reason  domain.Reason
		timeout int
	}{
		"timeout": {`sleep 30`, nil, "TIMEOUT", 1},
		"cancellation": {`sleep 30`, func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(300*time.Millisecond, cancel)
			return ctx, cancel
		}, "CANCELED", 5},
		// Exercise the byte bound without racing the one-second timeout cases.
		// Zero bytes are sufficient: the process limit precedes JSON decoding.
		"output limit":         {`head -c 4200000 /dev/zero; exit 0`, nil, "LIMIT_EXCEEDED", 5},
		"malformed":            {`echo not-json; exit 0`, nil, "EVIDENCE_CORRUPT", 5},
		"exit 2 silent":        {`echo usage >&2; exit 2`, nil, "OUTPUT_MISSING", 5},
		"partial then timeout": {`head -c 100 @TEMPLATE@; sleep 30`, nil, "TIMEOUT", 1},
		"wrong target":         {`sed "s#@TARGET@#/elsewhere#g" @TEMPLATE@; exit 0`, nil, "EVIDENCE_CORRUPT", 5},
	} {
		t.Run(name, func(t *testing.T) {
			exe := fake(t, accept, tc.body)
			f := frozen(t, "approved\n", harness, true)
			f.Case.Case.Verification.TimeoutSeconds = tc.timeout
			ctx, cancel := context.WithCancel(context.Background())
			if tc.ctx != nil {
				ctx, cancel = tc.ctx()
			}
			defer cancel()
			start := time.Now()
			v, blobs := Client{Executable: exe}.Verify(ctx, f)
			if v.Status != "ERROR" || v.Reason != tc.reason || v.RunID != nil {
				t.Fatalf("%+v", v)
			}
			if len(blobs) != 3 || len(v.Evidence) != 3 || time.Since(start) > 10*time.Second {
				t.Fatalf("evidence=%d elapsed=%s", len(blobs), time.Since(start))
			}
			if tc.reason == "LIMIT_EXCEEDED" && (len(blobs[0].Data) != maxEvidence || v.Evidence[0].Bytes != int64(maxEvidence)) {
				t.Fatalf("stdout bound: retained=%d evidence=%d want=%d", len(blobs[0].Data), v.Evidence[0].Bytes, maxEvidence)
			}
		})
	}
}

func TestInvarisMissingExecutable(t *testing.T) {
	harness := harnessBytes("/guard")
	for _, exe := range []string{"/nonexistent/invaris", "relative/invaris", ""} {
		v, blobs := Client{Executable: exe}.Verify(context.Background(), frozen(t, "approved\n", harness, true))
		if v.Status != "ERROR" || v.Reason != "TOOL_MISSING" || len(blobs) != 0 {
			t.Fatalf("%q: %+v", exe, v)
		}
	}
}

func TestInvarisConsentAndHarnessCollision(t *testing.T) {
	harness := harnessBytes("/guard")
	exe := fake(t, templateFor("ACCEPT", harness), `sed "s#@TARGET@#$d#g" @TEMPLATE@; exit 0`)
	v, _ := Client{Executable: exe}.Verify(context.Background(), frozen(t, "approved\n", harness, false))
	if v.Status != "UNAVAILABLE" || v.Reason != "COMMAND_NOT_AUTHORIZED" || calls(exe) != 0 {
		t.Fatalf("ran without consent: %+v", v)
	}
	f := frozen(t, "approved\n", harness, true)
	os.WriteFile(filepath.Join(f.SnapshotRoot, domain.HarnessName), []byte(`{"agent":"supplied"}`), 0600)
	v, _ = Client{Executable: exe}.Verify(context.Background(), f)
	if v.Status != "ERROR" || v.Reason != "PATH_UNSAFE" || calls(exe) != 0 {
		t.Fatalf("imported harness replaced frozen harness: %+v", v)
	}
	f = frozen(t, "approved\n", []byte(`{"version":"invaris.dev/v1alpha2"}`), true)
	v, _ = Client{Executable: exe}.Verify(context.Background(), f)
	if v.Status != "ERROR" || calls(exe) != 0 {
		t.Fatalf("ran an unsupported harness: %+v", v)
	}
}

// TestRealInvaris needs a separately provisioned binary built from PinnedRevision.
func TestRealInvaris(t *testing.T) {
	exe := os.Getenv("EVARIS_TEST_INVARIS")
	if exe == "" {
		t.Skip("EVARIS_TEST_INVARIS unset: real Invaris compatibility NOT verified (blocks the P1 release gate)")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("unsupported OS")
	}
	guard := filepath.Join(canonical(t), "guard")
	build := exec.Command("go", "build", "-o", guard, "../../../testdata/invaris/guard")
	if b, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build guard: %v %s", e, b)
	}
	harness := harnessBytes(guard)
	for _, tc := range []struct {
		content, status string
		exit            int
	}{{"approved\n", "ACCEPT", 0}, {"rejected\n", "REJECT", 1}, {"", "REFUSE", 2}} {
		v, blobs := Client{Executable: exe}.Verify(context.Background(), frozen(t, tc.content, harness, true))
		if v.Status != tc.status || v.ExitCode == nil || *v.ExitCode != tc.exit || v.ConfigDigest == nil || domain.Digest(*v.ConfigDigest) != digestOf(harness) {
			t.Fatalf("%q: %+v\nstdout: %s\nstderr: %s", tc.content, v, blobs[0].Data, blobs[1].Data)
		}
		var raw struct {
			SchemaVersion string `json:"schemaVersion"`
			Decision      string `json:"decision"`
		}
		if e := json.Unmarshal(blobs[0].Data, &raw); e != nil || raw.SchemaVersion != "invaris.evidence/v1alpha2" || raw.Decision != tc.status {
			t.Fatalf("raw evidence: %+v %v", raw, e)
		}
		t.Logf("%s exit=%d runId=%s findings=%d", v.Status, *v.ExitCode, *v.RunID, len(v.Findings))
	}
}

func TestVerificationMessages(t *testing.T) {
	harness := harnessBytes("/guard")
	for name, tc := range map[string]struct {
		exe     func(t *testing.T) string
		prepare func(f application.FrozenRun)
		reason  domain.Reason
		message string
	}{
		"relative executable": {func(*testing.T) string { return "relative/invaris" }, nil, "TOOL_MISSING",
			"verification executable: TOOL_MISSING; the invaris executable is not an absolute path to an executable file"},
		"imported harness": {func(t *testing.T) string { return fake(t, templateFor("ACCEPT", harness), "exit 0") },
			func(f application.FrozenRun) {
				os.WriteFile(filepath.Join(f.SnapshotRoot, domain.HarnessName), []byte(`{}`), 0600)
			}, "PATH_UNSAFE",
			"verification scratch: PATH_UNSAFE; the imported tree contains a symbolic link, a special file or its own invaris.json"},
		"no report": {func(t *testing.T) string { return fake(t, templateFor("ACCEPT", harness), "exit 0") }, nil, "OUTPUT_MISSING",
			"verification command (command exited 0): OUTPUT_MISSING; invaris printed no report"},
		"malformed report": {func(t *testing.T) string { return fake(t, templateFor("ACCEPT", harness), "echo '{}'; exit 0") }, nil, "EVIDENCE_CORRUPT",
			"verification evidence (command exited 0): EVIDENCE_CORRUPT; the invaris report does not match the frozen harness or the check profile"},
	} {
		f := frozen(t, "approved\n", harness, true)
		if tc.prepare != nil {
			tc.prepare(f)
		}
		v, _ := Client{Executable: tc.exe(t)}.Verify(context.Background(), f)
		if v.Reason != tc.reason || v.Message != tc.message || !domain.ValidGraderMessage(v.Message) {
			t.Errorf("%s: reason %s message %q", name, v.Reason, v.Message)
		}
	}
}
