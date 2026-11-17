package fsrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func loadPreparation(ctx context.Context, store, id string) (p domain.Preparation, err error) {
	if !recordID.MatchString(id) {
		return p, fail("EVIDENCE_CORRUPT", "preparation ID")
	}
	err = readRecord(ctx, filepath.Join(store, "preparations", id), "preparation.json", &p)
	if err == nil && (p.ID != id || p.SchemaVersion != "evaris.preparation/v1alpha1") {
		err = fail("EVIDENCE_CORRUPT", id)
	}
	if err == nil {
		err = validatePreparation(p)
	}
	return
}
func loadStore(ctx context.Context, store string) (application.Plan, error) {
	if _, e := rootPath(store); e != nil {
		return application.Plan{}, e
	}
	if _, e := readFile(filepath.Join(store, "COMMITTED")); e != nil {
		return application.Plan{}, fail("EVIDENCE_CORRUPT", store)
	}
	b, e := readFile(filepath.Join(store, "experiment.json"))
	if e != nil {
		return application.Plan{}, e
	}
	frozen := inputRoot(store, digest(b))
	if e = verifyRecord(ctx, frozen); e != nil {
		return application.Plan{}, e
	}
	return (Repository{}).Load(ctx, filepath.Join(frozen, "experiment.json"))
}
func parseImport(b []byte) (q domain.ImportRequest, err error) {
	// The import format alone permits nullable observed fields and diff. Validate its
	// shape with RawMessages, then strict-decode the exact typed host metadata.
	type wire struct {
		SchemaVersion     string              `json:"schemaVersion"`
		PreparationID     string              `json:"preparationId"`
		CaseID            string              `json:"caseId"`
		ArmID             string              `json:"armId"`
		ConfigDigest      domain.Digest       `json:"configDigest"`
		FixtureDigest     domain.Digest       `json:"fixtureDigest"`
		SourceRevision    string              `json:"sourceRevision"`
		FinalResponse     string              `json:"finalResponse"`
		FinalTree         string              `json:"finalTree,omitempty"`
		FinalSource       *domain.FinalSource `json:"finalSource,omitempty"`
		DeliveredRevision *string             `json:"deliveredRevision,omitempty"`
		Transcripts       []string            `json:"transcripts"`
		Diff              json.RawMessage     `json:"diff"`
		Observed          json.RawMessage     `json:"observed"`
		Limitations       []string            `json:"limitations"`
		Origin            string              `json:"origin"`
	}
	// decode's nullable exceptions are limited to these explicit wire fields.
	var w wire
	if err = decode(b, &w); err != nil {
		return
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	err = d.Decode(&q)
	if err != nil {
		return
	}
	var observed map[string]json.RawMessage
	if err = json.Unmarshal(w.Observed, &observed); err != nil {
		return
	}
	names := []string{"host", "model", "modelSettings", "tools", "permissions", "budget", "contextId", "freshContext", "startedAt", "finishedAt", "loadedInstructions", "agentClaim"}
	if len(observed) != len(names) {
		return q, fail("INVALID_CONFIG", "observed fields")
	}
	for _, n := range names {
		if _, ok := observed[n]; !ok {
			return q, fail("INVALID_CONFIG", n)
		}
	}
	for _, s := range []*string{q.Observed.Host, q.Observed.Model, q.Observed.Permissions, q.Observed.Budget, q.Observed.ContextID, q.Observed.AgentClaim} {
		if s != nil && strings.TrimSpace(*s) == "" {
			return q, fail("INVALID_CONFIG", "empty metadata")
		}
	}
	o := q.Observed
	if o.StartedAt != nil && o.FinishedAt != nil && o.FinishedAt.Before(*o.StartedAt) {
		return q, fail("INVALID_CONFIG", "metadata times")
	}
	if o.AgentClaim != nil && *o.AgentClaim != "completed" && *o.AgentClaim != "not-completed" && *o.AgentClaim != "unknown" {
		return q, fail("INVALID_CONFIG", `agentClaim must be "completed", "not-completed" or "unknown"`)
	}
	return
}
func (r Repository) Import(ctx context.Context, store, requestPath string) (out domain.Run, err error) {
	p, err := loadStore(ctx, store)
	if err != nil {
		return out, err
	}
	b, err := readFile(requestPath)
	if err != nil {
		return out, err
	}
	q, err := parseImport(b)
	if err != nil {
		return out, err
	}
	if q.SchemaVersion != "evaris.import/v1alpha1" {
		return out, fail("UNSUPPORTED_SCHEMA", "import")
	}
	if q.Origin != "external-agent" && q.Origin != "synthetic-control" {
		return out, fail("INVALID_CONFIG", "origin")
	}
	prep, err := loadPreparation(ctx, store, q.PreparationID)
	if err != nil {
		return out, err
	}
	var task *domain.PreparedTask
	for i := range prep.Tasks {
		t := &prep.Tasks[i]
		if t.CaseID == q.CaseID && t.ArmID == q.ArmID {
			task = t
		}
	}
	if task == nil {
		return out, fail("UNKNOWN_CASE", q.CaseID)
	}
	var fc application.FrozenCase
	for _, c := range p.Cases {
		if c.Case.ID == q.CaseID {
			fc = c
		}
	}
	switch {
	case q.ConfigDigest != p.ConfigDigest || prep.ConfigDigest != q.ConfigDigest:
		return out, fail("REVISION_MISMATCH", "import identity: configDigest must equal the prepared configuration digest "+string(prep.ConfigDigest))
	case q.FixtureDigest != task.FixtureDigest:
		return out, fail("REVISION_MISMATCH", "import identity: fixtureDigest must equal the prepared fixture digest "+string(task.FixtureDigest))
	case q.SourceRevision != fc.Case.SourceRevision:
		return out, fail("REVISION_MISMATCH", "import identity: sourceRevision must be the case's starting revision "+fc.Case.SourceRevision+", not the delivered commit")
	}
	var privatePaths []string
	privateBytes, e := readFile(filepath.Join(p.ConfigDir, ".evaris-private-sources.json"))
	if e != nil {
		return out, e
	}
	if e = json.Unmarshal(privateBytes, &privatePaths); e != nil {
		return out, fail("EVIDENCE_CORRUPT", "private source references")
	}
	base, e := filepath.Abs(filepath.Dir(requestPath))
	if e != nil {
		return out, e
	}
	resolve := func(s string) (string, error) {
		if strings.TrimSpace(s) == "" {
			return "", fail("OUTPUT_MISSING", "input path")
		}
		if !filepath.IsAbs(s) {
			s = filepath.Join(base, s)
		}
		s = filepath.Clean(s)
		for _, private := range append([]string{store, p.ConfigDir}, privatePaths...) {
			if within(s, private) || within(private, s) {
				return "", fail("PATH_UNSAFE", s)
			}
		}
		return s, nil
	}
	if (q.FinalTree == "") == (q.FinalSource == nil) {
		return out, fail("INVALID_CONFIG", "exactly one of finalTree and finalSource is required")
	}
	if d := q.DeliveredRevision; d != nil && (strings.TrimSpace(*d) != *d || *d == "" || len(*d) > 128 || strings.ContainsAny(*d, "\x00\n\t ")) {
		return out, fail("INVALID_CONFIG", "deliveredRevision")
	}
	var tree string
	var gitRef *domain.FileRef
	delivered := q.DeliveredRevision
	if src := q.FinalSource; src != nil {
		if src.Kind != "git" {
			return out, fail("INVALID_CONFIG", `finalSource.kind must be "git"`)
		}
		repo, e := resolve(src.Repository)
		if e != nil {
			return out, e
		}
		if tree, e = os.MkdirTemp("", "evaris-git-"); e != nil {
			return out, e
		}
		defer os.RemoveAll(tree)
		// Snapshots refuse symbolic links in the path (macOS /var is one).
		if tree, e = filepath.EvalSymlinks(tree); e != nil {
			return out, e
		}
		sha, ref, e := exportGitTree(ctx, repo, src.Revision, tree, p.Experiment.MaxEntries())
		if e != nil {
			return out, e
		}
		if delivered != nil && *delivered != sha {
			return out, fail("REVISION_MISMATCH", "deliveredRevision "+*delivered+" does not match the resolved commit "+sha)
		}
		if e = overlayGenerated(prep.TasksRoot, *task, tree); e != nil {
			return out, e
		}
		delivered, gitRef = &sha, &ref
	} else if tree, err = resolve(q.FinalTree); err != nil {
		return out, err
	}
	m, err := SnapshotWith(ctx, tree, SnapshotOptions{MaxEntries: p.Experiment.MaxEntries()})
	if err != nil {
		return out, err
	}
	response, err := resolve(q.FinalResponse)
	if err != nil {
		return out, err
	}
	rb, err := readFile(response)
	if err != nil || len(strings.TrimSpace(string(rb))) == 0 {
		return out, fail("OUTPUT_MISSING", "final response")
	}
	slot, err := reserve(filepath.Join(store, "slots", q.PreparationID+"-"+q.CaseID+"-"+q.ArmID))
	if err != nil {
		return out, err
	}
	defer cleanup(slot, &err)
	id, err := newID()
	if err != nil {
		return out, err
	}
	tx, err := reserve(filepath.Join(store, "runs", id))
	if err != nil {
		return out, err
	}
	defer cleanup(tx, &err)
	if err = copyManifest(ctx, tree, tx, "tree", m); err != nil {
		return out, err
	}
	final := domain.Manifest{}
	for _, f := range m {
		f.Path = "tree/" + f.Path
		final = append(final, f)
	}
	schema := domain.RunSchemaV1
	if delivered != nil {
		schema = domain.RunSchemaV2
	}
	out = domain.Run{SchemaVersion: schema, DeliveredRevision: delivered, FinalSource: q.FinalSource, GitExecutable: gitRef, ID: id, PreparationID: prep.ID, ExperimentID: p.Experiment.ID, CaseID: q.CaseID, ArmID: q.ArmID, ConfigDigest: q.ConfigDigest, FixtureDigest: q.FixtureDigest, TaskDigest: task.TaskDigest, PromptDigest: task.PromptDigest, OracleDigest: task.OracleDigest, Instructions: task.Instructions, SourceRevision: q.SourceRevision, ImportedAt: time.Now().UTC(), FinalFiles: final, Transcripts: []domain.FileRef{}, Observed: q.Observed, Origin: q.Origin, Limitations: q.Limitations}
	put := func(name string, b []byte) (domain.FileRef, error) {
		e := tx.write(name, b)
		return domain.FileRef{Path: name, SHA256: digest(b), Bytes: int64(len(b))}, e
	}
	out.FinalResponse, err = put("response.txt", rb)
	if err != nil {
		return out, err
	}
	for i, path := range q.Transcripts {
		src, e := resolve(path)
		if e != nil {
			return out, e
		}
		data, e := readFile(src)
		if e != nil {
			return out, e
		}
		ref, e := put("transcripts/"+stringID(i)+".txt", data)
		if e != nil {
			return out, e
		}
		out.Transcripts = append(out.Transcripts, ref)
	}
	if q.Diff != nil {
		src, e := resolve(*q.Diff)
		if e != nil {
			return out, e
		}
		data, e := readFile(src)
		if e != nil {
			return out, e
		}
		ref, e := put("diff.txt", data)
		if e != nil {
			return out, e
		}
		out.Diff = &ref
	}
	if err = tx.writeJSON("run.json", out); err != nil {
		return out, err
	}
	if err = slot.write("run-id", []byte(id)); err != nil {
		return out, err
	}
	if err = tx.seal(ctx); err != nil {
		return out, err
	}
	slot.committed = true
	return out, nil
}
func stringID(i int) string { return fmt.Sprintf("%03d", i) }
func (r Repository) LoadRun(ctx context.Context, store, id string) (out application.FrozenRun, err error) {
	if !recordID.MatchString(id) {
		return out, fail("EVIDENCE_CORRUPT", "run ID")
	}
	p, err := loadStore(ctx, store)
	if err != nil {
		return out, err
	}
	root := filepath.Join(store, "runs", id)
	if err = readRecord(ctx, root, "run.json", &out.Run); err != nil {
		return out, err
	}
	// v1alpha1 runs carry none of the v1alpha2 fields; v1alpha2 runs carry a
	// delivered revision, and a git source only with its executable.
	v1 := out.Run.SchemaVersion == domain.RunSchemaV1 && out.Run.DeliveredRevision == nil && out.Run.FinalSource == nil && out.Run.GitExecutable == nil
	v2 := out.Run.SchemaVersion == domain.RunSchemaV2 && out.Run.DeliveredRevision != nil && (out.Run.FinalSource == nil) == (out.Run.GitExecutable == nil)
	if out.Run.ID != id || !v1 && !v2 || out.Run.ConfigDigest != p.ConfigDigest {
		return out, fail("EVIDENCE_CORRUPT", id)
	}
	out.MaxEntries = p.Experiment.MaxEntries()
	out.Preparation, err = loadPreparation(ctx, store, out.Run.PreparationID)
	if err != nil {
		return out, err
	}
	for _, c := range p.Cases {
		if c.Case.ID == out.Run.CaseID {
			out.Case = c
		}
	}
	if out.Case.Case.ID == "" {
		return out, fail("UNKNOWN_CASE", out.Run.CaseID)
	}
	matched := false
	for _, task := range out.Preparation.Tasks {
		if task.CaseID == out.Run.CaseID && task.ArmID == out.Run.ArmID {
			matched = true
			if task.FixtureDigest != out.Run.FixtureDigest || task.TaskDigest != out.Run.TaskDigest || task.PromptDigest != out.Run.PromptDigest || task.OracleDigest != out.Run.OracleDigest {
				return out, fail("EVIDENCE_CORRUPT", "run preparation identity")
			}
		}
	}
	if !matched || out.Run.SourceRevision != out.Case.Case.SourceRevision || out.Run.ExperimentID != p.Experiment.ID {
		return out, fail("EVIDENCE_CORRUPT", "run identity")
	}
	b, err := readFile(filepath.Join(root, "run.json"))
	if err != nil {
		return out, err
	}
	if err = validateRun(ctx, root, out.Run, out.Preparation); err != nil {
		return out, err
	}
	out.RunDigest = digest(b)
	out.SnapshotRoot = filepath.Join(root, "tree")
	return out, nil
}
