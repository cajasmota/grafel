// Package enrichment defines the agent-driven enrichment pipeline. The
// indexer emits subjective enrichment "candidates" — entries that an
// external agent (via the MCP server) is expected to resolve. Resolutions
// are merged back onto entities on subsequent index runs so previously
// agent-resolved values are preserved.
//
// The split between deterministic enrichment (cyclomatic complexity,
// deprecation markers, test classification, ...) and subjective
// enrichment (one-line description, domain classification, role
// description) is described in issue #15. Deterministic enrichers stay
// inline in internal/enrichers/. Subjective enrichers live here as
// CandidateEmitters that emit Candidate records.
package enrichment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
)

// Candidate kinds. These are the canonical "kind" string values the agent
// branches on when deciding which prompt to apply.
const (
	KindDescribeEntity  = "describe_entity"
	KindClassifyDomain  = "classify_domain"
	KindInferXLangCall  = "infer_xlang_call"
	KindSummarizeAPI    = "summarize_api"
	KindFlagDeadCode    = "flag_dead_code"
	KindDescribeRole    = "describe_role"
	KindNameCommunity   = "name_community"
)

// communitySubjectID returns the synthetic subject ID for a community
// enrichment candidate. The prefix "community:" distinguishes it from
// entity IDs so the resolution consumer can route correctly.
func communitySubjectID(communityID int) string {
	return fmt.Sprintf("community:%d", communityID)
}

// CandidatesSchemaVersion is the integer version of the on-disk
// enrichment-candidates.json schema. Bump on a breaking change.
//
// v2 (ADR-0015 phase-1, issue #544) introduces the "repair_edge" kind. v1
// readers may safely skip-by-kind on unknown kinds, so a v2 file remains
// readable by v1 consumers — only writers that emit repair_edge entries
// need to set this to 2.
const CandidatesSchemaVersion = 2

// Candidate is one row in <repo>/.archigraph/enrichment-candidates.json.
// Subject_id is always the local entity id (NOT prefixed with repo).
type Candidate struct {
	ID                   string         `json:"id"`
	Kind                 string         `json:"kind"`
	SubjectID            string         `json:"subject_id"`
	Context              map[string]any `json:"context,omitempty"`
	PromptTemplate       string         `json:"prompt_template,omitempty"`
	ConfidenceFloor      float64        `json:"confidence_floor,omitempty"`
	DiscoveredAt         string         `json:"discovered_at,omitempty"`
	QualificationSignals []string       `json:"qualification_signals,omitempty"`
	// Score is the 0–100 prioritisation score for this candidate, computed at
	// emit time by ComputeScore. Higher scores indicate higher enrichment value
	// (UX-critical: used to sort the dashboard pending queue and determine the
	// criticality band displayed to the user).
	Score          int    `json:"score,omitempty"`
	// ScoreBreakdown is a human-readable string listing every modifier that
	// fired, e.g. "base:40 + ambiguous_name:+15 + articulation:+15 = 70".
	// Provided for debugging and agent reasoning.
	ScoreBreakdown string `json:"score_breakdown,omitempty"`
	// CriticalityBand is the tier derived from Score:
	//   critical (>=80) / high (60–79) / medium (40–59) / low (<40)
	CriticalityBand string `json:"criticality_band,omitempty"`
}

// kindBaseScore returns the base score for an entity kind.
// Values calibrated against the enrichment research from issue #1162.
func kindBaseScore(kind string) (int, string) {
	switch kind {
	// Public API surface — always highest priority.
	case "http_endpoint", "HTTPEndpoint", "SCOPE.HTTPEndpoint", "Route", "SCOPE.Route":
		return 80, "base_http_endpoint:80"
	// Named architectural roles.
	case "Service", "SCOPE.Service", "Controller", "SCOPE.Controller",
		"View", "SCOPE.View":
		return 65, "base_service_controller:65"
	case "Schema", "Model":
		return 60, "base_schema_model:60"
	case "DataAccess", "SCOPE.DataAccess":
		return 55, "base_data_access:55"
	// Background task / process kinds.
	case "Task", "SCOPE.ScheduledJob", "Process":
		return 45, "base_process:45"
	// Generic operations.
	case "Operation", "SCOPE.Operation":
		return 40, "base_operation:40"
	// Components.
	case "Component", "SCOPE.Component":
		return 35, "base_component:35"
	default:
		return 35, "base_default:35"
	}
}

// ambiguousNames is the set of very common verb names that indicate an entity
// whose role is unclear from the name alone (+15 modifier).
var ambiguousNames = map[string]bool{
	"process": true, "handle": true, "run": true, "make": true,
	"do": true, "main": true, "init": true, "setup": true,
	"update": true, "execute": true,
}

// criticalityBand returns the criticality tier name for a 0–100 score.
func criticalityBand(score int) string {
	switch {
	case score >= 80:
		return "critical"
	case score >= 60:
		return "high"
	case score >= 40:
		return "medium"
	default:
		return "low"
	}
}

