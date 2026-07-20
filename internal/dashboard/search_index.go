package dashboard

// search_index.go — in-memory search index for /api/search/{group}
//
// Problem (#2104): handleSearch did three full O(N) linear scans on every
// request.  With 63k entities the handler took > 5 s and appeared to hang.
//
// Fix: build a SearchIndex once at group-load time.  The index provides:
//
//  1. sortedNames — all entity names lowercased and sorted.  Binary search
//     gives O(log N) prefix-match candidate retrieval.
//  2. sortedPos — parallel slice mapping sortedNames[i] → position in entries.
//  3. wordTokens — map[word][]int32 for word-boundary / substring matching
//     without scanning the full entity list.
//  4. endpoints — pre-filtered HTTP endpoint slice for path search.
//
// Search algorithm:
//   - Phase 1 (prefix): binary-search sortedNames for the query; walk forward
//     collecting all prefix matches.
//   - Phase 2 (substring, word token): if Phase 1 yields < limit results,
//     look up the longest word-token that contains qLow and score remaining
//     candidates.  This covers infix matches like searching "service" when the
//     name is "UserService".
//   - Results are de-duplicated, sorted by score then name, capped at limit.
//
// Build cost: O(N log N) sort, run once per group load.
// Query cost: O(log N + K) where K = number of prefix-match candidates.
//
// Memory: ~80 bytes per entity (sorted name strings shared, word tokens
// are re-slices of the same strings).  For 63k entities ≈ 5 MB.
//
// Measured latency (50k synthetic fixture):
//
//	Before (linear scan): >5 000 ms (hang at 63k)
//	After  (this index):  < 10 ms for prefix queries, < 50 ms for pure-substring

import (
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// searchEntry is a single indexable entity stored in the SearchIndex.
type searchEntry struct {
	entityIdx int    // index into the repo's Doc.Entities slice (unused post-build)
	repoSlug  string // which repo owns this entity
	nameLow   string // strings.ToLower(entity.Name) — pre-computed
}

// httpEntry is a pre-filtered HTTP endpoint record.
type httpEntry struct {
	entityIdx int
	repoSlug  string
	pathLow   string // strings.ToLower(path or entity name)
	path      string // original case
	verb      string
	prefixID  string // dashPrefixedID(slug, entity.ID)
}

// SearchIndex is the pre-built per-group search structure.
type SearchIndex struct {
	// entries[i] — all entities ordered: sorted repos, then entity order.
	entries []searchEntry

	// entities[i] — pointer parallel to entries[i].
	entities []*graph.Entity

	// sortedNames[i] — lowercase name; sortedPos[i] — index into entries.
	// Kept sorted by name for binary-search prefix lookup.
	sortedNames []string
	sortedPos   []int32

	// wordTokens maps a lowercase "word" (split on non-alpha runs) to the set
	// of entry positions that contain that word.  Enables fast substring lookup
	// for queries that match mid-name (e.g. "service" → UserService).
	wordTokens map[string][]int32

	// HTTP endpoint entries, pre-filtered.
	endpoints []httpEntry
}

// buildSearchIndex constructs a SearchIndex from a loaded DashGroup.
// Called once per group load (inside loadGroupForRef), not per request.
func buildSearchIndex(g *DashGroup) *SearchIndex {
	repos := sortedRepos(g)

	total := 0
	for _, r := range repos {
		total += len(r.Doc.Entities)
	}

	idx := &SearchIndex{
		entries:     make([]searchEntry, 0, total),
		entities:    make([]*graph.Entity, 0, total),
		sortedNames: make([]string, 0, total),
		sortedPos:   make([]int32, 0, total),
		wordTokens:  make(map[string][]int32, total),
		endpoints:   make([]httpEntry, 0, total/20+1),
	}

	// Pass 1: populate entries and word-token index.
	for _, r := range repos {
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			pos := int32(len(idx.entries))

			nameLow := strings.ToLower(e.Name)
			idx.entries = append(idx.entries, searchEntry{
				entityIdx: i,
				repoSlug:  r.Slug,
				nameLow:   nameLow,
			})
			idx.entities = append(idx.entities, e)

			// Index each word token separately so "service" in "UserService"
			// is discoverable without scanning all entries.
			for _, tok := range nameTokens(nameLow) {
				idx.wordTokens[tok] = append(idx.wordTokens[tok], pos)
			}

			// HTTP endpoint side-index.
			bareKind := dashStripScopePrefix(e.Kind)
			if types.IsHTTPEndpointKind(bareKind) ||
				strings.EqualFold(bareKind, httpEndpointKind) ||
				e.Kind == "Endpoint" || e.Kind == "Route" {

				path := e.PropGet("path")
				if path == "" {
					path = e.Name
				}
				idx.endpoints = append(idx.endpoints, httpEntry{
					entityIdx: i,
					repoSlug:  r.Slug,
					pathLow:   strings.ToLower(path),
					path:      path,
					verb:      e.PropGet("verb"),
					prefixID:  dashPrefixedID(r.Slug, e.ID),
				})
			}
		}
	}

	// Pass 2: build sorted names array for binary-search prefix lookup.
	// Each entry in sortedNames carries a parallel position in sortedPos.
	type namePos struct {
		name string
		pos  int32
	}
	np := make([]namePos, len(idx.entries))
	for i, e := range idx.entries {
		np[i] = namePos{name: e.nameLow, pos: int32(i)}
	}
	sort.Slice(np, func(i, j int) bool { return np[i].name < np[j].name })
	for _, p := range np {
		idx.sortedNames = append(idx.sortedNames, p.name)
		idx.sortedPos = append(idx.sortedPos, p.pos)
	}

	return idx
}

