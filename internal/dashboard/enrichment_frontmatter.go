package dashboard

// enrichment_frontmatter.go — shared YAML frontmatter parser for enriched doc files.
//
// The /generate-docs skill emits YAML frontmatter at the top of every enriched
// entity doc file. This helper reads a Markdown file, extracts that frontmatter,
// and returns a typed EnrichmentFrontmatter struct.
//
// Schema (all fields optional; fall back to first-line summary when absent):
//
//	---
//	entity_id:    <id>
//	kind:         http_endpoint | process_flow | message_topic
//	disqualified: false
//	merged_into:  ""
//	rank:         0.78
//	group:        "orders"
//	group_label:  "Order processing"
//	summary:      "Free-text NL summary"
//	gaps:
//	  - "No error response documented for 4xx"
//	# Per-kind structured fields:
//	method:       GET
//	path:         /api/users
//	parameters:   [...]
//	responses:    {...}
//	auth:         "Bearer required"
//	tables_touched: [users]
//	steps:        [...]
//	preconditions:    "User is authenticated"
//	expected_outcome: "Order persisted, event emitted"
//	schema:           "{order_id, total, items}"
//	typical_payload_size_bytes: 256
//	volume_estimate:  "high"
//	expected_consumers: [order-fulfillment, analytics]
//	---

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// EnrichmentFrontmatter is the parsed representation of the YAML frontmatter
// block emitted by the /generate-docs skill for any enriched entity.
//
// All fields are optional; the zero value represents "no enrichment data".
type EnrichmentFrontmatter struct {
	// Universal fields.
	EntityID    string   `json:"entity_id,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Disqualified bool    `json:"disqualified,omitempty"`
	MergedInto  string   `json:"merged_into,omitempty"`
	Rank        float64  `json:"rank,omitempty"`
	Group       string   `json:"group,omitempty"`
	GroupLabel  string   `json:"group_label,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Gaps        []string `json:"gaps,omitempty"`

	// http_endpoint fields.
	Method        string            `json:"method,omitempty"`
	Path          string            `json:"path,omitempty"`
	Parameters    []map[string]any  `json:"parameters,omitempty"`
	Responses     map[string]any    `json:"responses,omitempty"`
	Auth          string            `json:"auth,omitempty"`
	TablesTouched []string          `json:"tables_touched,omitempty"`

	// process_flow fields.
	Steps           []string `json:"steps,omitempty"`
	Preconditions   string   `json:"preconditions,omitempty"`
	ExpectedOutcome string   `json:"expected_outcome,omitempty"`

	// message_topic fields.
	Schema                 string   `json:"schema,omitempty"`
	TypicalPayloadSizeBytes int     `json:"typical_payload_size_bytes,omitempty"`
	VolumeEstimate         string   `json:"volume_estimate,omitempty"`
	ExpectedConsumers      []string `json:"expected_consumers,omitempty"`
}

// HasData returns true when at least the summary or kind was populated —
// used by callers to decide whether to prefer frontmatter over a first-line scan.
func (f *EnrichmentFrontmatter) HasData() bool {
	return f != nil && (f.Summary != "" || f.Kind != "")
}

// ParseEnrichmentFrontmatter reads a Markdown file from disk, extracts the
// YAML frontmatter block (delimited by leading "---" lines), and returns a
// populated EnrichmentFrontmatter.
//
// Returns (nil, nil) when the file does not exist or has no frontmatter.
// Returns (nil, err) only for genuine I/O errors.
func ParseEnrichmentFrontmatter(filePath string) (*EnrichmentFrontmatter, error) {
	data, err := os.ReadFile(filePath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseFrontmatterBytes(data), nil
}

// parseFrontmatterBytes extracts and parses the YAML frontmatter from a
// Markdown byte slice. This is the pure-logic entry point used by tests.
func parseFrontmatterBytes(data []byte) *EnrichmentFrontmatter {
	lines := splitLines(string(data))
	yamlLines, _ := extractFrontmatterBlock(lines)
	if len(yamlLines) == 0 {
		return nil
	}
	return parseSimpleYAML(yamlLines)
}

// extractFrontmatterBlock returns (yaml_lines, remaining_lines).
// Expects the first non-empty line to be "---"; reads until the closing "---".
// Returns (nil, original) when no valid frontmatter is found.
func extractFrontmatterBlock(lines []string) (yaml []string, rest []string) {
	// Find opening "---".
	start := -1
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		if trimmed == "---" {
			start = i
		}
		break
	}
	if start < 0 {
		return nil, lines
	}

	// Read until closing "---".
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return lines[start+1 : i], lines[i+1:]
		}
		yaml = append(yaml, lines[i])
	}
	// No closing delimiter found — treat as malformed.
	return nil, lines
}