// ComputeScore calculates the 0–100 confidence score for a candidate, together
// with a human-readable breakdown and a criticality band. The entity e must be
// the same entity whose ID is c.SubjectID; passing nil produces a safe zero.
//
// Scoring formula (from issue #1131 spec):
//
//	Base by kind:
//	  http_endpoint / Route              → 80
//	  Service / Controller / View / Route → 65
//	  Schema / Model                     → 60
//	  DataAccess                         → 55
//	  Process / Task / ScheduledJob      → 45
//	  Operation                          → 40
//	  Component                          → 35 (default)
//
//	Positive modifiers:
//	  +20  is_god_node
//	  +15  is_articulation_point
//	  +10  pagerank >= 0.01
//	  +15  name in {process, handle, run, make, do, main, init, setup, update, execute}
//
//	Negative modifiers:
//	  -15  len(name) <= 4
//	  -10  source_file empty
//	  -20  all-lowercase-underscore name with len < 10 (private helper heuristic)
//
// The result is clamped to [0, 100].
func ComputeScore(e *graph.Entity) (score int, breakdown string, band string) {
	if e == nil {
		return 0, "nil_entity", "low"
	}

	base, baseLabel := kindBaseScore(e.Kind)
	total := base
	parts := []string{baseLabel}

	// +20 god node.
	if e.IsGodNode {
		total += 20
		parts = append(parts, "+god_node:20")
	}
	// +15 articulation point.
	if e.IsArticulationPt {
		total += 15
		parts = append(parts, "+articulation:15")
	}
	// +10 high pagerank.
	if e.PageRank != nil && *e.PageRank >= 0.01 {
		total += 10
		parts = append(parts, "+pagerank:10")
	}
	// +15 genuinely ambiguous name (common verb with no domain context).
	nameLower := strings.ToLower(e.Name)
	if ambiguousNames[nameLower] {
		total += 15
		parts = append(parts, "+ambiguous_name:15")
	}

	// -15 very short name (≤4 chars, hard to describe meaningfully).
	if len(e.Name) <= 4 {
		total -= 15
		parts = append(parts, "-short_name:15")
	}
	// -10 no source file (synthetic / cross-repo placeholder).
	if e.SourceFile == "" {
		total -= 10
		parts = append(parts, "-no_source_file:10")
	}
	// -20 private helper heuristic: all-lowercase with underscore prefix or
	// pure snake_case name shorter than 10 chars.
	if len(e.Name) < 10 && isPrivateHelper(e.Name) {
		total -= 20
		parts = append(parts, "-private_helper:20")
	}

	// Clamp.
	if total > 100 {
		total = 100
	}
	if total < 0 {
		total = 0
	}

	bd := strings.Join(parts, " ") + " = " + strconv.Itoa(total)
	return total, bd, criticalityBand(total)
}

// isPrivateHelper returns true for names that follow lowercase-underscore
// conventions typical of private helper functions (e.g. "__helper", "_run",
// "do_it", "run_fn").
func isPrivateHelper(name string) bool {
	if len(name) == 0 {
		return false
	}
	// Must start with underscore or be all lowercase with underscores/digits only.
	for _, ch := range name {
		if ch >= 'A' && ch <= 'Z' {
			return false // has uppercase → not private helper
		}
	}
	// Require underscore to indicate snake_case or dunder naming.
	return strings.ContainsRune(name, '_')
}

// Resolution is one row in <repo>/.archigraph/enrichment-resolutions.json.
// When index runs it loads resolutions and writes Value into the matching
// entity's Properties (under the resolution's Kind key) before final emit.
type Resolution struct {
	ID         string  `json:"id"`
	SubjectID  string  `json:"subject_id"`
	Kind       string  `json:"kind"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence,omitempty"`
	Reason     string  `json:"reason,omitempty"`
	ResolvedAt string  `json:"resolved_at,omitempty"`
}

// Rejection is one row in <repo>/.archigraph/enrichment-rejections.json.
// Rejected (subject_id, kind) pairs are skipped on subsequent index runs.
type Rejection struct {
	ID         string `json:"id"`
	SubjectID  string `json:"subject_id"`
	Kind       string `json:"kind"`
	Reason     string `json:"reason,omitempty"`
	RejectedAt string `json:"rejected_at,omitempty"`
}

// CandidateEmitter is implemented by every subjective enrichment source.
// Name returns a stable identifier (used in logs/metrics). EmitFor returns
// zero-or-more candidates for the given entity. The Document is supplied
// so emitters can inspect post-Pass-4 graph signals (community membership,
// centrality, articulation points, etc.).
type CandidateEmitter interface {
	Name() string
	EmitFor(entity *graph.Entity, doc *graph.Document) []Candidate
}

// candidateID is the deterministic ID of a candidate. Stable across runs
// for the same (subject_id, kind) tuple — this is what keeps the emit
// pipeline idempotent.
func candidateID(subjectID, kind string) string {
	h := sha256.New()
	h.Write([]byte(kind))
	h.Write([]byte{0})
	h.Write([]byte(subjectID))
	return "ec:" + hex.EncodeToString(h.Sum(nil))[:16]
}

// nowRFC3339 is a tiny indirection so tests can pin the timestamp via
// the DiscoveredAt field comparator without freezing time globally.
var nowRFC3339 = func() string { return time.Now().UTC().Format(time.RFC3339) }

// describeEntityNoiseKinds is the set of entity kinds that are structural or
// framework artefacts and therefore not meaningful targets for agent-written
// descriptions. Entities in this set are skipped by describeEntityEmitter.
var describeEntityNoiseKinds = map[string]bool{
	"SCOPE.Pattern":    true,
	"SCOPE.External":   true,
	"SCOPE.Heading":    true,
	"SCOPE.Stylesheet": true,
	"SCOPE.CodeBlock":  true,
	"SCOPE.Document":   true,
}

// selfDescriptiveOperationRE matches SCOPE.Operation names that are fully
// self-describing: the name is a verb prefix + capitalised noun, so a
// one-sentence description would be a trivial paraphrase of the name itself
// (e.g. "getUserById" → "Gets a user by ID"). Emitting candidates for these
// entities wastes agent budget without producing actionable signal.
//
// Pattern: verb prefix followed immediately by an uppercase letter, meaning
// the whole name encodes both the action and the subject.
var selfDescriptiveOperationRE = regexp.MustCompile(
	`^(get|set|is|has|can|validate|parse|format|create|delete|fetch|load|save|send|build|render|on|use)[A-Z][a-zA-Z]+$`,
)

