// Effect-classification propagation pass (#2764 Phase 1A substrate).
//
// This pass implements Phase 1A of the substrate epic (#2716): tag every
// Function/Operation entity with the union of side-effect primitives it
// directly or transitively touches.
//
// Pipeline:
//
//  1. For every repo, walk the unique source-file set, dispatch each file
//     through the registered substrate.EffectSnifferFor(lang). Each
//     EffectMatch is bound to its declaring function name + source line.
//
//  2. Bind each match to a graph entity via (repo, file, function-name).
//     This is the same lexical attribution shape constant_propagation.go
//     uses; it leverages the substrate sniffer's "nearest preceding
//     header" attribution so we don't need a per-language symbol table.
//
//  3. Propagate via fixed-point on the reversed CALLS graph: every
//     caller's effect set ← caller's direct set ∪ ⋃(callee effect set
//     scaled by hopDecay). Terminates when no set changes between
//     iterations. Bounded by maxPropagationIterations to guarantee
//     termination on pathological graphs.
//
//  4. Stamp results onto entity.Properties under stable keys (see
//     EffectPropertyKey*). The on-disk graph.fb / graph.json files are
//     not rewritten — Phase 1A keeps the result in-memory; the MCP
//     archigraph_effects tool reads off the loaded properties and a
//     sidecar <group>-links-effects.json file.
//
// Confidence model:
//
//   - Direct sink call: per-sink confidence from the sniffer (1.0 for
//     well-known primitives; 0.7–0.85 for heuristic matches).
//   - Transitive via CALLS: each hop multiplies confidence by hopDecay
//     (default 0.95). Confidence is the MAX across alternative paths,
//     so a single direct path always dominates a long transitive one.
package links

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/substrate"
)

// MethodEffectPropagation identifies sidecar artefacts produced by the
// Phase 1A effect-propagation pass. Method-segregated so re-runs touch
// only this pass's output.
const MethodEffectPropagation = "effect_propagation"

// maxPropagationIterations bounds the fixed-point loop. Empirically 6
// iterations cover the deepest call-chains observed on the upvate
// monorepo (handler → service → repo → orm-wrapper → external);
// anything beyond this is almost certainly a cyclic call structure and
// merits diagnostic surfacing rather than silent extension.
const maxPropagationIterations = 16

// hopDecay scales transitively-inherited confidence per CALLS hop. The
// issue spec calls out 0.05 per hop bounded; we use multiplicative 0.95
// so the decay compounds smoothly across long chains rather than
// disappearing at hop 20.
const hopDecay = 0.95

// EffectPropertyKey* are the stable names under which the propagation
// pass stamps effect results onto entity.Properties. The MCP tool
// reads these keys; tests assert their exact spelling.
const (
	EffectPropertyKeyList       = "effects" // comma-joined effect names in canonical order
	EffectPropertyKeyConfidence = "effect_confidence"
	EffectPropertyKeySinks      = "effect_sinks"
	EffectPropertyKeySource     = "effect_source" // "direct" | "transitive" | "pure"
)

