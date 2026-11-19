package fsrepo

import (
	"bufio"
	"bytes"
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/akkaraponph/evaris/internal/domain"
)

type claudeEvent struct {
	Type    string    `json:"type"`
	Subtype string    `json:"subtype"`
	Session string    `json:"session_id"`
	CWD     string    `json:"cwd"`
	Version string    `json:"claude_code_version"`
	Model   string    `json:"model"`
	Mode    string    `json:"permissionMode"`
	Tools   *[]string `json:"tools"`
	Message struct {
		Content []struct {
			Type      string          `json:"type"`
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			ToolUseID string          `json:"tool_use_id"`
			IsError   bool            `json:"is_error"`
			Content   json.RawMessage `json:"content"`
			Input     struct {
				FilePath string `json:"file_path"`
			} `json:"input"`
		} `json:"content"`
	} `json:"message"`
}

type transcriptInstruction struct {
	path string
	data string
}

func transcriptInstructions(root string, p domain.Preparation, caseID string) ([]transcriptInstruction, error) {
	for _, task := range p.Tasks {
		if task.CaseID != caseID || task.ArmID != "specialist" {
			continue
		}
		ins := make([]transcriptInstruction, 0, len(task.Instructions))
		for i, ref := range task.Instructions {
			data, err := readFile(filepath.Join(root, filepath.FromSlash(ref.Path)))
			if err != nil {
				return nil, err
			}
			if digest(data) != ref.SHA256 {
				return nil, fail("EVIDENCE_CORRUPT", "transcript instruction input")
			}
			ins = append(ins, transcriptInstruction{domain.InstructionWorkspacePath(i), normalizedOutput(string(data))})
		}
		return ins, nil
	}
	return nil, nil
}

type claudeToolUse struct{ name, path string }

// observeClaude recognizes a single completed --output-format stream-json
// --verbose session. Unobserved values remain unknown unless supplied by the
// operator. A transcript from a different task directory is refused.
func observeClaude(data []byte, workspace string, instructions []transcriptInstruction, dst *domain.Observed) error {
	bad := func(detail string) error { return fail("INVALID_CONFIG", "Claude transcript "+detail) }
	s := bufio.NewScanner(bytes.NewReader(data))
	s.Buffer(make([]byte, 4096), len(data)+1)
	if !utf8.Valid(data) || !s.Scan() {
		return bad("is empty or malformed")
	}
	var init claudeEvent
	if json.Unmarshal(s.Bytes(), &init) != nil || init.Type != "system" || init.Subtype != "init" ||
		init.Session == "" || init.Version == "" || init.Model == "" || init.Mode == "" || init.Tools == nil ||
		!filepath.IsAbs(init.CWD) || filepath.Clean(init.CWD) != workspace {
		return bad("does not identify the prepared workspace and host")
	}
	pending := map[string]claudeToolUse{}
	confirmed := map[string]bool{}
	finished := false
	for s.Scan() {
		var event claudeEvent
		if finished || json.Unmarshal(s.Bytes(), &event) != nil ||
			event.Type == "" || event.Session != "" && event.Session != init.Session ||
			event.Type == "system" && event.Subtype == "init" {
			return bad("must contain one complete session")
		}
		switch event.Type {
		case "result":
			finished = event.Session == init.Session
			if !finished {
				return bad("result lacks matching session identity")
			}
		case "assistant":
			for _, block := range event.Message.Content {
				if block.Type == "tool_use" && (block.Name == "Read" || block.Name == "Bash") && block.ID != "" {
					use := claudeToolUse{block.Name, block.Input.FilePath}
					if prior, ok := pending[block.ID]; ok && prior != use {
						return bad("contains conflicting tool identities")
					}
					pending[block.ID] = use
				}
			}
		case "user":
			for _, block := range event.Message.Content {
				if block.Type != "tool_result" {
					continue
				}
				use, ok := pending[block.ToolUseID]
				delete(pending, block.ToolUseID)
				if ok && !block.IsError {
					if name := confirmedInstruction(use, claudeText(block.Content), workspace, instructions); name != "" {
						confirmed[name] = true
					}
				}
			}
		}
	}
	if s.Err() != nil || !finished {
		return bad("is incomplete")
	}
	for _, pair := range []struct {
		field **string
		value string
	}{{&dst.Host, "Claude Code " + init.Version}, {&dst.Model, init.Model}, {&dst.Permissions, init.Mode}, {&dst.ContextID, init.Session}} {
		if *pair.field != nil && **pair.field != pair.value {
			return bad("conflicts with supplied observed metadata")
		}
		*pair.field = &pair.value
	}
	tools := slices.Clone(*init.Tools)
	slices.Sort(tools)
	if dst.Tools != nil {
		provided := slices.Clone(*dst.Tools)
		slices.Sort(provided)
		if !slices.Equal(provided, tools) {
			return bad("conflicts with supplied tools")
		}
	}
	dst.Tools = &tools
	if len(confirmed) > 0 {
		// Keep any other reads independently observed by the operator. Positive
		// transcript evidence always wins over an assertion of no reads.
		var loaded []string
		if dst.LoadedInstructions != nil {
			loaded = slices.Clone(*dst.LoadedInstructions)
		}
		for name := range confirmed {
			if !slices.Contains(loaded, name) {
				loaded = append(loaded, name)
			}
		}
		slices.Sort(loaded)
		dst.LoadedInstructions = &loaded
	}
	return nil
}

func claudeText(data json.RawMessage) string {
	var out string
	if json.Unmarshal(data, &out) == nil {
		return out
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(data, &blocks) == nil && len(blocks) == 1 && blocks[0].Type == "text" {
		return blocks[0].Text
	}
	return ""
}

var readLine = regexp.MustCompile(`^[ 	]*([0-9]+)[ 	]*(?:→|	)(.*)$`)

func confirmedInstruction(use claudeToolUse, output, workspace string, instructions []transcriptInstruction) string {
	text := normalizedOutput(output)
	if text == "" {
		return ""
	}
	match := ""
	for _, in := range instructions {
		if use.name == "Read" {
			p := use.path
			if !filepath.IsAbs(p) {
				p = filepath.Join(workspace, p)
			}
			if filepath.Clean(p) != filepath.Join(workspace, filepath.FromSlash(in.path)) {
				continue
			}
		}
		if text != in.data && (use.name != "Read" || normalizedOutput(unnumberRead(text)) != in.data) {
			continue
		}
		if match != "" {
			return "" // an unqualified Bash result cannot identify identical files
		}
		match = in.path
	}
	return match
}

func normalizedOutput(s string) string {
	return strings.TrimSuffix(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

func unnumberRead(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		fields := readLine.FindStringSubmatch(line)
		if len(fields) == 0 || strings.TrimLeft(fields[1], "0") != strconv.Itoa(i+1) {
			return ""
		}
		lines[i] = fields[2]
	}
	return strings.Join(lines, "\n")
}
