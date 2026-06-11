package dashboard

// handlers_flows_annotation.go — step-kind annotation and side-effect
// classification for the Flows v2 detail endpoint.
//
// classifyStepKind assigns a functional label to each step by examining
// outgoing edge kinds on the step entity.  aggregateFlowMeta derives the
// top-level flow annotations (entry_kind, flow_side_effects, complexity_score,
// is_cross_repo, data_lineage) from the classified step set.

import (
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// Step-kind constants
// ─────────────────────────────────────────────────────────────────────────────

const (
	StepKindHTTPFetch      = "http_fetch"
	StepKindDBQuery        = "db_query"
	StepKindDBWrite        = "db_write"
	StepKindMessagePublish = "message_publish"
	StepKindMessageConsume = "message_consume"
	StepKindTransform      = "transform"
	StepKindValidation     = "validation"
	StepKindSideEffect     = "side_effect"
	StepKindExternalLib    = "external_lib"
	StepKindTestAssert     = "test_assert"
	StepKindRender         = "render"
	StepKindFunctionCall   = "function_call" // fallback (per issue: "function_call")
)

// effectKinds lists the step kinds that count as observable side effects
// when aggregating flow_side_effects.
var effectKinds = map[string]bool{
	StepKindHTTPFetch:      true,
	StepKindDBWrite:        true,
	StepKindMessagePublish: true,
	StepKindSideEffect:     true,
}

// ─────────────────────────────────────────────────────────────────────────────
// annotatedStep — internal representation used during classification
// ─────────────────────────────────────────────────────────────────────────────

type annotatedStep struct {
	// Mirrors the public Step struct fields used for output.
	entityID   string
	entityKind string // raw entity Kind from the graph
	stepIndex  int

	// Outgoing edge map: edgeKind → list of ToIDs.
	outEdges map[string][]string

	// Whether this is the flow's entry step (step_index == 0).
	isEntry bool

	// Whether the entity lives in a different repo than the process entity.
	isExternalRepo bool

	// Derived fields.
	StepKind    string   `json:"step_kind"`
	SideEffects []string `json:"side_effects,omitempty"` // per-step side-effect targets
}

// ─────────────────────────────────────────────────────────────────────────────
// classifyStepKind — core classification logic
// ─────────────────────────────────────────────────────────────────────────────

// classifyStepKind returns the step-kind string for a step entity given:
//   - entityKind: the entity's Kind field from the graph
//   - outEdgeKinds: set of outgoing relationship kinds emitted by this entity
//   - inEdgeKinds: set of incoming relationship kinds targeting this entity (for message_consume)
//   - isEntry: true when this is the flow's first step
func classifyStepKind(entityKind string, outEdgeKinds map[string]bool, inEdgeKinds map[string]bool, isEntry bool) string {
	// ── HTTP fetch ──────────────────────────────────────────────────────────
	if outEdgeKinds["FETCHES"] || outEdgeKinds["HTTP_CALL"] {
		return StepKindHTTPFetch
	}
	if containsAny(entityKind, "Request", "Fetch", "Client", "HttpClient", "HTTPClient") {
		return StepKindHTTPFetch
	}

	// ── DB write (must precede db_query — write > read priority) ────────────
	if outEdgeKinds["WRITES_TO"] || outEdgeKinds["INSERTS_INTO"] ||
		outEdgeKinds["DELETES_FROM"] || outEdgeKinds["UPDATES"] {
		return StepKindDBWrite
	}

	// ── DB query ─────────────────────────────────────────────────────────────
	// READS_FROM/QUERIES are the imperative ORM read edges (Model.find()).
	// JOINS_COLLECTION (Mongo $lookup) and ACCESSES_TABLE are the data-access
	// topology edges emitted for FUNCTIONAL / fluent query-builder terminals —
	// a Mongo aggregation-pipeline builder (`mongo<…>().lookupOne(...).…`, #4320),
	// and the SQL builder family (knex/TypeORM) — whose terminal node carries no
	// imperative read edge. Without these, that terminal fell through to the
	// entity-kind substring matchers below and was mis-kinded `render` when the
	// builder/factory entity Kind happened to contain a view-ish token (#4337).
	if outEdgeKinds["READS_FROM"] || outEdgeKinds["QUERIES"] ||
		outEdgeKinds["JOINS_COLLECTION"] || outEdgeKinds["ACCESSES_TABLE"] {
		return StepKindDBQuery
	}
	// A SCOPE.DataAccess terminal is a data-access node even when its join
	// target is honestly unresolved (a non-static `from:` collection emits the
	// stage node but no JOINS_COLLECTION edge, #4320). Classify it as a DB query
	// step BY KIND — and, critically, BEFORE the render/test/validation entity-
	// kind substring matchers — so a query-builder terminal is never mis-kinded
	// `render`. Mirrors the db_query kind used for an imperative Model.find().
	if containsAny(entityKind, "DataAccess") {
		return StepKindDBQuery
	}

	// ── Message publish ──────────────────────────────────────────────────────
	if outEdgeKinds["PUBLISHES_TO"] || outEdgeKinds["PUBLISHES"] || outEdgeKinds["WS_EMITS"] {
		return StepKindMessagePublish
	}

	// ── Message consume — entry step with an incoming SUBSCRIBES_TO edge ────
	if isEntry && inEdgeKinds["SUBSCRIBES_TO"] {
		return StepKindMessageConsume
	}

	// ── Test assertion ───────────────────────────────────────────────────────
	if outEdgeKinds["ASSERTS"] {
		return StepKindTestAssert
	}
	if containsAny(entityKind, "Test", "Assert", "Spec") {
		return StepKindTestAssert
	}

	// ── UI render ────────────────────────────────────────────────────────────
	if containsAny(entityKind, "Component", "View", "Render", "Widget") {
		return StepKindRender
	}

	// ── Validation ───────────────────────────────────────────────────────────
	if containsAny(entityKind, "Validator", "Validation", "Schema") {
		return StepKindValidation
	}

	// ── Side effect (file IO, env mutation, global state) ───────────────────
	if outEdgeKinds["MUTATES_STATE"] || outEdgeKinds["WRITES_FILE"] || outEdgeKinds["READS_ENV"] {
		return StepKindSideEffect
	}

	// ── External lib call ────────────────────────────────────────────────────
	if outEdgeKinds["CALLS_EXTERNAL"] || outEdgeKinds["IMPORTS_FROM"] {
		return StepKindExternalLib
	}

	// ── Fallback ─────────────────────────────────────────────────────────────
	return StepKindFunctionCall
}

// containsAny returns true if s contains any of the given substrings
// (case-insensitive comparison).
func containsAny(s string, subs ...string) bool {
	sl := strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(sl, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Entry-kind inference
// ─────────────────────────────────────────────────────────────────────────────

// inferEntryKindFromKind returns the flow's entry_kind string from an already-
// resolved entityKind string and the set of incoming edge kinds on that entity.
// It is the lower-level helper; callers that have a *DashGroup and an entry ID
// should use inferEntryKind in handlers_flows.go instead.
func inferEntryKindFromKind(entityKind string, inEdgeKinds map[string]bool) string {
	el := strings.ToLower(entityKind)

	// HTTP handler / endpoint
	if containsAny(el, "handler", "endpoint", "route", "controller", "httphandler") {
		return "http_handler"
	}

	// Message / event consumer
	if containsAny(el, "consumer", "subscriber", "listener", "worker") ||
		inEdgeKinds["SUBSCRIBES_TO"] {
		return "message_consumer"
	}

	// Scheduled task / cron job
	if containsAny(el, "scheduler", "cron", "task", "job", "scheduled") {
		return "scheduled_task"
	}

	// UI component
	if containsAny(el, "component", "view", "page", "screen") {
		return "component"
	}

	return "function"
}

// ─────────────────────────────────────────────────────────────────────────────
// FlowMeta — top-level derived annotations
// ─────────────────────────────────────────────────────────────────────────────

// FlowMeta carries the top-level computed annotations added to the
// handleFlowDetail response.
type FlowMeta struct {
	EntryKind       string            `json:"entry_kind"`
	FlowSideEffects []string          `json:"flow_side_effects"`
	ComplexityScore float64           `json:"complexity_score"`
	IsCrossRepo     bool              `json:"is_cross_repo"`
	DataLineage     []DataLineagePair `json:"data_lineage"`
}

// DataLineagePair records a (read_source → write_sink) relationship observed
// within a single step that both reads from and writes to named targets.
type DataLineagePair struct {
	ReadSource string `json:"read_source"`
	WriteSink  string `json:"write_sink"`
}

// ─────────────────────────────────────────────────────────────────────────────
// stepEdgeMaps — builds per-entity outgoing/incoming edge maps for a group
// ─────────────────────────────────────────────────────────────────────────────

// stepEdgeMaps returns:
//   - outEdges:  entity-local-ID → {edgeKind → []toID}
//   - inEdgeKinds: entity-local-ID → {edgeKind → true}
func stepEdgeMaps(r *DashRepo) (
	outEdges map[string]map[string][]string,
	inEdgeKinds map[string]map[string]bool,
) {
	outEdges = map[string]map[string][]string{}
	inEdgeKinds = map[string]map[string]bool{}

	for _, rel := range r.Doc.Relationships {
		// outgoing
		if outEdges[rel.FromID] == nil {
			outEdges[rel.FromID] = map[string][]string{}
		}
		outEdges[rel.FromID][rel.Kind] = append(outEdges[rel.FromID][rel.Kind], rel.ToID)

		// incoming
		if inEdgeKinds[rel.ToID] == nil {
			inEdgeKinds[rel.ToID] = map[string]bool{}
		}
		inEdgeKinds[rel.ToID][rel.Kind] = true
	}
	return
}

// ─────────────────────────────────────────────────────────────────────────────
// annotateFlowSteps — attaches StepKind to every step and builds FlowMeta
// ─────────────────────────────────────────────────────────────────────────────

// AnnotatedStep mirrors the exported Step struct in handlers_flows.go but
// carries the additional Flows v2 fields.
type AnnotatedStep struct {
	EntityID string `json:"entity_id"`
	// Name is the entity's qualified name (e.g. "ReceivablesService.postSale").
	// Emitted as both `name` (consumed by the WebUI v2 step nodes) and `label`
	// for backward compatibility with the v1 frontend.
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	SourceFile  string   `json:"source_file"`
	StartLine   int      `json:"start_line"`
	Repo        string   `json:"repo"`
	StepIndex int    `json:"step_index"`
	EdgeKind  string `json:"edge_kind"`
	// EntityKind is the raw graph entity Kind of the step (e.g. "SCOPE.Operation").
	// Carried through so the server-side FlowDag payload (#4363) can label node
	// kinds from the resolved entity rather than re-deriving from step_kind.
	EntityKind  string   `json:"entity_kind,omitempty"`
	StepKind    string   `json:"step_kind"`
	SideEffects []string `json:"side_effects,omitempty"`
}

// annotateFlowSteps takes the resolved step list and the repos in the group
// and returns:
//   - annotated steps (each carrying step_kind + side_effects)
//   - FlowMeta with aggregated flow-level fields
//
// processRepo is the repo where the process entity lives (used for is_cross_repo).
func annotateFlowSteps(
	steps []rawStep,
	grp *DashGroup,
	processRepoSlug string,
	processEntryEntityID string,
) ([]AnnotatedStep, FlowMeta) {
	// Build per-repo edge maps (we only scan repos that appear in the step list).
	type repoEdges struct {
		out map[string]map[string][]string // localID → kind → []toID
		in  map[string]map[string]bool     // localID → kind → bool
	}
	edgeCache := map[string]*repoEdges{}
	getEdges := func(slug string) *repoEdges {
		if e, ok := edgeCache[slug]; ok {
			return e
		}
		r, ok := grp.Repos[slug]
		if !ok || r.Doc == nil {
			return &repoEdges{
				out: map[string]map[string][]string{},
				in:  map[string]map[string]bool{},
			}
		}
		out, in := stepEdgeMaps(r)
		re := &repoEdges{out: out, in: in}
		edgeCache[slug] = re
		return re
	}

	// Resolve entry entity kind from the process repo.
	var entryEntityKind string
	var entryInEdges map[string]bool
	if processEntryEntityID != "" {
		pe := getEdges(processRepoSlug)
		entryInEdges = pe.in[processEntryEntityID]
		// Look up kind.
		if r, ok := grp.Repos[processRepoSlug]; ok && r.Doc != nil {
			for i := range r.Doc.Entities {
				if r.Doc.Entities[i].ID == processEntryEntityID {
					entryEntityKind = r.Doc.Entities[i].Kind
					break
				}
			}
		}
	}

	annotated := make([]AnnotatedStep, 0, len(steps))
	sideEffectSet := map[string]bool{}
	kindSet := map[string]bool{}
	isCrossRepo := false

	// Data lineage: collect (readSrc, writeSink) pairs per step.
	var lineagePairs []DataLineagePair

	for _, s := range steps {
		// Determine repo slug for this step.
		repoSlug, localID := dashSplitPrefixed(s.EntityID)
		if repoSlug == "" {
			repoSlug = s.Repo
		}
		if repoSlug != processRepoSlug {
			isCrossRepo = true
		}

		pe := getEdges(repoSlug)
		outMap := pe.out[localID] // kind → []toID
		inMap := pe.in[localID]   // kind → bool

		// Flatten outMap keys into a set for classifier.
		outKindSet := make(map[string]bool, len(outMap))
		for k := range outMap {
			outKindSet[k] = true
		}

		kind := classifyStepKind(s.EntityKind, outKindSet, inMap, s.StepIndex == 0)
		kindSet[kind] = true

		// Per-step side-effect targets (the ToIDs of relevant edges).
		var stepSideEffects []string
		if kind == StepKindDBWrite {
			for _, to := range outMap["WRITES_TO"] {
				stepSideEffects = append(stepSideEffects, to)
			}
			for _, to := range outMap["INSERTS_INTO"] {
				stepSideEffects = append(stepSideEffects, to)
			}
			for _, to := range outMap["UPDATES"] {
				stepSideEffects = append(stepSideEffects, to)
			}
			for _, to := range outMap["DELETES_FROM"] {
				stepSideEffects = append(stepSideEffects, to)
			}
		} else if kind == StepKindMessagePublish {
			for _, to := range outMap["PUBLISHES_TO"] {
				stepSideEffects = append(stepSideEffects, to)
			}
			for _, to := range outMap["PUBLISHES"] {
				stepSideEffects = append(stepSideEffects, to)
			}
		} else if kind == StepKindHTTPFetch {
			for _, to := range outMap["FETCHES"] {
				stepSideEffects = append(stepSideEffects, to)
			}
		}

		// Aggregate flow-level side effects (by kind).
		if effectKinds[kind] {
			sideEffectSet[kind] = true
		}

		// Data lineage: a step that both reads (READS_FROM / QUERIES) and
		// writes (WRITES_TO / INSERTS_INTO / UPDATES / DELETES_FROM) in the
		// same step contributes one lineage pair.
		readTargets := append(outMap["READS_FROM"], outMap["QUERIES"]...)
		writeTargets := append(append(append(
			outMap["WRITES_TO"],
			outMap["INSERTS_INTO"]...,
		), outMap["UPDATES"]...), outMap["DELETES_FROM"]...)
		for _, rSrc := range readTargets {
			for _, wSink := range writeTargets {
				lineagePairs = append(lineagePairs, DataLineagePair{
					ReadSource: rSrc,
					WriteSink:  wSink,
				})
			}
		}

		annotated = append(annotated, AnnotatedStep{
			EntityID:    s.EntityID,
			Name:        s.Label,
			Label:       s.Label,
			SourceFile:  s.SourceFile,
			StartLine:   s.StartLine,
			Repo:        s.Repo,
			StepIndex:   s.StepIndex,
			EdgeKind:    s.EdgeKind,
			EntityKind:  s.EntityKind,
			StepKind:    kind,
			SideEffects: stepSideEffects,
		})
	}

	// Build flow_side_effects list (sorted for stability).
	flowSideEffects := make([]string, 0, len(sideEffectSet))
	for k := range sideEffectSet {
		flowSideEffects = append(flowSideEffects, k)
	}
	sortStrings(flowSideEffects)

	// complexity_score = step count × kind diversity.
	complexityScore := float64(len(steps)) * float64(len(kindSet))

	meta := FlowMeta{
		EntryKind:       inferEntryKindFromKind(entryEntityKind, entryInEdges),
		FlowSideEffects: flowSideEffects,
		ComplexityScore: complexityScore,
		IsCrossRepo:     isCrossRepo,
		DataLineage:     dedupeLineage(lineagePairs),
	}

	return annotated, meta
}

// dedupeLineage removes duplicate (read_source, write_sink) pairs while
// preserving insertion order.
func dedupeLineage(pairs []DataLineagePair) []DataLineagePair {
	seen := map[string]bool{}
	out := make([]DataLineagePair, 0, len(pairs))
	for _, p := range pairs {
		key := p.ReadSource + "\x00" + p.WriteSink
		if !seen[key] {
			seen[key] = true
			out = append(out, p)
		}
	}
	if out == nil {
		return []DataLineagePair{}
	}
	return out
}

// sortStrings sorts a slice of strings in place (avoids importing sort in
// callers that already have a local copy of the logic).
func sortStrings(ss []string) {
	// insertion sort — step-kind lists are tiny (< 10 elements).
	for i := 1; i < len(ss); i++ {
		key := ss[i]
		j := i - 1
		for j >= 0 && ss[j] > key {
			ss[j+1] = ss[j]
			j--
		}
		ss[j+1] = key
	}
}

// rawStep is the internal transfer type that carries the fields gathered from
// STEP_IN_PROCESS edges before annotation. It mirrors the old Step struct from
// handlers_flows.go so we can pass it into annotateFlowSteps without exposing
// the annotation internals to the handler.
type rawStep struct {
	EntityID   string
	Label      string
	SourceFile string
	StartLine  int
	Repo       string
	StepIndex  int
	EdgeKind   string
	EntityKind string // populated from the step entity
}
