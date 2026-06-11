// On-demand per-function control-flow graph (CFG) — #4822, control-flow epic
// #4820 part (b).
//
// Part (a) (effect_context.go) classified each EFFECT's enclosing block
// (conditional / condition text / in_loop) and computed a per-function
// cyclomatic complexity. This file is the complementary, WHOLE-FUNCTION view
// the flowchart (#4819) needs: a basic-block control-flow graph of one function
// — start → decisions/loops/process/terminal nodes joined by branch-true/false,
// switch-case, loop back-edge, and early-exit (return/throw) edges.
//
// CRITICAL DESIGN CONSTRAINT — ON DEMAND, NOT PERSISTED.
// A CFG is built FOR THE ONE REQUESTED FUNCTION on request and returned over
// MCP; no basic-block entities are ever written to the graph. That keeps the
// knowledge graph lean (the whole point of #4822). A small in-memory cache keyed
// by (entity id, source hash) lets repeated calls on an unchanged function skip
// the rebuild — see cfgCache.
//
// It REUSES part (a)'s machinery rather than reinventing it:
//   - enclosingBlocks()  — per-language block headers (Python by indentation,
//     brace languages by `{}` depth) with their absolute source span + the loop
//     flag + the predicate (condition) text.
//   - ClampToFunctionBody() — trims a padded window to the target function body.
//   - the effect sniffers (EffectSnifferFor) — annotate process nodes with the
//     effects they perform (db_read/db_write/http_out/…).
//   - ComputeFunctionComplexity() — the cyclomatic number surfaced alongside.
//
// HONEST LIMIT (per the epic): this is response/effect/decision-GRANULARITY
// control flow, not a compiler-grade CFG of every sub-expression. It captures
// if/elif/else, switch/case, for/while loops (with the back-edge), try/catch,
// and early return/throw exits, plus the effects/calls inside each block. It
// does NOT model: goto, exceptions propagating across non-adjacent frames,
// short-circuit `&&`/`||` as separate edges, ternaries as their own nodes, or
// labelled break/continue targets. Languages: Python + JS/TS first (matching
// part (a)'s validated set); other families fall through to a degenerate
// single-process CFG until extended (epic #4830 / #4820 follow-ups).
package substrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// CFGNodeShape is the downstream flowchart node shape. Stable string contract
// consumed by the dashboard renderer (#4819).
type CFGNodeShape string

const (
	// ShapeStart is the single function entry node.
	ShapeStart CFGNodeShape = "start"
	// ShapeEnd is the implicit fall-through exit (function returns at the end of
	// its body). One per CFG.
	ShapeEnd CFGNodeShape = "end"
	// ShapeReturn is an explicit `return` terminal.
	ShapeReturn CFGNodeShape = "return"
	// ShapeThrow is a `throw`/`raise` terminal.
	ShapeThrow CFGNodeShape = "throw"
	// ShapeDecision is an if / elif / else-if / switch / case header (a branch
	// point). Carries the predicate text in CFGNode.Condition.
	ShapeDecision CFGNodeShape = "decision"
	// ShapeLoop is a for / while / foreach loop header. Carries the loop
	// predicate and is the target of a back-edge from its body tail.
	ShapeLoop CFGNodeShape = "loop"
	// ShapeProcess is a straight-line statement / effect block.
	ShapeProcess CFGNodeShape = "process"
)

// CFGEdgeKind names the control-flow relationship between two nodes.
type CFGEdgeKind string

const (
	// EdgeSeq is sequential fall-through.
	EdgeSeq CFGEdgeKind = "seq"
	// EdgeTrue is the taken branch of a decision (then / case body / loop body
	// entry).
	EdgeTrue CFGEdgeKind = "branch_true"
	// EdgeFalse is the not-taken branch of a decision (else / skip / loop exit).
	EdgeFalse CFGEdgeKind = "branch_false"
	// EdgeBack is a loop back-edge (body tail → loop header).
	EdgeBack CFGEdgeKind = "loop_back"
	// EdgeExit is an early function exit (return / throw → end), so the flowchart
	// can draw the short-circuit out of the body.
	EdgeExit CFGEdgeKind = "exit"
)

