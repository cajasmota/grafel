package mcp

import (
	"math"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/types"
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
	// #2666 — discriminator literal terms are mixed in at a modest weight so
	// queries like "checklistType 2" surface the enclosing entity. We weight
	// below docstrings (which are author-curated prose) but above stop-word
	// boilerplate. Both the var name and the literal value contribute.
	weightDiscriminator = 0.5
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
// already applied to the term frequencies. It is a TRANSIENT structure built by
// buildDocTerms and folded into the postings index during BuildBM25 — it is
// never retained in the resident BM25Index (#5871 L1 compaction).
type docTerms struct {
	tf     map[string]float64 // term -> weighted frequency
	length float64            // sum of weighted frequencies (acts as |d|)
}

// posting is one entry in an inverted-index postings list: the document index
// plus the weighted term frequency of that term in that document, folded inline
// (#5871 L1 compaction). Storing tf here — 8 bytes/posting — eliminates the
// 427k per-entity `tf map[string]float64` that dominated the resident index
// (~245 MB of Go-map overhead on the corpus). Search reads tf directly from the
// posting with no second lookup.
type posting struct {
	doc int32
	tf  float32
}

// BM25Index is a per-repo BM25 index over entities, with multi-source weights.
type BM25Index struct {
	// entities maps a doc index (postings/docLen position) to the entity's
	// VECTOR INDEX — the int32 position of that entity in the graph.fb / Document
	// vector order (memory epic #5850, Path P PR3b / issue #5871 L4). It is the
	// identity permutation today (doc index i == vector index i, since every
	// entity is indexed with no skips), but is stored explicitly so Search never
	// assumes identity and so the index retains NO `*graph.Entity`. Retaining
	// 427k live entity pointers here would re-pin ~the whole Document (~608 MB on
	// the corpus) and defeat the ADR-0027 mmap flip; the vector index is 4 bytes
	// and resolves to an entity on demand at Search-return time via resolve.
	entities []int32
	// resolve turns a doc's vector index into a freshly materialized
	// *graph.Entity at Search-return time (never a retained pointer). It is set by
	// the builder/getter, NOT stored per-doc, so it costs one closure regardless
	// of corpus size:
	//   - flag-OFF / nil / retired Reader: BuildBM25 wires a closure over the live
	//     Document that heap-copies doc.Entities[i] (GC-safe, no mmap read).
	//   - flag-ON (resident, non-retired Reader): getBM25 wires it to the repo's
	//     readerMu-guarded LabelIndex.at, which materializes the base entity from
	//     the mmap Reader + merges the group-algo overlay side-table on demand.
	// A nil resolve yields no entities (defensive; every production/build path
	// sets it).
	resolve   func(vectorIdx int32) *graph.Entity
	docLen    []float32 // per-doc |d| (sum of weighted frequencies), by doc index
	avgLen    float64
	totalDocs int

	// terms interns each unique term string to a dense uint32 ID (#5871 L2 term
	// interning). Pre-L2, postings was keyed by the raw term string — U≈283,586
	// unique terms on the corpus, each retaining its own string header (16
	// bytes) plus Go map-of-string overhead, ~7-9 MB resident. Interning stores
	// each term string exactly ONCE (here, as a map key) and everything
	// downstream (postings) references it by a 4-byte ID instead of copying the
	// string. IDs are assigned densely (0..len(terms)-1) in first-seen order
	// during the build, so they double as the index into postings.
	terms map[string]uint32
	// postings is an inverted index: term ID -> sorted list of {doc, tf}
	// entries for the documents that contain that term (#3923 postings, #5871
	// L1 tf fold-in, #5871 L2 term interning). Indexed by dense term ID
	// (postings[id] == the postings list for the term whose ID is id) rather
	// than keyed by the term string — a slice indexed by a dense 0..U-1 ID is
	// denser than a map keyed by string OR by uint32, since it needs no hash
	// buckets at all. Search consults it to visit ONLY the documents that
	// contain at least one query term instead of scanning all totalDocs
	// documents, and reads the weighted tf directly from each posting. For the
	// common case where a query term occurs in a small fraction of the corpus
	// this keeps Search at O(Σ df(term)) — sublinear in corpus size. The
	// document frequency df(term) is exactly len(postings[id]); it is no
	// longer stored separately.
	postings [][]posting
}

// intern assigns (or looks up) the dense uint32 ID for a term string, growing
// idx.postings to match. Both build paths (BuildBM25 and BuildBM25FromReader)
// call this in the SAME per-document, per-term iteration order (doc-term maps
// are visited via `for term, tf := range d.tf`, which is non-deterministic per
// call — but only the ASSIGNMENT of new IDs during a single build matters for
// that build's own internal consistency; cross-build DeepEqual parity holds
// because both paths tokenize the SAME entities in the SAME doc order and fold
// each doc's terms into postings identically, so the resulting term->ID
// assignment is a function of (doc order, per-doc term set) alone — the two
// builds see byte-identical inputs, hence byte-identical interning).
func (idx *BM25Index) intern(term string) uint32 {
	if id, ok := idx.terms[term]; ok {
		return id
	}
	id := uint32(len(idx.terms))
	idx.terms[term] = id
	idx.postings = append(idx.postings, nil)
	return id
}

// foldDocTerms interns doc i's terms and appends its postings, in a
// deterministic order shared by BOTH build paths. d.tf is a Go map, so
// `range d.tf` visits keys in randomized order per call; if two terms are
// first seen (across the whole build) in the same document, the order they
// are interned in determines which gets the lower ID. Sorting each
// document's term keys before interning fixes that order to a pure function
// of (doc order, per-doc term set) — which is identical between BuildBM25 and
// BuildBM25FromReader (same entities, same vector order, same tokenizer) — so
// the two paths intern every term under the SAME ID and produce byte-equal
// `terms` dicts and `postings` slices (extends the #5871 PR3b parity
// contract to the interned structure).
func (idx *BM25Index) foldDocTerms(doc int32, d docTerms) {
	keys := make([]string, 0, len(d.tf))
	for term := range d.tf {
		keys = append(keys, term)
	}
	sort.Strings(keys)
	for _, term := range keys {
		id := idx.intern(term)
		idx.postings[id] = append(idx.postings[id], posting{doc: doc, tf: float32(d.tf[term])})
	}
}

// BuildBM25 builds a BM25 index for a single graph document. This is the
// flag-OFF / nil-Reader / retired-Reader path (getBM25); it is also the parity
// baseline the Reader-sourced BuildBM25FromReader must match byte-for-byte
// (same postings / docLen / tokens). It stores only the per-doc vector index
// (idx.entities[i] == i) and wires resolve to a live-Document heap-copy closure,
// so it retains NO `*graph.Entity` of its own — the Document it closes over is
// lr.Doc, already retained on the flag-OFF path (issue #5871 L4).
func BuildBM25(doc *graph.Document) *BM25Index {
	idx := &BM25Index{
		entities: make([]int32, len(doc.Entities)),
		docLen:   make([]float32, len(doc.Entities)),
		terms:    make(map[string]uint32),
	}
	totalLen := 0.0
	for i := range doc.Entities {
		e := &doc.Entities[i]
		idx.entities[i] = int32(i) // doc index == vector index (identity, no skips)
		// buildDocTerms produces a TRANSIENT weighted bag (a per-doc map); we
		// fold it into the postings index and let it be GC'd — only the postings
		// list (with tf inline) and the scalar docLen survive as resident state.
		d := buildDocTerms(e)
		idx.docLen[i] = float32(d.length)
		totalLen += d.length
		// foldDocTerms interns each of this doc's terms (assigning a dense ID on
		// first sight, in sorted order for cross-build determinism, #5871 L2)
		// and appends (doc, tf) to that ID's postings list; because i increases
		// monotonically the postings lists stay sorted by doc index by
		// construction (Search relies on this for the ascending tie-break and
		// df(term) == len(postings[id])).
		idx.foldDocTerms(int32(i), d)
	}
	idx.totalDocs = len(idx.entities)
	if idx.totalDocs > 0 {
		idx.avgLen = totalLen / float64(idx.totalDocs)
	}
	// Resolve a vector index to a fresh heap copy of the live Document row. The
	// closure captures doc (== lr.Doc, retained anyway on the flag-OFF path), NOT
	// a slice of entity pointers — so the index adds no per-entity retention. The
	// returned pointer is a fresh heap copy (like getByID/LabelIndex.at), so
	// callers keying on Entity.ID are behavior-neutral vs the old aliased pointer.
	idx.resolve = func(vi int32) *graph.Entity {
		if vi < 0 || int(vi) >= len(doc.Entities) {
			return nil
		}
		e := doc.Entities[vi] // heap copy — a fresh pointer, not an alias into Doc
		return &e
	}
	return idx
}

// BuildBM25FromReader builds a BM25 index by iterating the resident mmap Reader
// instead of a materialized graph.Document (memory epic #5850, Path P PR3b /
// issue #5871 L4). This is the FLIP PREREQUISITE: post-flip lr.Doc is emptied,
// so BuildBM25(lr.Doc) would build an EMPTY index; the Reader still holds every
// row, so we tokenize from it.
//
// Byte-parity with BuildBM25(doc): the Reader holds the same rows in the same
// vector order the Document loader produces, and graph.MaterializeEntity decodes
// each row byte-identically to the Document's entity. Feeding the materialized
// entity through the SAME buildDocTerms yields the SAME weighted bag, so the
// postings (doc, tf), docLen, avgLen and totalDocs are byte-equal to the
// Document-sourced build over the same graph.fb (TestBM25ReaderBuildParity_PR3b).
//
// No-retention contract: each entity is materialized ONLY to tokenize it and is
// discarded at the end of the loop iteration — it is a local value, never stored
// in the index. The index keeps only the int32 vector index per doc, so building
// from the Reader does NOT re-pin the ~608 MB of entities the flip is dropping.
// resolve is NOT set here (this function does not know the repo's
// readerMu/LabelIndex); getBM25 wires resolve to the readerMu-guarded
// LabelIndex.at after this returns.
//
// Callers on the wired handler path MUST hold the owning repo's readerMu around
// this call (the mmap is dereferenced per row), exactly like
// buildAdjacencyFromReader in getAdjacency.
func BuildBM25FromReader(r *fbreader.Reader) *BM25Index {
	n := 0
	if r != nil {
		n = r.EntityCount()
	}
	idx := &BM25Index{
		entities: make([]int32, n),
		docLen:   make([]float32, n),
		terms:    make(map[string]uint32),
	}
	totalLen := 0.0
	for i := 0; i < n; i++ {
		// Materialize the entity TRANSIENTLY: buildDocTerms reads Name /
		// SourceFile / PropLookup(docstring, discriminators) / Kind to tokenize.
		// e is a local value that goes out of scope (and becomes GC-eligible) at
		// the end of this iteration — it is never retained by the index.
		e := graph.MaterializeEntity(r, i)
		idx.entities[i] = int32(i) // doc index == reader vector index (identity)
		d := buildDocTerms(&e)
		idx.docLen[i] = float32(d.length)
		totalLen += d.length
		// Deterministic interning order (#5871 L2), see foldDocTerms.
		idx.foldDocTerms(int32(i), d)
	}
	idx.totalDocs = n
	if n > 0 {
		idx.avgLen = totalLen / float64(n)
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
	if e.PropLen() > 0 {
		if ds, ok := e.PropLookup("docstring"); ok && ds != "" {
			if len(ds) > docstringLimitChars {
				ds = ds[:docstringLimitChars]
			}
			add(ds, weightDocstring, true)
		}
		// #2666 — discriminator pairs: stamp by the extractors as a
		// comma-separated "var=value,var2=value2" string in Properties
		// (mirror of the DISCRIMINATES_ON edges). Mix both the variable
		// name and the literal value into the doc terms at a modest weight
		// so queries like "checklistType 2" rank the enclosing entity
		// higher. Tokenize via addIdentifier so camelCase / snake_case
		// vars still split into sub-tokens; literals go through tokenize
		// directly (numbers/strings need digit-boundary splitting).
		if pairs, ok := e.PropLookup("discriminators"); ok && pairs != "" {
			for _, pair := range strings.Split(pairs, ",") {
				eq := strings.IndexByte(pair, '=')
				if eq <= 0 || eq >= len(pair)-1 {
					continue
				}
				varName := pair[:eq]
				literal := pair[eq+1:]
				// Variable name: use the identifier-aware path so the full
				// camelCase token plus its sub-tokens are all indexed.
				for _, t := range tokenizeIdentifier(varName) {
					d.tf[t] += weightDiscriminator
					d.length += weightDiscriminator
				}
				// Literal value: plain tokenize. Numeric literals like "2"
				// would normally fall below the min-token-length of 2, so
				// add the raw literal as well to make exact-number queries
				// score.
				for _, t := range tokenize(literal) {
					d.tf[t] += weightDiscriminator
					d.length += weightDiscriminator
				}
				if literal != "" {
					raw := strings.ToLower(literal)
					d.tf[raw] += weightDiscriminator
					d.length += weightDiscriminator
				}
			}
		}
	}
	// #5782 (ADR-0025) ask #5: a SCOPE.ChannelBinding's Name is just the bare
	// channel value (e.g. "orders-out") — nothing in the indexed text says
	// "channel", "binding", "topic", or names its direction/bound topic, so a
	// natural-language bm25 query ("what channel bindings connect config to
	// kafka topics") never surfaces it even though the entity exists (it was
	// only reachable via search=substring + kind_filter=SCOPE.ChannelBinding).
	// Fold the channel/direction/topic/connector properties in as additional
	// searchable text (docstring-weighted: author-adjacent metadata, not an
	// identifier), plus the literal words "channel"/"binding" so the kind
	// itself is a matchable term.
	if e.Kind == string(types.EntityKindChannelBinding) {
		add("channel binding", weightDocstring, false)
		add(e.PropGet("direction"), weightDocstring, false)
		add(e.PropGet("topic"), weightDocstring, false)
		add(e.PropGet("connector"), weightDocstring, false)
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
// first. Entities are matched by ID (not pointer identity): the two rankers
// may hand back distinct *graph.Entity allocations for the same logical
// entity (e.g. once mmap-backed hits are materialized independently per
// ranker, ADR-0027), so keying on Entity.ID is what keeps them fusing into a
// single result. The returned hits carry the fused score and a Source tag.
func FuseRRF(bm25, semantic []Hit) []Hit {
	type agg struct {
		entity *graph.Entity
		score  float64
		inBM25 bool
		inSem  bool
	}
	order := []string{}
	byEntity := map[string]*agg{}
	get := func(e *graph.Entity) *agg {
		a, ok := byEntity[e.ID]
		if !ok {
			a = &agg{entity: e}
			byEntity[e.ID] = a
			order = append(order, e.ID)
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
	for _, id := range order {
		a := byEntity[id]
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
	// #3923: accumulate scores per candidate document by walking the postings
	// list of each query term, instead of scanning all totalDocs documents.
	// A document contributes to the result iff it appears in at least one query
	// term's postings list — identical to the old `if score > 0` predicate,
	// since only matching terms ever added to the score. Query terms are NOT
	// deduplicated: a repeated query term contributes twice, exactly as the old
	// per-document term loop summed it twice, preserving scores bit-for-bit.
	//
	// scoreByDoc maps a doc index to its running BM25 score.
	scoreByDoc := make(map[int32]float64)
	for _, t := range terms {
		// #5871 L2: postings is indexed by dense term ID, not the raw term
		// string. A missing entry in b.terms means the term never appeared in
		// the corpus — identical to the old empty-postings-list skip.
		id, ok := b.terms[t]
		if !ok {
			continue
		}
		plist := b.postings[id]
		// df(term) == len(postings[id]) after the #5871 fold-in — the separate
		// df map is gone. An empty postings list means the term is absent, which
		// also makes idf undefined, so skip (identical to the old df==0 guard).
		df := len(plist)
		if df == 0 {
			continue
		}
		idf := math.Log(1.0 + (float64(b.totalDocs)-float64(df)+0.5)/(float64(df)+0.5))
		for _, p := range plist {
			// tf is read DIRECTLY from the posting (no second per-doc map probe);
			// the per-doc length comes from docLen[p.doc].
			tf := float64(p.tf)
			lenNorm := 1.0
			if b.avgLen > 0 {
				lenNorm = 1 - bm25B + bm25B*(float64(b.docLen[p.doc])/b.avgLen)
			}
			scoreByDoc[p.doc] += idf * (tf * (bm25K1 + 1)) / (tf + bm25K1*lenNorm)
		}
	}
	type scoredDoc struct {
		di    int32
		score float64
	}
	scored := make([]scoredDoc, 0, len(scoreByDoc))
	for di, score := range scoreByDoc {
		if score > 0 {
			scored = append(scored, scoredDoc{di: di, score: score})
		}
	}
	// Sort by score desc, breaking ties by ascending doc index. The old
	// implementation appended hits in ascending doc order and called
	// sort.Slice (unstable) keyed only on score; an explicit ascending-index
	// tie-break makes the ranking deterministic regardless of map iteration
	// order while preserving the score ordering callers depend on.
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].di < scored[j].di
	})
	// Resolve each ranked doc index -> vector index -> *graph.Entity at RETURN
	// time via b.resolve (issue #5871 L4). The index retains no entity pointers;
	// resolve materializes on demand — a fresh Document heap copy on the flag-OFF
	// path, or the readerMu-guarded LabelIndex.at (mmap Reader + overlay
	// side-table) on the flag-ON path. A nil resolve (defensive) or an
	// unresolvable index (e.g. a retired mapping falling back to an emptied Doc)
	// is skipped, so hits may be shorter than scored — apply limit AFTER
	// resolution so the returned slice honors the requested cap.
	hits := make([]Hit, 0, len(scored))
	for _, sd := range scored {
		if b.resolve == nil {
			break
		}
		ent := b.resolve(b.entities[sd.di])
		if ent == nil {
			continue
		}
		hits = append(hits, Hit{Entity: ent, Score: sd.score})
		if limit > 0 && len(hits) >= limit {
			break
		}
	}
	return hits
}
