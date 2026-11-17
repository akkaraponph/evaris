package fsrepo

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
)

// gitRepo commits files (path -> content; "->target" makes a symlink) in a new
// repository outside every Evaris directory and returns it with the commit.
func gitRepo(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := canonicalTemp(t)
	run := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Env = gitEnv()
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o700)
		if target, ok := strings.CutPrefix(content, "->"); ok {
			if err := os.Symlink(target, p); err != nil {
				t.Fatal(err)
			}
			continue
		}
		os.WriteFile(p, []byte(content), 0o600)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "delivered")
	return dir, run("rev-parse", "HEAD")
}

// gitRequest rewrites the standard import request to use a git final source.
func gitRequest(t *testing.T, p domain.Preparation, config, repo, revision string, edit func(map[string]any)) string {
	t.Helper()
	path := request(t, p, config)
	b, _ := os.ReadFile(path)
	var q map[string]any
	json.Unmarshal(b, &q)
	delete(q, "finalTree")
	q["finalSource"] = map[string]any{"kind": "git", "repository": repo, "revision": revision}
	if edit != nil {
		edit(q)
	}
	b, _ = json.Marshal(q)
	os.WriteFile(path, b, 0o600)
	return path
}

func TestGitImportOverlaysGeneratedFiles(t *testing.T) {
	config, store, p := prepared(t)
	repo, sha := gitRepo(t, map[string]string{"input.txt": "Original public input.\n", "result.txt": "approved\n"})
	run, e := (Repository{}).Import(context.Background(), store, gitRequest(t, p, config, repo, "HEAD", nil))
	if e != nil {
		t.Fatal(e)
	}
	if run.SchemaVersion != domain.RunSchemaV2 || run.DeliveredRevision == nil || *run.DeliveredRevision != sha || run.GitExecutable == nil || run.FinalSource.Revision != "HEAD" {
		t.Fatalf("run %+v", run)
	}
	hasTask := false
	for _, f := range run.FinalFiles {
		hasTask = hasTask || f.Path == "tree/TASK.txt"
	}
	if !hasTask {
		t.Fatalf("TASK.txt not overlaid: %+v", run.FinalFiles)
	}
	s := application.Service{Repo: Repository{}, Grader: passGrader{}}
	v, e := s.Evaluate(context.Background(), store, run.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	for _, g := range v.Graders {
		if g.CriterionID == "source-authorization" && g.Status != "PASS" {
			t.Fatalf("source authorization %+v", g)
		}
	}
	if _, e := (Repository{}).LoadRun(context.Background(), store, run.ID); e != nil {
		t.Fatalf("reload v1alpha2 run: %v", e)
	}
}

func TestGitImportRefusesSymlink(t *testing.T) {
	config, store, p := prepared(t)
	repo, _ := gitRepo(t, map[string]string{"input.txt": "Original public input.\n", "link": "->input.txt"})
	_, e := (Repository{}).Import(context.Background(), store, gitRequest(t, p, config, repo, "HEAD", nil))
	if e == nil || !strings.Contains(e.Error(), "PATH_UNSAFE") || !strings.Contains(e.Error(), "symbolic link: link") {
		t.Fatalf("symlink: %v", e)
	}
}

func TestGitImportRefusesGeneratedFile(t *testing.T) {
	config, store, p := prepared(t)
	repo, _ := gitRepo(t, map[string]string{"input.txt": "Original public input.\n", "TASK.txt": "agent's own task"})
	_, e := (Repository{}).Import(context.Background(), store, gitRequest(t, p, config, repo, "HEAD", nil))
	if e == nil || !strings.Contains(e.Error(), "generated file TASK.txt") {
		t.Fatalf("committed TASK.txt: %v", e)
	}
}

func TestGitImportRevisionMismatch(t *testing.T) {
	config, store, p := prepared(t)
	repo, _ := gitRepo(t, map[string]string{"input.txt": "Original public input.\n"})
	path := gitRequest(t, p, config, repo, "HEAD", func(q map[string]any) { q["deliveredRevision"] = strings.Repeat("0", 40) })
	if _, e := (Repository{}).Import(context.Background(), store, path); e == nil || !strings.Contains(e.Error(), "REVISION_MISMATCH") {
		t.Fatalf("mismatch: %v", e)
	}
	path = gitRequest(t, p, config, repo, "no-such-branch", nil)
	if _, e := (Repository{}).Import(context.Background(), store, path); e == nil || !strings.Contains(e.Error(), "is not a commit") {
		t.Fatalf("unknown revision: %v", e)
	}
	path = gitRequest(t, p, config, repo, "--output=/tmp/x", nil)
	if _, e := (Repository{}).Import(context.Background(), store, path); e == nil || !strings.Contains(e.Error(), "INVALID_CONFIG") {
		t.Fatalf("option-like revision: %v", e)
	}
}

func TestImportNeedsExactlyOneSource(t *testing.T) {
	config, store, p := prepared(t)
	repo, _ := gitRepo(t, map[string]string{"input.txt": "x"})
	both := gitRequest(t, p, config, repo, "HEAD", func(q map[string]any) { q["finalTree"] = filepath.Join(p.TasksRoot, p.Tasks[0].Workspace) })
	if _, e := (Repository{}).Import(context.Background(), store, both); e == nil || !strings.Contains(e.Error(), "exactly one") {
		t.Fatalf("both: %v", e)
	}
	neither := gitRequest(t, p, config, repo, "HEAD", func(q map[string]any) { delete(q, "finalSource") })
	if _, e := (Repository{}).Import(context.Background(), store, neither); e == nil || !strings.Contains(e.Error(), "exactly one") {
		t.Fatalf("neither: %v", e)
	}
}

func TestTreeImportRunVersions(t *testing.T) {
	config, store, p := prepared(t)
	run, e := (Repository{}).Import(context.Background(), store, request(t, p, config))
	if e != nil || run.SchemaVersion != domain.RunSchemaV1 || run.DeliveredRevision != nil {
		t.Fatalf("plain tree import %+v %v", run, e)
	}
	config, store, p = prepared(t)
	path := request(t, p, config)
	b, _ := os.ReadFile(path)
	var q map[string]any
	json.Unmarshal(b, &q)
	q["deliveredRevision"] = "abc123"
	b, _ = json.Marshal(q)
	os.WriteFile(path, b, 0o600)
	run, e = (Repository{}).Import(context.Background(), store, path)
	if e != nil || run.SchemaVersion != domain.RunSchemaV2 || *run.DeliveredRevision != "abc123" || run.FinalSource != nil {
		t.Fatalf("tree import with deliveredRevision %+v %v", run, e)
	}
	if _, e := (Repository{}).LoadRun(context.Background(), store, run.ID); e != nil {
		t.Fatal(e)
	}
}

// git archive honours export-ignore, so such files are absent from the
// imported tree; the documentation states this.
func TestGitImportHonoursExportIgnore(t *testing.T) {
	config, store, p := prepared(t)
	repo, _ := gitRepo(t, map[string]string{"input.txt": "Original public input.\n", ".gitattributes": "notes.txt export-ignore\n", "notes.txt": "x"})
	run, e := (Repository{}).Import(context.Background(), store, gitRequest(t, p, config, repo, "HEAD", nil))
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range run.FinalFiles {
		if f.Path == "tree/notes.txt" {
			t.Fatal("export-ignore file imported")
		}
	}
}