// qualifyHTTPKinds is the set of entity kinds that represent public API
// surface — HTTP endpoints and route definitions — that always qualify for
// enrichment because their intent and contract must be documented.
var qualifyHTTPKinds = map[string]bool{
	"http_endpoint":      true,
	"HTTPEndpoint":       true,
	"Route":              true,
	"SCOPE.Route":        true,
	"SCOPE.HTTPEndpoint": true,
}

// qualifyHighArchKinds is the set of entity kinds that represent named
// architectural roles (controllers, services, background tasks, etc.). These
// are not self-describing from their name alone and benefit from an
// agent-written description that explains their responsibility.
var qualifyHighArchKinds = map[string]bool{
	"Controller":         true,
	"Service":            true,
	"SCOPE.Service":      true,
	"SCOPE.Controller":   true,
	"SCOPE.ExternalAPI":  true,
	"SCOPE.ScheduledJob": true,
	"SCOPE.DataAccess":   true,
	"Model":              true,
	"Task":               true,
	"View":               true,
}

// qualifyComplexComponentRE matches SCOPE.Component names whose name starts
// with an uppercase letter and encodes an architectural pattern suffix
// (Manager, Handler, Provider, Context, Reducer, Store, Orchestrator,
// Coordinator). The pattern requires an uppercase letter before the suffix
// so it fires on "OrderManager" but not on "setCurrentPage" or path names
// that happen to contain these words. File-path names containing "/" are
// excluded separately by the caller.
var qualifyComplexComponentRE = regexp.MustCompile(
	`^[A-Z][A-Za-z]*(Manager|Handler|Provider|Context|Reducer|Orchestrat|Coordinator)$`,
)

// containsSlash reports whether s contains a "/" character. Used to detect
// file-path-like component names (e.g. "src/hooks/useAuth.ts") which are
// module-level containers rather than individually describable entities.
func containsSlash(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return true
		}
	}
	return false
}

// qualifiesForEnrichment returns true when entity e is a research-validated
// candidate for agent enrichment, together with the signals that drove the
// decision. The default policy is NOT to enrich: an entity qualifies ONLY
// when it hits at least one positive criterion.
//
// Signal hierarchy (from Phase 1 research on a 500-entity sample of the
// target codebase, 2026-05-21):
//  1. http_endpoint / Route          → 100% enrichment value (public API surface)
//  2. god_node / articulation_point  → high structural importance
//  3. high_arch_kind                 → named architectural role
//  4. complex_component              → SCOPE.Component with architectural name
//  5. ambiguous_name                 → very short lowercase name (no semantic signal)
//
// Kinds explicitly excluded:
//   - SCOPE.Schema, SCOPE.Pattern, SCOPE.External, SCOPE.Heading,
//     SCOPE.Stylesheet, SCOPE.CodeBlock, SCOPE.Document  (noise / structural)
//   - SCOPE.Operation / SCOPE.Component with self-descriptive names
//   - Plain state variables and small helpers (the long tail)
//
// Empirical target: 20-30% of entities in a typical codebase qualify.
func qualifiesForEnrichment(e *graph.Entity) (qualified bool, signals []string) {
	if e == nil || e.Name == "" {
		return false, nil
	}

	// --- Noise kinds: never qualify ---
	if describeEntityNoiseKinds[e.Kind] {
		return false, nil
	}

	// --- Signal 1: HTTP endpoint / Route (public API surface) ---
	if qualifyHTTPKinds[e.Kind] {
		return true, []string{"http_endpoint"}
	}

	// --- Signal 2: structural importance ---
	if e.IsGodNode {
		signals = append(signals, "god_node")
	}
	if e.IsArticulationPt {
		signals = append(signals, "articulation_point")
	}
	if len(signals) > 0 {
		// Self-descriptive operation names are excluded even when structurally
		// central (the name already communicates the purpose; describe_entity
		// adds no value). Exception: god nodes still warrant description because
		// they are hubs that developers need context on beyond the name alone.
		if (e.Kind == "SCOPE.Operation" || e.Kind == "Operation") &&
			selfDescriptiveOperationRE.MatchString(e.Name) &&
			!e.IsGodNode {
			// Articulation point but self-descriptive name — skip describe_entity.
			// describe_role is still valid and is handled by describeRoleEmitter
			// separately (which has its own selfDescriptiveOperationRE guard that
			// already correctly filters non-god nodes via its own check).
			return false, nil
		}
		return true, signals
	}

	// --- Signal 3: named architectural role ---
	if qualifyHighArchKinds[e.Kind] {
		return true, []string{"high_arch_kind:" + e.Kind}
	}

	// --- Signal 4: complex SCOPE.Component (architectural pattern in name) ---
	// File-path names (containing "/") are module-level containers, not
	// individually describable components, so they are excluded.
	if e.Kind == "SCOPE.Component" &&
		!containsSlash(e.Name) &&
		qualifyComplexComponentRE.MatchString(e.Name) {
		return true, []string{"complex_component"}
	}

	// --- Signal 5: genuinely ambiguous name ---
	// A single-word, all-lowercase name of 2–9 characters that is NOT a common
	// programming/domain term qualifies when attached to an Operation or
	// Component kind, because without a description the reader has no hint
	// about the entity's purpose. Common state-variable terms (data, loading,
	// error, form, …) are excluded because they are self-explanatory in context.
	if (e.Kind == "SCOPE.Operation" || e.Kind == "SCOPE.Component" || e.Kind == "Operation") &&
		!selfDescriptiveOperationRE.MatchString(e.Name) &&
		!commonProgrammingTerms[e.Name] {
		n := e.Name
		if len(n) >= 2 && len(n) <= 9 {
			allLower := true
			for i := 0; i < len(n); i++ {
				if n[i] >= 'A' && n[i] <= 'Z' || n[i] == '.' || n[i] == ':' {
					allLower = false
					break
				}
			}
			if allLower {
				return true, []string{"ambiguous_name"}
			}
		}
	}

	// Default: does not qualify
	return false, nil
}

