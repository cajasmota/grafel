package enrichment

// dynamic_baseurl.go surfaces consumer-side http_endpoint entities whose
// baseURL cannot be resolved statically. These entities are genuine
// cross-repo calls that static analysis can't link to a producer — they
// deserve human/agent annotation rather than silent omission.
//
// Two patterns are detected:
//
//  1. runtime_dynamic=true — the URL was built from a runtime env-var
//     concatenation (process.env.API_URL + "/path", os.environ["BASE"] +
//     "/path", System.getenv("X") + "/path", etc.). The env-var prefix
//     was stripped at synthesis time so only the static suffix survives in
//     the canonical path. An agent can supply the base URL host/prefix so
//     the cross-repo link resolves to the correct producer.
//
//  2. dynamic_baseurl=true — the canonical path starts with a `{<name>}`
//     placeholder (first segment is a runtime variable). This arises from
//     template literals like `/${tenantId}/contracts/${contractId}` where
//     the leading segment determines which backend the call targets. These
//     are structurally unmatchable by static analysis.
//
// Each detected entity is emitted as an enrichment Candidate of kind
// KindDynamicBaseURLEndpoint with category "cross-repo runtime". The
// candidates appear in archigraph_repairs action=list so an agent can
// submit a baseURL hint via the repairs/enrichments curation surface.
//
// After annotation the link pass re-runs against the curated baseURL and
// the consumer endpoint can resolve to the correct producer.
//
// This implements issue #708 (ADR-0015 extension for structural
// unmatchables).

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// KindDynamicBaseURLEndpoint is the enrichment candidate kind for
// http_endpoint entities whose baseURL is runtime-determined and cannot
// be statically resolved.
const KindDynamicBaseURLEndpoint = "dynamic_baseurl_endpoint"

// CategoryCrossRepoRuntime is the category string written to every
// dynamic-baseurl candidate's context so archigraph_repairs consumers can
// filter by category.
const CategoryCrossRepoRuntime = "cross-repo runtime"

// dynamicBaseURLCandidateID produces a stable ec:<hex16> identifier for
// a dynamic-baseurl candidate. Keyed on (entityID, kind) so it is
// reproducible across index runs as long as the entity ID is stable.
func dynamicBaseURLCandidateID(entityID string) string {
	h := sha256.New()
	h.Write([]byte(KindDynamicBaseURLEndpoint))
	h.Write([]byte{0})
	h.Write([]byte(entityID))
	return "ec:" + hex.EncodeToString(h.Sum(nil))[:16]
}

// CollectDynamicBaseURLCandidates walks doc.Entities and emits a
// Candidate of kind KindDynamicBaseURLEndpoint for every http_endpoint
// entity that exhibits either of the two dynamic-baseurl signals:
//
//   - Properties["runtime_dynamic"] == "true"  — env-var URL prefix
//   - Properties["dynamic_baseurl"] == "true"   — path starts with {<name>}
//
// The function is deliberately independent of the resolver so it can run
// unconditionally after the synthesis pass completes (no --enable-repair-
// candidates gate required). It is additive: existing candidates in
// enrichment-candidates.json are merged by the WriteCandidates caller.
func CollectDynamicBaseURLCandidates(doc *graph.Document) []Candidate {
	if doc == nil {
		return nil
	}

	out := make([]Candidate, 0, 16)
	seen := make(map[string]bool, 16)

	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind != "http_endpoint" {
			continue
		}
		props := e.Properties
		if props == nil {
			continue
		}
		// Only consumer-side synthetics are dynamic-baseurl candidates.
		// Producer-side endpoints ARE the target; consumer-side are the
		// callers whose target is indeterminate. The pattern_type property
		// set by the synthesis pass distinguishes them.
		if props["pattern_type"] != "http_endpoint_client_synthesis" {
			continue
		}

		isDynamic := false
		dynamicKind := ""

		switch {
		case props["runtime_dynamic"] == "true":
			isDynamic = true
			dynamicKind = "env-var-baseurl"
		case props["dynamic_baseurl"] == "true":
			isDynamic = true
			dynamicKind = "leading-path-placeholder"
		}

		if !isDynamic {
			continue
		}

		id := dynamicBaseURLCandidateID(e.ID)
		if seen[id] {
			continue
		}
		seen[id] = true

		path := props["path"]
		verb := props["verb"]
		framework := props["framework"]
		sourceCaller := props["source_caller"]

		ctx := map[string]any{
			"category":      CategoryCrossRepoRuntime,
			"dynamic_kind":  dynamicKind,
			"endpoint_id":   e.ID,
			"endpoint_name": e.Name,
			"path":          path,
			"verb":          verb,
			"framework":     framework,
			"source_file":   e.SourceFile,
			"start_line":    e.StartLine,
			"language":      e.Language,
		}
		if sourceCaller != "" {
			ctx["source_caller"] = sourceCaller
		}
		// Carry the raw dynamic-prefix identifier name when it can be
		// extracted from the path (first {<name>} segment, with or without
		// a leading slash: `{tenantId}/...` or `/{tenantId}/...`).
		if dynamicKind == "leading-path-placeholder" {
			// Strip an optional leading slash before looking for {name}.
			stripped := strings.TrimPrefix(path, "/")
			if strings.HasPrefix(stripped, "{") {
				if end := strings.IndexByte(stripped[1:], '}'); end >= 0 {
					ctx["dynamic_prefix_var"] = stripped[1 : 1+end]
				}
			}
		}
		// Expose the static path suffix (everything after the first {…}/
		// segment) as a hint for the agent when searching the producer side.
		ctx["static_path_suffix"] = staticPathSuffix(path)

		out = append(out, Candidate{
			ID:           id,
			Kind:         KindDynamicBaseURLEndpoint,
			SubjectID:    e.ID,
			Context:      ctx,
			DiscoveredAt: nowRFC3339(),
		})
	}

	// Stable sort so byte output is deterministic across index runs.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SubjectID != out[j].SubjectID {
			return out[i].SubjectID < out[j].SubjectID
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// staticPathSuffix strips the first dynamic segment from a URL path so
// the remainder can be used to locate the producer endpoint.
//
// Examples:
//
//	{tenantId}/contracts/{contractId}   →  /contracts/{contractId}
//	{param}/users/{id}                  →  /users/{id}
//	/{tenantId}/contracts/{contractId}  →  /contracts/{contractId}
//	/users/{id}                         →  /users/{id}  (no leading param)
func staticPathSuffix(path string) string {
	// Normalise: strip a leading slash before checking for a placeholder.
	leadingSlash := strings.HasPrefix(path, "/")
	stripped := strings.TrimPrefix(path, "/")
	if !strings.HasPrefix(stripped, "{") {
		// No leading placeholder — return as-is (re-add stripped slash).
		if leadingSlash {
			return "/" + stripped
		}
		return stripped
	}
	// Skip past the first {…} segment (placeholder + optional slash).
	end := strings.IndexByte(stripped, '}')
	if end < 0 {
		return "/"
	}
	rest := stripped[end+1:]
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}
	return rest
}
