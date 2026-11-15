//go:build linux || darwin

package process

import (
	"context"
	"github.com/akkaraponph/evaris/internal/domain"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCommandTimeoutAndCrash(t *testing.T) {
	for _, tc := range []struct {
		arg     string
		seconds int
		reason  domain.Reason
	}{{"sleep 5", 1, "TIMEOUT"}, {"kill -KILL $$", 1, "IO_ERROR"}, {"sleep 5 & exit 0", 2, "IO_ERROR"}} {
		start := time.Now()
		r := Run(context.Background(), t.TempDir(), domain.Command{Program: "/bin/sh", Args: []string{"-c", tc.arg}, TimeoutSeconds: tc.seconds})
		if r.Reason != tc.reason || time.Since(start) > 4*time.Second {
			t.Fatalf("%s: %s", tc.arg, r.Reason)
		}
	}
}
func TestEnvironmentAllowlist(t *testing.T) {
	t.Setenv("EVARIS_SECRET_TEST", "do-not-inherit")
	r := Run(context.Background(), t.TempDir(), domain.Command{Program: "/bin/sh", Args: []string{"-c", "test -z \"$EVARIS_SECRET_TEST\""}})
	if r.Reason != "" || r.ExitCode == nil || *r.ExitCode != 0 {
		t.Fatal(r)
	}
}
func TestCommandOutputLimit(t *testing.T) {
	r := Run(context.Background(), t.TempDir(), domain.Command{Program: "/bin/sh", Args: []string{"-c", "head -c 70000 /dev/zero"}, EnvAllowlist: []string{"PATH"}})
	if r.Reason != "LIMIT_EXCEEDED" || len(r.Stdout) != 64<<10 {
		t.Fatal(r.Reason, len(r.Stdout))
	}
}
func TestProcessGroupCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Run(ctx, filepath.Dir(os.TempDir()), domain.Command{Program: "/bin/sh", Args: []string{"-c", "sleep 5"}})
	if r.Reason != "CANCELED" {
		t.Fatal(r)
	}
}
