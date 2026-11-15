package acceptance

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/akkaraponph/evaris/internal/domain"
)

// harness builds the CLI once per test into a canonical temporary root.
type harness struct {
	t      *testing.T
	root   string
	binary string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	binary := filepath.Join(root, "evaris")
	if b, e := exec.Command("go", "build", "-o", binary, "../../cmd/evaris").CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, b)
	}
	return &harness{t: t, root: root, binary: binary}
}

// project copies an original example into a new private directory.
func (h *harness) project(src, name string) string {
	h.t.Helper()
	dst := filepath.Join(h.root, name)
	e := filepath.WalkDir(src, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "evidence" {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		info, _ := d.Info()
		return os.WriteFile(target, b, info.Mode().Perm()|0600)
	})
	if e != nil {
		h.t.Fatal(e)
	}
	return dst
}

// run executes the CLI and returns exit code and stdout, decoding into out when set.
func (h *harness) run(out any, args ...string) (int, []byte) {
	h.t.Helper()
	cmd := exec.Command(h.binary, args...)
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, e := cmd.Output()
	code := 0
	if e != nil {
		x, ok := e.(*exec.ExitError)
		if !ok {
			h.t.Fatal(e)
		}
		code = x.ExitCode()
	}
	if out != nil && len(b) > 0 {
		if e = json.Unmarshal(b, out); e != nil {
			h.t.Fatalf("JSON %v: %s (stderr %s)", args, b, stderr.String())
		}
	}
	h.t.Logf("%v -> %d %s", args, code, stderr.String())
	return code, b
}

func (h *harness) must(code int, out any, args ...string) []byte {
	h.t.Helper()
	got, b := h.run(out, args...)
	if got != code {
		h.t.Fatalf("%v exit=%d want=%d: %s", args, got, code, b)
	}
	return b
}

// control writes an authored control into a prepared workspace and imports it.
func (h *harness) control(store string, prep domain.Preparation, task domain.PreparedTask, content string, observed domain.Observed) domain.Run {
	h.t.Helper()
	tree := filepath.Join(prep.TasksRoot, task.Workspace)
	if content != "" {
		if e := os.WriteFile(filepath.Join(tree, "result.txt"), []byte(content), 0600); e != nil {
			h.t.Fatal(e)
		}
	}
	response := filepath.Join(h.root, prep.ID+"-"+task.CaseID+"-"+task.ArmID+".txt")
	os.WriteFile(response, []byte("Synthetic control; no model run."), 0600)
	q := domain.ImportRequest{SchemaVersion: "evaris.import/v1alpha1", PreparationID: prep.ID, CaseID: task.CaseID, ArmID: task.ArmID, ConfigDigest: prep.ConfigDigest, FixtureDigest: task.FixtureDigest, SourceRevision: "initial", FinalResponse: response, FinalTree: tree, Transcripts: []string{}, Observed: observed, Origin: "synthetic-control", Limitations: []string{"Synthetic control, not model performance."}}
	b, _ := json.Marshal(q)
	request := response + ".json"
	os.WriteFile(request, b, 0600)
	var run domain.Run
	h.must(0, &run, "import", "--store", store, "--record", request)
	return run
}
