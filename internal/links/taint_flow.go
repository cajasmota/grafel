// Taint-flow propagation pass (#2772 Phase 2B substrate).
//
// Implements Phase 2B of the substrate epic (#2716): mark every
// function entity that owns a taint source, BFS forward across the
// CALLS graph, and emit a SecurityFinding for every source→...→sink
// path that is not broken by a sanitizer of the matching category.
//
// Pipeline:
//
//  1. For every repo, walk the unique source-file set, dispatch each
//     file through substrate.TaintSnifferFor(lang). Each TaintMatch
//     is bound to its declaring function via the same nearest-header
//     heuristic the effect_propagation pass uses.
//
//  2. Bind each TaintMatch to a graph entity via (repo, file, name).
//
//  3. Build the CALLS forward adjacency. For every function that
//     owns at least one TaintKindSource match, run a bounded BFS
//     (default depth 6, the same horizon as the call-chains
//     Phase 1A's reverse-CALLS fixed-point handles). On each visited
//     function:
//     - if it owns a sanitizer match for the source's category,
//     STOP propagation through this function for that category;
//     - if it owns a sink match for any active category that has
//     not yet been sanitised, emit a SecurityFinding.
//
//  4. Confidence model: source confidence × ∏(0.95 per hop), capped
//     by the sink confidence. A direct source→sink with no hops is
//     `min(source_conf, sink_conf)`; each additional hop multiplies
//     by hopDecay. Findings below the confidenceFloor are dropped.
//
//  5. Write findings to a sidecar <group>-links-taint.json document
//     for the MCP archigraph_security_findings tool to read.
//
// Storage model: same as the effect pass — no new entity kind. The
// SecurityFinding records are persisted as the sidecar JSON only; the
// graph is annotated with a `taint_role` property on functions that
// own a source / sink / sanitizer match so MCP queries can ask
// "is this function a known source?" without re-running the sniffer.
package links

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/substrate"
)

// MethodTaintFlow identifies sidecar artefacts produced by the Phase
// 2B taint-flow pass.
const MethodTaintFlow = "taint_flow"

// maxTaintPropagationDepth bounds the forward BFS from each source.
// Empirically 6 covers handler→service→repo→raw-sql chains in upvate
// and similar monorepos; anything beyond is almost always a cycle and
// will be caught by the visited-set guard.
const maxTaintPropagationDepth = 6

// taintHopDecay scales confidence per CALLS hop, mirroring the effect
// pass.
const taintHopDecay = 0.95

// taintConfidenceFloor drops findings whose final confidence would be
// below this threshold. The issue spec calls "conservative > aggressive"
// — anything below 0.5 is too speculative to surface without a manual
// confirmation pass.
const taintConfidenceFloor = 0.5

// TaintFindingFloor returns taintConfidenceFloor for MCP consumers
// that want to surface the pass's drop threshold in tool responses.
func TaintFindingFloor() float64 { return taintConfidenceFloor }

// TaintRolePropertyKey is the stable name under which the pass stamps
// a function's lattice role (`source`, `sink`, `sanitizer`, or a
// comma-joined union) onto entity.Properties.
const TaintRolePropertyKey = "taint_role"

// TaintFindingCountPropertyKey records, on the source function, how
// many SecurityFinding records were emitted with that function as the
// source. Lets MCP queries rank entry points by exposure.
const TaintFindingCountPropertyKey = "taint_finding_count"

