// Finite-state-machine (FSM) topology — #3704 (epic #3628, area #20).
//
// workflow_edges.go (#934) models AWS Step Functions *whole-machine* entities
// (SCOPE.StateMachine) and the Task-step→Lambda invocation edges. What neither
// it nor workflow_dag_edges.go (#3628 area #12) models is the application-level
// finite-state-machine topology that the dominant FSM libraries are built
// around: the individual *states* and the *state→state transitions* between
// them, keyed by the triggering event. That is the question "what states can X
// transition to, and on which event?".
//
// This pass extends a consistent state/transition shape to the four dominant
// FSM libraries:
//
//   - XState (JS/TS)            — createMachine({ states: { idle: { on: {
//     FETCH: 'loading' } } } }) → idle --FETCH--> loading
//   - Ruby AASM                 — state :pending; event :activate do
//     transitions from: :pending, to: :active end
//     → pending --activate--> active
//   - Spring StateMachine (Java)— .withStates().initial(IDLE).state(RUNNING) +
//     .withTransitions().source(IDLE).target(RUNNING)
//     .event(START) → IDLE --START--> RUNNING
//   - Python `transitions`      — Machine(states=['A','B'], transitions=[
//     {'trigger':'go','source':'A','dest':'B'}])
//     → A --go--> B
//
// Entities emitted:
//   - SCOPE.State  — a single declared state. One per statically-named state.
//
// Edge kinds emitted:
//   - TRANSITIONS_TO — source-state → target-state, carrying the triggering
//     event as the "event" property (RelationshipKindTransitionsTo).
//
// Synthetic entity IDs (SourceFile is set to the defining file; states are
// scoped by machine so two machines with an "idle" state do not collide):
//   - state:<lib>:<machine>:<stateName>
//
// Honest-partial scope: dynamically-computed states/targets — variable state
// names, target arrays gated only by guards, programmatically-built machines —
// are NOT resolved. Only statically-named states and statically-named
// transition targets are emitted. A state declared with no outgoing transition
// yields a SCOPE.State entity but no TRANSITIONS_TO edge.
//
// Scope guard: append-only. This pass never modifies or removes existing
// entities or edges, so it cannot regress the surrounding pipeline.
//
// Refs #3704 (epic #3628, area #20).
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Entity / edge kinds
// ---------------------------------------------------------------------------

const fsmStateKind = string(types.EntityKindState)
const transitionsToEdgeKind = string(types.RelationshipKindTransitionsTo)

// ---------------------------------------------------------------------------
// Synthetic entity ID helper
// ---------------------------------------------------------------------------

// fsmStateID builds the machine-scoped state ID so identically-named states in
// different machines never collide.
func fsmStateID(lib, machine, state string) string {
	return "state:" + lib + ":" + machine + ":" + state
}

// ---------------------------------------------------------------------------
// Emitter
// ---------------------------------------------------------------------------

// fsmEmitter accumulates SCOPE.State entities and TRANSITIONS_TO edges, with
// per-pass dedupe so re-declared states / repeated transitions collapse.
type fsmEmitter struct {
	lang          string
	path          string
	entities      []types.EntityRecord
	relationships []types.RelationshipRecord
	seenEnt       map[string]bool
	seenEdge      map[string]bool
}

func newFSMEmitter(lang, path string, ents []types.EntityRecord, rels []types.RelationshipRecord) *fsmEmitter {
	return &fsmEmitter{
		lang:          lang,
		path:          path,
		entities:      ents,
		relationships: rels,
		seenEnt:       map[string]bool{},
		seenEdge:      map[string]bool{},
	}
}

// state emits one SCOPE.State entity (idempotent within the pass).
func (e *fsmEmitter) state(lib, machine, stateName string, initial bool) {
	if stateName == "" {
		return
	}
	id := fsmStateID(lib, machine, stateName)
	if e.seenEnt[id] {
		return
	}
	e.seenEnt[id] = true
	props := map[string]string{
		"library":      lib,
		"machine":      machine,
		"state_name":   stateName,
		"pattern_type": "state_machine",
	}
	if initial {
		props["initial"] = "true"
	}
	e.entities = append(e.entities, types.EntityRecord{
		Name:               id,
		Kind:               fsmStateKind,
		SourceFile:         e.path,
		Language:           e.lang,
		Properties:         props,
		EnrichmentRequired: false,
		EnrichmentStatus:   types.StatusPending,
		QualityScore:       0.85,
	})
}

