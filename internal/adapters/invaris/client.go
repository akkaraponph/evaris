package invaris

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/akkaraponph/evaris/internal/adapters/fsrepo"
	"github.com/akkaraponph/evaris/internal/adapters/process"
	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
)

// Client invokes `<Executable> check <scratch>/invaris.json` on a fresh copy of
// the imported final tree. It implements application.Verifier.
type Client struct {
	Executable string
	// Env names the only inherited environment variables; nil means PATH only.
	Env []string
}

type processRecord struct {
	Program      string   `json:"program"`
	Args         []string `json:"args"`
	EnvAllowlist []string `json:"envAllowlist"`
	// InvarisVersion is what `--version` printed (nil for a legacy build).
	InvarisVersion *string         `json:"invarisVersion"`
	StartedAt      time.Time       `json:"startedAt"`
	FinishedAt     time.Time       `json:"finishedAt"`
	ExitCode       *int            `json:"exitCode"`
	Reason         domain.Reason   `json:"reason"`
	Executable     *domain.FileRef `json:"executable"`
	ScratchBefore  domain.Manifest `json:"scratchBefore"`
	ScratchAfter   domain.Manifest `json:"scratchAfter"`
	ScratchChanged bool            `json:"scratchChanged"`
	StdoutBytes    int             `json:"stdoutBytes"`
	StderrBytes    int             `json:"stderrBytes"`
}

func (c Client) Verify(ctx context.Context, f application.FrozenRun) (domain.Verification, []application.Blob) {
	cfg := f.Case.Case.Verification
	v := domain.Verification{Status: "ERROR", Evidence: []domain.FileRef{}, Findings: []domain.Finding{}}
	if cfg == nil {
		v.Status = "NOT_CONFIGURED"
		return v, nil
	}
	v.Required = cfg.Required
	if !f.AllowCommands {
		v.Status, v.Reason = "UNAVAILABLE", "COMMAND_NOT_AUTHORIZED"
		return v, nil
	}
	fail := func(step domain.CommandStep, r domain.Reason) (domain.Verification, []application.Blob) {
		v.Reason, v.Message = r, domain.CommandMessage(step, false, nil, r, "")
		return v, nil
	}
	h, e := ParseHarness(f.Case.Harness)
	if e != nil {
		return fail(domain.StepVerifyHarness, reason(e, "INVALID_CONFIG"))
	}
	if !filepath.IsAbs(c.Executable) {
		return fail(domain.StepVerifyExecutable, "TOOL_MISSING")
	}
	if info, e := os.Stat(c.Executable); e != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return fail(domain.StepVerifyExecutable, "TOOL_MISSING")
	}
	scratch, e := os.MkdirTemp("", "evaris-invaris-")
	if e != nil {
		return fail(domain.StepVerifyScratch, "IO_ERROR")
	}
	defer os.RemoveAll(scratch)
	if scratch, e = filepath.EvalSymlinks(scratch); e != nil {
		return fail(domain.StepVerifyScratch, "IO_ERROR")
	}
	if e = copyTree(f.SnapshotRoot, scratch); e != nil {
		return fail(domain.StepVerifyScratch, reason(e, "IO_ERROR"))
	}
	harnessPath := filepath.Join(scratch, domain.HarnessName)
	// Exclusive creation: an imported invaris.json never stands in for the frozen harness.
	w, e := os.OpenFile(harnessPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return fail(domain.StepVerifyScratch, "PATH_UNSAFE")
	}
	_, e = w.Write(f.Case.Harness)
	if ce := w.Close(); e == nil {
		e = ce
	}
	if e != nil {
		return fail(domain.StepVerifyScratch, "IO_ERROR")
	}
	env := c.Env
	if env == nil {
		env = []string{"PATH"}
	}
	// Probe the version only once nothing else refuses the run.
	version, supported := ProbeVersion(ctx, c.Executable)
	if !supported {
		v.Status = "UNAVAILABLE"
		return fail(domain.StepVerifyExecutable, "UNSUPPORTED_TOOL_VERSION")
	}
	cmd := domain.Command{Program: c.Executable, Args: []string{"check", harnessPath}, EnvAllowlist: env, TimeoutSeconds: cfg.TimeoutSeconds}
	before, be := fsrepo.Snapshot(ctx, scratch)
	res := process.RunLimited(ctx, scratch, cmd, maxEvidence, 64<<10)
	after, ae := fsrepo.Snapshot(context.WithoutCancel(ctx), scratch)
	v.ExitCode, v.Executable = res.ExitCode, res.Executable
	rec := processRecord{Program: cmd.Program, Args: cmd.Args, EnvAllowlist: env, InvarisVersion: version, StartedAt: res.StartedAt, FinishedAt: res.FinishedAt, ExitCode: res.ExitCode, Reason: res.Reason, Executable: res.Executable, ScratchBefore: before, ScratchAfter: after, StdoutBytes: len(res.Stdout), StderrBytes: len(res.Stderr)}
	rec.ScratchChanged = be != nil || ae != nil || !sameManifest(before, after)
	meta, _ := json.MarshalIndent(rec, "", "  ")
	blobs := []application.Blob{{Path: "evidence/verification/stdout.json", Data: res.Stdout}, {Path: "evidence/verification/stderr.txt", Data: res.Stderr}, {Path: "evidence/verification/process.json", Data: append(meta, '\n')}}
	for _, b := range blobs {
		v.Evidence = append(v.Evidence, domain.FileRef{Path: b.Path, SHA256: domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(b.Data))), Bytes: int64(len(b.Data))})
	}
	switch {
	case ctx.Err() != nil && res.Reason == "":
		v.Reason = "CANCELED"
	case res.Reason != "":
		// Timeout, cancellation and output limits override any partial JSON.
		v.Reason = res.Reason
		v.Message = domain.CommandMessage(domain.StepVerifyRun, true, res.ExitCode, v.Reason, "")
	case len(res.Stdout) == 0:
		v.Reason = "OUTPUT_MISSING"
		v.Message = domain.CommandMessage(domain.StepVerifyRun, true, res.ExitCode, v.Reason, "")
	default:
		o, e := ValidateEvidence(res.Stdout, res.ExitCode, h, domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(f.Case.Harness))), scratch)
		if e != nil {
			v.Reason = reason(e, "EVIDENCE_CORRUPT")
			v.Message = domain.CommandMessage(domain.StepVerifyEvidence, true, res.ExitCode, v.Reason, "")
			break
		}
		v.Status = o.Decision
		v.SchemaVersion, v.RunID, v.ConfigDigest = &o.SchemaVersion, &o.RunID, &o.ConfigDigest
		v.Findings = o.Findings
		v.Reason = map[string]domain.Reason{"ACCEPT": "", "REJECT": "ASSERTION_FAILED", "REFUSE": "VERIFIER_REFUSED"}[o.Decision]
	}
	return v, blobs
}