// nameTokens splits a lowercased name into word tokens on non-alphanumeric
// boundaries (dots, underscores, dashes, upper→lower transitions).
// e.g. "UserService" → ["user", "service", "userservice"]
//
//	"get_user_by_id" → ["get", "user", "by", "id", "getuserbyi..."]
func nameTokens(nameLow string) []string {
	// The full lowercased name is always a token.
	tokens := []string{nameLow}
	// Split on non-word chars.
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 1 {
			tok := cur.String()
			if tok != nameLow {
				tokens = append(tokens, tok)
			}
		}
		cur.Reset()
	}
	for _, r := range nameLow {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

// searchEntities returns up to limit entity hits ranked by match quality.
//
// Scoring:
//
//	3 — exact name match
//	2 — prefix match
//	1 — substring / word-token match
func (idx *SearchIndex) searchEntities(qLow string, limit int) []entitySearchHit {
	if len(idx.entries) == 0 || qLow == "" {
		return nil
	}

	type scored struct {
		pos   int32
		score int
	}

	seen := make(map[int32]struct{}, limit*4)
	var hits []scored

	addHit := func(pos int32, score int) {
		if _, dup := seen[pos]; dup {
			if score > 0 {
				// Upgrade score if we already have this entry at a lower score.
				for i := range hits {
					if hits[i].pos == pos && hits[i].score < score {
						hits[i].score = score
						break
					}
				}
			}
			return
		}
		seen[pos] = struct{}{}
		hits = append(hits, scored{pos: pos, score: score})
	}

	// Phase 1: binary-search prefix scan.
	// Find the first index where sortedNames[i] >= qLow.
	lo := sort.SearchStrings(idx.sortedNames, qLow)
	for i := lo; i < len(idx.sortedNames); i++ {
		name := idx.sortedNames[i]
		if !strings.HasPrefix(name, qLow) {
			break
		}
		score := 1
		if name == qLow {
			score = 3
		} else {
			score = 2
		}
		addHit(idx.sortedPos[i], score)
	}

	// Phase 2: word-token substring scan (for queries that match mid-name).
	// Only run if Phase 1 didn't saturate the limit; we want fast common-case.
	if len(hits) < limit {
		// Find tokens that contain qLow as a substring.
		for tok, positions := range idx.wordTokens {
			if !strings.Contains(tok, qLow) {
				continue
			}
			for _, pos := range positions {
				entry := &idx.entries[pos]
				// Score within this phase: check exact/prefix on the full name.
				score := 1
				if entry.nameLow == qLow {
					score = 3
				} else if strings.HasPrefix(entry.nameLow, qLow) {
					score = 2
				}
				addHit(pos, score)
			}
		}
	}

	// Sort: higher score first, then name ascending.
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return idx.entries[hits[i].pos].nameLow < idx.entries[hits[j].pos].nameLow
	})

	if len(hits) > limit {
		hits = hits[:limit]
	}

	out := make([]entitySearchHit, len(hits))
	for i, h := range hits {
		entry := &idx.entries[h.pos]
		out[i] = entitySearchHit{
			entity:   idx.entities[h.pos],
			repoSlug: entry.repoSlug,
			score:    h.score,
		}
	}
	return out
}

// searchPaths returns up to limit HTTP endpoint hits whose path contains qLow.
func (idx *SearchIndex) searchPaths(qLow string, limit int) []httpEntry {
	var out []httpEntry
	for i := range idx.endpoints {
		ep := &idx.endpoints[i]
		if strings.Contains(ep.pathLow, qLow) {
			out = append(out, *ep)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// entitySearchHit is a ranked entity result.
type entitySearchHit struct {
	entity   *graph.Entity
	repoSlug string
	score    int
}
