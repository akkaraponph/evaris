package fsrepo

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/akkaraponph/evaris/internal/domain"
)

// gitFileBytes bounds one exported file, as Snapshot bounds imported files.
const gitFileBytes = 10 << 20

// gitEnv is the environment for git: the caller's, without GIT_* variables
// that could point git at another repository or index.
func gitEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	return env
}

// gitExecutable resolves git on PATH and identifies its bytes.
func gitExecutable() (string, domain.FileRef, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return "", domain.FileRef{}, fail("TOOL_MISSING", "git")
	}
	if path, err = filepath.EvalSymlinks(path); err != nil {
		return "", domain.FileRef{}, fail("TOOL_MISSING", "git")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", domain.FileRef{}, fail("TOOL_MISSING", "git")
	}
	return path, domain.FileRef{Path: path, SHA256: digest(b), Bytes: int64(len(b)), Executable: true}, nil
}

func runGit(ctx context.Context, git, repo string, stdout io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, git, append([]string{"-C", repo}, args...)...)
	cmd.Env = gitEnv()
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = stdout, &stderr
	if err := cmd.Run(); err != nil {
		first, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
		return fmt.Errorf("git %s: %s", args[0], first)
	}
	return nil
}

// exportGitTree resolves revision to a commit in repo and writes that
// commit's tree into the empty directory dst. Symbolic links, submodules and
// other non-regular entries are PATH_UNSAFE rather than skipped, so the
// imported tree is exactly the commit's regular files.
func exportGitTree(ctx context.Context, repo, revision, dst string, maxEntries int) (string, domain.FileRef, error) {
	git, ref, err := gitExecutable()
	if err != nil {
		return "", ref, err
	}
	if revision == "" || strings.HasPrefix(revision, "-") || strings.ContainsAny(revision, "\x00\n") {
		return "", ref, fail("INVALID_CONFIG", "finalSource revision")
	}
	var out bytes.Buffer
	if err := runGit(ctx, git, repo, &out, "rev-parse", "--verify", "--end-of-options", revision+"^{commit}"); err != nil {
		return "", ref, fail("REVISION_MISMATCH", "finalSource revision "+revision+" is not a commit in the repository")
	}
	sha := strings.TrimSpace(out.String())
	out.Reset()
	if err := runGit(ctx, git, repo, &out, "ls-tree", "-r", "-z", "--full-tree", sha); err != nil {
		return "", ref, fail("IO_ERROR", "git ls-tree")
	}
	for _, entry := range strings.Split(strings.TrimSuffix(out.String(), "\x00"), "\x00") {
		mode, _, _ := strings.Cut(entry, " ")
		_, name, _ := strings.Cut(entry, "\t")
		switch mode {
		case "120000":
			return "", ref, fail("PATH_UNSAFE", "delivered commit has a symbolic link: "+name)
		case "160000":
			return "", ref, fail("PATH_UNSAFE", "delivered commit has a submodule: "+name)
		}
	}
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := runGit(ctx, git, repo, pw, "archive", "--format=tar", sha)
		pw.CloseWithError(err)
		done <- err
	}()
	extractErr := extractTar(pr, dst, maxEntries)
	if extractErr == nil {
		// git pads the archive past the end marker; read it so git can exit cleanly.
		_, _ = io.Copy(io.Discard, pr)
	}
	pr.CloseWithError(errors.New("extraction stopped"))
	gitErr := <-done
	if extractErr != nil {
		return "", ref, extractErr
	}
	if gitErr != nil {
		return "", ref, fail("IO_ERROR", "git archive")
	}
	return sha, ref, nil
}

func extractTar(r io.Reader, dst string, maxEntries int) error {
	tr := tar.NewReader(r)
	entries := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fail("IO_ERROR", "git archive stream")
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := strings.TrimSuffix(h.Name, "/")
		if !domain.Relative(name) {
			return fail("PATH_UNSAFE", "git archive entry "+h.Name)
		}
		if entries++; entries > maxEntries {
			return fail("LIMIT_EXCEEDED", fmt.Sprintf("entries: more than %d in the delivered commit", maxEntries))
		}
		target := filepath.Join(dst, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if h.Size > gitFileBytes {
				return fail("LIMIT_EXCEEDED", name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			mode := os.FileMode(0o600)
			if h.Mode&0o111 != 0 {
				mode = 0o700
			}
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, io.LimitReader(tr, gitFileBytes))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		default:
			return fail("PATH_UNSAFE", "delivered commit has a non-regular entry: "+name)
		}
	}
}

// overlayGenerated writes the prepared task's generated files (TASK.txt and
// frozen instructions) into an exported tree, byte-identical to the prepared
// workspace and checked against the preparation's digests. A delivered
// commit that already contains one is refused rather than overwritten.
func overlayGenerated(tasksRoot string, task domain.PreparedTask, tree string) error {
	for _, ref := range task.GeneratedFiles {
		target := filepath.Join(tree, filepath.FromSlash(ref.Path))
		if _, err := os.Lstat(target); err == nil {
			return fail("PATH_UNSAFE", "delivered commit contains the generated file "+ref.Path)
		}
		b, err := readFile(filepath.Join(tasksRoot, task.Workspace, filepath.FromSlash(ref.Path)))
		if err != nil || digest(b) != ref.SHA256 || int64(len(b)) != ref.Bytes {
			return fail("EVIDENCE_CORRUPT", "prepared generated file "+ref.Path)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		mode := os.FileMode(0o600)
		if ref.Executable {
			mode = 0o700
		}
		if err := os.WriteFile(target, b, mode); err != nil {
			return err
		}
	}
	return nil
}
