package invaris

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/akkaraponph/evaris/internal/application"
	"github.com/akkaraponph/evaris/internal/domain"
)

var campaignVersions = []string{"invaris.evidence/v1alpha4", "invaris.evidence/v1alpha5"}

type campaignPolicy struct {
	NoSurvivors    bool     `json:"requireNoSurvivors"`
	AllProven      bool     `json:"requireAllGuardrailsProven"`
	NoInconclusive bool     `json:"requireNoInconclusive"`
	NoMixed        bool     `json:"requireNoMixed"`
	NoInvalid      bool     `json:"requireNoInvalid"`
	MinScore       *float64 `json:"minMutationScore"`
}

type campaignExperiment struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Harness   string  `json:"harness"`
	Digest    string  `json:"harnessDigest"`
	Reference string  `json:"referenceId"`
	Repeat    int     `json:"repeat"`
	Selection string  `json:"selection"`
	Stability string  `json:"stability"`
	Verdict   *string `json:"aggregateVerdict"`
	Attempts  []struct {
		ID        string  `json:"id"`
		Index     int     `json:"index"`
		Execution string  `json:"execution"`
		Verdict   *string `json:"verdict"`
		Mutation  *struct {
			ID      string `json:"id"`
			Verdict string `json:"verdict"`
		} `json:"mutation"`
		Proof *struct {
			ID       string `json:"id"`
			Status   string `json:"status"`
			Strength string `json:"evidenceStrength"`
		} `json:"proof"`
	} `json:"attempts"`
}

func campaignInputs(harnesses []application.Blob) ([]string, map[string]string) {
	names := make([]string, 0, len(harnesses))
	digests := make(map[string]string, len(harnesses))
	for _, h := range harnesses {
		names = append(names, h.Path)
		digests[h.Path] = fmt.Sprintf("sha256:%x", sha256.Sum256(h.Data))
	}
	return names, digests
}