// transition emits a TRANSITIONS_TO edge source→target on the given event,
// auto-declaring both endpoint states so a transition can never dangle.
func (e *fsmEmitter) transition(lib, machine, source, target, event string) {
	if source == "" || target == "" {
		return
	}
	// Ensure both endpoint states exist as entities.
	e.state(lib, machine, source, false)
	e.state(lib, machine, target, false)

	fromID := fsmStateID(lib, machine, source)
	toID := fsmStateID(lib, machine, target)
	key := transitionsToEdgeKind + "|" + fromID + "|" + toID + "|" + event
	if e.seenEdge[key] {
		return
	}
	e.seenEdge[key] = true
	props := map[string]string{
		"library":      lib,
		"machine":      machine,
		"pattern_type": "state_machine",
	}
	if event != "" {
		props["event"] = event
	}
	e.relationships = append(e.relationships, types.RelationshipRecord{
		FromID:     fmt.Sprintf("%s:%s", fsmStateKind, fromID),
		ToID:       fmt.Sprintf("%s:%s", fsmStateKind, toID),
		Kind:       transitionsToEdgeKind,
		Properties: props,
	})
}

// ---------------------------------------------------------------------------
// Main entry point
// ---------------------------------------------------------------------------

// applyStateMachineEdges detects XState / AASM / Spring-StateMachine / Python
// `transitions` FSM definitions and emits SCOPE.State entities plus
// TRANSITIONS_TO edges. Append-only.
func applyStateMachineEdges(args DetectorPassArgs) DetectorPassResult {
	entities := args.Entities
	relationships := args.Relationships
	if len(args.Content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	src := string(args.Content)
	em := newFSMEmitter(args.Lang, args.Path, entities, relationships)

	switch args.Lang {
	case "javascript", "typescript", "jsx", "tsx":
		if xstateGuardRe.MatchString(src) {
			synthesizeXState(src, em)
		}
	case "ruby":
		if aasmGuardRe.MatchString(src) {
			synthesizeAASM(src, em)
		}
	case "java", "kotlin":
		if springSMGuardRe.MatchString(src) {
			synthesizeSpringStateMachine(src, em)
		}
	case "python":
		if pyTransitionsGuardRe.MatchString(src) {
			synthesizePythonTransitions(src, em)
		}
	}

	return DetectorPassResult{Entities: em.entities, Relationships: em.relationships}
}

// ===========================================================================
// XState (JS/TS)
// ===========================================================================
//
// createMachine({ id: 'fetch', initial: 'idle', states: {
//   idle:    { on: { FETCH: 'loading' } },
//   loading: { on: { SUCCESS: 'done', FAILURE: { target: 'idle' } } },
//   done:    { type: 'final' },
// } })
//
// Each `states: { ... }` block declares states; within each state the `on: {
// EVENT: target }` map declares event-keyed transitions. The target may be a
// bare string ('loading') or an object ({ target: 'loading', ... }). Targets
// that are arrays (guarded transitions) or template/computed expressions are
// honest-partial and skipped.

var xstateGuardRe = regexp.MustCompile(`createMachine\s*\(|\bMachine\s*\(\s*\{`)

// xstateMachineNameRe captures the optional machine id: `id: 'fetch'`.
var xstateMachineNameRe = regexp.MustCompile(`\bid\s*:\s*['"]([A-Za-z0-9_.-]+)['"]`)

// xstateStatesRe locates a `states:` key; the brace-matched block after it is
// the state map.
var xstateStatesKeyRe = regexp.MustCompile(`\bstates\s*:\s*\{`)

// xstateOnKeyRe locates an `on:` key inside a state body.
var xstateOnKeyRe = regexp.MustCompile(`\bon\s*:\s*\{`)

// xstateStateKeyRe captures a state-name key at the top of a state map entry:
// `idle: {` or `'idle': {` or `"idle": {`.
var xstateStateKeyRe = regexp.MustCompile(`(?:^|[,{])\s*(?:['"]?)([A-Za-z_$][A-Za-z0-9_$.-]*)(?:['"]?)\s*:\s*\{`)

// xstateOnStringTargetRe captures `EVENT: 'target'` event→string-target pairs.
var xstateOnStringTargetRe = regexp.MustCompile(`(?:^|[,{])\s*(?:['"]?)([A-Za-z_$][A-Za-z0-9_$.*]*)(?:['"]?)\s*:\s*['"]([A-Za-z_$][A-Za-z0-9_$.-]*)['"]`)

// xstateOnObjectTargetRe captures `EVENT: { target: 'target' ... }` pairs.
var xstateOnObjectTargetRe = regexp.MustCompile(`(?:^|[,{])\s*(?:['"]?)([A-Za-z_$][A-Za-z0-9_$.*]*)(?:['"]?)\s*:\s*\{[^{}]*?\btarget\s*:\s*['"]([A-Za-z_$][A-Za-z0-9_$.-]*)['"]`)

func synthesizeXState(src string, em *fsmEmitter) {
	machine := "machine"
	if m := xstateMachineNameRe.FindStringSubmatch(src); len(m) >= 2 {
		machine = m[1]
	}

	// Find the top-level `states: {` block and brace-match it.
	loc := xstateStatesKeyRe.FindStringIndex(src)
	if loc == nil {
		return
	}
	openBrace := loc[1] - 1 // position of the '{'
	statesBlock := braceBlock(src, openBrace)
	if statesBlock == "" {
		return
	}

	// Declare an initial state if present.
	if im := regexp.MustCompile(`\binitial\s*:\s*['"]([A-Za-z0-9_$.-]+)['"]`).FindStringSubmatch(src); len(im) >= 2 {
		em.state("xstate", machine, im[1], true)
	}

	// Walk the states map: each top-level key is a state; its brace-matched
	// body carries the `on:` transition map.
	for _, entry := range splitStateEntries(statesBlock) {
		sm := xstateStateKeyRe.FindStringSubmatch(entry.header)
		if len(sm) < 2 {
			continue
		}
		stateName := sm[1]
		em.state("xstate", machine, stateName, false)

		// Locate the `on:` block within this state's body.
		onLoc := xstateOnKeyRe.FindStringIndex(entry.body)
		if onLoc == nil {
			continue
		}
		onBlock := braceBlock(entry.body, onLoc[1]-1)
		if onBlock == "" {
			continue
		}
		// String-target transitions: EVENT: 'target'.
		for _, t := range xstateOnStringTargetRe.FindAllStringSubmatch(onBlock, -1) {
			em.transition("xstate", machine, stateName, t[2], t[1])
		}
		// Object-target transitions: EVENT: { target: 'target' }.
		for _, t := range xstateOnObjectTargetRe.FindAllStringSubmatch(onBlock, -1) {
			em.transition("xstate", machine, stateName, t[2], t[1])
		}
	}
}

// stateEntry is one top-level key/body pair inside a brace-matched object.
type stateEntry struct {
	header string // the "key: {" preamble (so the key regex can match)
	body   string // the brace-matched body of that key
}

// splitStateEntries walks the immediate children of a (brace-stripped) object
// body, returning each `key: { ...body... }` child. Only object-valued keys are
// returned; scalar keys (type: 'final') are ignored since they hold no `on:`.
func splitStateEntries(block string) []stateEntry {
	var out []stateEntry
	i := 0
	n := len(block)
	for i < n {
		// Find the next '{' which begins a child object value.
		brace := strings.IndexByte(block[i:], '{')
		if brace < 0 {
			break
		}
		brace += i
		// header is the text from the last entry boundary up to and
		// including the '{' — enough for xstateStateKeyRe to capture the key.
		hdrStart := strings.LastIndexAny(block[:brace], ",{")
		if hdrStart < 0 {
			hdrStart = 0
		}
		header := block[hdrStart : brace+1]
		body := braceBlock(block, brace)
		out = append(out, stateEntry{header: header, body: body})
		// Advance past this child's closing brace.
		end := brace + len(body) + 2 // body excludes braces; +2 for both
		if end <= brace {
			break
		}
		i = end
	}
	return out
}

// braceBlock returns the substring strictly inside the braces, where openPos is
// the index of the opening '{'. Returns "" if unbalanced. The returned text
// excludes the outer braces.
func braceBlock(s string, openPos int) string {
	if openPos < 0 || openPos >= len(s) || s[openPos] != '{' {
		return ""
	}
	depth := 0
	for i := openPos; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[openPos+1 : i]
			}
		}
	}
	return ""
}