// runTaintFlowPass is the entry point invoked from RunAllPasses after
// the effect propagation pass. It mutates entity.Properties for
// taint_role and emits the <group>-links-taint.json sidecar.
func runTaintFlowPass(graphs []repoGraph, paths Paths, _ map[string]bool) (PassResult, error) {
	res := PassResult{Pass: "taint_flow"}

	// Per-function taint matches, bucketed by (repo, file, fn-name).
	type fnKey struct{ repo, file, fn string }
	matches := map[fnKey][]substrate.TaintMatch{}
	scannedFiles := 0
	for _, g := range graphs {
		fileSet := map[string]bool{}
		for _, e := range g.Entities {
			if e.SourceFile != "" {
				fileSet[e.SourceFile] = true
			}
		}
		for file := range fileSet {
			lang := substrate.LanguageForPath(file)
			if lang == "" {
				continue
			}
			sniff := substrate.TaintSnifferFor(lang)
			if sniff == nil {
				continue
			}
			srcRoot := repoSourcePathFor(g.Repo)
			if srcRoot == "" {
				srcRoot = g.FileRoot
			}
			abs := filepath.Join(srcRoot, file)
			content, err := os.ReadFile(abs)
			if err != nil {
				continue
			}
			scannedFiles++
			for _, m := range sniff(string(content)) {
				if m.Function == "" {
					continue
				}
				k := fnKey{repo: g.Repo, file: file, fn: m.Function}
				matches[k] = append(matches[k], m)
			}
		}
	}
	if scannedFiles == 0 {
		return res, nil
	}

	// Bind each fnKey to its graph entity ID. Reuse the effect
	// propagation's binder — it's the same (repo, file, name) shape
	// and the function-like-kind set hasn't drifted between passes.
	binder := newEffectBinder(graphs)
	byEntity := map[string][]substrate.TaintMatch{}
	entityRepo := map[string]string{}
	for k, ms := range matches {
		eid := binder.lookup(k.repo, k.file, k.fn)
		if eid == "" {
			continue
		}
		full := entityKey(k.repo, eid)
		byEntity[full] = append(byEntity[full], ms...)
		entityRepo[full] = k.repo
	}

	// Forward CALLS adjacency.
	calleesOf := map[string][]string{}
	for _, g := range graphs {
		for _, e := range g.Edges {
			if !strings.EqualFold(e.Kind, "CALLS") && e.Kind != "calls" {
				continue
			}
			src := entityKey(g.Repo, e.FromID)
			tgt := entityKey(g.Repo, e.ToID)
			calleesOf[src] = append(calleesOf[src], tgt)
		}
	}

	// Walk forward from every entity that owns at least one source
	// match. Each finding pairs a source with a sink, tracking the
	// path of entity IDs traversed and the active sanitised-category
	// set so a sanitizer mid-path cleanses downstream sinks of that
	// category only.
	findings := computeFindings(byEntity, entityRepo, calleesOf, graphs)

	// Stamp the lattice role on every annotated entity so MCP queries
	// can ask "is this function a source / sink / sanitizer?" without
	// re-running the sniffer.
	stamped := stampTaintRoles(graphs, byEntity, findings)
	res.LinksAdded = stamped
	res.Candidates = scannedFiles

	if paths.Links != "" {
		sidecar := strings.TrimSuffix(paths.Links, ".json") + "-taint.json"
		if err := writeTaintDoc(sidecar, findings); err != nil {
			return res, fmt.Errorf("write taint doc: %w", err)
		}
	}
	return res, nil
}

// SecurityFinding is one source→sink path that the pass identified.
// Exported because the MCP tool serialises it.
type SecurityFinding struct {
	// Fingerprint is a stable id derived from (source_id, sink_id,
	// category). Re-runs do not duplicate findings.
	Fingerprint string `json:"fingerprint"`
	// SourceID is the entity that owns the taint source primitive.
	SourceID string `json:"source_id"`
	// SourcePrimitive identifies which source rule fired
	// (e.g. "req.body/query/headers").
	SourcePrimitive string `json:"source_primitive"`
	// SourceLine is the 1-indexed line of the source primitive.
	SourceLine int `json:"source_line"`
	// SinkID is the entity that owns the sink primitive.
	SinkID string `json:"sink_id"`
	// SinkPrimitive identifies which sink rule fired.
	SinkPrimitive string `json:"sink_primitive"`
	// SinkLine is the 1-indexed line of the sink primitive.
	SinkLine int `json:"sink_line"`
	// Category labels the finding (sql_injection, command_injection,
	// path_traversal, xss, redos, deserialization, ssrf, generic).
	Category substrate.TaintCategory `json:"category"`
	// Path is the ordered list of entity IDs visited from source to
	// sink (inclusive of both endpoints).
	Path []string `json:"path"`
	// Confidence is the final aggregated confidence in [0, 1] after
	// per-hop decay and source/sink confidence multiplication.
	Confidence float64 `json:"confidence"`
	// Repo is the repo slug that owns the source entity.
	Repo string `json:"repo"`
}

