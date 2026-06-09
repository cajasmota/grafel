package ingest

import (
	"sort"
	"strings"
)

// minTokenLen is the minimum identifier-token length eligible for mention
// linking. Tokens shorter than this are skipped regardless of whether they
// match an entity name — short tokens (e.g. "id", "db", "ok") collide with
// far too many entity names and prose words to link with precision.
const minTokenLen = 4

// NameTarget is one resolvable code-entity name in the current index. ID is the
// stamped graph entity ID the MENTIONS edge will point at. Name is the exact
// token the document text must contain (a bare entity Name or its trailing
// qualified-name segment). Kind is retained for diagnostics/properties.
type NameTarget struct {
	Name string
	ID   string
	Kind string
}

// Mention is one resolved Section→entity link.
type Mention struct {
	// SectionIndex indexes into the []Section the caller linked.
	SectionIndex int
	// Token is the exact identifier token that matched.
	Token string
	// TargetID is the graph entity ID to point the MENTIONS edge at.
	TargetID string
	// TargetKind is the matched entity's kind (edge property, diagnostics).
	TargetKind string
}

// LinkMentions scans each section's body for identifier tokens that EXACTLY
// match a single code-entity name and returns one Mention per (section, token)
// pair. It is precision-first and deterministic.
//
// Precision rules (under-link rather than mislink — noisy links erode trust):
//   - Case-sensitive, whole-token match only. A token is a maximal run of
//     [A-Za-z0-9_]; "FooService" in prose matches an entity named exactly
//     "FooService" but the substring inside "MyFooServiceThing" does not.
//   - Tokens shorter than minTokenLen are skipped.
//   - Language keywords / very common words (skipWords) are skipped even if an
//     entity happens to share the name.
//   - AMBIGUOUS tokens are dropped: if a token resolves to more than one
//     DISTINCT entity ID, no edge is emitted (we cannot know which is meant).
//   - At most one Mention per (section, token): a token repeated within a
//     section yields a single edge.
//
// The targets index is built by IndexNames from the current graph entities.
func LinkMentions(sections []Section, targets map[string][]NameTarget) []Mention {
	var out []Mention
	for si := range sections {
		seen := map[string]bool{}
		for _, tok := range identifierTokens(sections[si].Body) {
			if seen[tok] {
				continue
			}
			if len(tok) < minTokenLen {
				continue
			}
			if skipWords[strings.ToLower(tok)] {
				continue
			}
			cands := targets[tok]
			if len(cands) == 0 {
				continue
			}
			// Ambiguity guard: require exactly one DISTINCT target ID.
			id := cands[0].ID
			distinct := true
			for _, c := range cands[1:] {
				if c.ID != id {
					distinct = false
					break
				}
			}
			if !distinct {
				continue
			}
			seen[tok] = true
			out = append(out, Mention{
				SectionIndex: si,
				Token:        tok,
				TargetID:     cands[0].ID,
				TargetKind:   cands[0].Kind,
			})
		}
	}
	// Deterministic ordering: by section, then token.
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].SectionIndex != out[b].SectionIndex {
			return out[a].SectionIndex < out[b].SectionIndex
		}
		return out[a].Token < out[b].Token
	})
	return out
}

// IndexNames builds the token→targets lookup used by LinkMentions from the
// provided (name, qualifiedName, id, kind) tuples. For each entity it indexes
// the bare Name and the entity's OWN simple name (the final segment of the
// qualified name). It deliberately does NOT index ancestor segments of a
// qualified name (e.g. the parent class of a method): doing so would map a
// class name to BOTH the class entity AND every one of its methods, which the
// ambiguity guard would then drop — under-linking the class. Names that are
// keywords, too short, or non-token shaped are not indexed (they could never
// match a linkable token anyway).
func IndexNames(tuples []NameTuple) map[string][]NameTarget {
	idx := map[string][]NameTarget{}
	add := func(name, id, kind string) {
		if !isLinkableName(name) {
			return
		}
		t := NameTarget{Name: name, ID: id, Kind: kind}
		// Deduplicate identical (name,id) entries.
		for _, ex := range idx[name] {
			if ex.ID == id {
				return
			}
		}
		idx[name] = append(idx[name], t)
	}
	for _, tu := range tuples {
		add(tu.Name, tu.ID, tu.Kind)
		if seg := lastSegment(tu.QualifiedName); seg != "" {
			add(seg, tu.ID, tu.Kind)
		}
	}
	// Sort each bucket by ID for deterministic ambiguity handling/output.
	for k := range idx {
		sort.SliceStable(idx[k], func(a, b int) bool { return idx[k][a].ID < idx[k][b].ID })
	}
	return idx
}

// NameTuple is the minimal projection of a graph entity the indexer feeds to
// IndexNames.
type NameTuple struct {
	Name          string
	QualifiedName string
	ID            string
	Kind          string
}

// isLinkableName reports whether name is eligible to be a mention target: it
// must be a single identifier token (no spaces/punctuation), at least
// minTokenLen long, and not a skip word.
func isLinkableName(name string) bool {
	if len(name) < minTokenLen {
		return false
	}
	if skipWords[strings.ToLower(name)] {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !isIdentByte(name[i]) {
			return false
		}
	}
	return true
}

// lastSegment returns the final identifier segment of a qualified name, split
// on common separators. e.g. "pkg/order.OrderService.placeOrder" -> "placeOrder".
// Returns "" when qn is empty or has no trailing segment.
func lastSegment(qn string) string {
	if qn == "" {
		return ""
	}
	repl := strings.NewReplacer("/", "\x00", ".", "\x00", "#", "\x00", ":", "\x00")
	raw := strings.Split(repl.Replace(qn), "\x00")
	for i := len(raw) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(raw[i]); s != "" {
			return s
		}
	}
	return ""
}

// identifierTokens extracts the distinct-position maximal identifier tokens
// from body text. A token is a maximal run of [A-Za-z0-9_]. Order is preserved.
func identifierTokens(body string) []string {
	var out []string
	start := -1
	for i := 0; i < len(body); i++ {
		if isIdentByte(body[i]) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			out = append(out, body[start:i])
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, body[start:])
	}
	return out
}

func isIdentByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// skipWords are tokens that must never be linked even when an entity shares the
// name: programming keywords across the languages archigraph indexes, plus a
// handful of extremely common English/prose words that frequently appear in
// documentation. Kept explicit and lowercase; matching is case-insensitive for
// the skip check only (entity matching itself stays case-sensitive).
var skipWords = func() map[string]bool {
	words := []string{
		// Cross-language keywords / reserved-ish identifiers.
		"abstract", "async", "await", "break", "case", "catch", "class",
		"const", "continue", "default", "defer", "delete", "else", "enum",
		"export", "extends", "false", "final", "finally", "func", "function",
		"goto", "import", "interface", "null", "none", "package", "private",
		"protected", "public", "return", "static", "struct", "super", "switch",
		"this", "throw", "throws", "true", "type", "typeof", "void", "while",
		"yield", "self", "from", "with", "match", "where", "select", "insert",
		"update", "table", "index", "value", "values",
		// Very common prose words that look like identifiers.
		"about", "also", "always", "because", "before", "both", "code",
		"data", "does", "done", "each", "into", "just", "like", "more",
		"most", "must", "name", "note", "only", "other", "over", "same",
		"some", "such", "than", "that", "their", "them", "then", "there",
		"these", "they", "this", "thus", "used", "uses", "using", "very",
		"what", "when", "which", "will", "with", "your", "here", "have",
		"file", "files", "test", "tests", "page", "section", "document",
	}
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}()