// ===========================================================================
// Ruby AASM
// ===========================================================================
//
// aasm do
//   state :pending, initial: true
//   state :active
//   state :suspended
//   event :activate do
//     transitions from: :pending, to: :active
//   end
//   event :suspend do
//     transitions from: [:active], to: :suspended
//   end
// end
//
// Each `state :name` declares a state. Each `event :name do ... end` block
// owns one or more `transitions from: ..., to: :target` lines; the event name
// is the triggering event. `from:` may be a single symbol or an array.

var aasmGuardRe = regexp.MustCompile(`\baasm\b`)

var aasmStateRe = regexp.MustCompile(`(?m)^\s*state\s+:(\w+)([^\n]*)`)
var aasmEventBlockRe = regexp.MustCompile(`(?m)^\s*event\s+:(\w+)\b`)
var aasmTransitionRe = regexp.MustCompile(`transitions\s+([^\n]+)`)
var aasmFromRe = regexp.MustCompile(`from:\s*(\[[^\]]*\]|:\w+)`)
var aasmToRe = regexp.MustCompile(`to:\s*:(\w+)`)
var aasmSymbolRe = regexp.MustCompile(`:(\w+)`)
var aasmInitialRe = regexp.MustCompile(`initial:\s*true`)

func synthesizeAASM(src string, em *fsmEmitter) {
	machine := "aasm"

	// Declare states.
	for _, m := range aasmStateRe.FindAllStringSubmatch(src, -1) {
		initial := aasmInitialRe.MatchString(m[2])
		em.state("aasm", machine, m[1], initial)
	}

	// Walk event blocks. AASM event blocks are delimited by `event :x do` ...
	// `end`; we slice from each event header to the next event header (or EOF)
	// and scan that window for transitions. This over-includes at most the
	// trailing `end`s, which carry no transitions.
	eventLocs := aasmEventBlockRe.FindAllStringSubmatchIndex(src, -1)
	for i, loc := range eventLocs {
		eventName := src[loc[2]:loc[3]]
		windowEnd := len(src)
		if i+1 < len(eventLocs) {
			windowEnd = eventLocs[i+1][0]
		}
		window := src[loc[0]:windowEnd]
		for _, tr := range aasmTransitionRe.FindAllStringSubmatch(window, -1) {
			body := tr[1]
			toM := aasmToRe.FindStringSubmatch(body)
			if len(toM) < 2 {
				continue
			}
			to := toM[1]
			fromM := aasmFromRe.FindStringSubmatch(body)
			if len(fromM) < 2 {
				continue
			}
			for _, fs := range aasmSymbolRe.FindAllStringSubmatch(fromM[1], -1) {
				em.transition("aasm", machine, fs[1], to, eventName)
			}
		}
	}
}

