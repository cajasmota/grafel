// archigraph_effects MCP tool (#2764 Phase 1A substrate).
//
// Returns the effect classification for a given entity. Schema:
//
//   {
//     "entity_id":     "<id-or-qname>",
//     "resolved":      { "id": ..., "name": ..., "kind": ..., "repo": ... },
//     "effects":       ["db_write", "http_out"],
//     "confidences":   { "db_write": 0.85, "http_out": 1.00 },
//     "sinks":         { "db_write": ["orm.write"], ... },
//     "effect_source": "direct" | "transitive" | "pure",
//     "explanation":   "..."
//   }
//
// The effect set is read from the on-disk effects sidecar
// (<group>-links-effects.json, written by
// internal/links/effect_propagation.go) — the canonical source — with a
// fallback to entity.Properties for the in-process link-run case. An
// entity present in neither is reported as "pure" with low confidence —
// per #2764 spec absence of detection does not prove absence of effect.
//
// Reading off entity.Properties alone is insufficient: the propagation
// pass stamps properties on an ephemeral in-memory graph during the link
// run and never persists them to graph.fb, so the daemon-loaded graph
// that serves MCP has empty effect properties. Before #2804 this made
// every entity report "pure" (the marquee Phase 1A failure). We now load
// the sidecar that the pass does persist.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/links"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// effectsSidecarEntry mirrors links.effectEntry — the per-entity record
// in <group>-links-effects.json. Kept local (unexported in links) so the
// MCP surface can decode it without widening the links API.
type effectsSidecarEntry struct {
	EntityID    string              `json:"entity_id"`
	Effects     []string            `json:"effects"`
	Confidences map[string]float64  `json:"confidences"`
	Sinks       map[string][]string `json:"sinks,omitempty"`
	Source      string              `json:"source"`
}

type effectsSidecarDoc struct {
	Version int                   `json:"version"`
	Method  string                `json:"method"`
	Entries []effectsSidecarEntry `json:"entries"`
}

// effectsSidecarPath is the conventional path for the
// <group>-links-effects.json sidecar written by
// internal/links/effect_propagation.go. Mirrors reachabilitySidecarPath.
func effectsSidecarPath(group string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".archigraph", "groups", group+"-links-effects.json")
}

// loadEffectsSidecar reads + parses the sidecar into a map keyed by the
// prefixed entity ID ("<repo>::<local>"). ok=false on any failure
// (missing file is the common, non-error case → fall back to in-memory
// properties).
func loadEffectsSidecar(group string) (map[string]effectsSidecarEntry, bool) {
	path := effectsSidecarPath(group)
	if path == "" {
		return nil, false
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var doc effectsSidecarDoc
	if err := json.Unmarshal(buf, &doc); err != nil {
		return nil, false
	}
	idx := make(map[string]effectsSidecarEntry, len(doc.Entries))
	for _, e := range doc.Entries {
		idx[e.EntityID] = e
	}
	return idx, true
}

// handleEffects implements archigraph_effects. The entity_id parameter
// accepts an id, qualified name, label, or cross-repo prefixed id
// ("<repo>:<id>"), mirroring archigraph_inspect's resolver semantics.
func (s *Server) handleEffects(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	key := argString(req, "entity_id", "")
	if key == "" {
		return mcpapi.NewToolResultError("missing required argument: entity_id"), nil
	}
	groupName, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))

	// Canonical effects live in the on-disk sidecar written by the link
	// pass; entity.Properties are only populated during an in-process run.
	sidecar, _ := loadEffectsSidecar(groupName)

	// Cross-repo prefixed ID? Resolve repo first for unambiguous lookup.
	if rprefix, local := splitPrefixed(key); rprefix != "" {
		if r, ok := lg.Repos[rprefix]; ok && r.Doc != nil {
			if e, ok := r.LabelIndex.ByID[local]; ok {
				return jsonResult(buildEffectsPayload(r.Repo, e, sidecar)), nil
			}
		}
	}
	// Collect every label/qname/id match across the considered repos.
	type matchPair struct {
		ent  *graph.Entity
		repo string
	}
	var matches []matchPair
	for _, r := range repos {
		for _, hit := range r.LabelIndex.LookupAll(key) {
			matches = append(matches, matchPair{ent: hit, repo: r.Repo})
		}
	}
	if len(matches) == 0 {
		return mcpapi.NewToolResultError(fmt.Sprintf("not found: %s", key)), nil
	}
	if len(matches) > 1 {
		out := make([]map[string]any, 0, len(matches))
		for _, m := range matches {
			out = append(out, map[string]any{
				"id":             prefixedID(m.repo, m.ent.ID),
				"qualified_name": m.ent.QualifiedName,
				"label":          m.ent.Name,
				"repo":           m.repo,
				"source_file":    m.ent.SourceFile,
			})
		}
		return jsonResult(map[string]any{
			"ambiguous":     true,
			"entity_id":     key,
			"matches":       out,
			"how_to_choose": "Re-call archigraph_effects with the prefixed id field (e.g. \"repo:1234abcd\").",
		}), nil
	}
	return jsonResult(buildEffectsPayload(matches[0].repo, matches[0].ent, sidecar)), nil
}