// parseSimpleYAML is a minimal YAML parser sufficient for the frontmatter
// schema. It handles:
//   - Scalar: key: value
//   - Quoted scalar: key: 'value' or key: "value"
//   - Boolean: key: true/false
//   - Float: key: 0.78
//   - Int: key: 256
//   - Block sequence (indented "- item")
//   - Inline sequence: key: [a, b, c]
//
// It does NOT handle nested mappings (e.g. parameters items) with full
// fidelity — those are captured as raw strings. The backend uses only the
// scalar and list fields; structured sub-fields (parameters, responses) are
// left for a future full-YAML pass.
func parseSimpleYAML(lines []string) *EnrichmentFrontmatter {
	fm := &EnrichmentFrontmatter{}
	var currentKey string
	var inList bool

	for _, line := range lines {
		// Detect block-sequence item belonging to the current list key.
		stripped := strings.TrimLeft(line, " \t")
		if inList && strings.HasPrefix(stripped, "- ") {
			item := strings.TrimPrefix(stripped, "- ")
			item = unquote(strings.TrimSpace(item))
			appendListField(fm, currentKey, item)
			continue
		}

		// Detect a new key: value pair.
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colonIdx])
		rawVal := strings.TrimSpace(line[colonIdx+1:])

		// Inline list: key: [a, b, c]
		if strings.HasPrefix(rawVal, "[") && strings.HasSuffix(rawVal, "]") {
			inner := rawVal[1 : len(rawVal)-1]
			parts := splitCommaList(inner)
			for _, p := range parts {
				appendListField(fm, key, p)
			}
			currentKey = key
			inList = false
			continue
		}

		// Empty value signals start of a block sequence.
		if rawVal == "" {
			currentKey = key
			inList = true
			continue
		}

		inList = false
		currentKey = key
		setScalarField(fm, key, rawVal)
	}
	return fm
}

// setScalarField maps a key-value pair onto the appropriate EnrichmentFrontmatter field.
func setScalarField(fm *EnrichmentFrontmatter, key, val string) {
	val = unquote(val)
	switch key {
	case "entity_id":
		fm.EntityID = val
	case "kind":
		fm.Kind = val
	case "disqualified":
		fm.Disqualified = val == "true"
	case "merged_into":
		fm.MergedInto = val
	case "rank":
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			fm.Rank = f
		}
	case "group":
		fm.Group = val
	case "group_label":
		fm.GroupLabel = val
	case "summary":
		fm.Summary = val
	case "method":
		fm.Method = val
	case "path":
		fm.Path = val
	case "auth":
		fm.Auth = val
	case "preconditions":
		fm.Preconditions = val
	case "expected_outcome":
		fm.ExpectedOutcome = val
	case "schema":
		fm.Schema = val
	case "typical_payload_size_bytes":
		if n, err := strconv.Atoi(val); err == nil {
			fm.TypicalPayloadSizeBytes = n
		}
	case "volume_estimate":
		fm.VolumeEstimate = val
	}
}

// appendListField appends item to the appropriate slice field on fm.
func appendListField(fm *EnrichmentFrontmatter, key, item string) {
	switch key {
	case "gaps":
		fm.Gaps = append(fm.Gaps, item)
	case "tables_touched":
		fm.TablesTouched = append(fm.TablesTouched, item)
	case "steps":
		fm.Steps = append(fm.Steps, item)
	case "expected_consumers":
		fm.ExpectedConsumers = append(fm.ExpectedConsumers, item)
	}
	// parameters, responses: intentionally skipped — complex sub-objects
	// are left for a future full-YAML pass.
}

// unquote removes surrounding single or double quotes from a YAML scalar.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') ||
			(s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// splitCommaList splits a comma-separated inline list, trimming whitespace and
// quotes from each element.
func splitCommaList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = unquote(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// splitLines splits a string on \n (handles \r\n too).
func splitLines(s string) []string {
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

// extractEnrichmentFromFile is the top-level helper used by all three handler
// files. It attempts to read YAML frontmatter from filePath and returns
// (frontmatter, firstLineFallback).
//
// When frontmatter is present and has a summary, firstLineFallback is "".
// When frontmatter is absent, frontmatter is nil and firstLineFallback is
// the first non-heading non-empty line of the file (the legacy behaviour).
func extractEnrichmentFromFile(filePath string) (*EnrichmentFrontmatter, string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, ""
	}
	text := string(data)
	fm := parseFrontmatterBytes(data)
	if fm != nil && fm.HasData() {
		return fm, ""
	}
	// Fall back to first-line summary scan.
	summary := firstLineSummary(text, 150)
	return nil, summary
}

// firstLineSummary returns the first non-empty, non-heading line of text,
// truncated to maxLen characters.
func firstLineSummary(text string, maxLen int) string {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || trimmed == "---" {
			continue
		}
		if len(trimmed) > maxLen {
			return trimmed[:maxLen] + "..."
		}
		return trimmed
	}
	return ""
}