// computeFindings is the core BFS. For every entity that owns a
// TaintKindSource match, walk forward across CALLS up to
// maxTaintPropagationDepth hops. At each hop:
//
//   - if the visited entity owns a sanitizer for the source's
//     category (or TaintCategoryGeneric), record the category as
//     sanitised on the active path so downstream sinks of that
//     category are skipped;
//   - if the visited entity owns a sink whose category is active
//     and not sanitised, emit one SecurityFinding (deduplicated by
//     fingerprint across alternative paths to the same sink).
//
// Per-finding confidence = source_conf × sink_conf × (decay ^ hops).
func computeFindings(byEntity map[string][]substrate.TaintMatch, entityRepo map[string]string, calleesOf map[string][]string, _ []repoGraph) []SecurityFinding {
	// Dedup findings across alternative paths — keep the highest
	// confidence per (source_id, sink_id, category). The key type is
	// declared inline (not a named alias) so it matches the inline
	// struct literal used at the emitFindingsFrom call sites without
	// the Go type checker rejecting it as a distinct named type.
	best := map[struct {
		src, sink string
		cat       substrate.TaintCategory
	}]SecurityFinding{}

	// Stable iteration over source entities.
	sources := make([]string, 0, len(byEntity))
	for id, ms := range byEntity {
		if hasKind(ms, substrate.TaintKindSource) {
			sources = append(sources, id)
		}
	}
	sort.Strings(sources)

	for _, srcID := range sources {
		for _, sm := range byEntity[srcID] {
			if sm.Kind != substrate.TaintKindSource {
				continue
			}
			// BFS from this source. Each queue entry carries the
			// path, the running confidence, and the set of sanitised
			// categories accumulated along the path.
			type qe struct {
				node      string
				path      []string
				conf      float64
				sanitised categorySet
				depth     int
			}
			// Seed the initial sanitizer set with sanitizers that live
			// in the SAME function as the source. Without this, a
			// handler that both reads req.body AND wraps the value in
			// a parameterised cursor.execute (the standard safe shape)
			// would emit a false-positive SQL finding.
			initial := categorySet{}
			for _, sib := range byEntity[srcID] {
				if sib.Kind != substrate.TaintKindSanitizer {
					continue
				}
				initial.add(sib.Category)
				if sib.Category == substrate.TaintCategoryGeneric {
					initial.add(substrate.TaintCategorySQL)
					initial.add(substrate.TaintCategoryXSS)
					initial.add(substrate.TaintCategoryCommand)
					initial.add(substrate.TaintCategoryPath)
					initial.add(substrate.TaintCategoryReDoS)
				}
			}
			start := qe{node: srcID, path: []string{srcID}, conf: sm.Confidence, sanitised: initial, depth: 0}
			visited := map[string]bool{srcID: true}
			queue := []qe{start}
			// Direct-on-source sink check (a function that both reads
			// req.body and execs subprocess on it without leaving).
			emitFindingsFrom(byEntity[srcID], sm, srcID, srcID, []string{srcID}, sm.Confidence, initial, entityRepo, best)

			for len(queue) > 0 {
				cur := queue[0]
				queue = queue[1:]
				if cur.depth >= maxTaintPropagationDepth {
					continue
				}
				// Stable callee order so output is deterministic.
				callees := append([]string(nil), calleesOf[cur.node]...)
				sort.Strings(callees)
				for _, callee := range callees {
					if visited[callee] {
						continue
					}
					visited[callee] = true
					nextConf := cur.conf * taintHopDecay
					if nextConf < taintConfidenceFloor {
						continue
					}
					calleeMatches := byEntity[callee]
					nextSan := cur.sanitised.clone()
					for _, cm := range calleeMatches {
						if cm.Kind == substrate.TaintKindSanitizer {
							nextSan.add(cm.Category)
							// Generic sanitizer (e.g. zod schema)
							// cleanses every category that lacks a
							// dedicated sanitizer in this language.
							if cm.Category == substrate.TaintCategoryGeneric {
								nextSan.add(substrate.TaintCategorySQL)
								nextSan.add(substrate.TaintCategoryXSS)
								nextSan.add(substrate.TaintCategoryCommand)
								nextSan.add(substrate.TaintCategoryPath)
								nextSan.add(substrate.TaintCategoryReDoS)
							}
						}
					}
					nextPath := append(append([]string(nil), cur.path...), callee)
					emitFindingsFrom(calleeMatches, sm, srcID, callee, nextPath, nextConf, nextSan, entityRepo, best)
					queue = append(queue, qe{node: callee, path: nextPath, conf: nextConf, sanitised: nextSan, depth: cur.depth + 1})
				}
			}
		}
	}

	out := make([]SecurityFinding, 0, len(best))
	for _, f := range best {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		return out[i].Fingerprint < out[j].Fingerprint
	})
	return out
}

