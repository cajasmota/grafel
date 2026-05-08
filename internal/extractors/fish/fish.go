// Package fish implements a regex-based extractor for Fish shell source files (.fish).
//
// Fish's function…end syntax is distinct from POSIX shell and the bash tree-sitter
// grammar does not parse it well, so this extractor is regex-only.
//
// Extracted entities:
//   - function <name>                 → Kind="SCOPE.Operation", Subtype="function"
//   - complete --command <name> …     → Kind="SCOPE.Operation", Subtype="completion"
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

// Patterns mirror the functional requirements in MX-1058.
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
)

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

	for _, m := range functionRE.FindAllStringSubmatchIndex(stripped, -1) {
		name := stripped[m[2]:m[3]]
		key := "fn:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(stripped[:m[0]], "\n") + 1
		endLine := findBlockEnd(stripped, m[1], startLine)
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
		})
	}

	// Completions — dedupe on name across both long and short flag forms.
	for _, m := range completionLongRE.FindAllStringSubmatchIndex(stripped, -1) {
		name := stripped[m[2]:m[3]]
		appendCompletion(&entities, seen, stripped, name, m[0], file.Path)
	}
	for _, m := range completionShortRE.FindAllStringSubmatchIndex(stripped, -1) {
		name := stripped[m[2]:m[3]]
		appendCompletion(&entities, seen, stripped, name, m[0], file.Path)
	}

	return entities, nil
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

// findBlockEnd returns the line number of the matching `end` keyword for a
// `function` block starting at byte pos. If no matching `end` is found, the
// function's start line is returned (graceful degradation on malformed input).
//
// Fish uses function … end with other block constructs (if/while/for/switch/begin)
// that also close with `end`. We track depth by scanning for recognised opening
// keywords and the terminating `end`.
func findBlockEnd(src string, pos int, startLine int) int {
	// Advance past the remainder of the header line (everything between pos
	// and the next newline is part of the function-signature line).
	if nl := strings.IndexByte(src[pos:], '\n'); nl >= 0 {
		pos += nl + 1
	} else {
		return startLine
	}

	depth := 1
	line := startLine // the next full line we read is startLine+1.
	i := pos
	n := len(src)
	for i < n {
		line++
		nl := strings.IndexByte(src[i:], '\n')
		var segment string
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
				return line
			}
		}
	}
	return startLine
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
