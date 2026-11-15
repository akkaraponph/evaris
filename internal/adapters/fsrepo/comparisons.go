package fsrepo

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/akkaraponph/evaris/internal/domain"
)

// validateComparison checks the record's own invariants: no transitions are
// stored for a non-comparable result, and every pair names listed evaluations.
func validateComparison(c domain.Comparison) error {
	bad := fail("EVIDENCE_CORRUPT", "comparison")
	if c.SchemaVersion != "evaris.comparison/v1alpha1" || !recordID.MatchString(c.ID) || c.CreatedAt.IsZero() || c.Mode != "arms" && c.Mode != "revisions" {
		return bad
	}
	if c.LeftEvaluationIDs == nil || c.RightEvaluationIDs == nil || c.Reasons == nil || c.Pairs == nil || c.Limitations == nil {
		return bad
	}
	if c.Comparable != (len(c.Reasons) == 0) || !c.Comparable && len(c.Pairs) != 0 {
		return bad
	}
	left, right := map[string]bool{}, map[string]bool{}
	for _, id := range c.LeftEvaluationIDs {
		if !recordID.MatchString(id) || left[id] {
			return bad
		}
		left[id] = true
	}
	for _, id := range c.RightEvaluationIDs {
		if !recordID.MatchString(id) || right[id] {
			return bad
		}
		right[id] = true
	}
	for _, p := range c.Pairs {
		if !left[p.LeftID] || !right[p.RightID] || !domain.Transitions[p.Transition] || p.CaseID == "" || p.Dimension == "" {
			return bad
		}
	}
	return nil
}

// SaveComparison publishes a new immutable comparison record in the store.
func (r Repository) SaveComparison(ctx context.Context, store string, c domain.Comparison) (err error) {
	if err = validateComparison(c); err != nil {
		return fail("INVALID_CONFIG", "comparison")
	}
	if _, err = loadStore(ctx, store); err != nil {
		return
	}
	if err = ensureDirs(store, "comparisons"); err != nil {
		return
	}
	tx, err := reserve(filepath.Join(store, "comparisons", c.ID))
	if err != nil {
		return
	}
	defer cleanup(tx, &err)
	if err = tx.writeJSON("comparison.json", c); err != nil {
		return
	}
	return tx.seal(ctx)
}

// LoadComparisons returns committed, validated comparisons sorted by creation.
// Left evaluation IDs (and right ones in arms mode) must exist in this store;
// revisions-mode right IDs belong to another store and are not resolved here.
func (r Repository) LoadComparisons(ctx context.Context, store string) ([]domain.Comparison, error) {
	out := []domain.Comparison{}
	entries, e := os.ReadDir(filepath.Join(store, "comparisons"))
	if os.IsNotExist(e) {
		if _, e = loadStore(ctx, store); e != nil {
			return nil, e
		}
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	es, _, e := r.LoadEvaluations(ctx, store)
	if e != nil {
		return nil, e
	}
	known := map[string]bool{}
	for _, v := range es {
		known[v.ID] = true
	}
	for _, d := range entries {
		root := filepath.Join(store, "comparisons", d.Name())
		if _, e := os.Lstat(filepath.Join(root, "COMMITTED")); os.IsNotExist(e) {
			continue
		}
		var c domain.Comparison
		if e = readRecord(ctx, root, "comparison.json", &c); e != nil {
			return nil, e
		}
		if c.ID != d.Name() || validateComparison(c) != nil {
			return nil, fail("EVIDENCE_CORRUPT", "comparison")
		}
		ids := c.LeftEvaluationIDs
		if c.Mode == "arms" {
			ids = append(append([]string{}, ids...), c.RightEvaluationIDs...)
		}
		for _, id := range ids {
			if !known[id] {
				return nil, fail("EVIDENCE_CORRUPT", "comparison evaluation")
			}
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt) || out[i].CreatedAt.Equal(out[j].CreatedAt) && out[i].ID < out[j].ID
	})
	return out, nil
}
