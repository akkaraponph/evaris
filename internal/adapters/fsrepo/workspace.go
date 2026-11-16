package fsrepo

import (
	"context"
	"path/filepath"
)

// Init reserves a private workspace exclusively. A collision never overwrites
// user configuration, including when the existing path is a dangling symlink.
func (r Repository) Init(ctx context.Context, root string) (config string, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return
	}
	tx, err := reserve(root)
	if err != nil {
		return "", err
	}
	defer cleanup(tx, &err)
	if err = ensureDirs(root, "fixtures", "instructions", "reviewer", "tasks"); err != nil {
		return
	}
	// Evidence is initialized by Prepare: an empty directory is not a valid store.
	files := []struct{ path, text string }{
		{"experiment.json", starterExperiment},
		{"fixtures/sample/README.txt", "Sample fixture. Write result.txt as requested in TASK.txt.\n"},
		{"task.txt", "Create result.txt containing exactly hello followed by a newline.\n"},
		{"instructions/specialist.txt", "Check the requested filename and exact content before finishing.\n"},
		{"reviewer/oracle.json", `{
  "schemaVersion": "evaris.oracle/v1alpha1",
  "criteria": [
    {"id": "result", "kind": "exact", "required": true, "path": "result.txt", "expected": "hello\n"}
  ]
}
`},
	}
	for _, file := range files {
		if err = ctx.Err(); err != nil {
			return
		}
		if err = tx.write(file.path, []byte(file.text)); err != nil {
			return
		}
	}
	config = filepath.Join(root, "experiment.json")
	if _, err = r.Load(ctx, config); err != nil {
		return
	}
	if err = ctx.Err(); err != nil {
		return
	}
	// This is editable configuration, not an immutable evidence record.
	tx.committed = true
	return
}

const starterExperiment = `{
  "schemaVersion": "evaris.experiment/v1alpha1",
  "id": "example",
  "revision": "initial",
  "cases": [
    {
      "id": "sample",
      "fixture": "fixtures/sample",
      "task": "task.txt",
      "oracle": "reviewer/oracle.json",
      "sourceRevision": "initial",
      "allowedChanges": ["result.txt"],
      "requiresReview": false
    }
  ],
  "arms": [
    {"id": "baseline", "instructions": []},
    {"id": "specialist", "instructions": [{"path": "instructions/specialist.txt", "revision": "initial"}]}
  ],
  "outputRoot": "evidence",
  "constraints": {"tools": "local", "permissions": "workspace", "budget": "manual"}
}
`
