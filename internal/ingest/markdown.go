// Package ingest implements deterministic markdown documentation ingestion
// for archigraph (Layer 1 of epic #4294, issue #4306).
//
// It is FULLY DETERMINISTIC: a markdown file is parsed by a pure line/heading
// scanner into one Document node and a tree of heading-delimited Section nodes,
// and section body text is linked to code entities by EXACT identifier-token
// match only. There are NO LLM calls, NO network access, and NO API keys — any
// semantic/LLM enrichment is delegated to a later layer (the agent). The whole
// subsystem is gated behind an opt-in flag (--ingest-docs, default OFF); when
// the flag is off this package is never invoked and adds zero overhead.
package ingest

import (
	"strings"
)

// Section is a heading-delimited block within a markdown document.
//
// Depth is the ATX heading level (1 for "# H", 2 for "## H", ...). HeadingText
// is the trimmed heading text with the leading '#'s and any trailing '#'s
// removed. StartLine is the 1-indexed line of the heading itself; EndLine is
// the 1-indexed last line of the section's full hierarchical span (the line
// before the next heading of equal-or-shallower depth, or the last line of the
// file) — this is the span get_source quotes, INCLUDING nested subsections.
//
// Body is the section's OWN direct text only: the lines strictly after the
// heading up to (but excluding) the next heading of ANY depth. It deliberately
// does NOT re-include nested subsection text, so the mention linker counts each
// reference against exactly one section (no parent/child double-counting).
//
// ParentIndex is the index (into the Sections slice returned by ParseDocument)
// of the nearest enclosing section of shallower depth, or -1 when the section
// is a top-level child of the Document.
type Section struct {
	HeadingText string
	Depth       int
	StartLine   int
	EndLine     int
	Body        string
	ParentIndex int
	// bodyEnd is the 1-indexed last line of this section's OWN direct content
	// (the line before the next heading of ANY depth). Internal; slices Body.
	bodyEnd int
}

// Document is the file-level node produced for one markdown file.
type Document struct {
	// RelPath is the repo-relative, slash-separated path of the markdown file.
	RelPath string
	// Title is the text of the first level-1 heading, or "" when the file has
	// none. Purely informational; never used for identity.
	Title string
	// LineCount is the total number of lines in the file (1-indexed EndLine of
	// the Document span).
	LineCount int
}