// emitFindingsFrom checks every sink match on the visited entity
// against the running confidence + sanitised set and updates best.
func emitFindingsFrom(matches []substrate.TaintMatch, src substrate.TaintMatch, srcID, sinkID string, path []string, conf float64, sanitised categorySet, entityRepo map[string]string, best map[struct {
	src, sink string
	cat       substrate.TaintCategory
}]SecurityFinding) {
	for _, m := range matches {
		if m.Kind != substrate.TaintKindSink {
			continue
		}
		if sanitised.has(m.Category) {
			continue
		}
		// Category match. SSRF / Deserialization sources/sinks are
		// also category-tagged. Generic sources can flow to any sink
		// category; non-generic source categories MUST match the sink
		// category (e.g. a deserialization-tagged source like
		// pickle.loads doesn't trigger an SQL finding by itself —
		// the request-body source upstream already did).
		if src.Category != substrate.TaintCategoryGeneric && src.Category != m.Category {
			continue
		}
		final := conf * m.Confidence
		if final < taintConfidenceFloor {
			continue
		}
		fp := fingerprint(srcID, sinkID, m.Category, src.Line, m.Line)
		k := struct {
			src, sink string
			cat       substrate.TaintCategory
		}{src: srcID, sink: sinkID, cat: m.Category}
		if existing, ok := best[k]; ok && existing.Confidence >= final {
			continue
		}
		best[k] = SecurityFinding{
			Fingerprint:     fp,
			SourceID:        srcID,
			SourcePrimitive: src.Primitive,
			SourceLine:      src.Line,
			SinkID:          sinkID,
			SinkPrimitive:   m.Primitive,
			SinkLine:        m.Line,
			Category:        m.Category,
			Path:            append([]string(nil), path...),
			Confidence:      final,
			Repo:            entityRepo[srcID],
		}
	}
}

// fingerprint derives a stable id for a finding. Including source +
// sink + category + line numbers means re-runs against unchanged code
// produce byte-identical fingerprints, so downstream consumers can
// dedupe without state.
func fingerprint(srcID, sinkID string, cat substrate.TaintCategory, srcLine, sinkLine int) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%d|%d", srcID, sinkID, cat, srcLine, sinkLine)
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// stampTaintRoles writes the per-entity lattice role onto Properties.
// Also stamps taint_finding_count on every source so MCP can rank
// exposed entry points.
func stampTaintRoles(graphs []repoGraph, byEntity map[string][]substrate.TaintMatch, findings []SecurityFinding) int {
	roleByEntity := map[string]map[substrate.TaintKind]bool{}
	for id, ms := range byEntity {
		for _, m := range ms {
			if roleByEntity[id] == nil {
				roleByEntity[id] = map[substrate.TaintKind]bool{}
			}
			roleByEntity[id][m.Kind] = true
		}
	}
	findingCount := map[string]int{}
	for _, f := range findings {
		findingCount[f.SourceID]++
	}
	stamped := 0
	for ri := range graphs {
		g := &graphs[ri]
		for ei := range g.Entities {
			e := &g.Entities[ei]
			full := entityKey(g.Repo, e.ID)
			roles := roleByEntity[full]
			if len(roles) == 0 {
				continue
			}
			if e.Properties == nil {
				e.Properties = map[string]string{}
			}
			// Canonical order: source, sink, sanitizer.
			parts := make([]string, 0, 3)
			for _, k := range []substrate.TaintKind{substrate.TaintKindSource, substrate.TaintKindSink, substrate.TaintKindSanitizer} {
				if roles[k] {
					parts = append(parts, string(k))
				}
			}
			e.Properties[TaintRolePropertyKey] = strings.Join(parts, ",")
			if c := findingCount[full]; c > 0 {
				e.Properties[TaintFindingCountPropertyKey] = fmt.Sprintf("%d", c)
			}
			stamped++
		}
	}
	return stamped
}

// hasKind reports whether any match in ms is of kind k.
func hasKind(ms []substrate.TaintMatch, k substrate.TaintKind) bool {
	for _, m := range ms {
		if m.Kind == k {
			return true
		}
	}
	return false
}

// categorySet is a tiny ordered-bit set over TaintCategory values.
// Used by the BFS to track which categories have been sanitised
// along the current path. Cheap to clone (one map alloc).
type categorySet struct {
	bits map[substrate.TaintCategory]struct{}
}

func (s *categorySet) has(c substrate.TaintCategory) bool {
	if s.bits == nil {
		return false
	}
	_, ok := s.bits[c]
	return ok
}

func (s *categorySet) add(c substrate.TaintCategory) {
	if s.bits == nil {
		s.bits = map[substrate.TaintCategory]struct{}{}
	}
	s.bits[c] = struct{}{}
}

func (s categorySet) clone() categorySet {
	if s.bits == nil {
		return categorySet{}
	}
	out := categorySet{bits: make(map[substrate.TaintCategory]struct{}, len(s.bits))}
	for k := range s.bits {
		out.bits[k] = struct{}{}
	}
	return out
}

// taintDocument is the on-disk shape of <group>-links-taint.json.
type taintDocument struct {
	Version  int               `json:"version"`
	Method   string            `json:"method"`
	Findings []SecurityFinding `json:"findings"`
}

func writeTaintDoc(path string, findings []SecurityFinding) error {
	doc := taintDocument{Version: 1, Method: MethodTaintFlow, Findings: findings}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
}
