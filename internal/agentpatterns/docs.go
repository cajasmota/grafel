// Phase 6 markdown generation for /generate-docs (ADR-0018).
//
// For each approved (is_candidate=false) Pattern, RenderMarkdown emits a
// doc at <docsRoot>/<category>/<pattern-id>.md following the house style
// of docs/quality/README.md and docs/specs/repair-trust-model.md.
//
// All code identifiers in headings MUST be wrapped in backticks per
// ADR-0007's slug-collision rule. CheckBacktickConvention enforces this
// invariant; CI may run it against the generated tree to fail builds when
// a pattern doc drifts.
package agentpatterns

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// RelatedPattern is a sibling pattern surfaced under "Related patterns".
// Edge is the relationship kind (SUPERSEDES, CO_APPLIES_WITH, PREREQUISITE).
type RelatedPattern struct {
	ID      string
	Trigger string
	Edge    string
}

// MarkdownInput is the data the renderer needs beyond the Pattern struct
// itself: the exemplar entities resolved to (file, line-range) tuples and
// the related patterns derived from outgoing graph edges. Both are
// populated by the /generate-docs coordinator using the existing MCP
// surface (archigraph_describe / archigraph_related) and passed in here.
type MarkdownInput struct {
	Pattern         Pattern
	ExemplarRefs    []ExemplarRef
	RelatedPatterns []RelatedPattern
}

// ExemplarRef is a resolved reference to an exemplar entity.
type ExemplarRef struct {
	EntityName string
	FilePath   string
	StartLine  int
	EndLine    int
}

// DocPathFor returns the canonical relative path for a pattern's doc:
// <category>/<id>.md.
func DocPathFor(p Pattern) string {
	return filepath.Join(string(p.Category), p.ID+".md")
}

// RenderMarkdown returns the Phase 6 markdown for one pattern. Returns
// the empty string + nil error when the pattern should be skipped
// (is_candidate=true).
func RenderMarkdown(in MarkdownInput) (string, error) {
	p := in.Pattern
	if p.IsCandidate {
		return "", nil
	}

	var b strings.Builder

	// Title — backtick code identifiers per ADR-0007.
	fmt.Fprintf(&b, "# %s\n\n", backtickCodeIdentifiers(p.Trigger.NaturalLanguage))

	// Front-matter block.
	fmt.Fprintf(&b, "- **Status**: Active\n")
	fmt.Fprintf(&b, "- **Category**: %s\n", p.Category)
	lastApplied := "never"
	if p.LastApplied > 0 {
		lastApplied = time.Unix(p.LastApplied, 0).UTC().Format("2006-01-02")
	}
	fmt.Fprintf(&b, "- **Confidence**: %.2f (%d observations, last applied %s)\n\n",
		p.Confidence, p.Observations, lastApplied)

	// When to use.
	fmt.Fprintf(&b, "## When to use\n\n%s\n\n", backtickCodeIdentifiers(p.Trigger.NaturalLanguage))

	// Recipe.
	if len(p.Steps) > 0 {
		fmt.Fprintf(&b, "## Recipe\n\n")
		for i, step := range p.Steps {
			fmt.Fprintf(&b, "%d. %s\n", i+1, backtickCodeIdentifiers(step))
		}
		b.WriteString("\n")
	}

	// Exemplars.
	if len(in.ExemplarRefs) > 0 {
		fmt.Fprintf(&b, "## Exemplars\n\n")
		fmt.Fprintf(&b, "| Entity | File | Lines |\n")
		fmt.Fprintf(&b, "|---|---|---|\n")
		for _, ex := range in.ExemplarRefs {
			lines := "—"
			if ex.StartLine > 0 {
				if ex.EndLine > 0 && ex.EndLine != ex.StartLine {
					lines = fmt.Sprintf("%d-%d", ex.StartLine, ex.EndLine)
				} else {
					lines = fmt.Sprintf("%d", ex.StartLine)
				}
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s |\n",
				ex.EntityName, ex.FilePath, lines)
		}
		b.WriteString("\n")
	}

	// Anti-patterns (public only).
	publicAnti := make([]AntiPattern, 0, len(p.AntiPatterns))
	for _, ap := range p.AntiPatterns {
		if ap.Private {
			continue
		}
		publicAnti = append(publicAnti, ap)
	}
	if len(publicAnti) > 0 {
		fmt.Fprintf(&b, "## Anti-patterns\n\n")
		for _, ap := range publicAnti {
			fmt.Fprintf(&b, "- **Don't**: %s\n", backtickCodeIdentifiers(ap.DoNot))
			fmt.Fprintf(&b, "  - **Reason**: %s\n", backtickCodeIdentifiers(ap.Reason))
		}
		b.WriteString("\n")
	}

	// Related patterns.
	if len(in.RelatedPatterns) > 0 {
		fmt.Fprintf(&b, "## Related patterns\n\n")
		// Sort for determinism.
		sorted := append([]RelatedPattern(nil), in.RelatedPatterns...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
		for _, rel := range sorted {
			cat, id := splitRelID(rel)
			fmt.Fprintf(&b, "- [`%s`](../%s/%s.md) (via %s)\n",
				rel.Trigger, cat, id, rel.Edge)
		}
		b.WriteString("\n")
	}

	return b.String(), nil
}

// WriteMarkdown writes the rendered doc for one pattern at:
//
//	<docsRoot>/<category>/<pattern-id>.md
//
// Returns the absolute path written (or "" if skipped).
func WriteMarkdown(docsRoot string, in MarkdownInput) (string, error) {
	md, err := RenderMarkdown(in)
	if err != nil {
		return "", err
	}
	if md == "" {
		return "", nil
	}
	rel := DocPathFor(in.Pattern)
	full := filepath.Join(docsRoot, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(md), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", full, err)
	}
	return full, nil
}

// DocURLFor returns the documentation_url value the /generate-docs
// coordinator should write back via action=refine (Phase 5).
func DocURLFor(p Pattern) string {
	return "docs/patterns/" + DocPathFor(p)
}

// ---------------------------------------------------------------------------
// ADR-0007 backtick-convention linter
// ---------------------------------------------------------------------------

// headingLine matches markdown ATX headings ("# Foo", "## Bar", ...).
var headingLine = regexp.MustCompile(`(?m)^(#{1,6})\s+(.*)$`)

// codeIdentifierPattern matches tokens that look like code identifiers we
// expect to be backticked. The intent is conservative — only flag tokens
// the indexer would obviously emit as entity names (CamelCase, snake_case
// with parens, ALLCAPS_WITH_UNDERSCORES, dotted.paths.with_calls()).
var codeIdentifierPattern = regexp.MustCompile(
	`(` +
		`\b[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)+\([^\)]*\)|` + // dotted call: foo.Bar(), pkg.Foo.bar(x)
		`\b[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)+\b|` + // dotted ident: foo.Bar, pkg.Foo.bar
		`\b[A-Za-z_][A-Za-z0-9_]*\([^\)]*\)|` + // bare call: foo(), foo(a, b)
		`\b[A-Z][a-z]+(?:[A-Z][a-z0-9]+){2,}\b|` + // CamelCase with ≥3 segments
		`\b[a-z]+(?:_[a-z0-9]+){2,}\b|` + // snake_case with ≥3 segments
		`\b[A-Z]{2,}_[A-Z0-9_]+\b` + // SCREAMING_SNAKE
		`)`,
)

// CheckBacktickConvention scans the markdown text and returns one error
// per heading line where a code-identifier-looking token appears outside
// backticks. Empty result means the doc is compliant.
//
// The intent is a CI gate: generated pattern docs must wrap every code
// identifier in headings in backticks (ADR-0007's slug-collision rule).
func CheckBacktickConvention(markdown string) []string {
	var violations []string
	for _, match := range headingLine.FindAllStringSubmatchIndex(markdown, -1) {
		headingText := markdown[match[4]:match[5]]
		stripped := stripCodeSpans(headingText)
		for _, tokenIdx := range codeIdentifierPattern.FindAllStringIndex(stripped, -1) {
			token := stripped[tokenIdx[0]:tokenIdx[1]]
			violations = append(violations,
				fmt.Sprintf("heading %q contains un-backticked code identifier %q", headingText, token))
		}
	}
	return violations
}

// CheckBacktickConventionDir walks docsRoot, applies CheckBacktickConvention
// to every *.md file, and returns a map of relative-path → violations. An
// empty map means the tree is compliant.
func CheckBacktickConventionDir(docsRoot string) (map[string][]string, error) {
	result := map[string][]string{}
	err := filepath.Walk(docsRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(p) != ".md" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		violations := CheckBacktickConvention(string(data))
		if len(violations) > 0 {
			rel, _ := filepath.Rel(docsRoot, p)
			result[rel] = violations
		}
		return nil
	})
	return result, err
}

