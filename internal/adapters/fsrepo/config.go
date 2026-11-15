package fsrepo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

type Repository struct {
	beforeCopy   func(string) error
	beforeCommit func() error
}

func fail(code domain.Reason, p string) error { return &domain.Failure{Code: code, Path: p} }
func digest(b []byte) domain.Digest           { return domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(b))) }

// Decode rejects duplicates, nulls, omitted required fields and trailing input before typed decoding.
func decode(b []byte, out any) error { return decodeMode(b, out, false) }
func decodeMode(b []byte, out any, nullable bool) error {
	if len(b) > 1<<20 {
		return fail("LIMIT_EXCEEDED", "JSON")
	}
	if !utf8.Valid(b) {
		return fail("INVALID_CONFIG", "JSON")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		if depth > 32 {
			return nil, fail("LIMIT_EXCEEDED", "JSON depth")
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
					return nil, fail("DUPLICATE_ID", "JSON key")
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
		return e
	}
	if _, ok := v.(map[string]any); !ok {
		return fail("INVALID_CONFIG", "JSON object")
	}
	if _, e = d.Token(); e != io.EOF {
		return fail("INVALID_CONFIG", "trailing JSON")
	}
	var presence func(any, reflect.Type) error
	presence = func(v any, t reflect.Type) error {
		if nullable && t.Kind() == reflect.Pointer && v == nil {
			return nil
		}
		if t == reflect.TypeOf(time.Time{}) || t == reflect.TypeOf(&time.Time{}) {
			if _, ok := v.(string); !ok {
				return fail("EVIDENCE_CORRUPT", "timestamp")
			}
			return nil
		}
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t == reflect.TypeOf(json.RawMessage{}) {
			return nil
		}
		if v == nil {
			return fail("INVALID_CONFIG", "null field")
		}
		if t == reflect.TypeOf(json.RawMessage{}) {
			return nil
		}
		switch t.Kind() {
		case reflect.Struct:
			m, ok := v.(map[string]any)
			if !ok {
				return fail("INVALID_CONFIG", "object")
			}
			for i := 0; i < t.NumField(); i++ {
				f := t.Field(i)
				tag := strings.Split(f.Tag.Get("json"), ",")
				name := tag[0]
				x, ok := m[name]
				if ok && name == "timeoutSeconds" {
					if n, yes := x.(json.Number); yes {
						v, e := n.Int64()
						if e != nil || v < 1 || v > 300 {
							return fail("INVALID_CONFIG", name)
						}
					}
				}
				if !ok {
					if len(tag) > 1 && tag[1] == "omitempty" {
						continue
					}
					return fail("INVALID_CONFIG", name)
				}
				if e := presence(x, f.Type); e != nil {
					return e
				}
			}
		case reflect.Slice:
			a, ok := v.([]any)
			if !ok {
				return fail("INVALID_CONFIG", "array")
			}
			for _, x := range a {
				if e := presence(x, t.Elem()); e != nil {
					return e
				}
			}
		}
		return nil
	}
	if e = presence(v, reflect.TypeOf(out).Elem()); e != nil {
		return e
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(out)
}
func safe(p string) error {
	p, e := filepath.Abs(p)
	if e != nil {
		return e
	}
	for q := p; ; q = filepath.Dir(q) {
		i, e := os.Lstat(q)
		if e != nil {
			return e
		}
		if i.Mode()&os.ModeSymlink != 0 {
			return fail("PATH_UNSAFE", p)
		}
		if q == filepath.Dir(q) {
			break
		}
	}
	return nil
}
func readFile(p string) ([]byte, error) {
	if e := safe(p); e != nil {
		return nil, e
	}
	before, e := os.Lstat(p)
	if e != nil {
		return nil, e
	}
	if !before.Mode().IsRegular() {
		return nil, fail("PATH_UNSAFE", p)
	}
	f, e := os.Open(p)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	i, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !i.Mode().IsRegular() || !os.SameFile(before, i) {
		return nil, fail("PATH_UNSAFE", p)
	}
	b, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if len(b) > 1<<20 {
		return nil, fail("LIMIT_EXCEEDED", p)
	}
	return b, e
}
func within(a, b string) bool {
	r, e := filepath.Rel(a, b)
	return e == nil && (r == "." || r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)))
}
func (r Repository) Load(ctx context.Context, p string) (plan application.Plan, err error) {
	defer func() {
		if err != nil {
			if _, ok := err.(*domain.Failure); !ok {
				err = fail("INVALID_CONFIG", "experiment inputs")
			}
		}
	}()
	if err = ctx.Err(); err != nil {
		return
	}
	p, err = filepath.Abs(p)
	if err != nil {
		return
	}
	plan.ConfigPath = p
	plan.ConfigDir = filepath.Dir(p)
	plan.ConfigBytes, err = readFile(p)
	if err != nil {
		return
	}
	if err = decode(plan.ConfigBytes, &plan.Experiment); err != nil {
		return
	}
	if err = domain.ValidateExperiment(plan.Experiment); err != nil {
		return
	}
	plan.ConfigDigest = digest(plan.ConfigBytes)
	plan.OutputRoot = filepath.Join(plan.ConfigDir, plan.Experiment.OutputRoot)
	for _, c := range plan.Experiment.Cases {
		for _, private := range plan.Experiment.Cases {
			if c.Task == private.Oracle {
				err = fail("PATH_UNSAFE", "task references oracle")
				return
			}
		}
		if filepath.Join(plan.ConfigDir, c.Task) == p {
			err = fail("PATH_UNSAFE", "task references configuration")
			return
		}
	}
	for _, a := range plan.Experiment.Arms {
		for _, in := range a.Instructions {
			if filepath.Join(plan.ConfigDir, in.Path) == p {
				err = fail("PATH_UNSAFE", "instruction references configuration")
				return
			}
			for _, c := range plan.Experiment.Cases {
				if in.Path == c.Oracle {
					err = fail("PATH_UNSAFE", "instruction references oracle")
					return
				}
			}
		}
	}
	for _, public := range plan.Experiment.Cases {
		root := filepath.Join(plan.ConfigDir, public.Fixture)
		for _, private := range plan.Experiment.Cases {
			if within(root, filepath.Join(plan.ConfigDir, private.Oracle)) {
				err = fail("PATH_UNSAFE", "cross-case oracle")
				return
			}
		}
	}
	for _, c := range plan.Experiment.Cases {
		fixture := filepath.Join(plan.ConfigDir, c.Fixture)
		oracle := filepath.Join(plan.ConfigDir, c.Oracle)
		if within(fixture, oracle) || within(fixture, p) || within(fixture, plan.OutputRoot) || within(plan.OutputRoot, fixture) || within(plan.OutputRoot, oracle) {
			err = fail("PATH_UNSAFE", c.ID)
			return
		}
		if err = safe(fixture); err != nil {
			return
		}
		info, e := os.Stat(fixture)
		if e != nil || !info.IsDir() {
			err = fail("INVALID_CONFIG", c.Fixture)
			return
		}
		fc := application.FrozenCase{Case: c}
		fc.Files, err = Snapshot(ctx, fixture)
		if err != nil {
			return
		}
		for _, ref := range fc.Files {
			for _, part := range strings.Split(ref.Path, "/") {
				if strings.EqualFold(part, "TASK.txt") || part == ".git" || part == ".evaris" || part == ".evaris-instructions" {
					err = fail("PATH_UNSAFE", ref.Path)
					return
				}
			}
		}
		fc.Task, err = readFile(filepath.Join(plan.ConfigDir, c.Task))
		if err != nil {
			return
		}
		if !utf8.Valid(fc.Task) || len(bytes.TrimSpace(fc.Task)) == 0 {
			err = fail("INVALID_CONFIG", c.Task)
			return
		}
		var b []byte
		b, err = readFile(oracle)
		if err != nil {
			return
		}
		if err = decode(b, &fc.Oracle); err != nil {
			return
		}
		if fc.Oracle.SchemaVersion != "evaris.oracle/v1alpha1" {
			err = fail("UNSUPPORTED_SCHEMA", c.Oracle)
			return
		}
		if len(fc.Oracle.Criteria) == 0 || len(fc.Oracle.Criteria) > 100 {
			err = fail("INVALID_CONFIG", c.Oracle)
			return
		}
		seen := map[string]bool{}
		required := false
		for _, g := range fc.Oracle.Criteria {
			if seen[g.ID] {
				err = fail("DUPLICATE_ID", g.ID)
				return
			}
			seen[g.ID] = true
			if g.Required {
				required = true
			}
			if !validID(g.ID) || g.ID == "source-authorization" {
				err = fail("INVALID_CONFIG", "criterion ID")
				return
			}
			switch g.Kind {
			case "exists", "unchanged":
				if g.Kind == "unchanged" {
					found := false
					for _, ref := range fc.Files {
						if ref.Path == g.Path {
							found = true
						}
					}
					if !found {
						err = fail("INVALID_CONFIG", g.ID)
						return
					}
				}
				if !domain.Relative(g.Path) || g.Expected != nil || g.Command != nil {
					err = fail("INVALID_CONFIG", g.ID)
					return
				}
			case "exact", "json-equals":
				if !domain.Relative(g.Path) || g.Expected == nil || bytes.Equal(bytes.TrimSpace(g.Expected), []byte("null")) || g.Command != nil {
					err = fail("INVALID_CONFIG", g.ID)
					return
				}
				if g.Kind == "exact" {
					var s string
					if json.Unmarshal(g.Expected, &s) != nil {
						err = fail("INVALID_CONFIG", g.ID)
						return
					}
				}
			case "command-exit":
				var code int
				if g.Command == nil || g.Path != "" || json.Unmarshal(g.Expected, &code) != nil || code < 0 || code > 255 || !filepath.IsAbs(g.Command.Program) || g.Command.Args == nil || g.Command.EnvAllowlist == nil || g.Command.TimeoutSeconds < 0 || g.Command.TimeoutSeconds > 300 {
					err = fail("INVALID_CONFIG", g.ID)
					return
				}
			default:
				err = fail("INVALID_CONFIG", g.ID)
				return
			}
		}
		for _, g := range fc.Oracle.Criteria {
			if g.Command != nil {
				seenEnv := map[string]bool{}
				for _, key := range g.Command.EnvAllowlist {
					if key == "" || strings.ContainsAny(key, "=\x00") || seenEnv[key] {
						err = fail("INVALID_CONFIG", g.ID)
						return
					}
					seenEnv[key] = true
				}
				if strings.ContainsRune(g.Command.Program, 0) {
					err = fail("INVALID_CONFIG", g.ID)
					return
				}
			}
		}
		if !required {
			err = fail("INVALID_CONFIG", "required criterion")
			return
		}
		plan.Cases = append(plan.Cases, fc)
	}
	for _, a := range plan.Experiment.Arms {
		for _, i := range a.Instructions {
			ip := filepath.Join(plan.ConfigDir, i.Path)
			if within(plan.OutputRoot, ip) {
				err = fail("PATH_UNSAFE", i.Path)
				return
			}
			for _, c := range plan.Experiment.Cases {
				if within(filepath.Join(plan.ConfigDir, c.Fixture), ip) {
					err = fail("PATH_UNSAFE", i.Path)
					return
				}
			}
			if _, err = readFile(ip); err != nil {
				return
			}
		}
	}
	return
}

func validID(s string) bool {
	if len(s) < 1 || len(s) > 64 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, r := range s {
		if r != '-' && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
