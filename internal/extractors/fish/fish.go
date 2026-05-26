// Package fish implements a regex-based extractor for Fish shell source files (.fish).
//
// Fish's function…end syntax is distinct from POSIX shell and the bash tree-sitter
// grammar does not parse it well, so this extractor is regex-only.
//
// Extracted entities:
//   - function <name>                 → Kind="SCOPE.Operation", Subtype="function"
//   - complete --command <name> …     → Kind="SCOPE.Operation", Subtype="completion"
//   - source / . <path>               → Kind="SCOPE.Component",  Subtype="import"
//     (carries one IMPORTS edge)
//
// Issue #371 (PORT-RELS-FISH) — emits the same three relationship kinds the
// other ported extractors emit:
//
//   - IMPORTS: every `source <path>` and `. <path>` (dot-source) directive
//     becomes a SCOPE.Component import-stub entity carrying a single IMPORTS
//     edge from the source file → the imported path. Properties carry
//     {source_module, import_kind="source"} matching the contract used by
//     the lua / razor extractors.
//
//   - CALLS: every command invocation (first word of a non-blank, non-keyword
//     line) inside a function body emits one CALLS edge per unique callee.
//     Self-recursion is dropped, fish keywords (if/while/for/switch/begin/
//     case/return/break/continue/and/or/not/end/else/function) are filtered.
//
//   - CONTAINS: the file itself acts as the structural container — a single
//     SCOPE.Component "file" entity is emitted per .fish source carrying one
//     CONTAINS edge per declared function with the canonical structural-ref
//     shape `scope:operation:method:fish:<file>:<name>`
//     (BuildOperationStructuralRef, Format A, #144).
//
// Registers itself via init() and is imported by registry_gen.go.
package fish

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("fish", &Extractor{})
}

// Extractor implements extractor.Extractor for Fish.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "fish" }

// Patterns mirror the functional requirements in.
var (
	// function <name>               — start of a function block (terminated by end).
	// The name must be a valid fish identifier (letters, digits, _ -).
	functionRE = regexp.MustCompile(
		`(?m)^[ \t]*function\s+([A-Za-z_][A-Za-z0-9_\-]*)`,
	)
	// complete --command <name>  OR  complete -c <name>
	// These declare tab-completion for an external command.
	completionLongRE = regexp.MustCompile(
		`(?m)^[ \t]*complete\s+(?:[^\n]*?)\-\-command\s+([A-Za-z_][A-Za-z0-9_\-]*)`,
	)
	completionShortRE = regexp.MustCompile(
		`(?m)^[ \t]*complete\s+(?:[^\n]*?)\-c\s+([A-Za-z_][A-Za-z0-9_\-]*)`,
	)
	// source <path>  or  . <path>   — fish import directives. The path may
	// be quoted with single or double quotes. We capture everything up to
	// end-of-line / first whitespace after the path token.
	sourceLongRE = regexp.MustCompile(
		`(?m)^[ \t]*source\s+(?:"([^"\n]+)"|'([^'\n]+)'|(\S+))`,
	)
	sourceDotRE = regexp.MustCompile(
		`(?m)^[ \t]*\.\s+(?:"([^"\n]+)"|'([^'\n]+)'|(\S+))`,
	)
	// First-word command head on a line (used for CALLS extraction inside
	// function bodies). Captures the leading identifier of an invocation.
	commandHeadRE = regexp.MustCompile(
		`(?m)^[ \t]*([A-Za-z_][A-Za-z0-9_\-]*)`,
	)
)

// fishKeywords are tokens that begin a line but are NOT command invocations
// (control flow + structural keywords). Filtered out of CALLS edges.
var fishKeywords = map[string]bool{
	"if": true, "else": true, "end": true, "while": true, "for": true,
	"switch": true, "case": true, "begin": true, "function": true,
	"return": true, "break": true, "continue": true,
	"and": true, "or": true, "not": true,
}