// runEffectPropagationPass is the entry point invoked from RunAllPasses
// after the constant propagation pass. It mutates entity.Properties on
// the in-memory repoGraphs (not on disk) and writes a sidecar JSON
// document for downstream consumers (MCP, dashboard).
func runEffectPropagationPass(graphs []repoGraph, paths Paths, _ map[string]bool) (PassResult, error) {
	res := PassResult{Pass: "effect_propagation"}

	// Sniff every source file once, bucketed per (repo, file, function).
	// Functions are also indexed by (repo, file) so the binder can match
	// substrate-reported names against graph entities even when the
	// extractor stored them as qualified or namespaced.
	type fnKey struct{ repo, file, fn string }
	direct := map[fnKey]*substrate.EffectSet{}
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
			sniff := substrate.EffectSnifferFor(lang)
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
				set := direct[k]
				if set == nil {
					set = &substrate.EffectSet{}
					direct[k] = set
				}
				set.Add(m.Effect, m.Confidence, m.Sink)
			}
		}
	}
	if scannedFiles == 0 {
		// No T1 source on disk — pass is a no-op; emit a result so the
		// caller's telemetry pipeline still sees the row.
		return res, nil
	}

	// Bind each direct fnKey to its graph entity ID and build the
	// reverse-CALLS index used by the fixed-point loop.
	binder := newEffectBinder(graphs)
	directByEntity := map[string]*substrate.EffectSet{}
	for k, set := range direct {
		if eid := binder.lookup(k.repo, k.file, k.fn); eid != "" {
			fullID := entityKey(k.repo, eid)
			if existing := directByEntity[fullID]; existing != nil {
				existing.Union(*set)
			} else {
				cp := *set
				directByEntity[fullID] = &cp
			}
		}
	}

	// Build adjacency: callers[targetEntityID] = list of caller entity IDs.
	callers := map[string][]string{}
	for _, g := range graphs {
		for _, e := range g.Edges {
			if !strings.EqualFold(e.Kind, "CALLS") && e.Kind != "calls" {
				continue
			}
			src := entityKey(g.Repo, e.FromID)
			tgt := entityKey(g.Repo, e.ToID)
			callers[tgt] = append(callers[tgt], src)
		}
	}

	// Fixed-point: seed each entity's effect set with its direct effects,
	// then iteratively merge callee sets back up the CALLS graph.
	effects := map[string]*substrate.EffectSet{}
	for id, set := range directByEntity {
		cp := *set
		effects[id] = &cp
	}
	// Build the reverse adjacency in (caller -> callees) form so we can
	// walk forward and propagate upward in the right direction.
	calleesOf := map[string][]string{}
	for callee, srcs := range callers {
		for _, c := range srcs {
			calleesOf[c] = append(calleesOf[c], callee)
		}
	}
	changed := true
	iterations := 0
	for changed && iterations < maxPropagationIterations {
		changed = false
		iterations++
		for caller, callees := range calleesOf {
			var merged substrate.EffectSet
			if e := effects[caller]; e != nil {
				merged = *e
			}
			before := merged
			for _, callee := range callees {
				if e := effects[callee]; e != nil {
					merged.UnionScaled(*e, hopDecay)
				}
			}
			if !effectSetsEqual(before, merged) {
				cp := merged
				effects[caller] = &cp
				changed = true
			}
		}
	}

	// Stamp results back onto entity.Properties so MCP/dashboard can read
	// them without re-running the pass.
	stamped := 0
	for ri := range graphs {
		g := &graphs[ri]
		for ei := range g.Entities {
			e := &g.Entities[ei]
			fullID := entityKey(g.Repo, e.ID)
			set, has := effects[fullID]
			if !has || set.IsEmpty() {
				continue
			}
			if e.Properties == nil {
				e.Properties = map[string]string{}
			}
			stampEffectProperties(e.Properties, *set, directByEntity[fullID] != nil)
			stamped++
		}
	}
	res.LinksAdded = stamped // re-used as "entities annotated" — surfaces in telemetry.
	res.Candidates = scannedFiles

	if paths.Links != "" {
		sidecar := strings.TrimSuffix(paths.Links, ".json") + "-effects.json"
		if err := writeEffectsDoc(sidecar, effects, directByEntity); err != nil {
			return res, fmt.Errorf("write effects doc: %w", err)
		}
	}
	return res, nil
}

// stampEffectProperties writes the effect annotation onto props using the
// canonical key set documented above. effectsDirect indicates whether
// the entity owns at least one direct sink (used to populate the
// effect_source key).
func stampEffectProperties(props map[string]string, set substrate.EffectSet, direct bool) {
	effs := set.AsList()
	if len(effs) == 0 {
		return
	}
	names := make([]string, len(effs))
	confs := make([]string, len(effs))
	sinks := make([]string, 0, len(effs)*2)
	for i, e := range effs {
		names[i] = string(e)
		confs[i] = fmt.Sprintf("%s=%.2f", e, set.Confidence(e))
		for _, s := range set.Sinks(e) {
			sinks = append(sinks, string(e)+":"+s)
		}
	}
	props[EffectPropertyKeyList] = strings.Join(names, ",")
	props[EffectPropertyKeyConfidence] = strings.Join(confs, ",")
	if len(sinks) > 0 {
		props[EffectPropertyKeySinks] = strings.Join(sinks, ",")
	}
	if direct {
		props[EffectPropertyKeySource] = "direct"
	} else {
		props[EffectPropertyKeySource] = "transitive"
	}
}

// effectSetsEqual reports structural equality between two EffectSets so
// the fixed-point loop can detect quiescence cheaply.
func effectSetsEqual(a, b substrate.EffectSet) bool {
	for _, e := range substrate.AllEffects() {
		if a.Has(e) != b.Has(e) {
			return false
		}
		if a.Has(e) && a.Confidence(e) != b.Confidence(e) {
			return false
		}
	}
	return true
}

