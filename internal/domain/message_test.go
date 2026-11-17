package domain

import "testing"

func TestEveryCatalogueMessageIsValid(t *testing.T) {
	exits := []*int{nil, new(int), func() *int { x := 255; return &x }(), func() *int { x := -1; return &x }()}
	for _, step := range catalogueSteps {
		for _, reason := range messageReasons {
			for _, limit := range []string{"", LimitEntries, LimitFileSize, LimitTreeBytes, LimitOutput} {
				for _, show := range []bool{false, true} {
					for _, exit := range exits {
						if m := CommandMessage(step, show, exit, reason, limit); !ValidGraderMessage(m) {
							t.Fatalf("catalogue message rejected: %q", m)
						}
					}
				}
			}
		}
	}
	for _, m := range []string{"free text", "command: TIMEOUT; extra", "command: TIMEOUT\n", "copy into scratch: PATH_UNSAFE (/etc/passwd)"} {
		if ValidGraderMessage(m) {
			t.Errorf("accepted %q", m)
		}
	}
}
