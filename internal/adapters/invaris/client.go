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
	campaign := cfg.Campaign()
	var h Harness
	var e error
	if campaign {
		names, _ := campaignInputs(f.Case.HarnessFiles)
		if _, e := domain.CampaignEntries(f.Case.Harness, names); e != nil {
			return fail(domain.StepVerifyHarness, reason(e, "INVALID_CONFIG"))
		}
	} else {
		if h, e = ParseHarness(f.Case.Harness); e != nil {
			return fail(domain.StepVerifyHarness, reason(e, "INVALID_CONFIG"))
		}
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
	// A check runs the frozen harness as invaris.json at the scratch root. A
	// campaign runs the frozen campaign file under its own name, with its
	// referenced harness files at their paths relative to it. Every file is
	// created exclusively, so imported files never stand in for frozen ones.
	harnessPath := filepath.Join(scratch, domain.HarnessName)
	files := []application.Blob{{Path: domain.HarnessName, Data: f.Case.Harness}}
	if campaign {
		harnessPath = filepath.Join(scratch, filepath.Base(cfg.Harness))
		files = append([]application.Blob{{Path: filepath.Base(cfg.Harness), Data: f.Case.Harness}}, f.Case.HarnessFiles...)
	}
	for _, b := range files {
		if r := writeExclusive(scratch, b); r != "" {
			return fail(domain.StepVerifyScratch, r)
		}
	}
	// Dependencies are linked at the scratch root, where an Invaris harness's
	// workspace block expects them. The imported tree must not already hold
	// the name, and the links stay out of the scratch manifests.
	var exclude []string
	for _, d := range cfg.Dependencies {
		if info, e := os.Stat(d.Source); e != nil || !info.IsDir() {
			return fail(domain.StepDependencies, "TOOL_MISSING")
		}
		if _, e := os.Lstat(filepath.Join(scratch, d.Name)); e == nil {
			return fail(domain.StepVerifyScratch, "PATH_UNSAFE")
		}
		if e := os.Symlink(d.Source, filepath.Join(scratch, d.Name)); e != nil {
			return fail(domain.StepDependencies, "IO_ERROR")
		}
		exclude = append(exclude, d.Name)
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
	action := domain.VerifyCheck
	if campaign {
		action = domain.VerifyCampaign
	}
	cmd := domain.Command{Program: c.Executable, Args: []string{action, harnessPath}, EnvAllowlist: env, TimeoutSeconds: cfg.TimeoutSeconds}
	limit := fsrepo.SnapshotOptions{MaxEntries: f.MaxEntries, Exclude: exclude}
	before, be := fsrepo.SnapshotWith(ctx, scratch, limit)
	res := process.RunLimited(ctx, scratch, cmd, maxEvidence, 64<<10)
	after, ae := fsrepo.SnapshotWith(context.WithoutCancel(ctx), scratch, limit)
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
		digest := domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(f.Case.Harness)))
		var o Outcome
		if campaign {
			o, e = ValidateCampaign(res.Stdout, res.ExitCode, f.Case.Harness, f.Case.HarnessFiles)
		} else {
			o, e = ValidateEvidence(res.Stdout, res.ExitCode, h, digest, scratch)
		}
		if e != nil {
			v.Reason = reason(e, "EVIDENCE_CORRUPT")
			v.Message = domain.CommandMessage(domain.StepVerifyEvidence, true, res.ExitCode, v.Reason, "")
			break
		}
		v.Status = o.Decision
		v.Campaign = o.Campaign
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

// writeExclusive creates b under root, refusing to replace anything. It
// returns "" on success or the reason code.
func writeExclusive(root string, b application.Blob) domain.Reason {
	p := filepath.Join(root, filepath.FromSlash(b.Path))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "PATH_UNSAFE"
	}
	w, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "PATH_UNSAFE"
	}
	_, err = w.Write(b.Data)
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "IO_ERROR"
	}
	return ""
}