// CFGEffect is a terse effect annotation on a process node, mirroring the
// EffectContext fields the flowchart cares about.
type CFGEffect struct {
	Effect string `json:"effect"`
	Sink   string `json:"sink,omitempty"`
}

// CFGNode is one basic block / decision / terminal in the function CFG.
type CFGNode struct {
	// ID is a stable per-CFG identifier ("n0", "n1", …) referenced by edges.
	ID string `json:"id"`
	// Shape drives the flowchart glyph (start/end/return/throw/decision/loop/
	// process).
	Shape CFGNodeShape `json:"shape"`
	// Line is the 1-indexed absolute source line the node anchors to (0 for the
	// synthetic start/end).
	Line int `json:"line,omitempty"`
	// Label is a short human caption for the node (the trimmed source line, or
	// "entry"/"exit").
	Label string `json:"label,omitempty"`
	// Condition is the predicate text for decision/loop nodes (e.g.
	// "if user.is_admin", "for row in rows"). Empty otherwise.
	Condition string `json:"condition,omitempty"`
	// Effects annotates a process node with the side effects performed at it.
	// Nil/empty for pure straight-line code.
	Effects []CFGEffect `json:"effects,omitempty"`
}

// CFGEdge is a directed control-flow edge between two CFGNode IDs.
type CFGEdge struct {
	From string      `json:"from"`
	To   string      `json:"to"`
	Kind CFGEdgeKind `json:"kind"`
}

// ControlFlowGraph is the on-demand CFG of a single function.
type ControlFlowGraph struct {
	// Language is the resolved language slug ("python", "jsts", …).
	Language string `json:"language"`
	// Supported is false when no block detector exists for the language; the CFG
	// then degenerates to start → one process → end (honest-partial).
	Supported bool `json:"supported"`
	// Cyclomatic is the McCabe cyclomatic complexity (decision points + 1).
	Cyclomatic int `json:"cyclomatic_complexity"`
	// BranchCount is the raw decision-point count (Cyclomatic - 1).
	BranchCount int `json:"branch_count"`
	// Nodes / Edges form the graph, in stable source order.
	Nodes []CFGNode `json:"nodes"`
	Edges []CFGEdge `json:"edges"`
}

// --- terminal detection ---------------------------------------------------

var (
	cfgReturnRe = regexp.MustCompile(`^\s*return\b`)
	cfgThrowRe  = regexp.MustCompile(`^\s*(?:raise|throw)\b`)
)

func cfgTerminalShape(line string) (CFGNodeShape, bool) {
	switch {
	case cfgThrowRe.MatchString(line):
		return ShapeThrow, true
	case cfgReturnRe.MatchString(line):
		return ShapeReturn, true
	}
	return "", false
}

// --- on-demand cache ------------------------------------------------------

type cfgCacheKey struct {
	entityID string
	hash     string
}

var (
	cfgCacheMu sync.Mutex
	cfgCache   = map[cfgCacheKey]*ControlFlowGraph{}
)

// SourceHash returns a short content hash used to key the on-demand CFG cache
// (so a stale entry is never served after the function's source changes).
func SourceHash(src string) string {
	sum := sha256.Sum256([]byte(src))
	return hex.EncodeToString(sum[:8])
}

// BuildControlFlowGraphCached returns the CFG for (entityID, src), reusing a
// cached build when the source hash is unchanged. entityID may be empty to skip
// caching entirely (e.g. ad-hoc file+symbol builds).
func BuildControlFlowGraphCached(entityID, lang, funcSource string, startLine int) *ControlFlowGraph {
	if entityID == "" {
		return BuildControlFlowGraph(lang, funcSource, startLine)
	}
	key := cfgCacheKey{entityID: entityID, hash: SourceHash(funcSource)}
	cfgCacheMu.Lock()
	if g, ok := cfgCache[key]; ok {
		cfgCacheMu.Unlock()
		return g
	}
	cfgCacheMu.Unlock()
	g := BuildControlFlowGraph(lang, funcSource, startLine)
	cfgCacheMu.Lock()
	cfgCache[key] = g
	cfgCacheMu.Unlock()
	return g
}

