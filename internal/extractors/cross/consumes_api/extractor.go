// Package consumes_api implements the cross-language API-consumption extractor.
//
// It restores the previously-orphaned consumes_api enricher (Python
// `consumes_api_enricher.py`, ported but unwired in internal/enrichers/) as a
// registered Pass-3 cross extractor that emits CONSUMES_API edges from an HTTP
// client call site to the server endpoint it consumes.
//
// What it emits
// -------------
//
//	CONSUMES_API : caller stub ("scope:component:http_caller:<file>")
//	             → endpoint entity ("scope:endpoint:<file>#<VERB>:<path>")
//
// for every HTTP client call in a file whose (verb, path) matches a server
// endpoint declared in the SAME file. This is the co-located client+server
// shape — BFFs, API gateways, Django views that call their own API,
// integration-test harnesses — where the consumption is statically
// determinable without crossing a file boundary.
//
// Why same-file only
// ------------------
// The cross-extractor contract (internal/extractor.Extractor) is strictly
// per-file: Extract sees one file at a time and has no group-wide / multi-repo
// view. The original enricher's same-org client→endpoint join across repos is
// therefore owned by the cross-repo link layer (internal/links/http_pass.go,
// which fires only for groups of ≥2 repos via synthetic http_endpoint
// entities). This extractor deliberately covers the complementary case the
// link layer never touches: consumption that lives entirely inside one file.
// Because both sides live in the same file they are by construction the same
// org / repo, so the original enricher's cross-org guard is trivially
// satisfied here.
//
// Complementary, not duplicate
// ----------------------------
//   - _cross_httpclient emits CALLS → SCOPE.ExternalAPI(url). That is the raw
//     outbound-call fact; it does not know which endpoint the URL resolves to.
//   - _cross_endpoint emits SCOPE.Operation endpoint entities + SERVES edges to
//     handlers. That is the server-side route catalog.
//   - internal/links/http_pass emits cross-repo MethodHTTP links between repos.
//
// CONSUMES_API is the *consumption* join the three above never produce: a
// direct caller → endpoint-identity edge. To avoid double-emitting the
// underlying entities, this extractor reuses the existing _cross_httpclient and
// _cross_endpoint extractors to discover calls and endpoints and emits ONLY the
// new CONSUMES_API relationship (carried on a thin, deduplicated entity record);
// it never re-emits the ExternalAPI / endpoint entities those extractors own.
//
// Registration key: "_cross_consumes_api"
package consumes_api

import (
	"context"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/extractors/cross/endpoint"
	"github.com/cajasmota/archigraph/internal/extractors/cross/httpclient"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("_cross_consumes_api", &Extractor{})
}

// Extractor implements extractor.Extractor for same-file API consumption.
type Extractor struct {
	// clientExtractor / endpointExtractor are reused to discover the call and
	// endpoint facts without duplicating any detection regexes. Both are
	// stateless, so a shared instance is safe for concurrent Extract calls.
	clientExtractor   httpclient.Extractor
	endpointExtractor endpoint.Extractor
}

// Language returns the registration key.
func (e *Extractor) Language() string { return "_cross_consumes_api" }

// ---------------------------------------------------------------------------
// Path / method matching — ported from internal/enrichers/consumes_api_enricher.go
// ---------------------------------------------------------------------------

// pathParamRe matches every path-parameter placeholder style so a client call
// path and a server endpoint path can be compared structurally regardless of
// the parameter-syntax each side spelled:
//   - {id}, {userId}      curly-brace (FastAPI / DRF router / endpoint canonical)
//   - :id, :userId        colon prefix (Express / Rails / endpoint canonical)
//   - <int:id>, <id>      angle bracket (Django / Flask)
//   - ${id}, {*}          JS template-literal / wildcard sentinels (httpclient)
var pathParamRe = regexp.MustCompile(`\$\{[^}]*\}|\{[^}]*\}|:[A-Za-z_]\w*|<[^>]+>`)

