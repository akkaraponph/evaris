package main

import (
	"context"
	"github.com/akkaraponph/evaris/internal/adapters/cli"
	"github.com/akkaraponph/evaris/internal/adapters/fsrepo"
	"github.com/akkaraponph/evaris/internal/adapters/graders"
	"github.com/akkaraponph/evaris/internal/adapters/invaris"
	"github.com/akkaraponph/evaris/internal/adapters/render"
	"github.com/akkaraponph/evaris/internal/application"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
)

// version is set for release builds with -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

// buildVersion falls back to the module version recorded by `go install module@vX.Y.Z`.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, cli.Runtime{Service: application.Service{Version: buildVersion(), Repo: fsrepo.Repository{CheckHarness: invaris.CheckHarness}, Grader: graders.Builtin{}, JSONReport: render.JSON, MarkdownReport: render.Markdown}, Version: buildVersion(), Verifier: func(p string) application.Verifier { return invaris.Client{Executable: p} }}))
}