func reason(e error, fallback domain.Reason) domain.Reason {
	if f, ok := e.(*domain.Failure); ok {
		return f.Code
	}
	return fallback
}
func sameManifest(a, b domain.Manifest) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// copyTree copies regular files only; links or special files abort the copy.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(src, p)
		if e != nil {
			return e
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return &domain.Failure{Code: "PATH_UNSAFE", Path: filepath.ToSlash(rel)}
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		return os.WriteFile(target, b, info.Mode().Perm()|0600)
	})
}

var versionLine = regexp.MustCompile(`^invaris (dev|v(\d+)\.(\d+)\.\d+(\S*))\n?$`)

// ProbeVersion runs `<exe> --version`. It returns the printed version and
// whether Evaris accepts it: a legacy build that has no --version (nil,
// true), "dev" or a pseudo-version (true), or a release whose major.minor
// is in SupportedReleases.
func ProbeVersion(ctx context.Context, exe string) (*string, bool) {
	res := process.RunLimited(ctx, filepath.Dir(exe), domain.Command{Program: exe, Args: []string{"--version"}, EnvAllowlist: []string{"PATH"}, TimeoutSeconds: 10}, 1<<10, 1<<10)
	return classifyVersion(res.Stdout, res.ExitCode)
}

func classifyVersion(stdout []byte, exit *int) (*string, bool) {
	m := versionLine.FindSubmatch(stdout)
	if exit == nil || *exit != 0 || m == nil {
		return nil, true
	}
	v := string(m[1])
	if v == "dev" || len(m[4]) > 0 {
		return &v, true
	}
	return &v, slices.Contains(SupportedReleases, string(m[2])+"."+string(m[3]))
}