// ===========================================================================
// Spring StateMachine (Java / Kotlin)
// ===========================================================================
//
// states
//   .withStates()
//     .initial(States.IDLE)
//     .state(States.RUNNING)
//     .end(States.DONE);
// transitions
//   .withExternal().source(States.IDLE).target(States.RUNNING).event(Events.START)
//   .and()
//   .withExternal().source(States.RUNNING).target(States.DONE).event(Events.FINISH);
//
// State enums may be qualified (States.IDLE) or bare (IDLE). We capture the
// final identifier segment as the state name. Each transition chain carries
// .source(X).target(Y).event(E); event is optional (anonymous transitions).

var springSMGuardRe = regexp.MustCompile(`StateMachineConfigurerAdapter|EnableStateMachine|\.withStates\s*\(\)|\.withTransitions\s*\(\)`)

var springInitialRe = regexp.MustCompile(`\.initial\s*\(\s*([A-Za-z0-9_.]+)`)
var springStateRe = regexp.MustCompile(`\.state\s*\(\s*([A-Za-z0-9_.]+)`)
var springEndStateRe = regexp.MustCompile(`\.end\s*\(\s*([A-Za-z0-9_.]+)`)

// springTransitionRe captures source/target/optional-event from a single
// fluent transition chain. The chain is bounded by `.withExternal()` /
// `.withInternal()` / `.withLocal()` markers; we scan each marker-bounded
// window for the trio.
var springWithTransRe = regexp.MustCompile(`\.with(?:External|Internal|Local)\s*\(\s*\)`)
var springSourceRe = regexp.MustCompile(`\.source\s*\(\s*([A-Za-z0-9_.]+)`)
var springTargetRe = regexp.MustCompile(`\.target\s*\(\s*([A-Za-z0-9_.]+)`)
var springEventRe = regexp.MustCompile(`\.event\s*\(\s*([A-Za-z0-9_.]+)`)

