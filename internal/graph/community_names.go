// Layer 1 deterministic naming for Louvain communities.
//
// For each community we collect its member entity names (label, qualified
// name, source-file basename), tokenise on camelCase / snake_case / kebab /
// path-separators, and compute TF-IDF where each community is a "document".
// The top 1-3 distinguishing terms become the community's auto_name (e.g.
// "order-viewset", "data-sync-task"). The result is written back onto each
// CommunityResult and serialised as `communities[].auto_name` in graph.json.
//
// Stdlib only — no new dependencies.
package graph

import (
	"math"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// stopwords skipped during tokenisation. Generic source-code noise that would
// otherwise dominate TF-IDF on small communities.
var stopwords = map[string]bool{
	"":      true,
	"a":     true,
	"an":    true,
	"and":   true,
	"as":    true,
	"at":    true,
	"by":    true,
	"for":   true,
	"from":  true,
	"in":    true,
	"is":    true,
	"of":    true,
	"on":    true,
	"or":    true,
	"the":   true,
	"to":    true,
	"with":  true,
	"go":    true,
	"py":    true,
	"js":    true,
	"ts":    true,
	"tsx":   true,
	"jsx":   true,
	"java":  true,
	"rb":    true,
	"src":   true,
	"lib":   true,
	"pkg":   true,
	"main":  true,
	"init":  true,
	"new":   true,
	"impl":  true,
	"util":  true,
	"utils": true,
	"test":  true,
	"tests": true,
	"spec":  true,
	"specs": true,
	"id":    true,
	"py_":   true,
}

// tokenize splits an identifier-like string into lowercase tokens by
// camelCase boundaries, snake_case, kebab-case, dots, slashes, and any other
// non-alphanumeric run. Single-character tokens and stopwords are dropped.
func tokenize(s string) []string {
	if s == "" {
		return nil
	}
	// First, split on path separators / dots / dashes / underscores / spaces.
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		// Then split each field on camelCase boundaries.
		for _, sub := range splitCamel(f) {
			lower := strings.ToLower(sub)
			if len(lower) <= 1 {
				continue
			}
			if stopwords[lower] {
				continue
			}
			out = append(out, lower)
		}
	}
	return out
}

// splitCamel breaks "OrderViewSet" into ["Order", "View", "Set"], "HTTPServer"
// into ["HTTP", "Server"], and leaves "order" / "ORDER" untouched.
func splitCamel(s string) []string {
	if s == "" {
		return nil
	}
	runes := []rune(s)
	out := []string{}
	start := 0
	for i := 1; i < len(runes); i++ {
		prev, cur := runes[i-1], runes[i]
		// Lower -> Upper transition: foo|Bar
		if unicode.IsLower(prev) && unicode.IsUpper(cur) {
			out = append(out, string(runes[start:i]))
			start = i
			continue
		}
		// Letter -> Digit or Digit -> Letter transition: v2|migrate
		if unicode.IsLetter(prev) != unicode.IsLetter(cur) {
			out = append(out, string(runes[start:i]))
			start = i
			continue
		}
		// Acronym boundary: HTTPServer -> HTTP|Server (Upper followed by
		// Upper-then-Lower).
		if i+1 < len(runes) && unicode.IsUpper(prev) && unicode.IsUpper(cur) && unicode.IsLower(runes[i+1]) {
			out = append(out, string(runes[start:i]))
			start = i
			continue
		}
	}
	out = append(out, string(runes[start:]))
	return out
}

// communityCorpusEntry is the per-entity input to the naming pass.
type communityCorpusEntry struct {
	communityID   int
	name          string
	qualifiedName string
	sourceFile    string
}

