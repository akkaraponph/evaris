package fsrepo

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/akkaraponph/evaris/internal/domain"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) (config, tasks string) {
	t.Helper()
	root := canonicalTemp(t)
	src := "../../../testdata/valid"
	e := filepath.WalkDir(src, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(src, p)
		dest := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		return os.WriteFile(dest, b, 0600)
	})
	if e != nil {
		t.Fatal(e)
	}
	return filepath.Join(root, "experiment.json"), filepath.Join(root, "tasks")
}
func prepared(t *testing.T) (config, store string, p domain.Preparation) {
	t.Helper()
	config, tasks := fixture(t)
	plan, e := (Repository{}).Load(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	p, e = (Repository{}).Prepare(context.Background(), plan, tasks)
	if e != nil {
		t.Fatal(e)
	}
	return config, plan.OutputRoot, p
}
func TestPrepareBlindArms(t *testing.T) {
	_, store, p := prepared(t)
	if len(p.Tasks) != 2 {
		t.Fatal(p)
	}
	a, b := p.Tasks[0], p.Tasks[1]
	if a.FixtureDigest != b.FixtureDigest || a.TaskDigest != b.TaskDigest {
		t.Fatal("unequal common inputs")
	}
	for _, task := range p.Tasks {
		root := filepath.Join(p.TasksRoot, task.Workspace)
		m, e := Snapshot(context.Background(), root)
		if e != nil {
			t.Fatal(e)
		}
		for _, f := range m {
			data, e := os.ReadFile(filepath.Join(root, f.Path))
			if e != nil {
				t.Fatal(e)
			}
			if bytes.Contains(data, []byte("evaris.oracle")) || bytes.Contains(data, []byte("reviewer/oracle")) {
				t.Fatal("oracle leakage")
			}
		}
		_, e = os.Stat(filepath.Join(root, ".evaris-instructions"))
		if task.ArmID == "baseline" && !os.IsNotExist(e) {
			t.Fatal("baseline instructions")
		}
		if task.ArmID == "specialist" && e != nil {
			t.Fatal(e)
		}
	}
	if e := verifyRecord(context.Background(), filepath.Join(store, "preparations", p.ID)); e != nil {
		t.Fatal(e)
	}
}
func TestArmsIndependent(t *testing.T) {
	config, _, p := prepared(t)
	a := filepath.Join(p.TasksRoot, p.Tasks[0].Workspace, "input.txt")
	b := filepath.Join(p.TasksRoot, p.Tasks[1].Workspace, "input.txt")
	before, _ := os.ReadFile(b)
	if e := os.WriteFile(a, []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(b)
	orig, _ := os.ReadFile(filepath.Join(filepath.Dir(config), "public/input.txt"))
	if !bytes.Equal(before, after) || !bytes.Equal(orig, before) {
		t.Fatal("copies share source")
	}
}
func TestPrepareRefuses(t *testing.T) {
	for _, kind := range []string{"existing", "symlink", "nested", "reserved"} {
		t.Run(kind, func(t *testing.T) {
			config, tasks := fixture(t)
			if kind == "existing" {
				os.Mkdir(tasks, 0700)
			}
			if kind == "symlink" {
				os.Symlink("missing", tasks)
			}
			if kind == "nested" {
				tasks = filepath.Join(filepath.Dir(config), "public/tasks")
			}
			if kind == "reserved" {
				os.WriteFile(filepath.Join(filepath.Dir(config), "public/TASK.txt"), []byte("x"), 0600)
			}
			p, e := (Repository{}).Load(context.Background(), config)
			if e != nil {
				if kind == "reserved" {
					return
				}
				t.Fatal(e)
			}
			if _, e = (Repository{}).Prepare(context.Background(), p, tasks); e == nil {
				t.Fatal("accepted unsafe prepare")
			}
		})
	}
}
func TestPrepareRollback(t *testing.T) {
	config, tasks := fixture(t)
	p, e := (Repository{}).Load(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = (Repository{}).Prepare(ctx, p, tasks); e == nil {
		t.Fatal("ignored cancellation")
	}
	if _, e = os.Lstat(tasks); !os.IsNotExist(e) {
		t.Fatal("orphan tasks")
	}
}
func TestExperimentStoreConflict(t *testing.T) {
	config, _, _ := prepared(t)
	b, _ := os.ReadFile(config)
	os.WriteFile(config, append(b, ' '), 0600)
	p, e := (Repository{}).Load(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = (Repository{}).Prepare(context.Background(), p, filepath.Join(filepath.Dir(config), "other-tasks")); e == nil {
		t.Fatal("accepted changed configuration")
	}
}
func request(t *testing.T, p domain.Preparation, config string) string {
	t.Helper()
	task := p.Tasks[0]
	response := filepath.Join(p.TasksRoot, "response.txt")
	os.WriteFile(response, []byte("actual external answer"), 0600)
	q := domain.ImportRequest{SchemaVersion: "evaris.import/v1alpha1", PreparationID: p.ID, CaseID: task.CaseID, ArmID: task.ArmID, ConfigDigest: p.ConfigDigest, FixtureDigest: task.FixtureDigest, SourceRevision: "initial", FinalTree: filepath.Join(p.TasksRoot, task.Workspace), FinalResponse: response, Transcripts: []string{}, Limitations: []string{}, Origin: "external-agent"}
	b, _ := jsonBytes(q)
	path := filepath.Join(filepath.Dir(config), "import.json")
	os.WriteFile(path, b, 0600)
	return path
}
func TestImportExternalRun(t *testing.T) {
	config, store, p := prepared(t)
	path := request(t, p, config)
	run, e := (Repository{}).Import(context.Background(), store, path)
	if e != nil {
		t.Fatal(e)
	}
	if run.Observed.Model != nil || len(run.FinalFiles) < 2 {
		t.Fatal("lost data or invented model")
	}
	f, e := (Repository{}).LoadRun(context.Background(), store, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(store, "runs", run.ID, f.Run.FinalResponse.Path))
	if string(b) != "actual external answer" {
		t.Fatal("response altered")
	}
	if _, e = (Repository{}).Import(context.Background(), store, path); e == nil {
		t.Fatal("duplicate import accepted")
	}
}
func TestImportRejects(t *testing.T) {
	for _, field := range []string{"caseId", "sourceRevision", "fixtureDigest", "preparationId", "finalResponse", "schemaVersion"} {
		t.Run(field, func(t *testing.T) {
			config, store, p := prepared(t)
			path := request(t, p, config)
			b, _ := os.ReadFile(path)
			var q map[string]any
			json.Unmarshal(b, &q)
			q[field] = "wrong"
			b, _ = json.Marshal(q)
			os.WriteFile(path, b, 0600)
			if _, e := (Repository{}).Import(context.Background(), store, path); e == nil {
				t.Fatal("accepted mismatch")
			}
			entries, _ := os.ReadDir(filepath.Join(store, "runs"))
			if len(entries) != 0 {
				t.Fatal("published failed import")
			}
		})
	}
}
func TestReadRunTampering(t *testing.T) {
	config, store, p := prepared(t)
	run, e := (Repository{}).Import(context.Background(), store, request(t, p, config))
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(store, "runs", run.ID, "response.txt"), []byte("tampered"), 0600)
	if _, e = (Repository{}).LoadRun(context.Background(), store, run.ID); e == nil {
		t.Fatal("accepted tampered evidence")
	}
}
func TestImportKeepsForbiddenEdit(t *testing.T) {
	config, store, p := prepared(t)
	path := request(t, p, config)
	os.WriteFile(filepath.Join(p.TasksRoot, p.Tasks[0].Workspace, "input.txt"), []byte("forbidden edit"), 0600)
	run, e := (Repository{}).Import(context.Background(), store, path)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(store, "runs", run.ID, "tree/input.txt"))
	if string(b) != "forbidden edit" {
		t.Fatal("discarded forbidden edit")
	}
}

func TestPrepareRejectsOracleAliases(t *testing.T) {
	for _, field := range []string{"task", "instruction"} {
		t.Run(field, func(t *testing.T) {
			config, _ := fixture(t)
			b, _ := os.ReadFile(config)
			var x domain.Experiment
			json.Unmarshal(b, &x)
			if field == "task" {
				x.Cases[0].Task = x.Cases[0].Oracle
			} else {
				x.Arms[1].Instructions[0].Path = x.Cases[0].Oracle
			}
			b, _ = jsonBytes(x)
			os.WriteFile(config, b, 0600)
			if _, e := (Repository{}).Load(context.Background(), config); e == nil {
				t.Fatal("oracle alias accepted")
			}
		})
	}
}
func TestPrepareSharedFixture(t *testing.T) {
	config, tasks := fixture(t)
	b, _ := os.ReadFile(config)
	var x domain.Experiment
	json.Unmarshal(b, &x)
	c := x.Cases[0]
	c.ID = "second"
	x.Cases = append(x.Cases, c)
	b, _ = jsonBytes(x)
	os.WriteFile(config, b, 0600)
	p, e := (Repository{}).Load(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	prep, e := (Repository{}).Prepare(context.Background(), p, tasks)
	if e != nil || len(prep.Tasks) != 4 {
		t.Fatalf("shared fixture: %v", e)
	}
}
func TestPrepareSecondArmFailure(t *testing.T) {
	config, tasks := fixture(t)
	r := Repository{beforeCopy: func(arm string) error {
		if arm == "specialist" {
			return os.ErrPermission
		}
		return nil
	}}
	p, e := r.Load(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.Prepare(context.Background(), p, tasks); e == nil {
		t.Fatal("ignored injected failure")
	}
	if _, e = os.Lstat(tasks); !os.IsNotExist(e) {
		t.Fatal("task rollback failed")
	}
	if _, e = os.Lstat(p.OutputRoot); !os.IsNotExist(e) {
		t.Fatal("private store rollback failed")
	}
}
func TestImportPrivateAndLinkedInputs(t *testing.T) {
	for _, kind := range []string{"oracle", "symlink", "missing-field", "null-array", "corrupt-preparation"} {
		t.Run(kind, func(t *testing.T) {
			config, store, p := prepared(t)
			path := request(t, p, config)
			b, _ := os.ReadFile(path)
			var q map[string]any
			json.Unmarshal(b, &q)
			switch kind {
			case "oracle":
				q["finalResponse"] = filepath.Join(filepath.Dir(config), "reviewer/oracle.json")
			case "symlink":
				link := filepath.Join(p.TasksRoot, "linked-response")
				os.Symlink(q["finalResponse"].(string), link)
				q["finalResponse"] = link
			case "missing-field":
				delete(q, "observed")
			case "null-array":
				q["transcripts"] = nil
			case "corrupt-preparation":
				os.WriteFile(filepath.Join(store, "preparations", p.ID, "preparation.json"), []byte("{}"), 0600)
			}
			b, _ = json.Marshal(q)
			os.WriteFile(path, b, 0600)
			if _, e := (Repository{}).Import(context.Background(), store, path); e == nil {
				t.Fatal("accepted unsafe request")
			}
		})
	}
}
func TestImportUnknownMetadata(t *testing.T) {
	config, store, p := prepared(t)
	run, e := (Repository{}).Import(context.Background(), store, request(t, p, config))
	if e != nil {
		t.Fatal(e)
	}
	if run.Observed.Model != nil || run.Observed.Host != nil || run.Observed.LoadedInstructions != nil {
		t.Fatal("invented metadata")
	}
}

func TestPrepareSourceChangedDuringCopy(t *testing.T) {
	config, tasks := fixture(t)
	r := Repository{beforeCopy: func(arm string) error {
		if arm == "specialist" {
			return os.WriteFile(filepath.Join(filepath.Dir(config), "public/input.txt"), []byte("changed during prepare"), 0600)
		}
		return nil
	}}
	p, e := r.Load(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.Prepare(context.Background(), p, tasks); e == nil {
		t.Fatal("source mutation accepted")
	}
	if _, e = os.Stat(tasks); !os.IsNotExist(e) {
		t.Fatal("tasks retained after mutation")
	}
}
func TestPreparePublicationFailure(t *testing.T) {
	config, tasks := fixture(t)
	r := Repository{beforeCommit: func() error { return os.ErrPermission }}
	p, e := r.Load(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.Prepare(context.Background(), p, tasks); e == nil {
		t.Fatal("publication failure ignored")
	}
	if _, e = os.Stat(tasks); !os.IsNotExist(e) {
		t.Fatal("workspaces not rolled back")
	}
	entries, _ := os.ReadDir(filepath.Join(p.OutputRoot, "preparations"))
	if len(entries) != 0 {
		t.Fatal("partial preparation retained")
	}
}
func TestImportNeverExecutesText(t *testing.T) {
	config, store, p := prepared(t)
	path := request(t, p, config)
	sentinel := filepath.Join(p.TasksRoot, "EXECUTED")
	payload := "touch " + sentinel
	os.WriteFile(filepath.Join(p.TasksRoot, "response.txt"), []byte(payload), 0600)
	os.WriteFile(filepath.Join(p.TasksRoot, p.Tasks[0].Workspace, "run-me.sh"), []byte("#!/bin/sh\n"+payload), 0700)
	if _, e := (Repository{}).Import(context.Background(), store, path); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(sentinel); !os.IsNotExist(e) {
		t.Fatal("import executed content")
	}
}
func TestReadRunStrictSchema(t *testing.T) {
	config, store, p := prepared(t)
	run, e := (Repository{}).Import(context.Background(), store, request(t, p, config))
	if e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(store, "runs", run.ID)
	b, _ := os.ReadFile(filepath.Join(root, "run.json"))
	var obj map[string]any
	json.Unmarshal(b, &obj)
	delete(obj, "origin")
	b, _ = json.Marshal(obj)
	os.WriteFile(filepath.Join(root, "run.json"), b, 0600)
	os.Remove(filepath.Join(root, "COMMITTED"))
	tx := &transaction{root: root}
	if e = tx.seal(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e = (Repository{}).LoadRun(context.Background(), store, run.ID); e == nil {
		t.Fatal("valid checksum bypassed required schema")
	}
}

func TestEmptyFixtureAndFinalTree(t *testing.T) {
	config, tasks := fixture(t)
	os.Remove(filepath.Join(filepath.Dir(config), "public/input.txt"))
	plan, e := (Repository{}).Load(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	p, e := (Repository{}).Prepare(context.Background(), plan, tasks)
	if e != nil {
		t.Fatal(e)
	}
	path := request(t, p, config)
	empty := filepath.Join(tasks, "empty")
	os.Mkdir(empty, 0700)
	b, _ := os.ReadFile(path)
	var q map[string]any
	json.Unmarshal(b, &q)
	q["finalTree"] = empty
	b, _ = json.Marshal(q)
	os.WriteFile(path, b, 0600)
	r, e := (Repository{}).Import(context.Background(), plan.OutputRoot, path)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.FinalFiles) != 0 {
		t.Fatal("empty tree changed")
	}
	if _, e = (Repository{}).LoadRun(context.Background(), plan.OutputRoot, r.ID); e != nil {
		t.Fatal(e)
	}
}

func TestPrepareTaskInsideFixture(t *testing.T) {
	config, tasks := fixture(t)
	b, _ := os.ReadFile(config)
	var x domain.Experiment
	json.Unmarshal(b, &x)
	x.Cases[0].Task = "public/requirements.txt"
	b, _ = jsonBytes(x)
	os.WriteFile(config, b, 0600)
	os.WriteFile(filepath.Join(filepath.Dir(config), "public/requirements.txt"), []byte("Public task"), 0600)
	p, e := (Repository{}).Load(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = (Repository{}).Prepare(context.Background(), p, tasks); e != nil {
		t.Fatal(e)
	}
}
func TestPrepareOracleSentinel(t *testing.T) {
	config, tasks := fixture(t)
	os.WriteFile(filepath.Join(filepath.Dir(config), "reviewer/oracle.json"), []byte(`{"schemaVersion":"evaris.oracle/v1alpha1","criteria":[{"id":"secret","kind":"exact","required":true,"path":"result.txt","expected":"ORACLE_SECRET_42"}]}`), 0600)
	p, e := (Repository{}).Load(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	prep, e := (Repository{}).Prepare(context.Background(), p, tasks)
	if e != nil {
		t.Fatal(e)
	}
	for _, task := range prep.Tasks {
		root := filepath.Join(tasks, task.Workspace)
		m, e := Snapshot(context.Background(), root)
		if e != nil {
			t.Fatal(e)
		}
		for _, f := range m {
			b, _ := os.ReadFile(filepath.Join(root, f.Path))
			if bytes.Contains(b, []byte("ORACLE_SECRET_42")) {
				t.Fatal("leaked reviewer sentinel")
			}
		}
	}
}
