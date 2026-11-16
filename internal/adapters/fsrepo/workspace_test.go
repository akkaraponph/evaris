package fsrepo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Cancel after initialization has begun to verify rollback, not just the
// preflight context check. Calls occur only in this test's goroutine.
type initCancelContext struct {
	context.Context
	calls int
}

func (c *initCancelContext) Err() error {
	c.calls++
	if c.calls >= 4 {
		return context.Canceled
	}
	return nil
}

func TestInitRollsBackPartialWorkspace(t *testing.T) {
	root := filepath.Join(canonicalTemp(t), ".evaris")
	ctx := &initCancelContext{Context: context.Background()}
	if _, err := (Repository{}).Init(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatal("partial workspace remains", err)
	}
}

func TestInitRejectsLinkedParent(t *testing.T) {
	root := canonicalTemp(t)
	target := canonicalTemp(t)
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := (Repository{}).Init(context.Background(), filepath.Join(root, "link", ".evaris")); err == nil {
		t.Fatal("accepted linked parent")
	}
	if _, err := os.Lstat(filepath.Join(target, ".evaris")); !os.IsNotExist(err) {
		t.Fatal("wrote through symlink", err)
	}
}
