// archigraph_effects MCP tool (#2764 Phase 1A substrate).
//
// Returns the effect classification for a given entity. Schema:
//
//	{
//	  "entity_id":     "<id-or-qname>",
//	  "resolved":      { "id": ..., "name": ..., "kind": ..., "repo": ... },
//	  "effects":       ["db_write", "http_out"],
//	  "confidences":   { "db_write": 0.85, "http_out": 1.00 },
//	  "sinks":         { "db_write": ["orm.write"], ... },
//	  "effect_source": "direct" | "transitive" | "pure",
//	  "explanation":   "..."
//	}
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
	"github.com/cajasmota/archigraph/internal/substrate"

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
	// Prefer $HOME so tests using t.Setenv("HOME", tmpDir) resolve the same
	// sidecar location on every OS — on Windows os.UserHomeDir() reads
	// USERPROFILE and ignores HOME.
	home := os.Getenv("HOME")
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return ""
		}
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

	// #4423 — opt-in branches facet. Only computed when include=branches so
	// the default effects payload stays byte-identical (no regression).
	wantBranches := includeWants(req, "branches")

	// Cross-repo prefixed ID? Resolve repo first for unambiguous lookup.
	if rprefix, local := splitPrefixed(key); rprefix != "" {
		if r, ok := lg.Repos[rprefix]; ok && r.Doc != nil {
			if e, ok := r.LabelIndex.ByID[local]; ok {
				out := buildEffectsPayload(r.Repo, e, sidecar)
				attachBranchesFacet(out, r, e, wantBranches)
				return jsonResult(out), nil
			}
		}
	}
	// Collect every label/qname/id match across the considered repos.
	type matchPair struct {
		ent  *graph.Entity
		repo *LoadedRepo
	}
	var matches []matchPair
	for _, r := range repos {
		for _, hit := range r.LabelIndex.LookupAll(key) {
			matches = append(matches, matchPair{ent: hit, repo: r})
		}
	}
	if len(matches) == 0 {
		return mcpapi.NewToolResultError(fmt.Sprintf("not found: %s", key)), nil
	}
	if len(matches) > 1 {
		out := make([]map[string]any, 0, len(matches))
		for _, m := range matches {
			out = append(out, map[string]any{
				"id":             prefixedID(m.repo.Repo, m.ent.ID),
				"qualified_name": m.ent.QualifiedName,
				"label":          m.ent.Name,
				"repo":           m.repo.Repo,
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
	out := buildEffectsPayload(matches[0].repo.Repo, matches[0].ent, sidecar)
	attachBranchesFacet(out, matches[0].repo, matches[0].ent, wantBranches)
	return jsonResult(out), nil
}

// includeWants reports whether the comma/space-separated `include` argument
// requests facet. Accepts repeated facets ("branches,foo") and a bare match.
func includeWants(req mcpapi.CallToolRequest, facet string) bool {
	raw := argString(req, "include", "")
	if raw == "" {
		return false
	}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' }) {
		if strings.EqualFold(strings.TrimSpace(part), facet) {
			return true
		}
	}
	return false
}

// attachBranchesFacet computes and attaches the #4423 `branches` facet to an
// effects payload when requested. It reads the resolved function's source
// window (StartLine..EndLine) from disk, selects the per-language branch
// analyzer, and enumerates the function's except/early-return/env-gate/guard
// branches. No-op when not requested (default output unchanged), when the
// language has no registered analyzer (honest-partial), or when the source is
// unreadable.
func attachBranchesFacet(out map[string]any, lr *LoadedRepo, e *graph.Entity, want bool) {
	if !want || out == nil || lr == nil || e == nil {
		return
	}
	lang := substrate.LanguageForPath(e.SourceFile)
	analyzer := substrate.BranchAnalyzerFor(lang)
	if analyzer == nil {
		// Honest-partial: tell the caller the facet is unsupported for this
		// language rather than silently implying the function is branchless.
		out["branches_supported"] = false
		out["branches_note"] = fmt.Sprintf(
			"branch classification not yet implemented for language %q (epic #4419); supported: %v",
			lang, substrate.BranchLanguages())
		return
	}
	start, end := branchSourceSpan(e)
	if start <= 0 {
		return
	}
	abs := e.SourceFile
	if !filepath.IsAbs(abs) && lr.Path != "" {
		abs = filepath.Join(lr.Path, e.SourceFile)
	}
	src, err := readRawSourceWindow(abs, start, end)
	if err != nil || src == "" {
		return
	}
	facets := analyzer(src, start)
	out["branches_supported"] = true
	out["branches"] = branchFacetsToJSON(facets)
}

// branchSourceSpan returns the [start,end] line window for an entity's body,
// applying the same degenerate-span fallback handleGetNodeSource uses so a
// synthetic end<=start entity still yields a usable window.
func branchSourceSpan(e *graph.Entity) (int, int) {
	start := e.StartLine
	end := e.EndLine
	if start <= 0 {
		return 0, 0
	}
	if end <= start {
		end = start + 400 // generous body fallback; analyzer self-bounds by indent
	}
	return start, end
}

// branchFacetsToJSON converts analyzer facets to the public JSON shape.
func branchFacetsToJSON(facets []substrate.BranchFacet) []map[string]any {
	out := make([]map[string]any, 0, len(facets))
	for _, f := range facets {
		m := map[string]any{
			"kind":      string(f.Kind),
			"condition": f.Condition,
			"outcome":   string(f.Outcome),
			"line":      f.Line,
		}
		if f.EnvVar != "" {
			m["env_var"] = f.EnvVar
		}
		if f.Returns != nil {
			r := map[string]any{}
			if f.Returns.Status != "" {
				r["status"] = f.Returns.Status
			}
			if f.Returns.Shape != "" {
				r["shape"] = f.Returns.Shape
			}
			if len(r) > 0 {
				m["returns"] = r
			}
		}
		out = append(out, m)
	}
	return out
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
