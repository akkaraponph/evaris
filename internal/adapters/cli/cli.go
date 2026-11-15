package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
	"io"
	"strings"
)

type Runtime struct {
	Service application.Service
	Version string
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer, runtime Runtime) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(stdout, "usage: evaris <validate|prepare|import|evaluate|report> [flags]")
		return 0
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "INVALID_CONFIG: expected subcommand")
		return 2
	}
	name := args[0]
	if name != "validate" && name != "prepare" && name != "import" && name != "evaluate" && name != "report" {
		fmt.Fprintln(stderr, "INVALID_CONFIG: unknown subcommand")
		return 2
	}
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(stderr)
	var experiment, tasks, store, record, run string
	var allow bool
	if name == "evaluate" || name == "report" {
		f.StringVar(&store, "store", "", "absolute evidence store")
		if name == "evaluate" {
			f.StringVar(&run, "run", "", "run ID")
			f.BoolVar(&allow, "allow-commands", false, "authorize trusted commands")
		}
	} else if name == "import" {
		f.StringVar(&store, "store", "", "absolute evidence store")
		f.StringVar(&record, "record", "", "import request JSON")
	} else {
		f.StringVar(&experiment, "experiment", "", "experiment JSON file")
		if name == "prepare" {
			f.StringVar(&tasks, "tasks-root", "", "new absolute task root")
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
	if f.NArg() != 0 || name == "validate" && experiment == "" || name == "prepare" && (experiment == "" || tasks == "") || name == "import" && (store == "" || record == "") || name == "evaluate" && (store == "" || run == "") || name == "report" && store == "" {
		fmt.Fprintln(stderr, "INVALID_CONFIG: required flags missing")
		return 2
	}
	var out any
	var e error
	switch name {
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
	}
	if e != nil {
		fmt.Fprintln(stderr, e)
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
