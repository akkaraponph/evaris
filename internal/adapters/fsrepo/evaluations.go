package fsrepo

import (
	"bytes"
	"context"
	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
	"os"
	"path/filepath"
)

func (r Repository) SaveEvaluation(ctx context.Context, store string, v domain.Evaluation, blobs []application.Blob) (err error) {
	if !recordID.MatchString(v.ID) {
		return fail("INVALID_CONFIG", "evaluation ID")
	}
	f, e := r.LoadRun(ctx, store, v.RunID)
	if e != nil {
		return e
	}
	if err = validateEvaluation(v, f); err != nil {
		return
	}
	if _, err = r.LoadRun(ctx, store, v.RunID); err != nil {
		return
	}
	if err = ensureDirs(store, "evaluations"); err != nil {
		return
	}
	tx, err := reserve(filepath.Join(store, "evaluations", v.ID))
	if err != nil {
		return
	}
	defer cleanup(tx, &err)
	if err = tx.writeJSON("evaluation.json", v); err != nil {
		return
	}
	for _, b := range blobs {
		if err = tx.write(b.Path, b.Data); err != nil {
			return
		}
	}
	for _, ref := range evaluationRefs(v) {
		b, e := readFile(filepath.Join(tx.root, ref.Path))
		if e != nil || digest(b) != ref.SHA256 || int64(len(b)) != ref.Bytes {
			return fail("EVIDENCE_CORRUPT", "evaluation evidence")
		}
	}
	return tx.seal(ctx)
}
func (r Repository) LoadPreparations(ctx context.Context, store string) (out []domain.Preparation, err error) {
	if _, err = loadStore(ctx, store); err != nil {
		return
	}
	out = []domain.Preparation{}
	entries, err := os.ReadDir(filepath.Join(store, "preparations"))
	if err != nil {
		return
	}
	for _, d := range entries {
		if !recordID.MatchString(d.Name()) {
			return nil, fail("EVIDENCE_CORRUPT", "preparation name")
		}
		if _, e := os.Lstat(filepath.Join(store, "preparations", d.Name(), "COMMITTED")); os.IsNotExist(e) {
			continue
		}
		p, e := loadPreparation(ctx, store, d.Name())
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return
}
func (r Repository) LoadEvaluations(ctx context.Context, store string) (es []domain.Evaluation, runs []domain.Run, err error) {
	if _, err = loadStore(ctx, store); err != nil {
		return
	}
	es = []domain.Evaluation{}
	runs = []domain.Run{}
	entries, err := os.ReadDir(filepath.Join(store, "runs"))
	if err != nil {
		return
	}
	for _, d := range entries {
		if _, e := os.Lstat(filepath.Join(store, "runs", d.Name(), "COMMITTED")); os.IsNotExist(e) {
			continue
		}
		f, e := r.LoadRun(ctx, store, d.Name())
		if e != nil {
			return nil, nil, e
		}
		runs = append(runs, f.Run)
	}
	entries, err = os.ReadDir(filepath.Join(store, "evaluations"))
	if os.IsNotExist(err) {
		return es, runs, nil
	}
	if err != nil {
		return
	}
	for _, d := range entries {
		root := filepath.Join(store, "evaluations", d.Name())
		if _, e := os.Lstat(filepath.Join(root, "COMMITTED")); os.IsNotExist(e) {
			continue
		}
		var v domain.Evaluation
		if err = readRecord(ctx, root, "evaluation.json", &v); err != nil {
			return
		}
		f, e := r.LoadRun(ctx, store, v.RunID)
		if e != nil {
			return nil, nil, e
		}
		if err = validateEvaluation(v, f); err != nil {
			return
		}
		for _, ref := range evaluationRefs(v) {
			b, e := readFile(filepath.Join(root, ref.Path))
			if e != nil || digest(b) != ref.SHA256 || int64(len(b)) != ref.Bytes {
				return nil, nil, fail("EVIDENCE_CORRUPT", "evaluation evidence")
			}
		}
		a := domain.Assess(v.Graders, v.Review, v.Verification)
		if v.SchemaVersion != "evaris.evaluation/v1alpha1" || v.ID != d.Name() || v.RunDigest != f.RunDigest || v.OracleDigest != f.Run.OracleDigest || v.Assessment != a.Final || v.Execution != a.Execution || v.AgentOutcome != a.AgentOutcome {
			return nil, nil, fail("EVIDENCE_CORRUPT", "evaluation")
		}
		es = append(es, v)
	}
	return
}
func (r Repository) SaveReport(ctx context.Context, store string, v domain.Report, j, m []byte) (err error) {
	expected, e := jsonBytes(v)
	if e != nil || !bytes.Equal(expected, j) || v.SchemaVersion != "evaris.report/v1alpha1" {
		return fail("INVALID_CONFIG", "report JSON")
	}
	if !recordID.MatchString(v.ID) {
		return fail("INVALID_CONFIG", "report ID")
	}
	if err = ensureDirs(store, "reports"); err != nil {
		return
	}
	tx, err := reserve(filepath.Join(store, "reports", v.ID))
	if err != nil {
		return
	}
	defer cleanup(tx, &err)
	if err = tx.write("report.json", j); err != nil {
		return
	}
	if err = tx.write("report.md", m); err != nil {
		return
	}
	return tx.seal(ctx)
}
