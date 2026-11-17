package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// Grader messages are built only from this catalogue: a step, the command's
// exit code, a reason code, a limit kind and a fixed hint. They never carry
// paths, command text, output or oracle values, so a message cannot leak
// reviewer-private content and re-evaluating a run gives the same text.
// The evaluation layer keeps a message only when ValidGraderMessage accepts it.

// CommandStep names the part of a command-exit grade that failed.
type CommandStep string

const (
	StepCopy           CommandStep = "copy into scratch"
	StepSnapshotBefore CommandStep = "scratch snapshot before the command"
	StepSnapshotAfter  CommandStep = "scratch snapshot after the command"
	StepCleanup        CommandStep = "remove scratch"
	StepCommand        CommandStep = "command"
	StepDependencies   CommandStep = "link dependencies"

	// Steps of an Invaris verification.
	StepVerifyHarness    CommandStep = "verification harness"
	StepVerifyExecutable CommandStep = "verification executable"
	StepVerifyScratch    CommandStep = "verification scratch"
	StepVerifyRun        CommandStep = "verification command"
	StepVerifyEvidence   CommandStep = "verification evidence"
)

// Limit kinds name which bound a LIMIT_EXCEEDED hit.
const (
	LimitEntries   = "entries"
	LimitFileSize  = "file size"
	LimitTreeBytes = "tree bytes"
	LimitOutput    = "output"
)

var messageHints = map[string]string{
	string(StepSnapshotBefore) + "/LIMIT_EXCEEDED":             "list generated or dependency directories in snapshotExclude",
	string(StepSnapshotAfter) + "/LIMIT_EXCEEDED":              "list generated output such as .next in snapshotExclude",
	string(StepCopy) + "/PATH_UNSAFE":                          "the imported tree contains a symbolic link or special file",
	string(StepCommand) + "/LIMIT_EXCEEDED":                    "stdout or stderr exceeded 64 KiB",
	string(StepCommand) + "/TIMEOUT":                           "raise timeoutSeconds or shorten the command",
	string(StepCommand) + "/TOOL_MISSING":                      "the program is not readable on this host",
	string(StepCommand) + "/IO_ERROR":                          "the command was killed by a signal, left processes behind or could not be started",
	string(StepDependencies) + "/TOOL_MISSING":                 "a declared dependency directory does not exist on this host",
	string(StepSnapshotBefore) + "/PATH_UNSAFE":                "the scratch copy contains a symbolic link or special file",
	string(StepVerifyExecutable) + "/TOOL_MISSING":             "the invaris executable is not an absolute path to an executable file",
	string(StepVerifyScratch) + "/PATH_UNSAFE":                 "the imported tree contains a symbolic link, a special file or its own invaris.json",
	string(StepVerifyRun) + "/TIMEOUT":                         "raise verification.timeoutSeconds",
	string(StepVerifyRun) + "/LIMIT_EXCEEDED":                  "invaris output exceeded the evidence bound",
	string(StepVerifyRun) + "/OUTPUT_MISSING":                  "invaris printed no report",
	string(StepVerifyEvidence) + "/UNSUPPORTED_SCHEMA":         "this Invaris version is not supported; see docs/invaris.md",
	string(StepVerifyExecutable) + "/UNSUPPORTED_TOOL_VERSION": "this Invaris release is not supported; see docs/invaris.md",
	string(StepVerifyEvidence) + "/EVIDENCE_CORRUPT":           "the invaris report does not match the frozen harness or the check profile",
}

var messageReasons = []Reason{"IO_ERROR", "LIMIT_EXCEEDED", "PATH_UNSAFE", "EVIDENCE_CORRUPT", "TIMEOUT", "TOOL_MISSING", "INVALID_CONFIG",
	"CANCELED", "OUTPUT_MISSING", "UNSUPPORTED_SCHEMA", "UNSUPPORTED_TOOL_VERSION"}

// catalogueSteps lists every step, for the message pattern.
var catalogueSteps = []CommandStep{StepCopy, StepSnapshotBefore, StepSnapshotAfter, StepCleanup, StepCommand, StepDependencies,
	StepVerifyHarness, StepVerifyExecutable, StepVerifyScratch, StepVerifyRun, StepVerifyEvidence}

// CommandMessage formats a catalogue message. exit is nil when the command
// did not run to completion or the step precedes it; showExit says whether
// the exit belongs in the message at all.
func CommandMessage(step CommandStep, showExit bool, exit *int, reason Reason, limit string) string {
	var b strings.Builder
	b.WriteString(string(step))
	if showExit {
		if exit != nil {
			fmt.Fprintf(&b, " (command exited %d)", *exit)
		} else {
			b.WriteString(" (command did not finish)")
		}
	}
	b.WriteString(": " + string(reason))
	if limit != "" {
		b.WriteString(" (" + limit + ")")
	}
	if hint, ok := messageHints[string(step)+"/"+string(reason)]; ok {
		b.WriteString("; " + hint)
	}
	return b.String()
}

var graderMessagePattern = func() *regexp.Regexp {
	alt := func(xs []string) string {
		q := make([]string, len(xs))
		for i, x := range xs {
			q[i] = regexp.QuoteMeta(x)
		}
		return "(?:" + strings.Join(q, "|") + ")"
	}
	steps := make([]string, len(catalogueSteps))
	for i, s := range catalogueSteps {
		steps[i] = string(s)
	}
	reasons := make([]string, len(messageReasons))
	for i, r := range messageReasons {
		reasons[i] = string(r)
	}
	hints := make([]string, 0, len(messageHints))
	for _, h := range messageHints {
		hints = append(hints, h)
	}
	return regexp.MustCompile("^" + alt(steps) + `(?: \(command (?:exited -?[0-9]{1,3}|did not finish)\))?: ` + alt(reasons) +
		`(?: \(` + alt([]string{LimitEntries, LimitFileSize, LimitTreeBytes, LimitOutput}) + `\))?(?:; ` + alt(hints) + ")?$")
}()

// ValidGraderMessage reports whether m is empty or a catalogue message.
func ValidGraderMessage(m string) bool {
	return m == "" || graderMessagePattern.MatchString(m)
}
