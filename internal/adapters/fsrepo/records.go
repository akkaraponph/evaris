package fsrepo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/akkaraponph/evaris/internal/domain"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var recordID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func newID() (string, error) {
	b := make([]byte, 16)
	_, e := rand.Read(b)
	return hex.EncodeToString(b), e
}
func jsonBytes(v any) ([]byte, error) {
	b, e := json.MarshalIndent(v, "", "  ")
	return append(b, '\n'), e
}
func (t *transaction) writeJSON(name string, v any) error {
	b, e := jsonBytes(v)
	if e != nil {
		return e
	}
	if len(b) > 1<<20 {
		return fail("LIMIT_EXCEEDED", name)
	}
	return t.write(name, b)
}
func (t *transaction) seal(ctx context.Context) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	m, e := Snapshot(ctx, t.root)
	if e != nil {
		return e
	}
	if e = t.write("COMMITTED", []byte(manifestDigest(m))); e != nil {
		return e
	}
	t.committed = true
	return nil
}
func verifyRecord(ctx context.Context, root string) error {
	b, e := readFile(filepath.Join(root, "COMMITTED"))
	if e != nil {
		return fail("EVIDENCE_CORRUPT", root)
	}
	m, e := Snapshot(ctx, root)
	if e != nil {
		return e
	}
	out := domain.Manifest{}
	for _, f := range m {
		if f.Path != "COMMITTED" {
			out = append(out, f)
		}
	}
	if string(manifestDigest(out)) != string(b) {
		return fail("EVIDENCE_CORRUPT", root)
	}
	return nil
}
func cleanup(t *transaction, err *error) {
	if t != nil {
		if e := t.rollback(); e != nil {
			*err = errors.Join(*err, fmt.Errorf("cleanup %s: %w", t.root, e))
		}
	}
}
func rootPath(p string) (string, error) {
	if !filepath.IsAbs(p) {
		return "", fail("PATH_UNSAFE", p)
	}
	p = filepath.Clean(p)
	if e := safe(p); e != nil {
		return "", e
	}
	return p, nil
}
func ensureDirs(root string, names ...string) error {
	for _, n := range names {
		p := filepath.Join(root, n)
		if e := os.Mkdir(p, 0700); e != nil && !errors.Is(e, os.ErrExist) {
			return e
		}
		if e := safe(p); e != nil {
			return e
		}
		i, e := os.Stat(p)
		if e != nil || !i.IsDir() {
			return fail("PATH_UNSAFE", p)
		}
	}
	return nil
}
func readRecord(ctx context.Context, root, name string, v any) error {
	if e := verifyRecord(ctx, root); e != nil {
		return e
	}
	b, e := readFile(filepath.Join(root, name))
	if e != nil {
		return e
	}
	if e = decodeMode(b, v, true); e != nil {
		return fail("EVIDENCE_CORRUPT", name)
	}
	return nil
}
func inputRoot(store string, d domain.Digest) string {
	return filepath.Join(store, "inputs", strings.TrimPrefix(string(d), "sha256:"))
}
