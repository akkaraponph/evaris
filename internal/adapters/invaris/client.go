package invaris

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
	Program        string          `json:"program"`
	Args           []string        `json:"args"`
	EnvAllowlist   []string        `json:"envAllowlist"`
	PinnedRevision string          `json:"pinnedRevision"`
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
	h, e := ParseHarness(f.Case.Harness)
	if e != nil {
		v.Reason = reason(e, "INVALID_CONFIG")
		return v, nil
	}
	if !filepath.IsAbs(c.Executable) {
		v.Reason = "TOOL_MISSING"
		return v, nil
	}
	if info, e := os.Stat(c.Executable); e != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		v.Reason = "TOOL_MISSING"
		return v, nil
	}
	scratch, e := os.MkdirTemp("", "evaris-invaris-")
	if e != nil {
		v.Reason = "IO_ERROR"
		return v, nil
	}
	defer os.RemoveAll(scratch)
	if scratch, e = filepath.EvalSymlinks(scratch); e != nil {
		v.Reason = "IO_ERROR"
		return v, nil
	}
	if e = copyTree(f.SnapshotRoot, scratch); e != nil {
		v.Reason = reason(e, "IO_ERROR")
		return v, nil
	}
	harnessPath := filepath.Join(scratch, domain.HarnessName)
	// Exclusive creation: an imported invaris.json never stands in for the frozen harness.
	w, e := os.OpenFile(harnessPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		v.Reason = "PATH_UNSAFE"
		return v, nil
	}
	_, e = w.Write(f.Case.Harness)
	if ce := w.Close(); e == nil {
		e = ce
	}
	if e != nil {
		v.Reason = "IO_ERROR"
		return v, nil
	}
	env := c.Env
	if env == nil {
		env = []string{"PATH"}
	}
	cmd := domain.Command{Program: c.Executable, Args: []string{"check", harnessPath}, EnvAllowlist: env, TimeoutSeconds: cfg.TimeoutSeconds}
	before, be := fsrepo.Snapshot(ctx, scratch)
	res := process.RunLimited(ctx, scratch, cmd, maxEvidence, 64<<10)
	after, ae := fsrepo.Snapshot(context.WithoutCancel(ctx), scratch)
	v.ExitCode, v.Executable = res.ExitCode, res.Executable
	rec := processRecord{Program: cmd.Program, Args: cmd.Args, EnvAllowlist: env, PinnedRevision: PinnedRevision, StartedAt: res.StartedAt, FinishedAt: res.FinishedAt, ExitCode: res.ExitCode, Reason: res.Reason, Executable: res.Executable, ScratchBefore: before, ScratchAfter: after, StdoutBytes: len(res.Stdout), StderrBytes: len(res.Stderr)}
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
	case len(res.Stdout) == 0:
		v.Reason = "OUTPUT_MISSING"
	default:
		o, e := ValidateEvidence(res.Stdout, res.ExitCode, h, domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(f.Case.Harness))), scratch)
		if e != nil {
			v.Reason = reason(e, "EVIDENCE_CORRUPT")
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
