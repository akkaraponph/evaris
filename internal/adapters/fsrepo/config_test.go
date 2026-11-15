package fsrepo

import (
	"context"
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