// commonProgrammingTerms is the set of short lowercase names that are
// self-explanatory in a React/Python/TypeScript context and should NOT
// trigger the ambiguous-name enrichment signal even when they are ≤ 9 chars.
// These terms are unambiguous to any developer without a written description.
var commonProgrammingTerms = map[string]bool{
	// React / component primitives
	"data": true, "loading": true, "error": true, "form": true, "modal": true,
	"state": true, "items": true, "list": true, "table": true, "row": true,
	"columns": true, "filters": true, "styles": true, "style": true,
	"title": true, "label": true, "value": true, "values": true,
	"onChange": true, "onPress": true, "onClick": true,
	// Auth / API primitives
	"token": true, "tokens": true, "id": true, "ids": true, "key": true,
	"payload": true, "params": true, "body": true, "headers": true,
	"status": true, "result": true, "results": true, "response": true,
	"request": true, "options": true, "config": true, "settings": true,
	// Pagination / list
	"limit": true, "offset": true, "page": true, "pages": true, "total": true,
	"count": true, "index": true, "size": true,
	// Component lifecycle / state
	"current": true, "next": true, "prev": true, "initial": true,
	"pending": true, "done": true, "open": true, "show": true, "hide": true,
	"active": true, "enabled": true, "visible": true, "checked": true,
	"selected": true, "focused": true, "editing": true, "saving": true,
	"progress": true, "refetch": true, "refresh": true, "reset": true,
	"clear": true, "submit": true,
	// Misc short terms
	"ref": true, "refs": true, "item": true, "type": true, "kind": true,
	"name": true, "text": true, "url": true, "path": true, "uri": true,
	"user": true, "role": true, "scope": true, "mode": true, "time": true,
	"date": true, "message": true, "msg": true,
}

// ---------------------------------------------------------------------------
// Built-in emitters
// ---------------------------------------------------------------------------

// describeEntityEmitter emits a candidate for any entity that passes the
// research-validated positive-selection predicate qualifiesForEnrichment.
//
// Prior behaviour (issue #1162): the emitter used a negative rule — emit for
// any entity that "lacks a description property". For a freshly-extracted
// graph nothing has a description, so everything qualified (22,427 candidates
// ≈ 100% of entities). The new rule inverts this: default policy is NOT to
// enrich; an entity qualifies only when it hits a positive signal.
type describeEntityEmitter struct{}

func (describeEntityEmitter) Name() string { return KindDescribeEntity }

func (describeEntityEmitter) EmitFor(e *graph.Entity, _ *graph.Document) []Candidate {
	if e == nil || e.Name == "" {
		return nil
	}
	// Skip template-literal names (extracted URL templates, not describable entities).
	if strings.Contains(e.Name, "${") {
		return nil
	}
	// Skip if already described.
	if v, ok := e.Properties["description"]; ok && v != "" {
		return nil
	}
	// Positive selection: emit only when the entity passes research-validated criteria.
	ok, sigs := qualifiesForEnrichment(e)
	if !ok {
		return nil
	}
	sc, bd, band := ComputeScore(e)
	return []Candidate{{
		ID:        candidateID(e.ID, KindDescribeEntity),
		Kind:      KindDescribeEntity,
		SubjectID: e.ID,
		Context: map[string]any{
			"name":        e.Name,
			"kind":        e.Kind,
			"language":    e.Language,
			"source_file": e.SourceFile,
			"signature":   e.Signature,
		},
		PromptTemplate:       "Describe the {{kind}} {{name}} in one sentence.",
		ConfidenceFloor:      0.6,
		DiscoveredAt:         nowRFC3339(),
		QualificationSignals: sigs,
		Score:                sc,
		ScoreBreakdown:       bd,
		CriticalityBand:      band,
	}}
}

// classifyDomainEmitter emits a candidate for entities that look
// architecturally significant — high pagerank or god-node flag — so the
// agent assigns a domain (auth, billing, search, ...).
type classifyDomainEmitter struct{}

func (classifyDomainEmitter) Name() string { return KindClassifyDomain }

func (classifyDomainEmitter) EmitFor(e *graph.Entity, _ *graph.Document) []Candidate {
	if e == nil || e.Name == "" {
		return nil
	}
	// Pre-check: skip noise kinds and self-descriptive operations uniformly.
	if describeEntityNoiseKinds[e.Kind] {
		return nil
	}
	if e.Kind == "SCOPE.Operation" && selfDescriptiveOperationRE.MatchString(e.Name) {
		return nil
	}
	if v, ok := e.Properties["domain"]; ok && v != "" {
		return nil
	}
	var sigs []string
	if e.IsGodNode {
		sigs = append(sigs, "god_node")
	}
	if e.PageRank != nil && *e.PageRank >= 0.01 {
		sigs = append(sigs, "high_pagerank")
	}
	if len(sigs) == 0 {
		return nil
	}
	sc, bd, band := ComputeScore(e)
	return []Candidate{{
		ID:        candidateID(e.ID, KindClassifyDomain),
		Kind:      KindClassifyDomain,
		SubjectID: e.ID,
		Context: map[string]any{
			"name":        e.Name,
			"kind":        e.Kind,
			"is_god_node": e.IsGodNode,
		},
		PromptTemplate:       "Classify the business domain of {{name}}.",
		ConfidenceFloor:      0.5,
		DiscoveredAt:         nowRFC3339(),
		QualificationSignals: sigs,
		Score:                sc,
		ScoreBreakdown:       bd,
		CriticalityBand:      band,
	}}
}

// describeRoleEmitter emits a candidate for clearly architectural nodes
// (god nodes, articulation points) so the agent assigns a role label
// (controller / adapter / policy / orchestrator / ...).
type describeRoleEmitter struct{}

func (describeRoleEmitter) Name() string { return KindDescribeRole }