// AssignCommunityNames computes a deterministic auto_name for every community
// in `results` using TF-IDF over the supplied entity corpus. Communities with
// no member tokens fall back to the legacy "community_<id>" label so callers
// can rely on auto_name being non-empty.
//
// The function mutates `results` in place. Order of `results` is preserved.
func AssignCommunityNames(results []CommunityResult, entities []Entity, communityOf map[string]int) {
	if len(results) == 0 {
		return
	}

	// Collect tokens per community. Token frequency inside a community
	// counts once per source occurrence (name + qname + basename are three
	// independent observations per entity).
	tokensByCommunity := make(map[int]map[string]int, len(results))
	for _, e := range entities {
		cid, ok := communityOf[e.ID]
		if !ok || cid < 0 {
			continue
		}
		bag := tokensByCommunity[cid]
		if bag == nil {
			bag = make(map[string]int)
			tokensByCommunity[cid] = bag
		}
		for _, t := range tokenize(e.Name) {
			bag[t]++
		}
		for _, t := range tokenize(e.QualifiedName) {
			bag[t]++
		}
		base := filepath.Base(e.SourceFile)
		// Strip extension so "orders.py" contributes "orders" not "orders" + "py".
		if ext := filepath.Ext(base); ext != "" {
			base = strings.TrimSuffix(base, ext)
		}
		for _, t := range tokenize(base) {
			bag[t]++
		}
	}

	// Document frequency: number of communities each token appears in.
	df := make(map[string]int)
	for _, bag := range tokensByCommunity {
		for tok := range bag {
			df[tok]++
		}
	}
	totalDocs := float64(len(tokensByCommunity))
	if totalDocs == 0 {
		// No tokens at all — fall back to legacy labels.
		for i := range results {
			results[i].AutoName = legacyName(results[i].ID)
		}
		return
	}

	for i := range results {
		cid := results[i].ID
		bag := tokensByCommunity[cid]
		if len(bag) == 0 {
			results[i].AutoName = legacyName(cid)
			continue
		}

		// Total term count for normalisation.
		total := 0
		for _, c := range bag {
			total += c
		}
		totalF := float64(total)

		type scored struct {
			term  string
			score float64
		}
		ranked := make([]scored, 0, len(bag))
		for term, count := range bag {
			tf := float64(count) / totalF
			// log((N+1)/(df+1)) + 1 — smooth IDF; handles single-community
			// corpora gracefully (idf collapses to 1, so TF ranks terms).
			idf := math.Log((totalDocs+1)/(float64(df[term])+1)) + 1
			ranked = append(ranked, scored{term, tf * idf})
		}
		// Deterministic ordering: score desc, then term asc.
		sort.Slice(ranked, func(a, b int) bool {
			if ranked[a].score != ranked[b].score {
				return ranked[a].score > ranked[b].score
			}
			return ranked[a].term < ranked[b].term
		})

		// Pick top 1-3 terms; cap at 3, but stop early if score collapses to 0.
		picked := make([]string, 0, 3)
		for _, s := range ranked {
			if s.score <= 0 {
				break
			}
			picked = append(picked, s.term)
			if len(picked) == 3 {
				break
			}
		}
		if len(picked) == 0 {
			results[i].AutoName = legacyName(cid)
			continue
		}
		// Conventional shape: 2 terms looks like "order-viewset"; if only
		// one strong term survives, use it alone; if three are tied, keep
		// all three.
		results[i].AutoName = strings.Join(picked, "-")
	}
}

func legacyName(cid int) string {
	// Match the pre-Layer-1 placeholder shape so the dashboard never sees
	// an empty string.
	b := strings.Builder{}
	b.WriteString("community_")
	// itoa is defined in the test helpers; re-implement locally to avoid
	// pulling in strconv just for one call.
	if cid == 0 {
		b.WriteString("0")
		return b.String()
	}
	neg := cid < 0
	n := cid
	if neg {
		n = -n
	}
	digits := make([]byte, 0, 8)
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	if neg {
		b.WriteByte('-')
	}
	for i := len(digits) - 1; i >= 0; i-- {
		b.WriteByte(digits[i])
	}
	return b.String()
}