// extractURLPath extracts the path component from a raw URL or URL pattern.
// Absolute URLs ("https://host/x") yield "/x"; root-relative URLs ("/x") yield
// "/x" unchanged. Returns "" when no usable path can be recovered.
//
// Ported from ExtractURLPath in the orphaned enricher, hardened for the
// root-relative case (url.Parse keeps the leading slash there) and for
// template-literal URLs whose "${...}" segments would otherwise confuse the
// host/path split.
func extractURLPath(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	// Root-relative paths parse with an empty Host; keep them verbatim.
	if strings.HasPrefix(rawURL, "/") {
		if i := strings.IndexAny(rawURL, "?#"); i >= 0 {
			return rawURL[:i]
		}
		return rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Path == "" {
		return ""
	}
	return parsed.Path
}

// normalizePath canonicalises a path for cross-side comparison: every parameter
// placeholder collapses to the sentinel {*}, the result is lower-cased, and a
// trailing slash (other than the bare root) is stripped. Mirrors the
// normalisation the endpoint extractor and links/http_pass apply so that
// "/api/users/{id}" (server) and "/api/users/123"-style template calls compare
// equal once the dynamic segment is a placeholder.
func normalizePath(path string) string {
	if path == "" {
		return ""
	}
	path = pathParamRe.ReplaceAllString(path, "{*}")
	path = strings.ToLower(path)
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
		if path == "" {
			path = "/"
		}
	}
	return path
}

// methodMatches returns true when the client call's verb is compatible with the
// endpoint's verb. An empty endpoint verb or the wildcard "*"/"ANY" matches any
// call; an empty call verb never matches a specific endpoint verb (we refuse to
// guess). Ported from MethodMatches in the orphaned enricher, extended to treat
// "ANY" (Django ViewSet) as a wildcard.
func methodMatches(callMethod, endpointMethod string) bool {
	em := strings.ToUpper(strings.TrimSpace(endpointMethod))
	if em == "" || em == "*" || em == "ANY" {
		return true
	}
	if callMethod == "" {
		return false
	}
	return strings.EqualFold(callMethod, em)
}

// ---------------------------------------------------------------------------
// Fact harvesting from the reused extractors
// ---------------------------------------------------------------------------

// clientCall is one outbound HTTP call site recovered from the httpclient
// extractor's CALLS edges.
type clientCall struct {
	callerRef string // the http_caller stub ID (FromID of the CALLS edge)
	method    string // HTTP verb (may be empty when the client form omits it)
	url       string // raw URL / pattern
	path      string // normalised path key for matching
}

// serverEndpoint is one server endpoint recovered from the endpoint extractor.
type serverEndpoint struct {
	entityID string // canonical endpoint entity ID (ToID of the CONSUMES_API edge)
	method   string // HTTP verb / "ANY" / "*"
	path     string // normalised path key for matching
	rawPath  string // canonical (non-normalised) path, for traceability props
}

// harvestClientCalls runs the reused httpclient extractor and lifts each
// CALLS(kind=external_http_call) edge into a clientCall. The httpclient
// extractor embeds the CALLS edge on the ExternalAPI entity it owns; we read it
// without re-emitting that entity.
func (e *Extractor) harvestClientCalls(ctx context.Context, file extractor.FileInput) []clientCall {
	recs, err := e.clientExtractor.Extract(ctx, file)
	if err != nil {
		return nil
	}
	var out []clientCall
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if !strings.EqualFold(rel.Kind, "CALLS") {
				continue
			}
			if rel.Properties["kind"] != "external_http_call" {
				continue
			}
			rawURL := rel.Properties["url"]
			p := normalizePath(extractURLPath(rawURL))
			if p == "" {
				continue
			}
			out = append(out, clientCall{
				callerRef: rel.FromID,
				method:    strings.ToUpper(strings.TrimSpace(rel.Properties["http_method"])),
				url:       rawURL,
				path:      p,
			})
		}
	}
	return out
}

