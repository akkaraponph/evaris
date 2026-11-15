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
	}{{[]string{"--help"}, 0}, {[]string{"validate"}, 2}, {[]string{"validate", "--unknown"}, 2}, {[]string{"validate", "--experiment=a", "--experiment=b"}, 2}, {[]string{"evaluate", "--store", "/s", "--run", "r", "--invaris", "relative/invaris"}, 2}, {[]string{"evaluate", "--store", "/s", "--run", "r", "--invaris", "/a", "--invaris", "/b"}, 2}, {[]string{"validate", "--experiment", "x", "--invaris", "/a"}, 2}, {[]string{"compare", "--left", "/a", "--right", "/b"}, 2}, {[]string{"compare", "--left", "/a", "--right", "/b", "--mode", "other"}, 2}} {
		var out, err bytes.Buffer
		if got := Run(context.Background(), tc.args, &out, &err, Runtime{Service: application.Service{Repo: fsrepo.Repository{}}}); got != tc.code {
			t.Fatalf("%v: %d", tc.args, got)
		}
		if tc.code == 2 && out.Len() != 0 {
			t.Fatal("stdout on error")
		}
	}
}

func TestCLIVersion(t *testing.T) {
	for _, c := range []struct{ in, want string }{{"", "evaris dev\n"}, {"v0.1.0", "evaris v0.1.0\n"}} {
		var out, errb bytes.Buffer
		if code := Run(context.Background(), []string{"--version"}, &out, &errb, Runtime{Version: c.in}); code != 0 || out.String() != c.want || errb.Len() != 0 {
			t.Fatalf("%q: code %d out %q err %q", c.in, code, out.String(), errb.String())
		}
	}
}