// Extract processes the Fish source and returns entity records.
// Never raises: on parse failure the extractor returns an empty list and
// the shared pipeline applies the default quality score.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	// Strip comments — Fish uses `#` to end-of-line.
	// Regexes are anchored to line-start, so simple comment stripping per-line
	// is sufficient to avoid matching `# function foo` lines.
	stripped := stripLineComments(src)

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	// Pass 1: file-level container entity. We populate its CONTAINS edges
	// as we discover functions below. Inserted at index 0 so subsequent
	// CONTAINS appends mutate entities[0].
	fileEntity := types.EntityRecord{
		Name:       file.Path,
		Kind:       "SCOPE.Component",
		Subtype:    "file",
		SourceFile: file.Path,
		Language:   "fish",
	}
	entities = append(entities, fileEntity)

	// Pass 2: IMPORTS — `source` and `.` (dot-source) directives become
	// SCOPE.Component import-stub entities, one per unique imported path.
	seenImport := make(map[string]bool)
	for _, m := range sourceLongRE.FindAllStringSubmatchIndex(stripped, -1) {
		path := importPathFromMatch(stripped, m)
		if path == "" || seenImport[path] {
			continue
		}
		seenImport[path] = true
		startLine := strings.Count(stripped[:m[0]], "\n") + 1
		entities = append(entities, makeImportEntity(file.Path, path, startLine))
	}
	for _, m := range sourceDotRE.FindAllStringSubmatchIndex(stripped, -1) {
		path := importPathFromMatch(stripped, m)
		if path == "" || seenImport[path] {
			continue
		}
		seenImport[path] = true
		startLine := strings.Count(stripped[:m[0]], "\n") + 1
		entities = append(entities, makeImportEntity(file.Path, path, startLine))
	}

	// Pass 3: function declarations. For each function we capture its body
	// substring so CALLS can be scanned. CONTAINS edges are appended to the
	// file entity (index 0).
	for _, m := range functionRE.FindAllStringSubmatchIndex(stripped, -1) {
		name := stripped[m[2]:m[3]]
		key := "fn:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(stripped[:m[0]], "\n") + 1
		endLine, bodyStart, bodyEnd := findBlockBounds(stripped, m[1], startLine)
		body := ""
		if bodyStart >= 0 && bodyEnd >= bodyStart && bodyEnd <= len(stripped) {
			body = stripped[bodyStart:bodyEnd]
		}
		calls := collectCalls(body, name)
		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Operation",
			Subtype:            "function",
			SourceFile:         file.Path,
			Language:           "fish",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          "function " + name,
			EnrichmentRequired: false,
			Relationships:      calls,
		})
		// CONTAINS edge: file → function (Format A structural-ref).
		toID := extractor.BuildOperationStructuralRef("fish", file.Path, name)
		entities[0].Relationships = append(entities[0].Relationships, types.RelationshipRecord{
			ToID: toID,
			Kind: "CONTAINS",
		})
	}

	// Pass 4: completions — dedupe on name across both long and short flag forms.
	for _, m := range completionLongRE.FindAllStringSubmatchIndex(stripped, -1) {
		name := stripped[m[2]:m[3]]
		appendCompletion(&entities, seen, stripped, name, m[0], file.Path)
	}
	for _, m := range completionShortRE.FindAllStringSubmatchIndex(stripped, -1) {
		name := stripped[m[2]:m[3]]
		appendCompletion(&entities, seen, stripped, name, m[0], file.Path)
	}

	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(entities, "fish")
	extractor.TagEntitiesLanguage(entities, "fish")
	return entities, nil
}

// importPathFromMatch returns the captured path from a source/dot-source
// regex match. The pattern has three alternation groups (double-quoted,
// single-quoted, bare); pick whichever captured.
func importPathFromMatch(src string, m []int) string {
	// Groups 1, 2, 3 → indices 2..7 in the submatch index slice.
	for g := 1; g <= 3; g++ {
		s, e := m[2*g], m[2*g+1]
		if s >= 0 && e > s {
			return src[s:e]
		}
	}
	return ""
}

// makeImportEntity builds the SCOPE.Component entity carrying a single
// IMPORTS edge for a `source` / `.` directive.
func makeImportEntity(filePath, importPath string, line int) types.EntityRecord {
	return types.EntityRecord{
		Name:       importPath,
		Kind:       "SCOPE.Component",
		Subtype:    "import",
		SourceFile: filePath,
		Language:   "fish",
		StartLine:  line,
		EndLine:    line,
		Relationships: []types.RelationshipRecord{
			{
				FromID: filePath,
				ToID:   importPath,
				Kind:   "IMPORTS",
				Properties: map[string]string{
					"source_module": importPath,
					"imported_name": importPath,
					"import_kind":   "source",
				},
			},
		},
	}
}