// buildEffectsPayload constructs the JSON-serialisable response body
// from a resolved graph entity. Centralised so cross-repo prefixed
// and label-lookup paths emit byte-identical shapes.
//
// Effect data is read from the on-disk sidecar (canonical) when present
// for this entity, otherwise from entity.Properties (in-process run),
// otherwise reported as pure.
func buildEffectsPayload(repo string, e *graph.Entity, sidecar map[string]effectsSidecarEntry) map[string]any {
	out := map[string]any{
		"entity_id": prefixedID(repo, e.ID),
		"resolved": map[string]any{
			"id":             prefixedID(repo, e.ID),
			"name":           e.Name,
			"kind":           e.Kind,
			"qualified_name": e.QualifiedName,
			"repo":           repo,
			"source_file":    e.SourceFile,
		},
	}

	// Canonical path: sidecar entry keyed by prefixed entity ID.
	if entry, ok := sidecar[prefixedID(repo, e.ID)]; ok && len(entry.Effects) > 0 {
		out["effects"] = entry.Effects
		out["effect_source"] = entry.Source
		out["confidences"] = entry.Confidences
		out["confidence"] = maxConfidence(entry.Confidences)
		if len(entry.Sinks) > 0 {
			out["sinks"] = entry.Sinks
		}
		out["explanation"] = explanationFor(entry.Effects, entry.Source)
		return out
	}

	// Fallback: in-memory properties (populated only during a live link run).
	rawEffs := ""
	if e.Properties != nil {
		rawEffs = e.Properties[links.EffectPropertyKeyList]
	}
	if rawEffs == "" {
		out["effects"] = []string{}
		out["effect_source"] = "pure"
		out["confidence"] = 0.3
		out["explanation"] = "No sink primitives detected in this function or its transitive callees. " +
			"Phase 1A confidence in 'pure' is low — absence of detection does not prove absence of effect."
		return out
	}
	effs := splitNonEmpty(rawEffs)
	confs := parseConfidences(e.Properties[links.EffectPropertyKeyConfidence])
	out["effects"] = effs
	out["effect_source"] = e.Properties[links.EffectPropertyKeySource]
	out["confidences"] = confs
	out["confidence"] = maxConfidence(confs)
	if sinks := parseSinks(e.Properties[links.EffectPropertyKeySinks]); len(sinks) > 0 {
		out["sinks"] = sinks
	}
	out["explanation"] = explanationFor(effs, e.Properties[links.EffectPropertyKeySource])
	return out
}

// maxConfidence returns the strongest per-effect confidence — the
// headline confidence for the entity's effect classification. An entity
// with a direct 1.0 sink reads as high-confidence even if it also has a
// weak transitive effect. Zero when the map is empty.
func maxConfidence(confs map[string]float64) float64 {
	max := 0.0
	for _, v := range confs {
		if v > max {
			max = v
		}
	}
	return max
}

// parseConfidences decodes the comma-joined "<effect>=<float>" form
// stamped onto entity.Properties by stampEffectProperties.
func parseConfidences(raw string) map[string]float64 {
	out := map[string]float64{}
	for _, part := range splitNonEmpty(raw) {
		eq := strings.IndexByte(part, '=')
		if eq <= 0 || eq == len(part)-1 {
			continue
		}
		var v float64
		_, err := fmt.Sscanf(part[eq+1:], "%f", &v)
		if err != nil {
			continue
		}
		out[part[:eq]] = v
	}
	return out
}

// parseSinks decodes the comma-joined "<effect>:<sink>" form into a
// per-effect string slice.
func parseSinks(raw string) map[string][]string {
	out := map[string][]string{}
	for _, part := range splitNonEmpty(raw) {
		col := strings.IndexByte(part, ':')
		if col <= 0 || col == len(part)-1 {
			continue
		}
		eff := part[:col]
		out[eff] = append(out[eff], part[col+1:])
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

func splitNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// explanationFor returns a short human-facing summary for the agent.
func explanationFor(effs []string, source string) string {
	if len(effs) == 0 {
		return "Pure (no detected sinks)."
	}
	switch source {
	case "direct":
		return fmt.Sprintf("Direct sink calls observed in this function for: %s.", strings.Join(effs, ", "))
	case "transitive":
		return fmt.Sprintf("Effects %s reach this function transitively through CALLS edges; confidence drops by %.0f%% per hop.", strings.Join(effs, ", "), (1-0.95)*100)
	}
	return strings.Join(effs, ", ")
}