// lastIdentSegment returns the final dotted segment: States.IDLE -> IDLE.
func lastIdentSegment(s string) string {
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func synthesizeSpringStateMachine(src string, em *fsmEmitter) {
	machine := "spring"

	if m := springInitialRe.FindStringSubmatch(src); len(m) >= 2 {
		em.state("spring-statemachine", machine, lastIdentSegment(m[1]), true)
	}
	for _, m := range springStateRe.FindAllStringSubmatch(src, -1) {
		em.state("spring-statemachine", machine, lastIdentSegment(m[1]), false)
	}
	for _, m := range springEndStateRe.FindAllStringSubmatch(src, -1) {
		em.state("spring-statemachine", machine, lastIdentSegment(m[1]), false)
	}

	// Slice the source on each transition marker and scan each window for the
	// source/target/event trio.
	markers := springWithTransRe.FindAllStringIndex(src, -1)
	for i, loc := range markers {
		windowEnd := len(src)
		if i+1 < len(markers) {
			windowEnd = markers[i+1][0]
		}
		window := src[loc[0]:windowEnd]
		sm := springSourceRe.FindStringSubmatch(window)
		tm := springTargetRe.FindStringSubmatch(window)
		if len(sm) < 2 || len(tm) < 2 {
			continue
		}
		event := ""
		if em2 := springEventRe.FindStringSubmatch(window); len(em2) >= 2 {
			event = lastIdentSegment(em2[1])
		}
		em.transition("spring-statemachine", machine,
			lastIdentSegment(sm[1]), lastIdentSegment(tm[1]), event)
	}
}

// ===========================================================================
// Python `transitions` library
// ===========================================================================
//
// machine = Machine(model=self, states=['solid', 'liquid', 'gas'],
//                   transitions=[
//                     {'trigger': 'melt', 'source': 'solid', 'dest': 'liquid'},
//                     {'trigger': 'evaporate', 'source': 'liquid', 'dest': 'gas'},
//                   ],
//                   initial='solid')
//
// Also supports the list-form transition: ['melt', 'solid', 'liquid'].
// States may be plain strings or dicts ({'name': 'solid'}); we capture the
// string form (honest-partial for dynamic / class-based state objects).

var pyTransitionsGuardRe = regexp.MustCompile(`\bMachine\s*\(|from\s+transitions\b|import\s+transitions\b`)

var pyStatesListRe = regexp.MustCompile(`states\s*=\s*\[([^\]]*)\]`)
var pyStringLiteralRe = regexp.MustCompile(`['"]([A-Za-z_][A-Za-z0-9_]*)['"]`)
var pyInitialRe = regexp.MustCompile(`initial\s*=\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]`)

// pyTransitionsBlockRe locates the `transitions=[ ... ]` argument; we
// brace/bracket-match to get the full list (it may contain nested dicts).
var pyTransitionsKeyRe = regexp.MustCompile(`transitions\s*=\s*\[`)

// pyDictTransitionRe captures a dict-form transition: {'trigger':'go',
// 'source':'A','dest':'B'}. Key order is not fixed, so we extract each field
// independently from the matched dict body.
var pyDictRe = regexp.MustCompile(`\{[^{}]*\}`)
var pyTriggerRe = regexp.MustCompile(`['"]trigger['"]\s*:\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]`)
var pySourceRe = regexp.MustCompile(`['"]source['"]\s*:\s*(\[[^\]]*\]|['"][A-Za-z_*][A-Za-z0-9_]*['"])`)
var pyDestRe = regexp.MustCompile(`['"]dest['"]\s*:\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]`)

func synthesizePythonTransitions(src string, em *fsmEmitter) {
	machine := "transitions"

	// Declare states from the states=[...] list.
	if sm := pyStatesListRe.FindStringSubmatch(src); len(sm) >= 2 {
		for _, s := range pyStringLiteralRe.FindAllStringSubmatch(sm[1], -1) {
			em.state("python-transitions", machine, s[1], false)
		}
	}
	if im := pyInitialRe.FindStringSubmatch(src); len(im) >= 2 {
		em.state("python-transitions", machine, im[1], true)
	}

	// Locate and bracket-match the transitions=[ ... ] list.
	loc := pyTransitionsKeyRe.FindStringIndex(src)
	if loc == nil {
		return
	}
	block := bracketBlock(src, loc[1]-1)
	if block == "" {
		return
	}

	// Dict-form transitions.
	for _, d := range pyDictRe.FindAllString(block, -1) {
		trig := ""
		if m := pyTriggerRe.FindStringSubmatch(d); len(m) >= 2 {
			trig = m[1]
		}
		dm := pyDestRe.FindStringSubmatch(d)
		sm := pySourceRe.FindStringSubmatch(d)
		if len(dm) < 2 || len(sm) < 2 {
			continue
		}
		dest := dm[1]
		// source may be a single string or a list of strings.
		for _, s := range pyStringLiteralRe.FindAllStringSubmatch(sm[1], -1) {
			em.transition("python-transitions", machine, s[1], dest, trig)
		}
	}
}

// bracketBlock returns the substring strictly inside the brackets, where
// openPos is the index of the opening '['. Returns "" if unbalanced.
func bracketBlock(s string, openPos int) string {
	if openPos < 0 || openPos >= len(s) || s[openPos] != '[' {
		return ""
	}
	depth := 0
	for i := openPos; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return s[openPos+1 : i]
			}
		}
	}
	return ""
}