func (describeRoleEmitter) EmitFor(e *graph.Entity, _ *graph.Document) []Candidate {
	if e == nil || e.Name == "" {
		return nil
	}
	// Pre-check: skip noise kinds and self-descriptive operations uniformly.
	if describeEntityNoiseKinds[e.Kind] {
		return nil
	}
	if e.Kind == "SCOPE.Operation" && selfDescriptiveOperationRE.MatchString(e.Name) {
		return nil
	}
	if v, ok := e.Properties["architectural_role"]; ok && v != "" {
		return nil
	}
	if !e.IsGodNode && !e.IsArticulationPt {
		return nil
	}
	var sigs []string
	if e.IsGodNode {
		sigs = append(sigs, "god_node")
	}
	if e.IsArticulationPt {
		sigs = append(sigs, "articulation_point")
	}
	sc, bd, band := ComputeScore(e)
	return []Candidate{{
		ID:        candidateID(e.ID, KindDescribeRole),
		Kind:      KindDescribeRole,
		SubjectID: e.ID,
		Context: map[string]any{
			"name":                  e.Name,
			"kind":                  e.Kind,
			"is_god_node":           e.IsGodNode,
			"is_articulation_point": e.IsArticulationPt,
		},
		PromptTemplate:       "Describe the architectural role of {{name}} (controller/adapter/policy/orchestrator/...).",
		ConfidenceFloor:      0.5,
		DiscoveredAt:         nowRFC3339(),
		QualificationSignals: sigs,
		Score:                sc,
		ScoreBreakdown:       bd,
		CriticalityBand:      band,
	}}
}

// DefaultEmitters returns the built-in emitter set. Callers are free to
// extend this slice; CollectCandidates accepts any []CandidateEmitter.
func DefaultEmitters() []CandidateEmitter {
	return []CandidateEmitter{
		describeEntityEmitter{},
		classifyDomainEmitter{},
		describeRoleEmitter{},
	}
}

// ---------------------------------------------------------------------------
// Collect / merge / persist
// ---------------------------------------------------------------------------

