package application

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/akkaraponph/evaris/internal/domain"
)

// latest selects the newest evaluation per run (createdAt, then ID) and counts
// the older ones, which are retained but never treated as independent trials.
func latest(es []domain.Evaluation) (map[string]domain.Evaluation, map[string]int) {
	out, older := map[string]domain.Evaluation{}, map[string]int{}
	for _, v := range es {
		old, ok := out[v.RunID]
		if ok {
			older[v.RunID]++
		}
		if !ok || v.CreatedAt.After(old.CreatedAt) || v.CreatedAt.Equal(old.CreatedAt) && v.ID > old.ID {
			out[v.RunID] = v
		}
	}
	return out, older
}

type compareSide struct {
	evals       []domain.Evaluation
	runs        []domain.Run
	reasons     []string
	limitations []string
}

// load validates one store and selects one evaluation per prepared case/arm.
func (s Service) load(ctx context.Context, store, name string, arms map[string]bool) (compareSide, error) {
	var out compareSide
	es, runs, e := s.Repo.LoadEvaluations(ctx, store)
	if e != nil {
		return out, e
	}
	ps, e := s.Repo.LoadPreparations(ctx, store)
	if e != nil {
		return out, e
	}
	out.runs = runs
	if len(ps) != 1 {
		out.reasons = append(out.reasons, fmt.Sprintf("%s store has %d committed preparations; exactly one is required, multiple trials are not paired", name, len(ps)))
	}
	chosen, older := latest(es)
	runIDs := []string{}
	for id := range older {
		runIDs = append(runIDs, id)
	}
	sort.Strings(runIDs)
	for _, id := range runIDs {
		out.limitations = append(out.limitations, fmt.Sprintf("%s: %d earlier evaluation(s) of run %s are re-gradings of the same output, not independent trials", name, older[id], id))
	}
	for _, p := range ps {
		for _, task := range p.Tasks {
			if !arms[task.ArmID] {
				continue
			}
			found := false
			for _, r := range runs {
				if r.PreparationID == p.ID && r.CaseID == task.CaseID && r.ArmID == task.ArmID {
					if v, ok := chosen[r.ID]; ok {
						out.evals = append(out.evals, v)
						found = true
					}
				}
			}
			if !found {
				out.reasons = append(out.reasons, fmt.Sprintf("%s: %s/%s has no evaluated run; an absent arm is incomplete, not an improvement", name, task.CaseID, task.ArmID))
			}
		}
	}
	return out, nil
}

// Compare loads, validates and compares two stores, then persists the result in
// the left store. A non-comparable result is still persisted with its reasons.
func (s Service) Compare(ctx context.Context, left, right, mode string) (domain.Comparison, error) {
	var leftArms, rightArms map[string]bool
	switch mode {
	case "arms":
		if filepath.Clean(left) != filepath.Clean(right) {
			return domain.Comparison{}, &domain.Failure{Code: "INVALID_CONFIG", Path: "arms mode compares baseline and specialist within one store"}
		}
		leftArms, rightArms = map[string]bool{"baseline": true}, map[string]bool{"specialist": true}
	case "revisions":
		leftArms = map[string]bool{"baseline": true, "specialist": true}
		rightArms = leftArms
	default:
		return domain.Comparison{}, &domain.Failure{Code: "INVALID_CONFIG", Path: "mode"}
	}
	l, e := s.load(ctx, left, "left", leftArms)
	if e != nil {
		return domain.Comparison{}, e
	}
	r, e := s.load(ctx, right, "right", rightArms)
	if e != nil {
		return domain.Comparison{}, e
	}
	c, e := domain.Compare(l.evals, r.evals, append(append([]domain.Run{}, l.runs...), r.runs...), mode)
	if e != nil {
		return domain.Comparison{}, e
	}
	if extra := append(append([]string{}, l.reasons...), r.reasons...); len(extra) > 0 {
		c.Comparable = false
		c.Pairs = []domain.Pair{}
		c.Reasons = append(c.Reasons, extra...)
		sort.Strings(c.Reasons)
	}
	c.Limitations = append(c.Limitations, append(l.limitations, r.limitations...)...)
	if c.ID, e = ID(); e != nil {
		return domain.Comparison{}, e
	}
	c.CreatedAt = time.Now().UTC()
	if e = s.Repo.SaveComparison(ctx, left, c); e != nil {
		return domain.Comparison{}, e
	}
	return c, nil
}
