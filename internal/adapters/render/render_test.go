package render

import (
	"bytes"
	"github.com/akkaraponph/evaris/internal/domain"
	"testing"
)

func TestStableRendering(t *testing.T) {
	r := domain.Report{ExperimentID: "<script>[x](secret)", Cases: []domain.CaseSummary{{Execution: "ERROR", AgentOutcome: "VIOLATED", Assessment: "FAIL"}}, Limitations: []string{"<img src=x>"}}
	a, _ := JSON(r)
	b, _ := JSON(r)
	if !bytes.Equal(a, b) {
		t.Fatal("unstable JSON")
	}
	a, _ = Markdown(r)
	b, _ = Markdown(r)
	if !bytes.Equal(a, b) || bytes.Contains(a, []byte("<script>")) || bytes.Contains(a, []byte("[x](secret)")) || !bytes.Contains(a, []byte("ERROR")) || !bytes.Contains(a, []byte("VIOLATED")) {
		t.Fatal("unsafe/incomplete Markdown")
	}
}
