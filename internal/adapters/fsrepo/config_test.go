package fsrepo

import (
	"context"
	"encoding/json"
	"github.com/akkaraponph/evaris/internal/domain"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateValidExperiment(t *testing.T) {
	p, e := (Repository{}).Load(context.Background(), "../../../testdata/valid/experiment.json")
	if e != nil {
		t.Fatal(e)
	}
	if p.Experiment.ID != "example" || !strings.HasPrefix(string(p.ConfigDigest), "sha256:") {
		t.Fatal(p)
	}
	if _, e = os.Stat(p.OutputRoot); !os.IsNotExist(e) {
		t.Fatal("validation wrote output")
	}
}
func TestConfigRejects(t *testing.T) {
	for _, s := range []string{`{}`, `null`, `{"schemaVersion":null}`, `{"schemaVersion":"x","schemaVersion":"y"}`, `{} {}`, `{"unknown":1}`, `{"schemaVersion":"evaris.experiment/v1alpha1","id":"x"}`} {
		t.Run(s, func(t *testing.T) {
			var v domain.Experiment
			if decode([]byte(s), &v) == nil {
				t.Fatal("accepted invalid JSON contract")
			}
		})
	}
}
func TestManifestStable(t *testing.T) {
	d := canonicalTemp(t)
	p := filepath.Join(d, "a")
	os.WriteFile(p, []byte("x"), 0600)
	a, e := Snapshot(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	b, e := Snapshot(context.Background(), d)
	if e != nil || manifestDigest(a) != manifestDigest(b) {
		t.Fatal("unstable")
	}
	os.Chmod(p, 0700)
	b, e = Snapshot(context.Background(), d)
	if e != nil || manifestDigest(a) == manifestDigest(b) {
		t.Fatal("mode not included")
	}
}
func TestRejectLinksAndSpecialFiles(t *testing.T) {
	d := canonicalTemp(t)
	os.Symlink("missing", filepath.Join(d, "link"))
	if _, e := Snapshot(context.Background(), d); e == nil {
		t.Fatal("accepted symlink")
	}
}
func TestCommitCollision(t *testing.T) {
	root := filepath.Join(canonicalTemp(t), "record")
	tx, e := reserve(root)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.rollback()
	if _, e = reserve(root); e == nil {
		t.Fatal("overwrote reservation")
	}
	if e = tx.write("record.json", []byte("{}")); e != nil {
		t.Fatal(e)
	}
	if e = tx.write("record.json", []byte("other")); e == nil {
		t.Fatal("overwrote file")
	}
	if e = tx.commit(); e != nil {
		t.Fatal(e)
	}
}

func canonicalTemp(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return p
}

func TestCommitWriteFailures(t *testing.T) {
	for _, stage := range []string{"write", "sync", "close"} {
		t.Run(stage, func(t *testing.T) {
			root := filepath.Join(canonicalTemp(t), "record")
			tx, e := reserve(root)
			if e != nil {
				t.Fatal(e)
			}
			tx.fault = func(s string) error {
				if s == stage {
					return os.ErrPermission
				}
				return nil
			}
			if e = tx.commit(); e == nil {
				t.Fatal("ignored fault")
			}
			if e = tx.rollback(); e != nil {
				t.Fatal(e)
			}
			if _, e = os.Stat(root); !os.IsNotExist(e) {
				t.Fatal("partial record left")
			}
		})
	}
}
func TestPartialRecordIgnored(t *testing.T) {
	root := filepath.Join(canonicalTemp(t), "record")
	tx, e := reserve(root)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.rollback()
	tx.write("record.json", []byte("{}"))
	if e = verifyRecord(context.Background(), root); e == nil {
		t.Fatal("accepted uncommitted")
	}
}
func TestRollbackPreservesReplacedRoot(t *testing.T) {
	parent := canonicalTemp(t)
	root := filepath.Join(parent, "record")
	tx, e := reserve(root)
	if e != nil {
		t.Fatal(e)
	}
	os.Rename(root, root+"-original")
	os.Mkdir(root, 0700)
	os.WriteFile(filepath.Join(root, "foreign"), []byte("preserve"), 0600)
	if e = tx.rollback(); e == nil {
		t.Fatal("did not detect replacement")
	}
	if _, e = os.Stat(filepath.Join(root, "foreign")); e != nil {
		t.Fatal("deleted foreign directory")
	}
}
func TestLimitsAtBoundary(t *testing.T) {
	root := canonicalTemp(t)
	p := filepath.Join(root, "large")
	f, e := os.Create(p)
	if e != nil {
		t.Fatal(e)
	}
	f.Truncate(10 << 20)
	f.Close()
	if _, e = Snapshot(context.Background(), root); e != nil {
		t.Fatal(e)
	}
	f, _ = os.OpenFile(p, os.O_WRONLY, 0600)
	f.Truncate((10 << 20) + 1)
	f.Close()
	if _, e = Snapshot(context.Background(), root); e == nil {
		t.Fatal("oversized file accepted")
	}
}

func TestCopyDoesNotShareInodes(t *testing.T) {
	src := canonicalTemp(t)
	os.WriteFile(filepath.Join(src, "file"), []byte("original"), 0600)
	m, e := Snapshot(context.Background(), src)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := reserve(filepath.Join(canonicalTemp(t), "copy"))
	if e != nil {
		t.Fatal(e)
	}
	defer tx.rollback()
	if e = copyManifest(context.Background(), src, tx, "tree", m); e != nil {
		t.Fatal(e)
	}
	a, _ := os.Stat(filepath.Join(src, "file"))
	b, _ := os.Stat(filepath.Join(tx.root, "tree/file"))
	if os.SameFile(a, b) {
		t.Fatal("shared inode")
	}
}
func TestSnapshotExcludeValidation(t *testing.T) {
	for name, tc := range map[string]struct {
		criterion string
		ok        bool
	}{
		"command with exclude": {`{"id":"scan","kind":"command-exit","required":true,"expected":0,"command":{"program":"/bin/true","args":[],"envAllowlist":[]},"snapshotExclude":[".next","node_modules/.vite"]}`, true},
		"absolute exclude":     {`{"id":"scan","kind":"command-exit","required":true,"expected":0,"command":{"program":"/bin/true","args":[],"envAllowlist":[]},"snapshotExclude":["/tmp"]}`, false},
		"duplicate exclude":    {`{"id":"scan","kind":"command-exit","required":true,"expected":0,"command":{"program":"/bin/true","args":[],"envAllowlist":[]},"snapshotExclude":[".next",".next"]}`, false},
		"exclude on exact":     {`{"id":"result","kind":"exact","required":true,"path":"result.txt","expected":"approved\n","snapshotExclude":[".next"]}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.CopyFS(dir, os.DirFS("../../../testdata/valid")); err != nil {
				t.Fatal(err)
			}
			oracle := `{"schemaVersion":"evaris.oracle/v1alpha1","criteria":[` + tc.criterion + `]}`
			if err := os.WriteFile(filepath.Join(dir, "reviewer", "oracle.json"), []byte(oracle), 0o600); err != nil {
				t.Fatal(err)
			}
			_, e := (Repository{}).Load(context.Background(), filepath.Join(dir, "experiment.json"))
			if (e == nil) != tc.ok || (e != nil && !strings.HasPrefix(e.Error(), "INVALID_CONFIG: scan") && !strings.HasPrefix(e.Error(), "INVALID_CONFIG: result")) {
				t.Fatalf("Load error = %v, want ok=%v", e, tc.ok)
			}
		})
	}
}

func TestDependencyValidation(t *testing.T) {
	deps := canonicalTemp(t)
	crit := func(deps string) string {
		return `{"id":"scan","kind":"command-exit","required":true,"expected":0,"command":{"program":"/bin/true","args":[],"envAllowlist":[]},"dependencies":` + deps + `}`
	}
	for name, tc := range map[string]struct {
		criterion string
		ok        bool
	}{
		"valid":              {crit(`[{"name":"node_modules","source":"` + deps + `"}]`), true},
		"nested name":        {crit(`[{"name":"a/node_modules","source":"` + deps + `"}]`), false},
		"relative source":    {crit(`[{"name":"node_modules","source":"deps"}]`), false},
		"duplicate name":     {crit(`[{"name":"n","source":"` + deps + `"},{"name":"n","source":"` + deps + `"}]`), false},
		"reserved name":      {crit(`[{"name":"workspace","source":"` + deps + `"}]`), false},
		"inside fixture":     {crit(`[{"name":"node_modules","source":"FIXTURE"}]`), false},
		"on exact criterion": {`{"id":"result","kind":"exact","required":true,"path":"result.txt","expected":"approved\n","dependencies":[{"name":"n","source":"` + deps + `"}]}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.CopyFS(dir, os.DirFS("../../../testdata/valid")); err != nil {
				t.Fatal(err)
			}
			c := strings.ReplaceAll(tc.criterion, "FIXTURE", filepath.Join(dir, "public"))
			oracle := `{"schemaVersion":"evaris.oracle/v1alpha1","criteria":[` + c + `]}`
			if err := os.WriteFile(filepath.Join(dir, "reviewer", "oracle.json"), []byte(oracle), 0o600); err != nil {
				t.Fatal(err)
			}
			_, e := (Repository{}).Load(context.Background(), filepath.Join(dir, "experiment.json"))
			if (e == nil) != tc.ok || e != nil && !strings.Contains(e.Error(), "scan") && !strings.Contains(e.Error(), "result") {
				t.Fatalf("Load error = %v, want ok=%v", e, tc.ok)
			}
		})
	}
}

func TestCampaignVerificationFrozen(t *testing.T) {
	config, tasks := fixture(t)
	dir := filepath.Dir(config)
	os.WriteFile(filepath.Join(dir, "reviewer", "campaign.json"), []byte(`{"version":"invaris.campaign/v1alpha1","name":"c"}`), 0o600)
	os.WriteFile(filepath.Join(dir, "reviewer", "mutate.json"), []byte(`{"version":"invaris.dev/v1alpha3"}`), 0o600)
	b, _ := os.ReadFile(config)
	var exp map[string]any
	json.Unmarshal(b, &exp)
	exp["cases"].([]any)[0].(map[string]any)["verification"] = map[string]any{"harness": "reviewer/campaign.json", "required": true, "timeoutSeconds": 60, "action": "campaign", "files": []any{"reviewer/mutate.json"}}
	b, _ = json.Marshal(exp)
	os.WriteFile(config, b, 0o600)
	plan, e := (Repository{}).Load(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	if fc := plan.Cases[0]; len(fc.HarnessFiles) != 1 || fc.HarnessFiles[0].Path != "mutate.json" {
		t.Fatalf("harness files %+v", fc.HarnessFiles)
	}
	if _, e := (Repository{}).Prepare(context.Background(), plan, tasks); e != nil {
		t.Fatal(e)
	}
	// Editing the source after preparation does not change the frozen copy.
	os.WriteFile(filepath.Join(dir, "reviewer", "mutate.json"), []byte(`{"edited":true}`), 0o600)
	frozen, e := loadStore(context.Background(), plan.OutputRoot)
	if e != nil || string(frozen.Cases[0].HarnessFiles[0].Data) != `{"version":"invaris.dev/v1alpha3"}` {
		t.Fatalf("frozen campaign file: %v %+v", e, frozen.Cases[0].HarnessFiles)
	}
	// A campaign file of another kind is refused.
	os.WriteFile(filepath.Join(dir, "reviewer", "campaign.json"), []byte(`{"version":"invaris.dev/v1alpha2"}`), 0o600)
	if _, e := (Repository{}).Load(context.Background(), config); e == nil || !strings.Contains(e.Error(), "invaris.campaign/v1alpha1") {
		t.Fatalf("non-campaign harness: %v", e)
	}
}
