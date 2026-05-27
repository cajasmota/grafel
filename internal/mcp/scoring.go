package mcp

import (
	"math"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/cajasmota/archigraph/internal/graph"
)

// BM25 standard parameters.
const (
	bm25K1 = 1.5
	bm25B  = 0.75
)

// Per-source BM25 weighting (multiplicative on the per-document term-frequency).
const (
	weightLabel     = 1.0
	weightFileStem  = 1.5
	weightPathDirs  = 0.8
	weightDocstring = 0.6
)

// docstringLimitChars caps docstring contribution to the first 200 characters.
const docstringLimitChars = 200

// stopWords is a small English stop list; only applied to docstrings (not
// to labels) per the brief.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "of": true,
	"in": true, "on": true, "at": true, "to": true, "for": true, "with": true,
	"is": true, "are": true, "be": true, "this": true, "that": true, "it": true,
	"as": true, "by": true, "from": true, "if": true, "but": true,
}

// docTerms holds the bag-of-words for one entity, with multi-source weighting
// already applied to the term frequencies.
type docTerms struct {
	tf     map[string]float64 // term -> weighted frequency
	length float64            // sum of weighted frequencies (acts as |d|)
}

// BM25Index is a per-repo BM25 index over entities, with multi-source weights.
type BM25Index struct {
	docs      []docTerms
	entities  []*graph.Entity
	df        map[string]int
	avgLen    float64
	totalDocs int
}

// BuildBM25 builds a BM25 index for a single graph document.
func BuildBM25(doc *graph.Document) *BM25Index {
	idx := &BM25Index{
		docs:     make([]docTerms, len(doc.Entities)),
		entities: make([]*graph.Entity, len(doc.Entities)),
		df:       make(map[string]int),
	}
	totalLen := 0.0
	for i := range doc.Entities {
		e := &doc.Entities[i]
		idx.entities[i] = e
		d := buildDocTerms(e)
		idx.docs[i] = d
		totalLen += d.length
		seen := make(map[string]bool, len(d.tf))
		for term := range d.tf {
			if seen[term] {
				continue
			}
			seen[term] = true
			idx.df[term]++
		}
	}
	idx.totalDocs = len(idx.entities)
	if idx.totalDocs > 0 {
		idx.avgLen = totalLen / float64(idx.totalDocs)
	}
	return idx
}

// buildDocTerms computes the weighted bag-of-words for a single entity.
func buildDocTerms(e *graph.Entity) docTerms {
	d := docTerms{tf: map[string]float64{}}
	add := func(s string, weight float64, isDocstring bool) {
		for _, t := range tokenize(s) {
			if isDocstring && stopWords[t] {
				continue
			}
			d.tf[t] += weight
			d.length += weight
		}
	}
	// label — index with subtokenization so prose queries can match identifiers.
	// The full lowercased identifier is added at full weightLabel; sub-tokens
	// are added at subtokenWeight (0.5×) so full-token matches outrank partials.
	addIdentifier := func(name string) {
		toks := tokenizeIdentifier(name)
		if len(toks) == 0 {
			return
		}
		// toks[0] is always the full lowercased identifier.
		full := toks[0]
		d.tf[full] += weightLabel
		d.length += weightLabel
		// Sub-tokens (toks[1:]) get reduced weight.
		subW := weightLabel * subtokenWeight
		for _, sub := range toks[1:] {
			d.tf[sub] += subW
			d.length += subW
		}
	}
	addIdentifier(e.Name)
	// file stem
	if e.SourceFile != "" {
		stem := strings.TrimSuffix(filepath.Base(e.SourceFile), filepath.Ext(e.SourceFile))
		add(stem, weightFileStem, false)
		// last 2 path dirs
		dir := filepath.Dir(e.SourceFile)
		dirs := []string{}
		for i := 0; i < 2 && dir != "." && dir != "/" && dir != ""; i++ {
			dirs = append(dirs, filepath.Base(dir))
			dir = filepath.Dir(dir)
		}
		add(strings.Join(dirs, " "), weightPathDirs, false)
	}
	// docstring (if any)
	if e.Properties != nil {
		if ds, ok := e.Properties["docstring"]; ok && ds != "" {
			if len(ds) > docstringLimitChars {
				ds = ds[:docstringLimitChars]
			}
			add(ds, weightDocstring, true)
		}
	}
	return d
}

// subtokenWeight is the TF multiplier applied to sub-tokens produced by
// tokenizeIdentifier. Full-token matches are always weighted at 1×; partial
// sub-token matches get 0.5× so they rank below exact-name matches.
const subtokenWeight = 0.5

