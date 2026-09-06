// blocks.go — CST-backed block spans for the OCaml extractor (#6812).
//
// # Why this file exists
//
// `struct … end`, `sig … end`, `object … end` and `begin … end` are the only
// structural judgement this extractor owns, and until this file landed it was
// made by a hand-rolled depth walker inside extractModuleBody: two regexes
// re-run at every byte, counting `end` against `struct|sig|object|begin`.
//
// #6812 measured that walker against a clone of github.com/ocaml/ocaml at
// 70165ff23e, over the 68 files that contain a real `inherit` token:
//
//   - 479 of 633 `object … end` blocks (75.7%) got the WRONG end;
//   - of the 408 (block, inherit) pairs it attributed, 230 were wrong —
//     recall 91.2% (19 missed), precision 48.3% (211 spurious).
//
// Four independent causes, three of which a concrete syntax tree removes by
// construction:
//
//  1. the scan never terminated on a balanced block — the opener took depth
//     0→1 and the matching `end` took it 1→0 without stopping, because the
//     loop only broke on an `end` seen AT depth 0, so well-formed blocks
//     always fell through to the indentation fallback;
//  2. `\b` was evaluated against a SLICE, not the source: `openKW`/`closeKW`
//     were re-run on `rest[i:]` and tested for `om[0] == 0`, and at offset 0
//     of a slice start-of-string satisfies `\b`, so `append`, `send`,
//     `backend`, `legend`, `myobject` all read as keywords;
//  3. OCaml block comments NEST and the skipper stopped at the first `*)`;
//  4. `{|…|}` quoted strings were unknown to it and scanned as code.
//
// (2), (3) and (4) are free here: the grammar tokenises. (1) does not arise:
// a block's extent is a node span, not the output of a counter.
//
// The grammar itself was already vendored, registered at
// internal/treesitter/adapters.go:69 and compiled in — it had simply never
// been asked for. There was no adapter mismatch, no version pin and no build
// tag standing in the way; the smoke test in
// internal/treesitter/ts/grammars/ocaml has been parsing OCaml on every CI run
// the whole time. The answer to "why was it registered and never used" is that
// this extractor predates the grammar's arrival and nothing forced the two
// together.
//
// # Contract
//
// blockIndex answers exactly one question, and deliberately the SAME question
// the depth walker answered, so the #6812 measurement harness compares like
// with like: given a position `from` from which the block's OPENING keyword is
// still ahead (extractor.go hands it the end of the `module Foo =` match, with
// `struct` still to come), where does that block's `end` token start?
//
// It does NOT bound how far ahead the opener may be. That is a deliberate
// parity choice, not an oversight: `refMatchEnd`, the independent reference in
// the measurement harness, has the same contract, and a `module Foo = Bar`
// alias with no block of its own therefore still adopts the next block in the
// file. That mis-span is pre-existing (the depth walker did the same thing),
// it is one of #6939's four gaps, and closing it here would have made the
// before/after figures incomparable.
//
// # When the CST cannot answer
//
// Three cases, all of which hand back to the indentation heuristic
// (extractLetBody) — which is where the depth walker already sent the majority
// of real blocks anyway, since defect (1) meant it almost never terminated:
//
//   - the grammar produced no tree at all;
//   - no block opener exists at or after `from`;
//   - the nearest opener has no `end` — either genuinely absent or supplied by
//     tree-sitter as a zero-width MISSING node inside an ERROR subtree.
//
// The third is the one the corpus forced: `testsuite/tests/generated-parse-
// errors/errors.ml` in the OCaml tree is generated, deliberately malformed
// source with 256 `object` against 38 `end`. Answering it with the nearest
// LATER block's `end` would be a fabricated span, so an unclosed opener is
// reported as unclosed and the caller falls back. This is graded by
// TestBlockIndex_MalformedInput_FallsBack.
package ocaml

