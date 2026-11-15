package main

import (
	"context"
	"github.com/akkaraponph/evaris/internal/adapters/cli"
	"github.com/akkaraponph/evaris/internal/adapters/fsrepo"
	"github.com/akkaraponph/evaris/internal/adapters/graders"
	"github.com/akkaraponph/evaris/internal/adapters/render"
	"github.com/akkaraponph/evaris/internal/application"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, cli.Runtime{Service: application.Service{Repo: fsrepo.Repository{}, Grader: graders.Builtin{}, JSONReport: render.JSON, MarkdownReport: render.Markdown}, Version: "dev"}))
}