// harvestServerEndpoints runs the reused endpoint extractor and lifts each
// emitted SCOPE.Operation router endpoint into a serverEndpoint.
func (e *Extractor) harvestServerEndpoints(ctx context.Context, file extractor.FileInput) []serverEndpoint {
	recs, err := e.endpointExtractor.Extract(ctx, file)
	if err != nil {
		return nil
	}
	var out []serverEndpoint
	for _, r := range recs {
		if r.Kind != "SCOPE.Operation" || r.Properties["provenance"] != "INFERRED_FROM_FRAMEWORK_ROUTER" {
			continue
		}
		rawPath := r.Properties["path"]
		p := normalizePath(rawPath)
		if p == "" {
			continue
		}
		out = append(out, serverEndpoint{
			entityID: r.Properties["ref"],
			method:   r.Properties["method"],
			path:     p,
			rawPath:  rawPath,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Extract implements extractor.Extractor
// ---------------------------------------------------------------------------

// consumesEntityID builds the stable identity for the thin carrier entity that
// holds a file's CONSUMES_API edges. One per file keeps the edge set
// deduplicated and avoids colliding with the ExternalAPI / endpoint entities
// the reused extractors own.
func consumesEntityID(filePath string) string {
	return "scope:consumes_api:" + filePath
}

// Extract discovers HTTP client calls and server endpoints in the file (by
// reusing the httpclient + endpoint extractors) and emits one CONSUMES_API edge
// for every (call, endpoint) pair whose verb is compatible and whose normalised
// path is identical. Files with no client call OR no endpoint produce nothing.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor._cross_consumes_api")
	ctx, span := tracer.Start(ctx, "indexer.consumes_api_enricher.enrich")
	defer span.End()

	span.SetAttributes(
		attribute.String("file_path", file.Path),
		attribute.String("language", file.Language),
	)

	if len(file.Content) == 0 {
		span.SetAttributes(attribute.Int("consumes_api_edges", 0))
		return nil, nil
	}

	calls := e.harvestClientCalls(ctx, file)
	endpoints := e.harvestServerEndpoints(ctx, file)
	if len(calls) == 0 || len(endpoints) == 0 {
		span.SetAttributes(
			attribute.Int("client_calls", len(calls)),
			attribute.Int("server_endpoints", len(endpoints)),
			attribute.Int("consumes_api_edges", 0),
		)
		return nil, nil
	}

	// Index endpoints by normalised path for an O(calls + endpoints) join.
	byPath := map[string][]serverEndpoint{}
	for _, ep := range endpoints {
		byPath[ep.path] = append(byPath[ep.path], ep)
	}

	carrierID := consumesEntityID(file.Path)
	rec := types.EntityRecord{
		Name:       file.Path,
		Kind:       "SCOPE.Component",
		Subtype:    "consumes_api",
		SourceFile: file.Path,
		Language:   file.Language,
		Properties: map[string]string{
			"ref":        carrierID,
			"provenance": "INFERRED_FROM_HTTP_CLIENT_CALL",
		},
		QualityScore: 0.8,
	}

	// Deterministic edge emission + per-(caller,endpoint,method) dedup so two
	// call sites in the same file to the same endpoint do not double-emit.
	seen := map[string]bool{}
	for _, c := range calls {
		matches := byPath[c.path]
		// Stable ordering so re-runs produce identical graph.json.
		sort.SliceStable(matches, func(i, j int) bool {
			return matches[i].entityID < matches[j].entityID
		})
		for _, ep := range matches {
			if !methodMatches(c.method, ep.method) {
				continue
			}
			dedupKey := c.callerRef + "\x00" + ep.entityID + "\x00" + c.method
			if seen[dedupKey] {
				continue
			}
			seen[dedupKey] = true
			rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
				FromID: c.callerRef,
				ToID:   ep.entityID,
				Kind:   string(types.RelationshipKindConsumesAPI),
				Properties: map[string]string{
					"method":        c.method,
					"matched_url":   c.url,
					"matched_path":  c.path,
					"endpoint_path": ep.rawPath,
					"via":           "same_file_http_consumption",
					"provenance":    "INFERRED_FROM_HTTP_CLIENT_CALL",
				},
			})
		}
	}

	span.SetAttributes(
		attribute.Int("client_calls", len(calls)),
		attribute.Int("server_endpoints", len(endpoints)),
		attribute.Int("consumes_api_edges", len(rec.Relationships)),
	)

	if len(rec.Relationships) == 0 {
		return nil, nil
	}
	return []types.EntityRecord{rec}, nil
}