import (
	"sort"
	"sync"

	"github.com/cajasmota/grafel/internal/indexstate"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tsgrammar "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/ocaml"
	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// blockOpeners are the four anonymous tokens that open an `… end` block in the
// tree-sitter-ocaml grammar. They appear as unnamed children of, respectively,
// `structure`, `signature`, `object_expression`/`class_body_type` and
// `parenthesized_expression`. Keying on the TOKEN rather than on the parent
// node type is what keeps `( … )` — which shares parenthesized_expression with
// `begin … end` — out of the index.
var blockOpeners = map[string]bool{
	"struct": true, "sig": true, "object": true, "begin": true,
}

// blockIndex records, for every block opener in a source file, the byte offset
// at which its closing `end` token starts (or -1 when the block has none).
type blockIndex struct {
	// openers is ascending, so a lookup is a binary search.
	openers []int
	end     map[int]int
	// parsed is false when the grammar produced no usable tree; every lookup
	// then declines and the caller falls back.
	parsed bool
	// hasError records whether the tree contains an ERROR node anywhere, i.e.
	// whether tree-sitter had to recover. It changes no answer this file gives
	// — a recovered tree is still consulted, block by block — and exists so a
	// caller can separate "this file is malformed" from "this block span is
	// wrong". The #6812 corpus measurement uses it to assert that every
	// residual disagreement with the reference is in a file the grammar itself
	// reports as broken, which is a much stronger claim than a bounded count.
	hasError bool
}

var (
	ocamlAdapter = official.New()
	ocamlGrammar = tsgrammar.Language()
)

// buildBlockIndex parses src with the vendored OCaml grammar and records every
// block span in it.
func buildBlockIndex(src string) *blockIndex {
	bi := &blockIndex{end: make(map[int]int)}
	parser, err := ocamlAdapter.NewParser(ocamlGrammar)
	if err != nil {
		return bi
	}
	defer parser.Close()

	// #5630 — this is a second parse of a file the dispatcher has usually
	// already parsed, so it must take the same daemon-wide parse slot that
	// treesitter.ParserFactory.Parse takes. GOMAXPROCS does not bound cgo, so
	// a parse outside the gate makes the in-process ceiling illusory. The slot
	// is held around the parse ALONE (not the walk below), and released via
	// defer inside the closure so a panic in Parse cannot leak it permanently.
	tree, perr := func() (ts.Tree, error) {
		indexstate.AcquireParseSlot()
		defer indexstate.ReleaseParseSlot()
		return parser.Parse([]byte(src))
	}()
	if perr != nil || tree == nil {
		return bi
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil {
		return bi
	}
	bi.parsed = true
	bi.collect(root)
	sort.Ints(bi.openers)
	return bi
}

// collect walks every node, named and anonymous, recording each block-opening
// token against the `end` token of the same parent node.
func (bi *blockIndex) collect(n ts.Node) {
	if n.IsError() {
		bi.hasError = true
	}
	opener, end := -1, -1
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if c == nil {
			continue
		}
		if !c.IsNamed() {
			switch {
			case blockOpeners[c.Type()] && opener < 0:
				opener = int(c.StartByte())
			case c.Type() == "end":
				// A MISSING `end` inserted by tree-sitter's error recovery is
				// zero-width. Treating it as a real closer would invent a span
				// ending where the parser gave up, which is precisely the kind
				// of fabricated answer this file exists to stop producing.
				if c.EndByte() > c.StartByte() {
					end = int(c.StartByte())
				}
			}
		}
		bi.collect(c)
	}
	if opener < 0 {
		return
	}
	if _, seen := bi.end[opener]; !seen {
		bi.openers = append(bi.openers, opener)
	}
	bi.end[opener] = end
}

// bodyEnd returns the byte offset at which the closing `end` of the first
// block opening at or after `from` starts. ok is false when the index cannot
// answer — see the "When the CST cannot answer" section of the package note.
func (bi *blockIndex) bodyEnd(from int) (int, bool) {
	if !bi.parsed {
		return 0, false
	}
	i := sort.SearchInts(bi.openers, from)
	if i >= len(bi.openers) {
		return 0, false
	}
	e := bi.end[bi.openers[i]]
	if e < from {
		// Either the block is unclosed (-1) or the `end` precedes `from`,
		// which would produce a negative-length body.
		return 0, false
	}
	return e, true
}

// ---------------------------------------------------------------------------
// Per-source memo
// ---------------------------------------------------------------------------

// extractModuleBody is called once per module entity and again per module from
// addModuleContains, and the #6812 harness calls it once per block. Parsing on
// every call would re-parse the same file up to a few dozen times, so the last
// few indexes are memoised by their exact source text.
//
// Keyed on string EQUALITY, not on a hash: a hash collision would silently
// hand one file's block spans to another, and this package has no way to
// notice. Equality is cheap in the case that matters — every caller inside one
// extraction passes the same string value, so the comparison short-circuits on
// the data pointer.
//
// The ring is small on purpose. It is a re-parse avoider for one file being
// walked, not a cache of the corpus; extraction workers run concurrently and a
// large ring would pin whole files' worth of trees' worth of maps for as long
// as the process lives.
var blockCache struct {
	mu   sync.Mutex
	ring [8]blockCacheEntry
	next int
}

type blockCacheEntry struct {
	src string
	idx *blockIndex
}

func blockIndexFor(src string) *blockIndex {
	blockCache.mu.Lock()
	for _, e := range blockCache.ring {
		if e.idx != nil && e.src == src {
			idx := e.idx
			blockCache.mu.Unlock()
			return idx
		}
	}
	blockCache.mu.Unlock()

	// Parse OUTSIDE the lock: it takes the parse gate, and holding a package
	// mutex across a gated cgo call would serialise every OCaml extraction in
	// the process behind one file.
	idx := buildBlockIndex(src)

	blockCache.mu.Lock()
	blockCache.ring[blockCache.next] = blockCacheEntry{src: src, idx: idx}
	blockCache.next = (blockCache.next + 1) % len(blockCache.ring)
	blockCache.mu.Unlock()
	return idx
}