// stripCodeSpans removes inline backtick spans so the regex only sees
// un-protected text.
var codeSpan = regexp.MustCompile("`[^`]*`")

func stripCodeSpans(s string) string {
	return codeSpan.ReplaceAllString(s, "")
}

// backtickCodeIdentifiers walks the supplied prose and wraps every match
// of codeIdentifierPattern that is NOT already inside a backtick span in
// backticks. Used by the renderer so authors of pattern.steps don't have
// to manually backtick everything.
func backtickCodeIdentifiers(s string) string {
	// Build a list of protected (already-backticked) spans so we don't
	// double-wrap.
	protected := codeSpan.FindAllStringIndex(s, -1)
	inProtected := func(start, end int) bool {
		for _, span := range protected {
			if start >= span[0] && end <= span[1] {
				return true
			}
		}
		return false
	}
	var b strings.Builder
	last := 0
	for _, match := range codeIdentifierPattern.FindAllStringIndex(s, -1) {
		if inProtected(match[0], match[1]) {
			continue
		}
		b.WriteString(s[last:match[0]])
		b.WriteByte('`')
		b.WriteString(s[match[0]:match[1]])
		b.WriteByte('`')
		last = match[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// categoryFromID — Related patterns don't carry their category in the
// RelatedPattern struct (we only have ID + Trigger + Edge from the
// outgoing-edge resolver). We could fix that by enriching the struct, but
// for now we default to the same category as the source pattern. The
// /generate-docs coordinator that constructs the edge-resolution layer
// can override by passing fully-qualified IDs of the form
// "<category>/<id>" — handled here.
func categoryFromID(rel RelatedPattern) string {
	cat, _ := splitRelID(rel)
	return cat
}

// splitRelID returns (category, id) from a RelatedPattern. The ID may be
// either bare ("middleware00000") or fully-qualified ("code/middleware00000").
// Bare IDs default to category "code" — callers that know the real
// category should pass the fully-qualified form.
func splitRelID(rel RelatedPattern) (category, id string) {
	if i := strings.Index(rel.ID, "/"); i > 0 {
		return rel.ID[:i], rel.ID[i+1:]
	}
	return "code", rel.ID
}
