package fsrepo

import (
	"context"
	"encoding/json"
	"github.com/akkaraponph/evaris/internal/domain"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Snapshot identifies bounded regular-file contents without following symbolic links.
func Snapshot(ctx context.Context, root string) (domain.Manifest, error) {
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
		entries++
		if entries > 2000 {
			return fail("LIMIT_EXCEEDED", "entries")
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
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
func manifestDigest(m domain.Manifest) domain.Digest {
	b, _ := json.MarshalIndent(m, "", "  ")
	return digest(append(b, '\n'))
}
