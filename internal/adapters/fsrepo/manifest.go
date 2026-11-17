package fsrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/akkaraponph/evaris/internal/domain"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SnapshotOptions narrows a snapshot. Exclude lists relative paths skipped
// with everything below them (skipped entries count toward no limit);
// MaxEntries bounds the entries walked, 0 meaning domain.DefaultMaxEntries.
type SnapshotOptions struct {
	Exclude    []string
	MaxEntries int
}

// Snapshot identifies bounded regular-file contents without following symbolic links.
func Snapshot(ctx context.Context, root string) (domain.Manifest, error) {
	return SnapshotWith(ctx, root, SnapshotOptions{})
}

// snapshotStored snapshots a committed store record. Its contents are pinned
// by an exact recorded manifest, so the entry bound is the ceiling: a record
// written under a raised experiment limit stays readable.
func snapshotStored(ctx context.Context, root string) (domain.Manifest, error) {
	return SnapshotWith(ctx, root, SnapshotOptions{MaxEntries: domain.MaxEntriesCeiling})
}

// SnapshotWith is Snapshot under o.
func SnapshotWith(ctx context.Context, root string, o SnapshotOptions) (domain.Manifest, error) {
	exclude := o.Exclude
	maxEntries := o.MaxEntries
	if maxEntries <= 0 {
		maxEntries = domain.DefaultMaxEntries
	}
	if e := safe(root); e != nil {
		return nil, e
	}
	manifest := domain.Manifest{}
	entries := 0
	var total int64
	e := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if p == root {
			return nil
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		if excluded(rel, exclude) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		entries++
		if entries > maxEntries {
			return fail("LIMIT_EXCEEDED", fmt.Sprintf("entries: more than %d under the snapshot root", maxEntries))
		}
		if !domain.Relative(rel) {
			return fail("PATH_UNSAFE", rel)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fail("PATH_UNSAFE", rel)
		}
		if d.IsDir() {
			return nil
		}
		before, e := d.Info()
		if e != nil {
			return e
		}
		if !before.Mode().IsRegular() {
			return fail("PATH_UNSAFE", rel)
		}
		if before.Size() > 10<<20 {
			return fail("LIMIT_EXCEEDED", rel)
		}
		f, e := os.Open(p)
		if e != nil {
			return e
		}
		opened, e := f.Stat()
		if e != nil {
			f.Close()
			return e
		}
		if !os.SameFile(before, opened) {
			f.Close()
			return fail("EVIDENCE_CORRUPT", rel)
		}
		b, e := io.ReadAll(io.LimitReader(f, (10<<20)+1))
		after, se := f.Stat()
		ce := f.Close()
		if e != nil {
			return e
		}
		if se != nil {
			return se
		}
		if ce != nil {
			return ce
		}
		if len(b) > 10<<20 {
			return fail("LIMIT_EXCEEDED", rel)
		}
		total += int64(len(b))
		if total > 100<<20 {
			return fail("LIMIT_EXCEEDED", "tree bytes")
		}
		if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
			return fail("EVIDENCE_CORRUPT", rel)
		}
		manifest = append(manifest, domain.FileRef{Path: rel, SHA256: digest(b), Bytes: int64(len(b)), Executable: before.Mode()&0111 != 0})
		return nil
	})
	return manifest, e
}
func excluded(rel string, exclude []string) bool {
	for _, x := range exclude {
		if rel == x || strings.HasPrefix(rel, x+"/") {
			return true
		}
	}
	return false
}

func manifestDigest(m domain.Manifest) domain.Digest {
	b, _ := json.MarshalIndent(m, "", "  ")
	return digest(append(b, '\n'))
}