// effectBinder maps (repo, file, function-name) to a graph entity ID
// using the per-repo entity list. Function entities are indexed by
// their Name; constructors and methods may collide with bare names
// from sibling files in the same repo — we resolve by file first, so
// each (file, name) pair binds locally.
type effectBinder struct {
	// byFile[repo][file][name] = entityID
	byFile map[string]map[string]map[string]string
}

func newEffectBinder(graphs []repoGraph) *effectBinder {
	b := &effectBinder{byFile: map[string]map[string]map[string]string{}}
	for _, g := range graphs {
		repoIdx := b.byFile[g.Repo]
		if repoIdx == nil {
			repoIdx = map[string]map[string]string{}
			b.byFile[g.Repo] = repoIdx
		}
		for _, e := range g.Entities {
			if !isFunctionLikeKind(e.Kind) {
				continue
			}
			if e.SourceFile == "" || e.Name == "" {
				continue
			}
			fileIdx := repoIdx[e.SourceFile]
			if fileIdx == nil {
				fileIdx = map[string]string{}
				repoIdx[e.SourceFile] = fileIdx
			}
			// Last-wins on duplicate (file, name) — same identifier
			// declared twice in one file is rare and the propagation
			// pass cannot tell them apart without line info; we accept
			// the precision loss in Phase 1A.
			fileIdx[e.Name] = e.ID
		}
	}
	return b
}

// lookup returns the entity ID for (repo, file, name), or "" when none
// is bound. Falls back to a bare-name match within the same file in
// case the sniffer captured a name that has a method-bearing receiver
// qualifier in the graph (e.g. extractor stores "Repo.Save" but the
// sniffer captured "Save"); we try a suffix-match on `.Name`.
func (b *effectBinder) lookup(repo, file, name string) string {
	fileIdx := b.byFile[repo][file]
	if fileIdx == nil {
		return ""
	}
	if id := fileIdx[name]; id != "" {
		return id
	}
	// Suffix match: extractor may have stored a qualified name. Avoid
	// O(n²) by only scanning when the bare-name lookup missed and only
	// returning when exactly one candidate ends in "."+name.
	suffix := "." + name
	var match string
	hits := 0
	for k, id := range fileIdx {
		if strings.HasSuffix(k, suffix) {
			match = id
			hits++
			if hits > 1 {
				return ""
			}
		}
	}
	return match
}

// isFunctionLikeKind reports whether kind names a function-shaped entity
// the propagation pass should annotate. Mirrors the kinds the CALLS
// extractor emits across T1 languages.
func isFunctionLikeKind(kind string) bool {
	switch kind {
	case "SCOPE.Function", "SCOPE.Operation", "SCOPE.Class", "SCOPE.UIComponent", "SCOPE.JSX":
		return true
	case "function", "method", "operation":
		return true
	}
	return false
}

// effectsDocument is the on-disk shape of <group>-links-effects.json.
type effectsDocument struct {
	Version int           `json:"version"`
	Method  string        `json:"method"`
	Entries []effectEntry `json:"entries"`
}

type effectEntry struct {
	EntityID    string             `json:"entity_id"`
	Effects     []string           `json:"effects"`
	Confidences map[string]float64 `json:"confidences"`
	Sinks       map[string][]string `json:"sinks,omitempty"`
	Source      string             `json:"source"` // "direct" | "transitive"
}

func writeEffectsDoc(path string, effects, direct map[string]*substrate.EffectSet) error {
	ids := make([]string, 0, len(effects))
	for id := range effects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	entries := make([]effectEntry, 0, len(ids))
	for _, id := range ids {
		set := effects[id]
		if set == nil || set.IsEmpty() {
			continue
		}
		effs := set.AsList()
		names := make([]string, len(effs))
		confs := map[string]float64{}
		sinks := map[string][]string{}
		for i, e := range effs {
			names[i] = string(e)
			confs[string(e)] = set.Confidence(e)
			if s := set.Sinks(e); len(s) > 0 {
				sinks[string(e)] = s
			}
		}
		src := "transitive"
		if direct[id] != nil {
			src = "direct"
		}
		entries = append(entries, effectEntry{
			EntityID:    id,
			Effects:     names,
			Confidences: confs,
			Sinks:       sinks,
			Source:      src,
		})
	}
	doc := effectsDocument{Version: 1, Method: MethodEffectPropagation, Entries: entries}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
}
