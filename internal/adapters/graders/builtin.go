package graders

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/akkaraponph/evaris/internal/adapters/fsrepo"
	"github.com/akkaraponph/evaris/internal/adapters/process"
	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

type Builtin struct{}

func (g Builtin) Grade(ctx context.Context, f application.FrozenRun, c domain.Criterion) (r domain.GraderResult, blobs []application.Blob) {
	r = domain.GraderResult{CriterionID: c.ID, Required: c.Required, Status: "PASS", Evidence: []domain.FileRef{}, GraderVersion: "builtin/1"}
	blobs = []application.Blob{}
	if ctx.Err() != nil {
		r.Status = "ERROR"
		r.Reason = "CANCELED"
		return
	}
	if c.Kind == "command-exit" {
		if !f.AllowCommands {
			r.Status = "UNAVAILABLE"
			r.Reason = "COMMAND_NOT_AUTHORIZED"
			return
		}
		return g.command(ctx, f, c)
	}
	if !domain.Relative(c.Path) {
		r.Status = "ERROR"
		r.Reason = "PATH_UNSAFE"
		return
	}
	p := filepath.Join(f.SnapshotRoot, c.Path)
	b, e := os.ReadFile(p)
	if e != nil {
		if os.IsNotExist(e) {
			r.Status = "FAIL"
			r.Reason = "ASSERTION_FAILED"
		} else {
			r.Status = "ERROR"
			r.Reason = "IO_ERROR"
		}
		return
	}
	pass := true
	switch c.Kind {
	case "exists":
	case "exact":
		var expected string
		e = json.Unmarshal(c.Expected, &expected)
		pass = e == nil && string(b) == expected
	case "json-equals":
		a, ea := strictJSON(b)
		v, ev := strictJSON(c.Expected)
		pass = ea == nil && ev == nil && equal(a, v)
	case "unchanged":
		pass = false
		h := domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(b)))
		for _, t := range f.Preparation.Tasks {
			if t.CaseID == f.Run.CaseID && t.ArmID == f.Run.ArmID {
				for _, ref := range t.InitialFiles {
					if ref.Path == c.Path {
						info, _ := os.Stat(p)
						pass = h == ref.SHA256 && info != nil && (info.Mode()&0111 != 0) == ref.Executable
					}
				}
			}
		}
	default:
		r.Status = "ERROR"
		r.Reason = "INVALID_GRADER_RESULT"
		return
	}
	if !pass {
		r.Status = "FAIL"
		r.Reason = "ASSERTION_FAILED"
	}
	return
}
func strictJSON(b []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		if depth > 32 {
			return nil, fmt.Errorf("depth")
		}
		t, e := d.Token()
		if e != nil {
			return nil, e
		}
		switch t {
		case json.Delim('{'):
			m := map[string]any{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return nil, e
				}
				key := k.(string)
				if _, ok := m[key]; ok {
					return nil, fmt.Errorf("duplicate")
				}
				v, e := read(depth + 1)
				if e != nil {
					return nil, e
				}
				m[key] = v
			}
			_, e = d.Token()
			return m, e
		case json.Delim('['):
			a := []any{}
			for d.More() {
				v, e := read(depth + 1)
				if e != nil {
					return nil, e
				}
				a = append(a, v)
			}
			_, e = d.Token()
			return a, e
		}
		return t, nil
	}
	v, e := read(0)
	if e != nil {
		return nil, e
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, fmt.Errorf("trailing")
	}
	return v, nil
}
func equal(a, b any) bool {
	switch x := a.(type) {
	case json.Number:
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		return canonicalNumber(string(x)) == canonicalNumber(string(y))
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok || !equal(v, w) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i, v := range x {
			if !equal(v, y[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}
func (g Builtin) command(ctx context.Context, f application.FrozenRun, c domain.Criterion) (domain.GraderResult, []application.Blob) {
	r := domain.GraderResult{CriterionID: c.ID, Required: c.Required, Status: "ERROR", Evidence: []domain.FileRef{}, GraderVersion: "builtin/1"}
	scratch, e := os.MkdirTemp("", "evaris-grade-")
	if e != nil {
		r.Reason = "IO_ERROR"
		return r, nil
	}

	defer os.RemoveAll(scratch)
	scratch, e = filepath.EvalSymlinks(scratch)
	if e != nil {
		r.Reason = "IO_ERROR"
		return r, nil
	}
	e = filepath.WalkDir(f.SnapshotRoot, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(f.SnapshotRoot, p)
		target := filepath.Join(scratch, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return &domain.Failure{Code: "PATH_UNSAFE", Path: filepath.ToSlash(rel) + " is a symbolic link"}
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		return os.WriteFile(target, b, info.Mode().Perm())
	})
	if c.Command == nil {
		r.Reason, r.Message = "INVALID_CONFIG", "command-exit criterion has no command"
		return r, nil
	}
	if e != nil {
		r.Reason, r.Message = failureCode(e, "IO_ERROR"), "copy the snapshot into scratch: "+e.Error()
		return r, nil
	}
	before, be := fsrepo.SnapshotExcluding(ctx, scratch, c.SnapshotExclude)
	result := process.Run(ctx, scratch, *c.Command)
	after, ae := fsrepo.SnapshotExcluding(context.WithoutCancel(ctx), scratch, c.SnapshotExclude)
	cleanupErr := os.RemoveAll(scratch)
	if be != nil || ae != nil || cleanupErr != nil {
		// Say which step failed and that the command itself finished, so a
		// passing command is not mistaken for a failing one.
		exit := "did not finish"
		if result.ExitCode != nil {
			exit = fmt.Sprintf("exited %d", *result.ExitCode)
		}
		switch {
		case ctx.Err() != nil:
			result.Reason = "CANCELED"
		case be != nil:
			result.Reason, r.Message = failureCode(be, "IO_ERROR"), "scratch snapshot before the command: "+be.Error()
		case ae != nil:
			result.Reason = failureCode(ae, "IO_ERROR")
			r.Message = "scratch snapshot after the command (command " + exit + "): " + ae.Error()
			if result.Reason == "LIMIT_EXCEEDED" {
				r.Message += "; list generated output such as .next in snapshotExclude"
			}
		default:
			result.Reason, r.Message = "IO_ERROR", "remove scratch: "+cleanupErr.Error()
		}
	}
	if r.Message == "" {
		switch result.Reason {
		case "TIMEOUT":
			r.Message = fmt.Sprintf("command exceeded its timeout of %d seconds", timeoutOf(*c.Command))
		case "LIMIT_EXCEEDED":
			r.Message = "command output exceeded 64 KiB on stdout or stderr"
		case "TOOL_MISSING":
			r.Message = "program " + c.Command.Program + " is not readable"
		case "IO_ERROR":
			r.Message = "command was killed by a signal, left processes behind, or could not be started"
		}
	}
	r.Reason = result.Reason
	var expected int
	_ = json.Unmarshal(c.Expected, &expected)
	if result.Reason == "" {
		r.Status = "FAIL"
		r.Reason = "ASSERTION_FAILED"
		if result.ExitCode != nil && *result.ExitCode == expected {
			r.Status = "PASS"
			r.Reason = ""
		}
	}
	data, _ := json.Marshal(struct {
		Process       process.ProcessResult
		Before, After domain.Manifest
		CleanupOK     bool
	}{result, before, after, cleanupErr == nil})
	name := "evidence/" + c.ID + ".json"
	r.Evidence = []domain.FileRef{{Path: name, SHA256: domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(data))), Bytes: int64(len(data))}}
	return r, []application.Blob{{Path: name, Data: data}}
}

// failureCode is the reason a domain.Failure carries, or fallback.
func failureCode(e error, fallback domain.Reason) domain.Reason {
	var f *domain.Failure
	if errors.As(e, &f) {
		return f.Code
	}
	return fallback
}

func timeoutOf(c domain.Command) int {
	if c.TimeoutSeconds == 0 {
		return 30
	}
	return c.TimeoutSeconds
}

// Normalize decimal notation without allocating a huge power of ten from an
// untrusted exponent. Work stays proportional to the JSON number's byte length.
func canonicalNumber(s string) string {
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign = "-"
		s = s[1:]
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == 'e' || r == 'E' })
	mantissa := parts[0]
	exp := new(big.Int)
	if len(parts) == 2 {
		exp.SetString(parts[1], 10)
	}
	fraction := 0
	if i := strings.IndexByte(mantissa, '.'); i >= 0 {
		fraction = len(mantissa) - i - 1
		mantissa = mantissa[:i] + mantissa[i+1:]
	}
	mantissa = strings.TrimLeft(mantissa, "0")
	if mantissa == "" {
		return "0"
	}
	trimmed := strings.TrimRight(mantissa, "0")
	shift := len(mantissa) - len(trimmed) - fraction
	exp.Add(exp, big.NewInt(int64(shift)))
	return sign + trimmed + "e" + exp.String()
}
