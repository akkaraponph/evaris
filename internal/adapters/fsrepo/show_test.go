package fsrepo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
)

// outputGrader records a command-style evidence blob with stdout "hello",
// a long stderr, and an ERROR with a catalogue message.
type outputGrader struct{}

func (outputGrader) Grade(_ context.Context, _ application.FrozenRun, c domain.Criterion) (domain.GraderResult, []application.Blob) {
	exit := 0
	data, _ := json.Marshal(struct {
		Process struct{ Stdout, Stderr []byte }
	}{struct{ Stdout, Stderr []byte }{[]byte("hello\n"), append(bytes.Repeat([]byte("e"), 5000), 0x01)}})
	path := "evidence/" + c.ID + ".json"
	ref := domain.FileRef{Path: path, SHA256: domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(data))), Bytes: int64(len(data))}
	msg := domain.CommandMessage(domain.StepSnapshotAfter, true, &exit, "LIMIT_EXCEEDED", domain.LimitEntries)
	return domain.GraderResult{CriterionID: c.ID, Status: "ERROR", Reason: "LIMIT_EXCEEDED", Message: msg, Evidence: []domain.FileRef{ref}, GraderVersion: "builtin/1"},
		[]application.Blob{{Path: path, Data: data}}
}

func showStore(t *testing.T) (string, domain.Evaluation, application.Service) {
	t.Helper()
	store, run, s := evaluated(t)
	s.Grader = outputGrader{}
	v, e := s.Evaluate(context.Background(), store, run.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	return store, v, s
}

func TestShowDecodesCommandOutput(t *testing.T) {
	store, v, s := showStore(t)
	text, e := s.Show(context.Background(), store, v.ID, "")
	if e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"criterion result (required): ERROR", "message: scratch snapshot after the command (command exited 0): LIMIT_EXCEEDED (entries)",
		"stdout (6 bytes):\n    hello\n", "stderr (5001 bytes):", "(truncated, 5001 bytes total)", "criterion source-authorization"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	one, e := s.Show(context.Background(), store, v.ID, "result")
	if e != nil || strings.Contains(one, "source-authorization") {
		t.Fatalf("criterion filter: %v\n%s", e, one)
	}
}

func TestShowEscapesControlBytes(t *testing.T) {
	store, v, s := showStore(t)
	text, _ := s.Show(context.Background(), store, v.ID, "result")
	if strings.ContainsRune(text, 0x01) {
		t.Fatal("control byte printed raw")
	}
}

func TestShowRefusesTamperedBlob(t *testing.T) {
	store, v, s := showStore(t)
	path := filepath.Join(store, "evaluations", v.ID, "evidence", "result.json")
	b, _ := os.ReadFile(path)
	os.WriteFile(path, bytes.Replace(b, []byte("aGVsbG8K"), []byte("aGVsbG9K"), 1), 0600)
	if _, e := s.Show(context.Background(), store, v.ID, ""); e == nil || !strings.Contains(e.Error(), "EVIDENCE_CORRUPT") {
		t.Fatalf("tampered blob: %v", e)
	}
}

func TestShowUnknownCriterion(t *testing.T) {
	store, v, s := showStore(t)
	if _, e := s.Show(context.Background(), store, v.ID, "nope"); e == nil || !strings.Contains(e.Error(), "INVALID_CONFIG") {
		t.Fatalf("unknown criterion: %v", e)
	}
	if _, e := s.Show(context.Background(), store, strings.Repeat("0", 32), ""); e == nil {
		t.Fatal("unknown evaluation accepted")
	}
}