// ParseDocument parses one markdown file's content into a Document node plus a
// flat, declaration-ordered slice of Section nodes whose ParentIndex fields
// encode the heading-depth hierarchy. relPath is stored verbatim on the
// returned Document (callers pass a repo-relative slash path).
//
// The scanner is deliberately small and dependency-free:
//   - ATX headings only ("# ".."###### "); a line is a heading when, after
//     stripping up to 3 leading spaces, it begins with 1–6 '#' characters
//     followed by a space or end-of-line. Setext headings (=== / ---) are not
//     treated as headings (they are rare and ambiguous with horizontal rules).
//   - Fenced code blocks (``` or ~~~) are tracked so that a '#' inside a code
//     fence is NOT mistaken for a heading.
//
// Output is deterministic: sections are returned in source order and line
// spans are derived purely from line positions.
func ParseDocument(relPath string, content []byte) (Document, []Section) {
	lines := splitLines(string(content))
	doc := Document{RelPath: relPath, LineCount: len(lines)}

	var sections []Section
	// stack holds indices (into sections) of the currently-open ancestor
	// sections, strictly increasing in depth. Used to assign ParentIndex and
	// to close sections (set EndLine) when a shallower/equal heading arrives.
	var stack []int

	inFence := false
	var fenceMarker string

	closeTo := func(depth, endLine int) {
		// Close every open section whose depth >= the incoming heading depth.
		for len(stack) > 0 {
			top := stack[len(stack)-1]
			if sections[top].Depth < depth {
				break
			}
			sections[top].EndLine = endLine
			stack = stack[:len(stack)-1]
		}
	}

	for i, raw := range lines {
		lineNo := i + 1

		// Track fenced code blocks so headings inside them are ignored.
		if marker, ok := fenceMarkerOf(raw); ok {
			if !inFence {
				inFence = true
				fenceMarker = marker
			} else if marker == fenceMarker {
				inFence = false
				fenceMarker = ""
			}
			continue
		}
		if inFence {
			continue
		}

		depth, text, ok := atxHeading(raw)
		if !ok {
			continue
		}

		// The immediately-preceding section (in source order) ends its OWN
		// direct body on the line before this heading, regardless of depth.
		if n := len(sections); n > 0 {
			sections[n-1].bodyEnd = lineNo - 1
		}

		// The new heading closes any open sections of equal-or-greater depth;
		// they end on the line before this heading.
		closeTo(depth, lineNo-1)

		parent := -1
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}

		sections = append(sections, Section{
			HeadingText: text,
			Depth:       depth,
			StartLine:   lineNo,
			EndLine:     len(lines), // provisional; finalized on close / EOF
			ParentIndex: parent,
		})
		idx := len(sections) - 1
		stack = append(stack, idx)

		if depth == 1 && doc.Title == "" {
			doc.Title = text
		}
	}

	// Close every still-open section at EOF.
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		sections[top].EndLine = len(lines)
		stack = stack[:len(stack)-1]
	}

	// The last section in source order has no following heading; its own body
	// runs to EOF.
	if n := len(sections); n > 0 && sections[n-1].bodyEnd == 0 {
		sections[n-1].bodyEnd = len(lines)
	}

	// Populate Body from the section's OWN direct content only: the 1-indexed
	// line range [StartLine+1, bodyEnd] (lines strictly after the heading, up
	// to and including bodyEnd). This excludes nested subsection text so a
	// mention is attributed to exactly one section. Mapped to the 0-indexed
	// lines slice as [StartLine, bodyEnd).
	for k := range sections {
		s := &sections[k]
		var b strings.Builder
		for ln := s.StartLine; ln < s.bodyEnd && ln < len(lines); ln++ {
			b.WriteString(lines[ln])
			b.WriteByte('\n')
		}
		s.Body = b.String()
	}

	return doc, sections
}

// splitLines splits s on '\n', dropping a single trailing '\r' per line so
// CRLF files behave identically to LF. A trailing empty line produced by a
// final '\n' is dropped to keep LineCount equal to the visible line count.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	// Drop the trailing empty element from a final newline.
	if n := len(parts); n > 0 && parts[n-1] == "" {
		parts = parts[:n-1]
	}
	for i := range parts {
		parts[i] = strings.TrimSuffix(parts[i], "\r")
	}
	return parts
}

// atxHeading reports whether line is an ATX heading and, if so, returns its
// depth (1–6) and trimmed heading text.
func atxHeading(line string) (depth int, text string, ok bool) {
	// Up to 3 leading spaces are permitted before the '#'s (CommonMark).
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	j := i
	for j < len(line) && line[j] == '#' {
		j++
	}
	level := j - i
	if level < 1 || level > 6 {
		return 0, "", false
	}
	// A heading marker must be followed by a space or end-of-line.
	if j < len(line) && line[j] != ' ' {
		return 0, "", false
	}
	rest := strings.TrimSpace(line[j:])
	// Strip an optional closing run of '#'s (e.g. "## Title ##").
	rest = strings.TrimRight(rest, "#")
	rest = strings.TrimSpace(rest)
	return level, rest, true
}

// fenceMarkerOf reports whether line opens or closes a fenced code block and,
// if so, returns a normalized marker ("```" or "~~~") used to match the
// opening and closing fences. Up to 3 leading spaces are permitted.
func fenceMarkerOf(line string) (marker string, ok bool) {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	rest := line[i:]
	switch {
	case strings.HasPrefix(rest, "```"):
		return "```", true
	case strings.HasPrefix(rest, "~~~"):
		return "~~~", true
	}
	return "", false
}
