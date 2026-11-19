package fsrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/akkaraponph/evaris/internal/domain"
)

func claudeStream(workspace, tool, input, output, resultSession string) []byte {
	return fmt.Appendf(nil, `{"type":"system","subtype":"init","session_id":"session","cwd":%q,"claude_code_version":"2.1.327","model":"model","permissionMode":"acceptEdits","tools":["Read","Bash"]}
{"type":"assistant","session_id":"session","message":{"content":[{"type":"tool_use","id":"read","name":%q,"input":{"file_path":%q}}]}}
{"type":"user","session_id":"session","message":{"content":[{"type":"tool_result","tool_use_id":"read","content":%q}]}}
{"type":"result","subtype":"success","session_id":%q}
`, workspace, tool, input, output, resultSession)
}

func TestClaudeInstructionEvidence(t *testing.T) {
	const root = "/workspace"
	p := domain.InstructionWorkspacePath(0)
	instructions := []transcriptInstruction{{p, "First line.\nSecond line."}}
	for _, tc := range []struct {
		name, tool, path, output, session string
		loaded, fail                      bool
	}{
		{"numbered read", "Read", p, "1\tFirst line.\n2\tSecond line.\n3\t", "session", true, false},
		{"shell result", "Bash", "", "First line.\nSecond line.\n", "session", true, false},
		{"partial read", "Read", p, "1\tFirst line.", "session", false, false},
		{"different file", "Read", "other.txt", "First line.\nSecond line.", "session", false, false},
		{"different session", "Read", p, "First line.\nSecond line.", "another", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var observed domain.Observed
			err := observeClaude(claudeStream(root, tc.tool, tc.path, tc.output, tc.session), root, instructions, &observed)
			if tc.fail {
				if err == nil {
					t.Fatal("inconsistent session accepted")
				}
				return
			}
			if err != nil || observed.ContextID == nil || *observed.ContextID != "session" ||
				(observed.LoadedInstructions != nil) != tc.loaded || observed.FreshContext != nil {
				t.Fatalf("unexpected metadata: %+v, %v", observed, err)
			}
			if tc.loaded && ((*observed.LoadedInstructions)[0] != p || len(*observed.LoadedInstructions) != 1) {
				t.Fatalf("instructions: %v", *observed.LoadedInstructions)
			}
		})
	}
}

func TestImportClaudeTranscriptRetainsEvidence(t *testing.T) {
	config, store, prep := prepared(t)
	requestPath := request(t, prep, config)
	data, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var q domain.ImportRequest
	if err := json.Unmarshal(data, &q); err != nil {
		t.Fatal(err)
	}
	q.ClaudeTranscript = filepath.Join(prep.TasksRoot, "host.jsonl")
	q.Transcripts = []string{q.ClaudeTranscript}
	write := func(path string, b []byte) {
		t.Helper()
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	data, err = json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	write(requestPath, data)
	write(q.ClaudeTranscript, claudeStream("/other", "Read", "TASK.txt", "Task", "session"))
	repo := Repository{}
	ctx := context.Background()
	if _, err := repo.Import(ctx, store, requestPath); err == nil {
		t.Fatal("associated an unrelated workspace transcript with the run")
	}
	stream := claudeStream(q.FinalTree, "Read", "TASK.txt", "Task", "session")
	write(q.ClaudeTranscript, stream)
	run, err := repo.Import(ctx, store, requestPath)
	if err != nil || run.Observed.Host == nil || *run.Observed.Host != "Claude Code 2.1.327" ||
		run.Observed.LoadedInstructions != nil || len(run.Transcripts) != 1 || run.Transcripts[0].SHA256 != digest(stream) {
		t.Fatalf("transcript import: %+v %v", run, err)
	}
	if _, err := repo.LoadRun(ctx, store, run.ID); err != nil {
		t.Fatal(err)
	}
}
