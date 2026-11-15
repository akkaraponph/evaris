package cli

import (
	"bytes"
	"context"
	"github.com/akkaraponph/evaris/internal/adapters/fsrepo"
	"github.com/akkaraponph/evaris/internal/application"
	"testing"
)

func TestCLIFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{{[]string{"--help"}, 0}, {[]string{"validate"}, 2}, {[]string{"validate", "--unknown"}, 2}, {[]string{"validate", "--experiment=a", "--experiment=b"}, 2}} {
		var out, err bytes.Buffer
		if got := Run(context.Background(), tc.args, &out, &err, Runtime{Service: application.Service{Repo: fsrepo.Repository{}}}); got != tc.code {
			t.Fatalf("%v: %d", tc.args, got)
		}
		if tc.code == 2 && out.Len() != 0 {
			t.Fatal("stdout on error")
		}
	}
}