// tokenizeIdentifier splits an identifier into its component sub-tokens and
// returns them deduplicated and lowercased, with the full lowercased identifier
// prepended so that exact-name queries can still outrank partial matches.
//
// Splitting rules (applied left-to-right in order):
//   - camelCase: uppercase letter NOT preceded by uppercase → word boundary
//     e.g. "unlockWithBiometrics" → ["unlock", "with", "biometrics"]
//   - snake_case: underscore separator
//     e.g. "soft_logout" → ["soft", "logout"]
//   - kebab-case: hyphen separator
//     e.g. "unlock-with-biometrics" → ["unlock", "with", "biometrics"]
//   - dot.case: period separator
//     e.g. "core.helper" → ["core", "helper"]
//   - digit boundaries: transition letter↔digit counts as a boundary
//     e.g. "oauth2Client" → ["oauth", "2", "client"]
//
// The full lowercased identifier is always the first element; sub-tokens are
// appended only if they differ from the full token and have length ≥ 2.
// Tokens are deduplicated; order is: full first, then sub-tokens in split order.
func tokenizeIdentifier(s string) []string {
	if s == "" {
		return nil
	}
	full := strings.ToLower(s)

	// Use the existing tokenize function which already handles camelCase,
	// snake_case, kebab-case, dot.case, and digit boundaries via the
	// non-letter/non-digit splitter after camelCase expansion.
	subs := tokenize(s)

	// Deduplicate: start with the full token, then append unique sub-tokens.
	seen := map[string]bool{full: true}
	out := []string{full}
	for _, sub := range subs {
		if !seen[sub] && len(sub) >= 2 {
			seen[sub] = true
			out = append(out, sub)
		}
	}
	return out
}

// tokenize lowercases, strips diacritics, and splits camelCase + snake_case
// into tokens. Tokens shorter than 2 chars are dropped.
func tokenize(s string) []string {
	if s == "" {
		return nil
	}
	// Strip diacritics by mapping non-ASCII letters down to their nearest base.
	// We use a simple approach: drop combining marks.
	cleaned := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, s)
	// Split camelCase by inserting a separator before each uppercase that
	// follows a lowercase or digit.
	var b strings.Builder
	var prev rune
	for _, r := range cleaned {
		if (unicode.IsUpper(r)) && (unicode.IsLower(prev) || unicode.IsDigit(prev)) {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
		prev = r
	}
	expanded := strings.ToLower(b.String())
	out := []string{}
	cur := strings.Builder{}
	flush := func() {
		if cur.Len() >= 2 {
			out = append(out, cur.String())
		}
		cur.Reset()
	}
	for _, r := range expanded {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// Hit is a scored entity returned from Search.
type Hit struct {
	Entity *graph.Entity
	Score  float64
	// Source records how this hit was surfaced for transparency: "bm25",
	// "semantic", or "bm25+semantic" after RRF fusion. Empty for legacy
	// BM25-only callers.
	Source string
}

// rrfK is the Reciprocal Rank Fusion constant. score = Σ 1/(rrfK + rank).
// 60 is the canonical value from Cormack et al.; no score normalization is
// applied across the two rankers (#461 / ADR-0019).
const rrfK = 60.0

// FuseRRF combines a BM25 ranking and a semantic ranking into a single ranked
// list via Reciprocal Rank Fusion. Rankings are passed already sorted best
// first. Entities are matched by pointer identity (both rankers index the same
// repo Doc). The returned hits carry the fused score and a Source tag.
func FuseRRF(bm25, semantic []Hit) []Hit {
	type agg struct {
		entity *graph.Entity
		score  float64
		inBM25 bool
		inSem  bool
	}
	order := []*graph.Entity{}
	byEntity := map[*graph.Entity]*agg{}
	get := func(e *graph.Entity) *agg {
		a, ok := byEntity[e]
		if !ok {
			a = &agg{entity: e}
			byEntity[e] = a
			order = append(order, e)
		}
		return a
	}
	for rank, h := range bm25 {
		a := get(h.Entity)
		a.score += 1.0 / (rrfK + float64(rank+1))
		a.inBM25 = true
	}
	for rank, h := range semantic {
		a := get(h.Entity)
		a.score += 1.0 / (rrfK + float64(rank+1))
		a.inSem = true
	}
	out := make([]Hit, 0, len(order))
	for _, e := range order {
		a := byEntity[e]
		src := "bm25"
		switch {
		case a.inBM25 && a.inSem:
			src = "bm25+semantic"
		case a.inSem:
			src = "semantic"
		}
		out = append(out, Hit{Entity: a.entity, Score: a.score, Source: src})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// Search runs a BM25 query and returns a sorted slice of hits, highest first.
// limit caps the result count; pass 0 for unlimited.
func (b *BM25Index) Search(query string, limit int) []Hit {
	if b == nil || b.totalDocs == 0 {
		return nil
	}
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}
	hits := make([]Hit, 0, b.totalDocs)
	for i, d := range b.docs {
		score := 0.0
		for _, t := range terms {
			tf, ok := d.tf[t]
			if !ok {
				continue
			}
			df := b.df[t]
			if df == 0 {
				continue
			}
			idf := math.Log(1.0 + (float64(b.totalDocs)-float64(df)+0.5)/(float64(df)+0.5))
			lenNorm := 1.0
			if b.avgLen > 0 {
				lenNorm = 1 - bm25B + bm25B*(d.length/b.avgLen)
			}
			score += idf * (tf * (bm25K1 + 1)) / (tf + bm25K1*lenNorm)
		}
		if score > 0 {
			hits = append(hits, Hit{Entity: b.entities[i], Score: score})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}
