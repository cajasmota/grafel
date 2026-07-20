// v2_paths_effective_effects.go — endpoint EFFECTIVE side-effect aggregation
// (#4489).
//
// Problem: the Paths-detail "Side effects" panel collected effects ONLY from
// edges whose FromID is the handler (QUERIES / ACCESSES_TABLE / EMITS / …; see
// classifyHandlerEdges). For a thin controller that delegates the actual DB
// write to a downstream service (POST create → service.create does the
// db_write), the handler has NO direct side-effect edge, so the panel read
// "(0)" even though the endpoint clearly writes.
//
// Fix (query-time, NO reindex): compute the endpoint's EFFECTIVE side-effects
// by walking the handler's downstream CALLS transitively (depth + node capped,
// cycle-guarded) and unioning the effect KINDS (db_read / db_write / http_out /
// fs / …) found on every reachable function. The effect kinds come from the
// SAME canonical source the `effects` MCP tool + the downstream-DAG cards use:
// the <group>-links-effects.json sidecar (loadDAGEffectsSidecar), with a
// fallback to the effect-propagation properties stamped on the entity.
//
// Each aggregated kind is tagged with where it was observed:
//
//   - "direct"     — a sink on the handler method itself.
//   - "downstream" — reached only via a delegated callee (the common thin-
//     controller case the ticket is about).
//
// This is framework-agnostic: it relies only on the universal CALLS edge and
// the language-neutral effect sidecar, so every stack (DRF, Spring, NestJS, …)
// benefits without any extractor change.

package dashboard

import (
	"sort"
	"strings"
)

// v2EffectiveEffect is one aggregated effect kind surfaced on the endpoint
// detail. Source distinguishes a sink on the handler itself ("direct") from one
// reached only through a delegated downstream callee ("downstream"), so the
// frontend can hint "via downstream service" rather than implying the thin
// controller does the write inline.
type v2EffectiveEffect struct {
	// Kind is the effect primitive: db_read | db_write | http_out | fs | … —
	// the same vocabulary the `effects` MCP tool emits.
	Kind string `json:"kind"`
	// Source is "direct" (sink on the handler) or "downstream" (reached only via
	// a delegated callee). When a kind is observed both ways "direct" wins.
	Source string `json:"source"`
}

// mergeEffectiveEffects aggregates effective effects across every (repo →
// handler-ID-set) the endpoint resolves to, merging per-repo results into one
// union. "direct" provenance outranks "downstream" when a kind appears both
// ways across repos. Deterministic order (kind asc). nil when nothing reachable.
func mergeEffectiveEffects(handlersByRepo map[string][]string, repoIdx map[string]*repoEntityIndex, effects map[string][]string) []v2EffectiveEffect {
	if len(handlersByRepo) == 0 {
		return nil
	}
	merged := map[string]string{} // kind -> source (direct wins)
	for slug, handlerIDs := range handlersByRepo {
		idx := repoIdx[slug]
		if idx == nil {
			continue
		}
		for _, ee := range aggregateEffectiveEffects(idx, handlerIDs, effects) {
			if merged[ee.Kind] == "direct" {
				continue
			}
			if ee.Source == "direct" || merged[ee.Kind] == "" {
				merged[ee.Kind] = ee.Source
			}
		}
	}
	if len(merged) == 0 {
		return nil
	}
	res := make([]v2EffectiveEffect, 0, len(merged))
	for kind, src := range merged {
		res = append(res, v2EffectiveEffect{Kind: kind, Source: src})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Kind < res[j].Kind })
	return res
}

// effectiveEffectsCaps bound the transitive CALLS walk so a pathological graph
// can never blow up the request. Mirrors the downstream-DAG caps (dagMaxDepth /
// dagMaxNodes) in spirit — depth covers handler → service → repo → orm-wrapper
// → external comfortably, and the node cap guards fan-out.
const (
	effectiveEffectsMaxDepth = 12
	effectiveEffectsMaxNodes = 1500
)

