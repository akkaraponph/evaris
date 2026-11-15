package acceptance

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestDependencyDirection(t *testing.T) {
	cmd := exec.Command("go", "list", "-json", "../domain", "../application")
	b, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	for d.More() {
		var p struct {
			ImportPath string
			Imports    []string
		}
		if e = d.Decode(&p); e != nil {
			t.Fatal(e)
		}
		for _, in := range p.Imports {
			if strings.Contains(in, "/adapters/") || strings.HasSuffix(p.ImportPath, "/domain") && (in == "os" || in == "os/exec") {
				t.Fatalf("%s imports %s", p.ImportPath, in)
			}
		}
	}
}