// ValidateCampaign pins the entire selected campaign to the reviewer's inputs.
// Only decision-bearing fields of the report are decoded here. Full native
// evidence, including child reports, is preserved for offline inspection.
func ValidateCampaign(b []byte, exit *int, config []byte, harnesses []application.Blob) (Outcome, error) {
	var o Outcome
	if len(b) > maxEvidence {
		return o, fail("LIMIT_EXCEEDED", "evidence")
	}
	names, digests := campaignInputs(harnesses)
	entries, err := domain.CampaignEntries(config, names)
	if err != nil {
		return o, err
	}
	var cfg struct {
		Policy *campaignPolicy `json:"acceptance"`
	}
	if json.Unmarshal(config, &cfg) != nil || cfg.Policy == nil {
		return o, fail("INVALID_CONFIG", "campaign acceptance policy")
	}
	var r struct {
		SchemaVersion string `json:"schemaVersion"`
		RunID         string `json:"runId"`
		Action        string `json:"action"`
		ConfigDigest  string `json:"campaignConfigDigest"`
		Decision      string `json:"decision"`
		Selection     struct {
			Mode     string   `json:"mode"`
			Only     []string `json:"only"`
			Selected int      `json:"selected"`
			Filtered int      `json:"filtered"`
		} `json:"selection"`
		Stages []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"stages"`
		Experiments []campaignExperiment `json:"experiments"`
	}
	if err := decodeJSON(b, &r, false); err != nil {
		return o, err
	}
	if !slices.Contains(campaignVersions, r.SchemaVersion) {
		return o, fail("UNSUPPORTED_SCHEMA", "campaign "+r.SchemaVersion)
	}
	if r.Action != "campaign" || !runIDPattern.MatchString(r.RunID) ||
		r.ConfigDigest != fmt.Sprintf("sha256:%x", sha256.Sum256(config)) {
		return o, fail("EVIDENCE_CORRUPT", "campaign identity")
	}
	if r.Selection.Mode != "all" || r.Selection.Only == nil || len(r.Selection.Only) != 0 || r.Selection.Filtered != 0 ||
		r.Selection.Selected != len(entries) || len(r.Experiments) != len(entries) {
		return o, fail("EVIDENCE_CORRUPT", "campaign selection")
	}
	if len(r.Stages) < 1 || len(r.Stages) > 2 || r.Stages[0].Name != "snapshot" ||
		(len(r.Stages) == 2 && r.Stages[1].Name != "cleanup") {
		return o, fail("EVIDENCE_CORRUPT", "campaign stages")
	}
	for _, stage := range r.Stages {
		if stage.Status != "OK" && stage.Status != "FAILED" {
			return o, fail("EVIDENCE_CORRUPT", "campaign stage status")
		}
	}
	complete := len(r.Stages) == 2 && r.Stages[0].Status == "OK" && r.Stages[1].Status == "OK"
	results := make([]domain.CampaignResult, 0, len(entries))
	for i, e := range entries {
		x := r.Experiments[i]
		if x.ID != e.ID || x.Kind != e.Kind || x.Harness != e.Harness || x.Digest != digests[e.Harness] ||
			x.Reference != e.Reference() || x.Repeat != e.Repeat || x.Selection != "SELECTED" || len(x.Attempts) != e.Repeat {
			return o, fail("EVIDENCE_CORRUPT", "campaign experiment "+e.ID)
		}
		result, executed, err := x.result(r.Stages[0].Status == "OK")
		if err != nil {
			return o, err
		}
		complete = complete && executed
		results = append(results, result)
	}
	want := map[string]int{"ACCEPT": 0, "REJECT": 1, "REFUSE": 2}
	code, ok := want[r.Decision]
	if !ok || exit == nil || *exit != code || r.Decision != cfg.Policy.decision(results, complete) {
		return o, fail("EVIDENCE_CORRUPT", "campaign decision and exit code")
	}
	return Outcome{Decision: r.Decision, SchemaVersion: r.SchemaVersion, RunID: r.RunID,
		ConfigDigest: r.ConfigDigest, Findings: []domain.Finding{}, Campaign: results}, nil
}

func (x campaignExperiment) result(snapshotOK bool) (domain.CampaignResult, bool, error) {
	r := domain.CampaignResult{ID: x.ID, Kind: x.Kind, Stability: "CONSISTENT"}
	executed, inconclusive := true, false
	for i, a := range x.Attempts {
		if a.ID != fmt.Sprintf("%s#%d", x.ID, i+1) || a.Index != i+1 ||
			(!snapshotOK && a.Execution != "NOT_STARTED") {
			return r, false, fail("EVIDENCE_CORRUPT", "campaign attempt "+x.ID)
		}
		switch a.Execution {
		case "EXECUTED", "CANCELED":
			if a.Verdict == nil || (*a.Verdict != "INCONCLUSIVE" &&
				(a.Execution == "CANCELED" || !x.conclusive(*a.Verdict))) {
				return r, false, fail("EVIDENCE_CORRUPT", "campaign attempt verdict "+x.ID)
			}
			if x.Kind == "mutation" {
				if a.Mutation == nil || a.Proof != nil || a.Mutation.ID != x.Reference || a.Mutation.Verdict != *a.Verdict {
					return r, false, fail("EVIDENCE_CORRUPT", "campaign mutation evidence "+x.ID)
				}
			} else if a.Proof == nil || a.Mutation != nil || a.Proof.ID != x.Reference || a.Proof.Status != *a.Verdict ||
				(a.Proof.Status == "PROVEN" && a.Proof.Strength != "strong") {
				return r, false, fail("EVIDENCE_CORRUPT", "campaign proof evidence "+x.ID)
			}
		case "NOT_STARTED", "REFUSED":
			if a.Verdict != nil || a.Mutation != nil || a.Proof != nil {
				return r, false, fail("EVIDENCE_CORRUPT", "campaign unexecuted verdict "+x.ID)
			}
		default:
			return r, false, fail("EVIDENCE_CORRUPT", "campaign attempt execution "+x.ID)
		}
		executed = executed && a.Execution == "EXECUTED"
		if a.Verdict == nil || *a.Verdict == "INCONCLUSIVE" {
			inconclusive = true
		} else if r.Verdict == "" {
			r.Verdict = *a.Verdict
		} else if *a.Verdict != r.Verdict {
			r.Stability = "MIXED"
		}
	}
	if inconclusive {
		r.Stability = "INCOMPLETE"
	}
	if r.Stability != "CONSISTENT" {
		r.Verdict = ""
	}
	if r.Stability != x.Stability || (x.Verdict == nil && r.Verdict != "") ||
		(x.Verdict != nil && (*x.Verdict == "" || *x.Verdict != r.Verdict)) {
		return r, false, fail("EVIDENCE_CORRUPT", "campaign aggregate "+x.ID)
	}
	return r, executed, nil
}

func (x campaignExperiment) conclusive(verdict string) bool {
	if x.Kind == "mutation" {
		return verdict == "KILLED" || verdict == "SURVIVED" || verdict == "INVALID"
	}
	return verdict == "PROVEN" || verdict == "UNPROVEN"
}

// decision follows Invaris's refusal-first acceptance semantics.
func (p campaignPolicy) decision(results []domain.CampaignResult, complete bool) string {
	if !complete {
		return "REFUSE"
	}
	var mutations, guards, killed, survived int
	var violated, unsupported, unknownMutation, unknownGuard bool
	for _, r := range results {
		unknown := r.Stability != "CONSISTENT"
		unsupported = unsupported || p.NoInconclusive && r.Stability == "INCOMPLETE" || p.NoMixed && r.Stability == "MIXED"
		if r.Kind == "mutation" {
			mutations++
			unknownMutation = unknownMutation || unknown
			switch r.Verdict {
			case "KILLED":
				killed++
			case "SURVIVED":
				survived++
			}
		} else {
			guards++
			unknownGuard = unknownGuard || unknown
		}
	}
	// Each enabled prohibition considers its own violations before unknowns.
	for _, rule := range []struct {
		enabled, unknown bool
		bad              string
	}{{p.NoSurvivors, unknownMutation, "SURVIVED"}, {p.NoInvalid, unknownMutation, "INVALID"}, {p.AllProven, unknownGuard, "UNPROVEN"}} {
		if rule.enabled {
			bad := slices.ContainsFunc(results, func(r domain.CampaignResult) bool { return r.Verdict == rule.bad })
			violated = violated || bad
			unsupported = unsupported || !bad && rule.unknown
		}
	}
	if p.MinScore != nil && mutations > 0 {
		eligible := killed + survived
		unsupported = unsupported || eligible == 0
		violated = violated || eligible > 0 && float64(killed)/float64(eligible) < *p.MinScore
	}
	switch {
	case unsupported:
		return "REFUSE"
	case violated:
		return "REJECT"
	case p.NoInconclusive || p.NoMixed || mutations > 0 && (p.NoSurvivors || p.NoInvalid || p.MinScore != nil) || guards > 0 && p.AllProven:
		return "ACCEPT"
	default:
		return "REFUSE"
	}
}
