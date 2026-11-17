package domain

import (
	"path"
	"strings"
)

// ValidPattern reports whether p is a usable allowedChanges or protectedPaths
// entry: a relative slash path whose segments are path.Match patterns, with
// "**" only as a whole segment. A plain path is a pattern that matches itself.
func ValidPattern(p string) bool {
	if !Relative(p) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "**" {
			continue
		}
		if strings.Contains(seg, "**") {
			return false
		}
		if _, err := path.Match(seg, ""); err != nil {
			return false
		}
	}
	return true
}

// MatchPattern reports whether the slash path name matches pattern. Segments
// match with path.Match; a "**" segment matches zero or more whole segments.
func MatchPattern(pattern, name string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegments(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(name); i++ {
				if matchSegments(pat[1:], name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, err := path.Match(pat[0], name[0]); err != nil || !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

// MatchAny reports whether any pattern matches name.
func MatchAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if MatchPattern(p, name) {
			return true
		}
	}
	return false
}