// --- builder --------------------------------------------------------------

// BuildControlFlowGraph constructs the on-demand CFG of one function. startLine
// is the 1-indexed absolute file line of the function's first line so node Line
// values are absolute (cross-referenceable with get_source). Pure +
// deterministic: identical input yields an identical graph.
func BuildControlFlowGraph(lang, funcSource string, startLine int) *ControlFlowGraph {
	complexity := ComputeFunctionComplexity(funcSource)
	g := &ControlFlowGraph{
		Language:    lang,
		Cyclomatic:  complexity.Cyclomatic,
		BranchCount: complexity.BranchCount,
	}
	if strings.TrimSpace(funcSource) == "" {
		g.Nodes = []CFGNode{{ID: "n0", Shape: ShapeStart, Label: "entry"}, {ID: "n1", Shape: ShapeEnd, Label: "exit"}}
		g.Edges = []CFGEdge{{From: "n0", To: "n1", Kind: EdgeSeq}}
		return g
	}
	clamped := ClampToFunctionBody(funcSource, lang)
	blocks := enclosingBlocks(clamped, lang, startLine)
	g.Supported = len(blocks) > 0 || hasBlockDetector(lang)

	// Effect sink lines (absolute) → their matches, for process-node annotation.
	effectsByLine := map[int][]CFGEffect{}
	if sniffer := EffectSnifferFor(lang); sniffer != nil {
		for _, m := range sniffer(clamped) {
			if m.Line <= 0 {
				continue
			}
			abs := startLine + m.Line - 1
			effectsByLine[abs] = append(effectsByLine[abs], CFGEffect{Effect: string(m.Effect), Sink: m.Sink})
		}
	}

	bld := &cfgBuilder{
		g:             g,
		startLine:     startLine,
		effectsByLine: effectsByLine,
		blocks:        sortBlocks(blocks),
	}
	bld.build(clamped)
	return g
}

// hasBlockDetector reports whether a language family has a block detector wired
// (so a branchless function is reported Supported=true, not "unsupported").
func hasBlockDetector(lang string) bool {
	return lang == "python" || lang == "ruby" || braceLangs[lang] || braceCFGLangs[lang]
}

func sortBlocks(blocks []blockHeader) []blockHeader {
	out := append([]blockHeader(nil), blocks...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].startLine < out[j].startLine })
	return out
}

type cfgBuilder struct {
	g             *ControlFlowGraph
	startLine     int
	effectsByLine map[int][]CFGEffect
	blocks        []blockHeader
	counter       int
	prev          string // id of the previous node to wire a seq edge from
	prevTerminal  bool   // previous node was a terminal (no fall-through)
}

func (b *cfgBuilder) newID() string {
	id := fmt.Sprintf("n%d", b.counter)
	b.counter++
	return id
}

func (b *cfgBuilder) addNode(n CFGNode) string {
	n.ID = b.newID()
	b.g.Nodes = append(b.g.Nodes, n)
	return n.ID
}

func (b *cfgBuilder) addEdge(from, to string, kind CFGEdgeKind) {
	if from == "" || to == "" {
		return
	}
	b.g.Edges = append(b.g.Edges, CFGEdge{From: from, To: to, Kind: kind})
}

