package fsrepo

import (
	"errors"
	"github.com/akkaraponph/evaris/internal/domain"
	"os"
	"path/filepath"
)

// transaction reserves its directory exclusively. Only its owner may roll it back.
type transaction struct {
	root      string
	committed bool
	identity  os.FileInfo
	fault     func(string) error
}

func reserve(root string) (*transaction, error) {
	if e := safe(filepath.Dir(root)); e != nil {
		return nil, e
	}
	if e := os.Mkdir(root, 0700); e != nil {
		if errors.Is(e, os.ErrExist) {
			return nil, fail("DEST_EXISTS", root)
		}
		return nil, e
	}
	info, e := os.Lstat(root)
	if e != nil {
		return nil, e
	}
	return &transaction{root: root, identity: info}, nil
}
func (t *transaction) owned() error {
	if t.identity == nil {
		return nil
	}
	i, e := os.Lstat(t.root)
	if e != nil || !os.SameFile(t.identity, i) {
		return fail("PATH_UNSAFE", t.root)
	}
	return nil
}
func (t *transaction) write(name string, b []byte) error {
	if e := t.owned(); e != nil {
		return e
	}
	if t.fault != nil {
		if e := t.fault("write"); e != nil {
			return e
		}
	}
	if !domain.Relative(name) {
		return fail("PATH_UNSAFE", name)
	}
	p := filepath.Join(t.root, filepath.FromSlash(name))
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	if e := safe(filepath.Dir(p)); e != nil {
		return e
	}
	f, e := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	if e == nil {
		if t.fault != nil {
			e = t.fault("sync")
		}
		if e == nil {
			e = f.Sync()
		}
	}
	ce := f.Close()
	if t.fault != nil {
		ce = errors.Join(ce, t.fault("close"))
	}
	return errors.Join(e, ce)
}
func (t *transaction) commit() error {
	if e := t.write("COMMITTED", []byte("complete\n")); e != nil {
		return e
	}
	t.committed = true
	return nil
}
func (t *transaction) rollback() error {
	if t.committed {
		return nil
	}
	if e := t.owned(); e != nil {
		return e
	}
	return os.RemoveAll(t.root)
}
