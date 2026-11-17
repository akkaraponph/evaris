package cli

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
	"io"
	"path/filepath"
	"strings"
)

type Runtime struct {
	Service application.Service
	Version string
	// Verifier builds the optional external verifier for an absolute --invaris path.
	Verifier func(executable string) application.Verifier
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer, runtime Runtime) int {
	if len(args) == 1 && args[0] == "--version" {
		v := runtime.Version
		if v == "" {
			v = "dev"
		}
		fmt.Fprintln(stdout, "evaris", v)
		return 0
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(stdout, "usage: evaris <init|validate|prepare|import|evaluate|report|compare> [flags] | --version")
		return 0
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "INVALID_CONFIG: expected subcommand")
		return 2
	}
	name := args[0]
	if name != "init" && name != "validate" && name != "prepare" && name != "import" && name != "evaluate" && name != "report" && name != "compare" && name != "show" {
		fmt.Fprintln(stderr, "INVALID_CONFIG: unknown subcommand")
		return 2
	}
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(stderr)
	var experiment, tasks, store, record, run, invarisPath, left, right, mode, evaluation, criterion string
	var allow bool
	if name == "show" {
		f.StringVar(&store, "store", "", "absolute evidence store (default .evaris/evidence)")
		f.StringVar(&evaluation, "evaluation", "", "evaluation ID")
		f.StringVar(&criterion, "criterion", "", "only this criterion (\"verification\" for verification)")
	} else if name == "evaluate" || name == "report" {
		f.StringVar(&store, "store", "", "absolute evidence store (default .evaris/evidence)")
		if name == "evaluate" {
			f.StringVar(&run, "run", "", "run ID")
			f.BoolVar(&allow, "allow-commands", false, "authorize trusted commands")
			f.StringVar(&invarisPath, "invaris", "", "absolute Invaris executable for configured verification")
		}
	} else if name == "compare" {
		f.StringVar(&left, "left", "", "absolute left evidence store (receives the comparison)")
		f.StringVar(&right, "right", "", "absolute right evidence store")
		f.StringVar(&mode, "mode", "", "arms or revisions")
	} else if name == "import" {
		f.StringVar(&store, "store", "", "absolute evidence store (default .evaris/evidence)")
		f.StringVar(&record, "record", "", "import request JSON")
	} else if name != "init" {
		f.StringVar(&experiment, "experiment", ".evaris/experiment.json", "experiment JSON file (default .evaris/experiment.json)")
		if name == "prepare" {
			f.StringVar(&tasks, "tasks-root", "", "new absolute task root (default fresh directory under .evaris/tasks)")
		}
	}
	// Parse one flag at a time so values beginning with '-' are not mistaken for flags.
	seen := map[string]bool{}
	for i := 1; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			continue
		}
		k := strings.SplitN(strings.TrimLeft(a, "-"), "=", 2)[0]
		if seen[k] {
			fmt.Fprintln(stderr, "INVALID_CONFIG: repeated flag")
			return 2
		}
		seen[k] = true
		if !strings.Contains(a, "=") && k != "h" && k != "help" && k != "allow-commands" {
			i++
		}
	}
	if e := f.Parse(args[1:]); e != nil {
		if e == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if f.NArg() != 0 || name == "validate" && experiment == "" || name == "prepare" && (experiment == "" || seen["tasks-root"] && tasks == "") || name == "import" && record == "" || name == "evaluate" && run == "" || name == "show" && evaluation == "" || seen["criterion"] && criterion == "" || seen["store"] && store == "" || name == "compare" && (left == "" || right == "" || mode != "arms" && mode != "revisions") {
		fmt.Fprintln(stderr, "INVALID_CONFIG: required flags missing")
		return 2
	}
	// Resolve only default locations. Explicit paths keep their existing rules.
	if name == "import" || name == "evaluate" || name == "report" || name == "show" {
		if !seen["store"] {
			var err error
			store, err = filepath.Abs(filepath.Join(".evaris", "evidence"))
			if err != nil {
				fmt.Fprintln(stderr, application.AsFailure(err))
				return 2
			}
		}
	}
	if name == "prepare" && !seen["tasks-root"] {
		var err error
		tasks, err = filepath.Abs(filepath.Join(".evaris", "tasks", rand.Text()))
		if err != nil {
			fmt.Fprintln(stderr, application.AsFailure(err))
			return 2
		}
	}
	if invarisPath != "" {
		if !filepath.IsAbs(invarisPath) || runtime.Verifier == nil {
			fmt.Fprintln(stderr, "INVALID_CONFIG: --invaris requires an absolute executable path")
			return 2
		}
		runtime.Service.Verifier = runtime.Verifier(filepath.Clean(invarisPath))
	}
	var out any
	var e error
	switch name {
	case "init":
		var config string
		config, e = runtime.Service.Init(ctx, ".evaris")
		out = struct {
			ConfigPath string `json:"configPath"`
		}{config}
	case "validate":
		p, err := runtime.Service.Validate(ctx, experiment)
		e = err
		out = struct {
			Valid        bool   `json:"valid"`
			ExperimentID string `json:"experimentId"`
			ConfigDigest any    `json:"configDigest"`
		}{true, p.Experiment.ID, p.ConfigDigest}
	case "prepare":
		out, e = runtime.Service.Prepare(ctx, experiment, tasks)
	case "evaluate":
		out, e = runtime.Service.Evaluate(ctx, store, run, allow)
	case "report":
		out, e = runtime.Service.Report(ctx, store)
	case "import":
		out, e = runtime.Service.Import(ctx, store, record)
	case "compare":
		out, e = runtime.Service.Compare(ctx, left, right, mode)
	case "show":
		// show prints text, not JSON, and exits 0 when it could read the evaluation.
		text, err := runtime.Service.Show(ctx, store, evaluation, criterion)
		if err != nil {
			fmt.Fprintln(stderr, application.AsFailure(err))
			return 2
		}
		if _, err := io.WriteString(stdout, text); err != nil {
			return 2
		}
		return 0
	}
	if e != nil {
		fmt.Fprintln(stderr, application.AsFailure(e))
		return 2
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if enc.Encode(out) != nil {
		return 2
	}
	switch v := out.(type) {
	case domain.Evaluation:
		if v.Assessment == "FAIL" {
			return 1
		}
		if v.Assessment != "PASS" {
			return 2
		}
	case domain.Comparison:
		// 1 = observed comparable regression; 2 = not comparable or unknown.
		if v.Comparable && v.Regressed() {
			return 1
		}
		if !v.Known() {
			return 2
		}
	case domain.Report:
		code := 0
		for _, row := range v.Cases {
			if row.Assessment == "FAIL" {
				return 1
			}
			if row.Assessment != "PASS" {
				code = 2
			}
		}
		return code
	}
	return 0
}
