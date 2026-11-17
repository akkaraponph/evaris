package application

import (
	"strings"
	"testing"
)

func TestStreamEscapesControlBytes(t *testing.T) {
	var b strings.Builder
	stream(&b, "x", []byte{'a', 0x01, 0xff, '\t', 'b', '\n'})
	if got := b.String(); got != "  x (6 bytes):\n    a\\x01\\xff\tb\n" {
		t.Fatalf("got %q", got)
	}
}