// aggregateEffectiveEffects walks the handler's downstream CALLS transitively
// and returns the union of effect kinds reachable from the handler set, each
// tagged direct/downstream.
//
// Inputs:
//   - idx:        the per-repo entity index (byID + the repo's relationships).
//   - handlerIDs: the LOCAL handler entity IDs to root the walk at.
//   - effects:    the canonical effect index keyed by PREFIXED id
//     ("<slug>::<localID>") loaded once per request from the links-effects
//     sidecar (loadDAGEffectsSidecar). May be nil → property fallback only.
//
// The walk is single-repo: the handler → service → repository chain lives in the
// endpoint's own repo (same assumption the downstream-DAG builder makes). It is
// depth + node capped and cycle-guarded via a visited set.
func aggregateEffectiveEffects(idx *repoEntityIndex, handlerIDs []string, effects map[string][]string) []v2EffectiveEffect {
	if idx == nil || len(handlerIDs) == 0 {
		return nil
	}

	// Forward CALLS adjacency over the repo, built once. Self-edges skipped.
	out := make(map[string][]string)
	for i := range idx.repo.Doc.Relationships {
		r := &idx.repo.Doc.Relationships[i]
		if r.Kind != "CALLS" || r.FromID == r.ToID {
			continue
		}
		out[r.FromID] = append(out[r.FromID], r.ToID)
	}

	handlerSet := make(map[string]bool, len(handlerIDs))
	for _, id := range handlerIDs {
		handlerSet[id] = true
	}

	// effectSource records the strongest provenance seen per effect kind.
	// "direct" outranks "downstream".
	effectSource := map[string]string{}
	record := func(local string, depth int) {
		for _, kind := range effectsForLocal(idx, local, effects) {
			src := "downstream"
			if depth == 0 && handlerSet[local] {
				src = "direct"
			}
			if effectSource[kind] == "direct" {
				continue // already strongest
			}
			effectSource[kind] = src
		}
	}

	// BFS from every handler, depth+node capped, cycle-guarded.
	type item struct {
		local string
		depth int
	}
	visited := make(map[string]bool, len(handlerIDs))
	queue := make([]item, 0, len(handlerIDs))
	for _, id := range handlerIDs {
		if !visited[id] {
			visited[id] = true
			queue = append(queue, item{local: id, depth: 0})
		}
	}

	for len(queue) > 0 && len(visited) <= effectiveEffectsMaxNodes {
		cur := queue[0]
		queue = queue[1:]
		record(cur.local, cur.depth)
		if cur.depth >= effectiveEffectsMaxDepth {
			continue
		}
		for _, to := range out[cur.local] {
			if visited[to] {
				continue
			}
			if len(visited) >= effectiveEffectsMaxNodes {
				break
			}
			visited[to] = true
			queue = append(queue, item{local: to, depth: cur.depth + 1})
		}
	}

	if len(effectSource) == 0 {
		return nil
	}
	res := make([]v2EffectiveEffect, 0, len(effectSource))
	for kind, src := range effectSource {
		res = append(res, v2EffectiveEffect{Kind: kind, Source: src})
	}
	// Deterministic order: kind asc.
	sort.Slice(res, func(i, j int) bool { return res[i].Kind < res[j].Kind })
	return res
}

// effectsForLocal returns the effect kinds for a local entity, mirroring the
// source precedence of the `effects` MCP tool + the downstream-DAG cards: the
// canonical sidecar (keyed by prefixed id) first, then the effect-propagation
// properties stamped on the entity (the in-process run case).
func effectsForLocal(idx *repoEntityIndex, local string, effects map[string][]string) []string {
	pid := dashPrefixedID(idx.repo.Slug, local)
	if effs := effects[pid]; len(effs) > 0 {
		return effs
	}
	if e := idx.byID[local]; e != nil && e.PropLen() > 0 {
		if raw := strings.TrimSpace(e.PropGet(effectPropertyKeyList)); raw != "" {
			return splitNonEmptyComma(raw)
		}
	}
	return nil
}