// collectCalls scans a function body line-by-line for command invocations
// and returns deduped CALLS edges. Skips fish keywords, `source`/`.`
// (those produce IMPORTS), and self-recursion.
func collectCalls(body, callerName string) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	seen := make(map[string]bool)
	var out []types.RelationshipRecord
	for _, line := range strings.Split(body, "\n") {
		m := commandHeadRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		head := m[1]
		if head == "" || fishKeywords[head] {
			continue
		}
		if head == callerName {
			continue
		}
		if head == "source" {
			// Dot-source `.` is not captured by commandHeadRE (it isn't
			// an identifier), so we only need to filter `source`.
			continue
		}
		if seen[head] {
			continue
		}
		seen[head] = true
		out = append(out, types.RelationshipRecord{
			ToID: head,
			Kind: "CALLS",
		})
	}
	return out
}

// appendCompletion builds a completion entity if one hasn't been seen already
// for this command name.
func appendCompletion(out *[]types.EntityRecord, seen map[string]bool, src, name string, startOffset int, path string) {
	key := "cmp:" + name
	if seen[key] {
		return
	}
	seen[key] = true
	startLine := strings.Count(src[:startOffset], "\n") + 1
	*out = append(*out, types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            "completion",
		SourceFile:         path,
		Language:           "fish",
		StartLine:          startLine,
		EndLine:            startLine,
		Signature:          "complete --command " + name,
		EnrichmentRequired: false,
	})
}

// findBlockBounds returns the line number of the matching `end` keyword for
// a `function` block starting at byte pos, plus the byte offsets [bodyStart,
// bodyEnd) of the function body (everything strictly between the header line
// and the closing `end`). On malformed input bodyStart=-1 and the start line
// is returned.
//
// Fish uses function … end with other block constructs (if/while/for/switch/begin)
// that also close with `end`. We track depth by scanning for recognised opening
// keywords and the terminating `end`.
func findBlockBounds(src string, pos, startLine int) (endLine, bodyStart, bodyEnd int) {
	// Advance past the remainder of the header line (everything between pos
	// and the next newline is part of the function-signature line).
	if nl := strings.IndexByte(src[pos:], '\n'); nl >= 0 {
		pos += nl + 1
	} else {
		return startLine, -1, -1
	}
	bodyStart = pos

	depth := 1
	line := startLine // the next full line we read is startLine+1.
	i := pos
	n := len(src)
	for i < n {
		line++
		nl := strings.IndexByte(src[i:], '\n')
		var segment string
		var segStart = i
		if nl < 0 {
			segment = src[i:]
			i = n
		} else {
			segment = src[i : i+nl]
			i += nl + 1
		}
		trimmed := strings.TrimSpace(segment)
		// Recognised block openers — keywords followed by whitespace or EOL.
		if isOpener(trimmed) {
			depth++
			continue
		}
		if trimmed == "end" || strings.HasPrefix(trimmed, "end ") || strings.HasPrefix(trimmed, "end;") {
			depth--
			if depth == 0 {
				return line, bodyStart, segStart
			}
		}
	}
	return startLine, -1, -1
}

// findBlockEnd is the legacy line-only wrapper retained for callers that
// don't need body bounds.
func findBlockEnd(src string, pos int, startLine int) int {
	endLine, _, _ := findBlockBounds(src, pos, startLine)
	return endLine
}

// isOpener reports whether the trimmed line starts a fish block that must be
// closed by `end`.
func isOpener(trimmed string) bool {
	openers := []string{"function ", "if ", "while ", "for ", "switch ", "begin"}
	for _, k := range openers {
		if strings.HasPrefix(trimmed, k) {
			return true
		}
	}
	return trimmed == "begin"
}

// stripLineComments removes the portion of each line starting at an unquoted
// `#`. Fish comments are line-terminated; quote handling is intentionally
// minimal — if a `#` appears inside a string literal we conservatively
// preserve it only when it is the first character of a token (shebang).
func stripLineComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	for _, line := range strings.SplitAfter(src, "\n") {
		// Preserve shebang lines as-is.
		if strings.HasPrefix(line, "#!") {
			b.WriteString(line)
			continue
		}
		// Find first unquoted `#`. We do not attempt full quote parsing; a
		// simple heuristic is adequate for structural extraction.
		if idx := strings.Index(line, "#"); idx >= 0 {
			// Preserve the newline terminator if present.
			nl := ""
			if strings.HasSuffix(line, "\n") {
				nl = "\n"
			}
			b.WriteString(line[:idx])
			b.WriteString(nl)
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}
