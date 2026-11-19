package domain

import (
	"encoding/json"
	"path"
)

// CampaignEntry identifies a reviewer-configured Invaris experiment.
type CampaignEntry struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Harness      string `json:"harness"`
	ExperimentID string `json:"experimentId"`
	ChallengeID  string `json:"challengeId"`
	Repeat       int    `json:"repeat"`
}

func (e CampaignEntry) Reference() string {
	if e.Kind == "mutation" {
		return e.ExperimentID
	}
	return e.ChallengeID
}

// CampaignEntries checks that all referenced harnesses are among the private
// frozen files. Invaris still validates and executes the full campaign.
func CampaignEntries(data []byte, harnesses []string) ([]CampaignEntry, error) {
	var c struct {
		Version     string          `json:"version"`
		Experiments []CampaignEntry `json:"experiments"`
	}
	if json.Unmarshal(data, &c) != nil || c.Version != "invaris.campaign/v1alpha1" || len(c.Experiments) == 0 {
		return nil, &Failure{Code: "INVALID_CONFIG", Path: "verification campaign must be invaris.campaign/v1alpha1 with experiments"}
	}
	files := make(map[string]bool, len(harnesses))
	for _, name := range harnesses {
		files[name] = true
	}
	seen := make(map[string]bool, len(c.Experiments))
	for _, e := range c.Experiments {
		if e.ID == "" || seen[e.ID] || e.Repeat < 1 || e.Repeat > 5 ||
			(e.Kind != "mutation" && e.Kind != "guardrail") || e.Reference() == "" {
			return nil, &Failure{Code: "INVALID_CONFIG", Path: "verification campaign experiment " + e.ID}
		}
		if !Relative(e.Harness) || path.Clean(e.Harness) != e.Harness || !files[e.Harness] {
			return nil, &Failure{Code: "INVALID_CONFIG", Path: "verification campaign harness must be listed in files: " + e.Harness}
		}
		seen[e.ID] = true
	}
	return c.Experiments, nil
}