// CollectCandidates runs every emitter against every entity in doc and
// returns the merged candidate set. Rejected (subject_id, kind) pairs are
// dropped. Output is sorted by (subject_id, kind) for stable diffs.
func CollectCandidates(doc *graph.Document, emitters []CandidateEmitter, rejected map[string]bool) []Candidate {
	if doc == nil {
		return nil
	}
	var out []Candidate
	seen := map[string]bool{}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		for _, em := range emitters {
			for _, c := range em.EmitFor(e, doc) {
				if c.ID == "" || seen[c.ID] {
					continue
				}
				key := c.SubjectID + "|" + c.Kind
				if rejected[key] {
					continue
				}
				seen[c.ID] = true
				out = append(out, c)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SubjectID != out[j].SubjectID {
			return out[i].SubjectID < out[j].SubjectID
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// candidatesPath returns the on-disk path for enrichment-candidates.json.
func candidatesPath(repoArchigraphDir string) string {
	return filepath.Join(repoArchigraphDir, "enrichment-candidates.json")
}

// resolutionsPath returns the on-disk path for enrichment-resolutions.json.
func resolutionsPath(repoArchigraphDir string) string {
	return filepath.Join(repoArchigraphDir, "enrichment-resolutions.json")
}

// rejectionsPath returns the on-disk path for enrichment-rejections.json.
func rejectionsPath(repoArchigraphDir string) string {
	return filepath.Join(repoArchigraphDir, "enrichment-rejections.json")
}

// candidatesEnvelope is the on-disk array form. We accept a bare array
// for forward-compatibility with the MCP candidate readers in
// internal/mcp/candidates.go (which already accept either form).
type candidatesEnvelope struct {
	Version    int         `json:"version"`
	Candidates []Candidate `json:"candidates"`
}

// WriteCandidates persists candidates atomically. The output is sorted +
// versioned so subsequent runs produce byte-identical files when nothing
// has changed.
//
// Idempotence (issue #53): emitters populate DiscoveredAt with a wall-clock
// timestamp, which would otherwise change every run and break byte-stable
// re-emission. To keep the on-disk file byte-stable for unchanged inputs,
// we merge with any prior on-disk candidate set and preserve the existing
// discovered_at for any candidate whose ID already appeared. New
// candidates keep whatever DiscoveredAt the emitter assigned.
func WriteCandidates(archigraphDir string, cs []Candidate) error {
	if err := os.MkdirAll(archigraphDir, 0o755); err != nil {
		return fmt.Errorf("enrichment: mkdir %s: %w", archigraphDir, err)
	}
	path := candidatesPath(archigraphDir)

	// Merge prior discovered_at values so a candidate ID seen on a previous
	// run keeps its original timestamp. This is what makes consecutive runs
	// over the same input produce byte-identical output.
	merged := mergeDiscoveredAt(path, cs)

	tmp := path + ".tmp"
	env := candidatesEnvelope{Version: CandidatesSchemaVersion, Candidates: merged}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("enrichment: marshal candidates: %w", err)
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("enrichment: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("enrichment: rename: %w", err)
	}
	return nil
}

// mergeDiscoveredAt returns a copy of cs in which any candidate whose ID
// matches a record in the prior on-disk file has its DiscoveredAt
// replaced with the prior value. Order is preserved. Missing or
// unparseable prior files are tolerated — in that case the input slice is
// returned unmodified.
func mergeDiscoveredAt(path string, cs []Candidate) []Candidate {
	data, err := os.ReadFile(path)
	if err != nil {
		return cs
	}
	prior := map[string]string{}
	// Tolerate either the envelope or the bare-array shape.
	var env candidatesEnvelope
	if err := json.Unmarshal(data, &env); err == nil && len(env.Candidates) > 0 {
		for _, p := range env.Candidates {
			if p.ID != "" && p.DiscoveredAt != "" {
				prior[p.ID] = p.DiscoveredAt
			}
		}
	} else {
		var arr []Candidate
		if err := json.Unmarshal(data, &arr); err == nil {
			for _, p := range arr {
				if p.ID != "" && p.DiscoveredAt != "" {
					prior[p.ID] = p.DiscoveredAt
				}
			}
		}
	}
	if len(prior) == 0 {
		return cs
	}
	out := make([]Candidate, len(cs))
	copy(out, cs)
	for i := range out {
		if t, ok := prior[out[i].ID]; ok {
			out[i].DiscoveredAt = t
		}
	}
	return out
}

// ReadResolutions reads enrichment-resolutions.json. Tolerates both the
// bare array and the {"resolutions": [...]} envelope. Returns nil on
// missing file.
func ReadResolutions(archigraphDir string) []Resolution {
	data, err := os.ReadFile(resolutionsPath(archigraphDir))
	if err != nil {
		return nil
	}
	var arr []Resolution
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr
	}
	var env struct {
		Resolutions []Resolution `json:"resolutions"`
	}
	if err := json.Unmarshal(data, &env); err == nil {
		return env.Resolutions
	}
	// Tolerate the MCP server's existing on-disk shape (CandidateID/NodeID).
	var legacy []struct {
		CandidateID string  `json:"candidate_id"`
		NodeID      string  `json:"node_id"`
		Kind        string  `json:"kind"`
		Value       string  `json:"value"`
		Confidence  float64 `json:"confidence,omitempty"`
		Reason      string  `json:"reason,omitempty"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil
	}
	out := make([]Resolution, 0, len(legacy))
	for _, l := range legacy {
		out = append(out, Resolution{
			ID:         l.CandidateID,
			SubjectID:  l.NodeID,
			Kind:       l.Kind,
			Value:      l.Value,
			Confidence: l.Confidence,
			Reason:     l.Reason,
		})
	}
	return out
}

// ReadRejections reads enrichment-rejections.json. Returns a set keyed by
// "<subject_id>|<kind>" for fast skip lookups during emission. Tolerates
// the MCP server's legacy {candidate_id, reason} shape: when we see one
// of those we record it by candidate_id (no subject/kind known), but we
// also register all on-disk Rejection records by their proper key.
func ReadRejections(archigraphDir string) map[string]bool {
	data, err := os.ReadFile(rejectionsPath(archigraphDir))
	if err != nil {
		return map[string]bool{}
	}
	out := map[string]bool{}
	var rejs []Rejection
	if err := json.Unmarshal(data, &rejs); err == nil {
		for _, r := range rejs {
			if r.SubjectID != "" && r.Kind != "" {
				out[r.SubjectID+"|"+r.Kind] = true
			}
			if r.ID != "" {
				out[r.ID] = true
			}
		}
		return out
	}
	// Legacy shape: array of {candidate_id, reason}. Register by the
	// candidate id only — the indexer also registers candidate-id keys
	// in CollectCandidates' `seen` set semantics, so a re-run of the
	// emitter will produce the same id and we can skip it.
	var legacy []struct {
		CandidateID string `json:"candidate_id"`
	}
	if err := json.Unmarshal(data, &legacy); err == nil {
		for _, l := range legacy {
			if l.CandidateID != "" {
				out[l.CandidateID] = true
			}
		}
	}
	return out
}

// CollectCandidatesSkippingRejected is a convenience wrapper that loads
// the rejections file and filters them out in one call.
func CollectCandidatesSkippingRejected(doc *graph.Document, emitters []CandidateEmitter, archigraphDir string) []Candidate {
	rej := ReadRejections(archigraphDir)
	cands := CollectCandidates(doc, emitters, rej)
	if len(rej) == 0 {
		return cands
	}
	// Also drop candidates whose ID was rejected directly (legacy shape).
	out := cands[:0]
	for _, c := range cands {
		if rej[c.ID] {
			continue
		}
		out = append(out, c)
	}
	return out
}

// ---------------------------------------------------------------------------
// EnrichmentTask — one-per-entity aggregated view (issue #1134)
// ---------------------------------------------------------------------------

// EnrichmentAction represents one pending subjective enrichment action for a
// subject entity. Actions are independently completable; completing one leaves
// the others pending so the whole task is not removed prematurely.
type EnrichmentAction struct {
	// Kind is the canonical action kind (e.g. "describe_entity", "classify_domain").
	Kind string `json:"kind"`
	// CandidateID is the stable candidate ID for this (subject, kind) pair.
	CandidateID string `json:"candidate_id"`
	// Reason describes why this entity was selected for this action.
	Reason string `json:"reason,omitempty"`
	// Score is the per-action confidence floor (0–1).
	Score float64 `json:"score,omitempty"`
	// Completed is true once an agent or a human has filed a resolution for
	// this (subject_id, kind) pair. Set by CollectTasks when a resolution is
	// present.
	Completed bool `json:"completed"`
}

// EnrichmentTask is the per-entity roll-up of all pending enrichment actions.
// One task per unique subject is what the dashboard and the MCP tool expose
// instead of the flat N-candidates-per-entity shape.
type EnrichmentTask struct {
	// SubjectID is the entity (or community) identifier.
	SubjectID string `json:"subject_id"`
	// SubjectKind is the entity Kind value (e.g. "class", "SCOPE.Component").
	SubjectKind string `json:"subject_kind,omitempty"`
	// SubjectName is the entity Name, included for display without a second lookup.
	SubjectName string `json:"subject_name,omitempty"`
	// PendingActions is the ordered list of actions that still need resolution.
	// Completed actions are included (with Completed=true) so callers can show
	// progress without a separate API call.
	PendingActions []EnrichmentAction `json:"pending_actions"`
	// OverallScore is the maximum Score across all pending (not-yet-completed)
	// actions. Used for entity-level prioritisation.
	OverallScore float64 `json:"overall_score"`
	// MaxActionScore is the highest score among ALL actions (pending or
	// completed). Useful for stable sort keys that don't change as actions
	// complete.
	MaxActionScore float64 `json:"max_action_score"`
	// Overdue is true when the task's oldest pending action was discovered more
	// than overdueDays ago with no resolution.
	Overdue bool `json:"overdue"`
	// DiscoveredAt is the RFC 3339 timestamp of the earliest pending action.
	DiscoveredAt string `json:"discovered_at,omitempty"`
	// Repo is the repository slug this task belongs to (set by callers).
	Repo string `json:"repo,omitempty"`
}

// overdueDays is the number of calendar days after which a pending enrichment
// task is considered overdue. Exposed as a var so tests can override it.
var overdueDays = 7

// CollectTasks runs all emitters against every entity in doc and returns one
// EnrichmentTask per unique subject. This is the canonical multi-action view
// requested in issue #1134.
//
// resolved maps "subject_id|kind" → true for pairs that already have a
// resolution; rejected maps the same key → true for pairs to skip entirely.
// Both maps may be nil.
//
// The returned slice is sorted by OverallScore DESC (high-priority first), then
// by SubjectID for a stable tiebreak.
func CollectTasks(
	doc *graph.Document,
	emitters []CandidateEmitter,
	rejected map[string]bool,
	resolved map[string]bool,
) []EnrichmentTask {
	if doc == nil {
		return nil
	}

	// entityKind / entityName indexed by ID for quick lookup.
	kindOf := make(map[string]string, len(doc.Entities))
	nameOf := make(map[string]string, len(doc.Entities))
	for i := range doc.Entities {
		e := &doc.Entities[i]
		kindOf[e.ID] = e.Kind
		nameOf[e.ID] = e.Name
	}

	// taskMap accumulates actions per subject.
	type taskEntry struct {
		actions      []EnrichmentAction
		discoveredAt string
	}
	taskMap := make(map[string]*taskEntry)
	// Preserve subject insertion order for stable output.
	var subjectOrder []string

	for i := range doc.Entities {
		e := &doc.Entities[i]
		for _, em := range emitters {
			for _, c := range em.EmitFor(e, doc) {
				if c.ID == "" {
					continue
				}
				key := c.SubjectID + "|" + c.Kind
				if rejected[key] {
					continue
				}
				completed := resolved[key]

				action := EnrichmentAction{
					Kind:        c.Kind,
					CandidateID: c.ID,
					Reason:      c.PromptTemplate,
					Score:       c.ConfidenceFloor,
					Completed:   completed,
				}

				entry, exists := taskMap[c.SubjectID]
				if !exists {
					entry = &taskEntry{}
					taskMap[c.SubjectID] = entry
					subjectOrder = append(subjectOrder, c.SubjectID)
				}
				entry.actions = append(entry.actions, action)
				if c.DiscoveredAt != "" && (entry.discoveredAt == "" || c.DiscoveredAt < entry.discoveredAt) {
					entry.discoveredAt = c.DiscoveredAt
				}
			}
		}
	}

	now := nowRFC3339()
	tasks := make([]EnrichmentTask, 0, len(taskMap))
	for _, sid := range subjectOrder {
		entry := taskMap[sid]

		var overallScore, maxScore float64
		for _, a := range entry.actions {
			if a.Score > maxScore {
				maxScore = a.Score
			}
			if !a.Completed && a.Score > overallScore {
				overallScore = a.Score
			}
		}

		overdue := false
		if entry.discoveredAt != "" {
			if t, err := time.Parse(time.RFC3339, entry.discoveredAt); err == nil {
				if time.Now().Sub(t) > time.Duration(overdueDays)*24*time.Hour {
					overdue = true
				}
			}
		}

		tasks = append(tasks, EnrichmentTask{
			SubjectID:      sid,
			SubjectKind:    kindOf[sid],
			SubjectName:    nameOf[sid],
			PendingActions: entry.actions,
			OverallScore:   overallScore,
			MaxActionScore: maxScore,
			Overdue:        overdue,
			DiscoveredAt:   entry.discoveredAt,
		})
		_ = now // used for overdue calc via time.Now()
	}

	// Sort: OverallScore DESC → MaxActionScore DESC → SubjectID ASC.
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].OverallScore != tasks[j].OverallScore {
			return tasks[i].OverallScore > tasks[j].OverallScore
		}
		if tasks[i].MaxActionScore != tasks[j].MaxActionScore {
			return tasks[i].MaxActionScore > tasks[j].MaxActionScore
		}
		return tasks[i].SubjectID < tasks[j].SubjectID
	})

	return tasks
}

// CandidatesFromTasks converts an []EnrichmentTask back into a flat []Candidate
// slice, preserving backward compatibility for callers that still use the flat
// shape (MCP candidate queries, CollectCandidates-based paths). Only
// not-yet-completed actions are included so the flat list represents the
// outstanding work.
func CandidatesFromTasks(tasks []EnrichmentTask) []Candidate {
	var out []Candidate
	for _, t := range tasks {
		for _, a := range t.PendingActions {
			if a.Completed {
				continue
			}
			out = append(out, Candidate{
				ID:              a.CandidateID,
				Kind:            a.Kind,
				SubjectID:       t.SubjectID,
				PromptTemplate:  a.Reason,
				ConfidenceFloor: a.Score,
				DiscoveredAt:    t.DiscoveredAt,
			})
		}
	}
	return out
}

// UniqueSubjectCount returns the number of distinct SubjectIDs in cs (the flat
// Candidate slice), which equals the number of EnrichmentTask rows that
// CollectTasks would produce for the same input. This is the display-friendly
// "X entities need enrichment" count from issue #1132.
func UniqueSubjectCount(cs []Candidate) int {
	seen := make(map[string]struct{}, len(cs))
	for _, c := range cs {
		seen[c.SubjectID] = struct{}{}
	}
	return len(seen)
}

// CollectCommunityCandidates emits one name_community candidate per community
// that does not yet have an AgentName assigned. The SubjectID uses the
// "community:<id>" prefix so consumers can distinguish them from entity
// candidates. Context includes the top-10 entities by centrality so the
// agent can infer a business label.
func CollectCommunityCandidates(doc *graph.Document, rejected map[string]bool) []Candidate {
	if doc == nil {
		return nil
	}
	var out []Candidate
	for i := range doc.Communities {
		c := &doc.Communities[i]
		if c.AgentName != "" {
			// Already resolved — skip.
			continue
		}
		sid := communitySubjectID(c.ID)
		key := sid + "|" + KindNameCommunity
		if rejected[key] {
			continue
		}
		// Top-10 entities by position in TopEntities (already ranked by centrality).
		top := c.TopEntities
		if len(top) > 10 {
			top = top[:10]
		}
		out = append(out, Candidate{
			ID:        candidateID(sid, KindNameCommunity),
			Kind:      KindNameCommunity,
			SubjectID: sid,
			Context: map[string]any{
				"community_id": c.ID,
				"auto_name":    c.AutoName,
				"size":         c.Size,
				"top_entities": top,
			},
			PromptTemplate:  "Give a concise business name (UpperCamelCase, ≤30 chars) for the module cluster whose top members are {{top_entities}}.",
			ConfidenceFloor: 0.6,
			DiscoveredAt:    nowRFC3339(),
		})
	}
	return out
}

// ApplyCommunityNameResolutions scans resolutions for kind="name_community"
// entries and writes the resolved Value into the matching community's
// AgentName field. Returns the count of applied resolutions.
func ApplyCommunityNameResolutions(doc *graph.Document, resolutions []Resolution) int {
	if doc == nil {
		return 0
	}
	applied := 0
	for _, r := range resolutions {
		if r.Kind != KindNameCommunity || r.Value == "" {
			continue
		}
		// SubjectID format: "community:<id>"
		var cid int
		if _, err := fmt.Sscanf(r.SubjectID, "community:%d", &cid); err != nil {
			continue
		}
		for i := range doc.Communities {
			if doc.Communities[i].ID == cid {
				doc.Communities[i].AgentName = r.Value
				applied++
				break
			}
		}
	}
	return applied
}

// AppendResolution appends one resolution record to
// <archigraphDir>/enrichment-resolutions.json atomically. The existing
// array is read, the new entry appended, and the file rewritten so
// callers never leave a half-written file.
func AppendResolution(archigraphDir string, res Resolution) error {
	if archigraphDir == "" {
		return fmt.Errorf("enrichment: archigraphDir is empty")
	}
	if err := os.MkdirAll(archigraphDir, 0o755); err != nil {
		return err
	}
	path := resolutionsPath(archigraphDir)
	cur := ReadResolutions(archigraphDir)
	cur = append(cur, res)
	data, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// AppendRejection appends one rejection record to
// <archigraphDir>/enrichment-rejections.json. Tolerates a missing file.
func AppendRejection(archigraphDir, candidateID, subjectID, kind, reason string) error {
	if archigraphDir == "" {
		return fmt.Errorf("enrichment: archigraphDir is empty")
	}
	if err := os.MkdirAll(archigraphDir, 0o755); err != nil {
		return err
	}
	path := rejectionsPath(archigraphDir)
	var cur []Rejection
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &cur)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	cur = append(cur, Rejection{
		ID:         candidateID,
		SubjectID:  subjectID,
		Kind:       kind,
		Reason:     reason,
		RejectedAt: now,
	})
	data, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// RemoveCandidateByID removes the candidate with the given ID from
// <archigraphDir>/enrichment-candidates.json (if present). Returns nil
// when the candidate is absent (idempotent).
func RemoveCandidateByID(archigraphDir, candidateID string) error {
	if archigraphDir == "" {
		return fmt.Errorf("enrichment: archigraphDir is empty")
	}
	path := candidatesPath(archigraphDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	// Parse using whichever shape is on disk.
	var candidates []Candidate
	var envelope candidatesEnvelope
	useEnvelope := false
	if jsonErr := json.Unmarshal(data, &envelope); jsonErr == nil && (envelope.Version > 0 || len(envelope.Candidates) > 0) {
		candidates = envelope.Candidates
		useEnvelope = true
	} else if jsonErr := json.Unmarshal(data, &candidates); jsonErr != nil {
		return jsonErr
	}
	// Filter out the target.
	filtered := candidates[:0]
	for _, c := range candidates {
		if c.ID != candidateID {
			filtered = append(filtered, c)
		}
	}
	var out []byte
	if useEnvelope {
		envelope.Candidates = filtered
		out, err = json.MarshalIndent(envelope, "", "  ")
	} else {
		out, err = json.MarshalIndent(filtered, "", "  ")
	}
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ApplyResolutions merges resolved enrichment values into the document
// in-place. The resolution.Value is written to entity.Properties under
// the resolution.Kind key, mirroring internal/mcp/enrichment.go.
func ApplyResolutions(doc *graph.Document, resolutions []Resolution) int {
	if doc == nil {
		return 0
	}
	byID := make(map[string]*graph.Entity, len(doc.Entities))
	for i := range doc.Entities {
		byID[doc.Entities[i].ID] = &doc.Entities[i]
	}
	applied := 0
	for _, r := range resolutions {
		if r.SubjectID == "" || r.Kind == "" {
			continue
		}
		e, ok := byID[r.SubjectID]
		if !ok {
			continue
		}
		if e.Properties == nil {
			e.Properties = map[string]string{}
		}
		e.Properties[r.Kind] = r.Value
		applied++
	}
	return applied
}
