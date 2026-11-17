package fsrepo

import (
	"context"
	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func copyManifest(ctx context.Context, src string, t *transaction, prefix string, m domain.Manifest) error {
	if !domain.Relative(filepath.ToSlash(prefix)) {
		return fail("PATH_UNSAFE", prefix)
	}
	if e := os.MkdirAll(filepath.Join(t.root, prefix), 0700); e != nil {
		return e
	}
	if e := safe(filepath.Join(t.root, prefix)); e != nil {
		return e
	}
	root, e := os.OpenRoot(src)
	if e != nil {
		return e
	}
	defer root.Close()
	for _, ref := range m {
		if e = ctx.Err(); e != nil {
			return e
		}
		p := filepath.Join(src, filepath.FromSlash(ref.Path))
		if e = safe(p); e != nil {
			return e
		}
		f, e := root.Open(ref.Path)
		if e != nil {
			return e
		}
		b, e := io.ReadAll(io.LimitReader(f, (10<<20)+1))
		ce := f.Close()
		if e == nil {
			e = ce
		}
		if e != nil {
			return e
		}
		if digest(b) != ref.SHA256 || int64(len(b)) != ref.Bytes {
			return fail("EVIDENCE_CORRUPT", ref.Path)
		}
		name := filepath.ToSlash(filepath.Join(prefix, ref.Path))
		if e = t.write(name, b); e != nil {
			return e
		}
		if ref.Executable {
			if e = os.Chmod(filepath.Join(t.root, filepath.FromSlash(name)), 0700); e != nil {
				return e
			}
		}
	}
	// The copy is compared with the already bounded manifest m exactly.
	after, e := SnapshotWith(ctx, src, SnapshotOptions{MaxEntries: domain.MaxEntriesCeiling})
	if e != nil {
		return e
	}
	if manifestDigest(after) != manifestDigest(m) {
		return fail("EVIDENCE_CORRUPT", src)
	}
	return nil
}
func (r Repository) Prepare(ctx context.Context, p application.Plan, tasksRoot string) (out domain.Preparation, err error) {
	if !filepath.IsAbs(tasksRoot) {
		return out, fail("PATH_UNSAFE", tasksRoot)
	}
	tasksRoot = filepath.Clean(tasksRoot)
	for _, c := range p.Experiment.Cases {
		sources := []string{filepath.Join(p.ConfigDir, c.Fixture), filepath.Join(p.ConfigDir, c.Task), filepath.Join(p.ConfigDir, c.Oracle), p.OutputRoot}
		if c.Verification != nil {
			sources = append(sources, filepath.Join(p.ConfigDir, c.Verification.Harness))
			for _, f := range c.Verification.Files {
				sources = append(sources, filepath.Join(p.ConfigDir, f))
			}
		}
		for _, src := range sources {
			if within(tasksRoot, src) || within(src, tasksRoot) {
				return out, fail("PATH_UNSAFE", tasksRoot)
			}
		}
	}
	for _, a := range p.Experiment.Arms {
		for _, in := range a.Instructions {
			if within(tasksRoot, filepath.Join(p.ConfigDir, in.Path)) {
				return out, fail("PATH_UNSAFE", "instruction root")
			}
		}
	}
	reviewerFiles, err := hashReviewerFiles(p, tasksRoot)
	if err != nil {
		return out, err
	}
	work, err := reserve(tasksRoot)
	if err != nil {
		return out, err
	}
	defer cleanup(work, &err)
	// Initialize the private store only after every source has been validated.
	var store *transaction
	if _, e := os.Lstat(p.OutputRoot); os.IsNotExist(e) {
		store, err = reserve(p.OutputRoot)
		if err != nil {
			return out, err
		}
		defer cleanup(store, &err)
		if err = store.write("experiment.json", p.ConfigBytes); err != nil {
			return out, err
		}
		if err = ensureDirs(p.OutputRoot, "inputs", "preparations", "runs", "slots"); err != nil {
			return out, err
		}
	} else {
		if err = safe(p.OutputRoot); err != nil {
			return out, err
		}
		b, e := readFile(filepath.Join(p.OutputRoot, "experiment.json"))
		if e != nil || digest(b) != p.ConfigDigest {
			return out, fail("REVISION_MISMATCH", p.OutputRoot)
		}
		if _, e = readFile(filepath.Join(p.OutputRoot, "COMMITTED")); e != nil {
			return out, fail("EVIDENCE_CORRUPT", p.OutputRoot)
		}
	}
	frozen := inputRoot(p.OutputRoot, p.ConfigDigest)
	if store != nil {
		if err = freezeInputs(ctx, p, frozen); err != nil {
			return out, err
		}
	} else if err = verifyRecord(ctx, frozen); err != nil {
		return out, err
	}
	id, e := newID()
	if e != nil {
		return out, e
	}
	out = domain.Preparation{SchemaVersion: "evaris.preparation/v1alpha1", ID: id, ExperimentID: p.Experiment.ID, ConfigDigest: p.ConfigDigest, CreatedAt: time.Now().UTC(), TasksRoot: tasksRoot, Tasks: []domain.PreparedTask{}, ReviewerFiles: reviewerFiles}
	if p.Experiment.Acceptance() {
		out.Mode = domain.ModeAcceptance
	}
	frozenPlan, e := r.Load(ctx, filepath.Join(frozen, "experiment.json"))
	if e != nil {
		return out, e
	}
	for _, fc := range frozenPlan.Cases {
		c := fc.Case
		src := filepath.Join(frozen, c.Fixture)
		m, e := SnapshotWith(ctx, src, SnapshotOptions{MaxEntries: frozenPlan.Experiment.MaxEntries()})
		if e != nil {
			return out, e
		}
		ob, e := readFile(filepath.Join(frozen, c.Oracle))
		if e != nil {
			return out, e
		}
		for _, a := range p.Experiment.Arms {
			if r.beforeCopy != nil {
				if err = r.beforeCopy(a.ID); err != nil {
					return out, err
				}
			}
			prefix := filepath.Join(id, c.ID, a.ID)
			if err = os.MkdirAll(filepath.Join(tasksRoot, prefix), 0700); err != nil {
				return out, err
			}
			if err = copyManifest(ctx, src, work, prefix, m); err != nil {
				return out, err
			}
			task := domain.PreparedTask{CaseID: c.ID, ArmID: a.ID, TaskDigest: digest(fc.Task), FixtureDigest: manifestDigest(m), OracleDigest: digest(ob), Instructions: []domain.InstructionRef{}, InitialFiles: m, GeneratedFiles: domain.Manifest{}, Workspace: filepath.ToSlash(prefix)}
			prompt := append([]byte{}, fc.Task...)
			for i, in := range a.Instructions {
				b, e := readFile(filepath.Join(frozen, in.Path))
				if e != nil {
					return out, e
				}
				name := domain.InstructionWorkspacePath(i)
				if err = work.write(filepath.ToSlash(filepath.Join(prefix, name)), b); err != nil {
					return out, err
				}
				task.Instructions = append(task.Instructions, domain.InstructionRef{Path: in.Path, Revision: in.Revision, SHA256: digest(b)})
				task.GeneratedFiles = append(task.GeneratedFiles, domain.FileRef{Path: name, SHA256: digest(b), Bytes: int64(len(b))})
				prompt = append(prompt, []byte("\nRead instruction: "+name+"\n")...)
			}
			task.PromptDigest = digest(prompt)
			task.GeneratedFiles = append(task.GeneratedFiles, domain.FileRef{Path: "TASK.txt", SHA256: task.PromptDigest, Bytes: int64(len(prompt))})
			if err = work.write(filepath.ToSlash(filepath.Join(prefix, "TASK.txt")), prompt); err != nil {
				return out, err
			}
			out.Tasks = append(out.Tasks, task)
		}
	}
	// Never commit a preparation against source bytes changed during copying.
	current, e := r.Load(ctx, p.ConfigPath)
	if e != nil || current.ConfigDigest != p.ConfigDigest {
		return out, fail("EVIDENCE_CORRUPT", "source configuration")
	}
	for _, fc := range p.Cases {
		limit := SnapshotOptions{MaxEntries: p.Experiment.MaxEntries()}
		a, e := SnapshotWith(ctx, filepath.Join(p.ConfigDir, fc.Case.Fixture), limit)
		if e != nil {
			return out, e
		}
		b, e := SnapshotWith(ctx, filepath.Join(frozen, fc.Case.Fixture), limit)
		if e != nil || manifestDigest(a) != manifestDigest(b) {
			return out, fail("EVIDENCE_CORRUPT", fc.Case.ID)
		}
	}
	for _, c := range p.Experiment.Cases {
		names := []string{c.Task, c.Oracle}
		if c.Verification != nil {
			names = append(names, c.Verification.Harness)
			names = append(names, c.Verification.Files...)
		}
		for _, name := range names {
			a, e := readFile(filepath.Join(p.ConfigDir, name))
			if e != nil {
				return out, e
			}
			b, e := readFile(filepath.Join(frozen, name))
			if e != nil || digest(a) != digest(b) {
				return out, fail("EVIDENCE_CORRUPT", name)
			}
		}
	}
	for _, a := range p.Experiment.Arms {
		for _, in := range a.Instructions {
			x, e := readFile(filepath.Join(p.ConfigDir, in.Path))
			if e != nil {
				return out, e
			}
			y, e := readFile(filepath.Join(frozen, in.Path))
			if e != nil || digest(x) != digest(y) {
				return out, fail("EVIDENCE_CORRUPT", in.Path)
			}
		}
	}
	rec, err := reserve(filepath.Join(p.OutputRoot, "preparations", id))
	if err != nil {
		return out, err
	}
	defer cleanup(rec, &err)
	if err = rec.writeJSON("preparation.json", out); err != nil {
		return out, err
	}
	if store != nil {
		if err = store.commit(); err != nil {
			return out, err
		}
	}
	if r.beforeCommit != nil {
		if err = r.beforeCommit(); err != nil {
			return out, err
		}
	}
	if err = rec.seal(ctx); err != nil {
		return out, err
	}
	work.committed = true
	return out, nil
}
func freezeInputs(ctx context.Context, p application.Plan, dst string) (err error) {
	t, err := reserve(dst)
	if err != nil {
		return err
	}
	defer cleanup(t, &err)
	if err = t.write("experiment.json", p.ConfigBytes); err != nil {
		return err
	}
	privatePaths := []string{p.ConfigPath}
	for _, c := range p.Experiment.Cases {
		privatePaths = append(privatePaths, filepath.Join(p.ConfigDir, c.Oracle))
		if c.Verification != nil {
			privatePaths = append(privatePaths, filepath.Join(p.ConfigDir, c.Verification.Harness))
			for _, f := range c.Verification.Files {
				privatePaths = append(privatePaths, filepath.Join(p.ConfigDir, f))
			}
		}
	}
	if err = t.writeJSON(".evaris-private-sources.json", privatePaths); err != nil {
		return err
	}
	written := map[string]bool{"experiment.json": true}
	put := func(name string) error {
		if written[name] {
			return nil
		}
		b, e := readFile(filepath.Join(p.ConfigDir, name))
		if e != nil {
			return e
		}
		if e = t.write(name, b); e != nil {
			return e
		}
		written[name] = true
		return nil
	}
	copied := map[string]bool{}
	for _, c := range p.Experiment.Cases {
		m, e := SnapshotWith(ctx, filepath.Join(p.ConfigDir, c.Fixture), SnapshotOptions{MaxEntries: p.Experiment.MaxEntries()})
		if e != nil {
			return e
		}
		for _, f := range m {
			for _, part := range strings.Split(f.Path, "/") {
				if part == ".git" || part == ".evaris" || part == ".evaris-instructions" || strings.EqualFold(part, "TASK.txt") {
					return fail("PATH_UNSAFE", f.Path)
				}
			}
		}
		if !copied[c.Fixture] {
			if err = copyManifest(ctx, filepath.Join(p.ConfigDir, c.Fixture), t, c.Fixture, m); err != nil {
				return err
			}
			copied[c.Fixture] = true
			for _, ref := range m {
				written[filepath.ToSlash(filepath.Join(c.Fixture, ref.Path))] = true
			}
		}
		if err = put(c.Task); err != nil {
			return err
		}
		if err = put(c.Oracle); err != nil {
			return err
		}
		if c.Verification != nil {
			if err = put(c.Verification.Harness); err != nil {
				return err
			}
			for _, f := range c.Verification.Files {
				if err = put(f); err != nil {
					return err
				}
			}
		}
	}
	for _, a := range p.Experiment.Arms {
		for _, i := range a.Instructions {
			if err = put(i.Path); err != nil {
				return err
			}
		}
	}
	return t.seal(ctx)
}

// hashReviewerFiles resolves the experiment's reviewer files against its
// directory and records their digests. Each must be a regular file of at most
// 10 MiB outside every fixture, the evidence store and the tasks root.
func hashReviewerFiles(p application.Plan, tasksRoot string) ([]domain.FileRef, error) {
	var refs []domain.FileRef
	for _, rel := range p.Experiment.ReviewerFiles {
		abs := filepath.Join(p.ConfigDir, filepath.FromSlash(rel))
		bad := within(p.OutputRoot, abs) || within(tasksRoot, abs)
		for _, c := range p.Experiment.Cases {
			bad = bad || within(filepath.Join(p.ConfigDir, c.Fixture), abs)
		}
		if bad {
			return nil, fail("PATH_UNSAFE", "reviewer file "+rel)
		}
		ref, err := hashReviewerFile(abs)
		if err != nil {
			return nil, fail("INVALID_CONFIG", "reviewer file "+rel+" must be a regular file of at most 10 MiB")
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func hashReviewerFile(abs string) (domain.FileRef, error) {
	info, err := os.Lstat(abs)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 10<<20 {
		return domain.FileRef{}, fail("PATH_UNSAFE", abs)
	}
	b, err := readFile(abs)
	if err != nil {
		return domain.FileRef{}, err
	}
	return domain.FileRef{Path: abs, SHA256: digest(b), Bytes: int64(len(b)), Executable: info.Mode()&0o111 != 0}, nil
}

// CheckReviewerFiles re-hashes the reviewer files recorded at preparation.
func (r Repository) CheckReviewerFiles(_ context.Context, refs []domain.FileRef) error {
	for _, want := range refs {
		got, err := hashReviewerFile(want.Path)
		if err != nil || got != want {
			return fail("EVIDENCE_CORRUPT", "reviewer file "+want.Path+" changed since preparation")
		}
	}
	return nil
}
