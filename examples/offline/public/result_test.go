package control

import (
	"os"
	"strings"
	"testing"
)

// Deliberately incomplete ordinary test: it only checks a nonempty summary.
func TestSummaryPresent(t *testing.T) {
	b, e := os.ReadFile("result.txt")
	if e != nil || strings.TrimSpace(string(b)) == "" {
		t.Fatal("missing summary")
	}
}