// build performs a single linear pass over the clamped body, emitting a node per
// significant line and wiring edges. Decision/loop headers found by
// enclosingBlocks become decision/loop nodes; their body lines become children
// reached by branch_true; a loop's body tail wires a back-edge to its header.
func (b *cfgBuilder) build(clamped string) {
	start := b.addNode(CFGNode{Shape: ShapeStart, Label: "entry"})
	b.prev = start
	b.prevTerminal = false

	headerAt := map[int]blockHeader{} // absolute startLine → header
	for _, blk := range b.blocks {
		headerAt[blk.startLine] = blk
	}
	// loopStack tracks open loops so a body-tail can back-edge to the header.
	type openLoop struct {
		headerID string
		endLine  int
	}
	var loopStack []openLoop

	lines := strings.Split(clamped, "\n")
	for i, raw := range lines {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		absLine := b.startLine + i

		// Close any loops whose scope has ended → wire the back-edge from the
		// last in-body node and pop.
		for len(loopStack) > 0 && absLine >= loopStack[len(loopStack)-1].endLine {
			lp := loopStack[len(loopStack)-1]
			if b.prev != "" && !b.prevTerminal && b.prev != lp.headerID {
				b.addEdge(b.prev, lp.headerID, EdgeBack)
			}
			loopStack = loopStack[:len(loopStack)-1]
		}

		label := strings.TrimSpace(raw)

		if blk, ok := headerAt[absLine]; ok {
			shape := ShapeDecision
			if blk.isLoop {
				shape = ShapeLoop
			}
			id := b.addNode(CFGNode{Shape: shape, Line: absLine, Label: truncLabel(label), Condition: blk.condition})
			if b.prev != "" && !b.prevTerminal {
				b.addEdge(b.prev, id, EdgeSeq)
			}
			b.prev = id
			b.prevTerminal = false
			if blk.isLoop {
				loopStack = append(loopStack, openLoop{headerID: id, endLine: blk.endLine})
			}
			continue
		}

		// Non-header statement: terminal, effect-bearing, or plain process.
		var n CFGNode
		if shape, term := cfgTerminalShape(label); term {
			n = CFGNode{Shape: shape, Line: absLine, Label: truncLabel(label)}
		} else {
			n = CFGNode{Shape: ShapeProcess, Line: absLine, Label: truncLabel(label)}
		}
		if effs := b.effectsByLine[absLine]; len(effs) > 0 {
			n.Effects = effs
		}
		// Whether the previous node is a branch header (decision/loop) decides
		// the incoming edge kind — capture BEFORE we append the new node.
		fromBranchHeader := b.prevIsBranchHeader()
		// Skip pure, eff-less, non-terminal process lines that merely repeat the
		// preceding flow to keep the payload terse (#2828): only keep process
		// nodes that either carry an effect, are a terminal, or are the first
		// statement of a branch body (i.e. follow a decision/loop header).
		if n.Shape == ShapeProcess && len(n.Effects) == 0 && !fromBranchHeader {
			continue
		}
		id := b.addNode(n)
		if b.prev != "" && !b.prevTerminal {
			kind := EdgeSeq
			if fromBranchHeader {
				kind = EdgeTrue
			}
			b.addEdge(b.prev, id, kind)
		}
		if n.Shape == ShapeReturn || n.Shape == ShapeThrow {
			b.prevTerminal = true
		}
		b.prev = id
		if n.Shape != ShapeReturn && n.Shape != ShapeThrow {
			b.prevTerminal = false
		}
	}

	// Close any still-open loops at end of body.
	for len(loopStack) > 0 {
		lp := loopStack[len(loopStack)-1]
		if b.prev != "" && !b.prevTerminal && b.prev != lp.headerID {
			b.addEdge(b.prev, lp.headerID, EdgeBack)
		}
		loopStack = loopStack[:len(loopStack)-1]
	}

	end := b.addNode(CFGNode{Shape: ShapeEnd, Label: "exit"})
	if b.prev != "" && !b.prevTerminal {
		b.addEdge(b.prev, end, EdgeSeq)
	}
	// Wire every terminal (return/throw) to the exit node so the flowchart shows
	// the early-exit edges.
	for _, n := range b.g.Nodes {
		if n.Shape == ShapeReturn || n.Shape == ShapeThrow {
			b.addEdge(n.ID, end, EdgeExit)
		}
	}
}

// prevIsBranchHeader reports whether the current `prev` node is a decision/loop
// header (so the next statement is the entry of a branch body — kept even if
// effectless, and reached by a branch_true edge).
func (b *cfgBuilder) prevIsBranchHeader() bool {
	for i := len(b.g.Nodes) - 1; i >= 0; i-- {
		if b.g.Nodes[i].ID == b.prev {
			s := b.g.Nodes[i].Shape
			return s == ShapeDecision || s == ShapeLoop
		}
	}
	return false
}

// truncLabel keeps node labels short for the flowchart (and the token budget).
func truncLabel(s string) string {
	const max = 80
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
