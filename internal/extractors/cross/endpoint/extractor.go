// Package endpoint implements the cross-language API endpoint catalog extractor.
//
// Scans source files for REST / gRPC / GraphQL endpoint declarations and emits
// SCOPE.Operation entities (with subtype "gin" / "express" / "grpc" / ...)
// together with SERVES relationship edges that link each endpoint to its
// handler function. See MX-1094 for the rationale on the Operation kind.
//
// Supported styles:
//
//   - REST routers:   Gin, Express, FastAPI, Flask, Spring, Django, Phoenix,
//     ASP.NET, Rails
//   - gRPC:           .proto service/rpc declarations (UNARY / SERVER_STREAM /
//     CLIENT_STREAM / BIDI_STREAM)
//   - GraphQL SDL:    Query / Mutation / Subscription block fields
//
// Entity kind:         "SCOPE.Operation" with subtype "endpoint"
// Relationship kind:   "SERVES"  (endpoint → handler function)
//
// MX-1094: Endpoints emit as SCOPE.Operation (with subtype carrying the
// router style — gin / express / grpc / graphql / etc.) because the graph's
// 14-type SCOPE allowlist does not include a standalone "Endpoint" type.
// SCOPE.Operation is the canonical bucket for any callable behaviour and
// the same convention is already used by the Java custom extractors
// (jakarta_ee.go, microprofile.go) for REST handlers.
//
// OTel span:   indexer.endpoint_extract
// Attributes:  language, framework, endpoint_count, file_path
//
// Registration key: "_cross_endpoint"
//
// The extractor short-circuits when no known web framework import is present
// in the file AND the file extension does not hint at .proto / .graphql, so
// the hot path on non-web files is a handful of regex matches over the import
// list only.
package endpoint

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("_cross_endpoint", &Extractor{})
}

// Extractor implements extractor.Extractor for API endpoint cataloging.
type Extractor struct{}

// Language returns the registration key.
func (e *Extractor) Language() string { return "_cross_endpoint" }

// ---------------------------------------------------------------------------
// Path normalisation
// ---------------------------------------------------------------------------

// pathParamExpressRE captures Express/Flask-style ":id" parameters.
var pathParamExpressRE = regexp.MustCompile(`:([A-Za-z_]\w*)`)

// pathParamAngleRE captures Flask/Django-style "<int:id>" / "<id>" parameters.
// The optional converter ("int:", "str:", "slug:" …) is stripped.
var pathParamAngleRE = regexp.MustCompile(`<(?:\w+:)?([A-Za-z_]\w*)>`)

// pathParamBraceRE captures FastAPI/Spring-style "{id}" parameters.
var pathParamBraceRE = regexp.MustCompile(`\{([A-Za-z_]\w*)\}`)

// normalisePath converts a raw route pattern from any supported syntax into
// the canonical `/users/{id}` form and returns the ordered list of parameter
// names. Trailing slashes (except the root "/") are stripped so
// "/users/" and "/users" collapse to the same canonical path.
//
// Malformed input is returned unchanged with an empty params list — callers
// are expected to log a warning and continue (see Behaviour rule #1 in
// MX-1049).
func normalisePath(raw string) (canonical string, params []string, ok bool) {
	if raw == "" {
		return "", nil, false
	}
	p := raw

	// Order matters: angle before brace so "<int:id>" is consumed before any
	// literal `{` scanner would try to.
	var names []string

	p = pathParamAngleRE.ReplaceAllStringFunc(p, func(s string) string {
		m := pathParamAngleRE.FindStringSubmatch(s)
		if len(m) >= 2 {
			names = append(names, m[1])
		}
		return "{" + m[1] + "}"
	})

	p = pathParamExpressRE.ReplaceAllStringFunc(p, func(s string) string {
		m := pathParamExpressRE.FindStringSubmatch(s)
		if len(m) >= 2 {
			names = append(names, m[1])
		}
		return "{" + m[1] + "}"
	})

	for _, m := range pathParamBraceRE.FindAllStringSubmatch(p, -1) {
		if len(m) >= 2 {
			// Skip duplicates from earlier normalisation passes.
			if !containsString(names, m[1]) {
				names = append(names, m[1])
			}
		}
	}

	// Ensure leading slash, strip trailing slash (except for root "/").
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		p = strings.TrimRight(p, "/")
	}

	return p, names, true
}

func containsString(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// File-extension hints
// ---------------------------------------------------------------------------

// forceFrameworkFromExt returns a framework name based on file extension for
// file types that do not carry an import list (.proto, .graphql, .gql).
// Empty string means "no extension override".
func forceFrameworkFromExt(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".proto":
		return "grpc"
	case ".graphql", ".gql", ".graphqls":
		return "graphql"
	}
	return ""
}

// ---------------------------------------------------------------------------
// Entity / relationship builders
// ---------------------------------------------------------------------------

// endpointEntityID builds a stable identity string for an endpoint entity.
// Format: "scope:endpoint:<file>#<method>:<path>"
func endpointEntityID(filePath, method, canonicalPath string) string {
	return "scope:endpoint:" + filePath + "#" + method + ":" + canonicalPath
}

// handlerRef builds the target ref for a SERVES edge. When the handler
// qualified name is empty we return "" so callers skip the relationship.
func handlerRef(filePath, qname string) string {
	if qname == "" {
		return ""
	}
	return "scope:operation:" + filePath + "#" + qname
}

// buildEntity assembles a SCOPE.Operation EntityRecord (subtype = router style)
// plus its SERVES edge to the handler function.
func buildEntity(
	filePath string,
	framework string,
	style string,
	language string,
	m frameworkMatch,
	canonicalPath string,
	params []string,
) types.EntityRecord {
	entityID := endpointEntityID(filePath, m.method, canonicalPath)
	props := map[string]string{
		"method":       m.method,
		"path":         canonicalPath,
		"framework":    framework,
		"style":        style,
		"handler_ref":  m.handlerQName,
		"raw_path":     m.rawPath,
		"params_csv":   strings.Join(params, ","),
		"param_count":  fmt.Sprintf("%d", len(params)),
		"request_type": "",  // reserved: populated by future AST-based pass
		"response_type": "", // reserved: populated by future AST-based pass
		"ref":          entityID,
		"provenance":   "INFERRED_FROM_FRAMEWORK_ROUTER",
	}

	rec := types.EntityRecord{
		Name: m.method + " " + canonicalPath,
		// MX-1094: the 14-type SCOPE allowlist has no "Endpoint" entry —
		// route handlers belong in SCOPE.Operation (the canonical bucket for
		// callable behaviour). The router style (gin/express/grpc/graphql/...)
		// is preserved on Subtype, mirroring jakarta_ee.go's `endpoint` subtype.
		Kind:         "SCOPE.Operation",
		SourceFile:   filePath,
		Language:     language,
		Subtype:      style,
		Properties:   props,
		QualityScore: 0.85,
	}

	if tRef := handlerRef(filePath, m.handlerQName); tRef != "" {
		rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
			FromID: entityID,
			ToID:   tRef,
			Kind:   "SERVES",
			Properties: map[string]string{
				"handler_qname": m.handlerQName,
				"framework":     framework,
				"method":        m.method,
			},
		})
	}

	return rec
}

// ---------------------------------------------------------------------------
// Extract implements extractor.Extractor
// ---------------------------------------------------------------------------

// Extract scans a source file for REST/gRPC/GraphQL endpoint declarations and
// emits SCOPE.Operation entities (subtype = router style) + SERVES edges. Files with no recognised web
// framework import (and not a .proto / .graphql SDL) are skipped and return
// an empty slice.
//
// Behaviour notes (see MX-1049):
//   - If a handler function name cannot be statically recovered, the endpoint
//     entity is still emitted, but no SERVES edge is added.
//   - Ambiguous files (multiple framework imports) resolve to the first entry
//     in frameworkOrder that matches — this makes the decision deterministic.
//   - Malformed route patterns are stored as `raw_path` with the best-effort
//     canonical path under `path`.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor._cross_endpoint")
	_, span := tracer.Start(ctx, "indexer.endpoint_extract")
	defer span.End()

	span.SetAttributes(
		attribute.String("file_path", file.Path),
		attribute.String("language", file.Language),
	)

	source := string(file.Content)
	if source == "" {
		span.SetAttributes(
			attribute.String("framework", ""),
			attribute.Int("endpoint_count", 0),
		)
		return nil, nil
	}

	tokens := extractImportTokens(source)
	force := forceFrameworkFromExt(file.Path)
	fw := selectFramework(tokens, force)
	if fw == nil {
		// Rule #1: unrecognised framework → skip entirely.
		span.SetAttributes(
			attribute.String("framework", ""),
			attribute.Int("endpoint_count", 0),
		)
		return nil, nil
	}

	matches := fw.detect(source)

	out := make([]types.EntityRecord, 0, len(matches))
	for _, m := range matches {
		canonical, params, ok := normalisePath(m.rawPath)
		if !ok || canonical == "" {
			// Rule: malformed patterns → store raw, keep params empty.
			canonical = m.rawPath
			params = nil
		}
		out = append(out, buildEntity(file.Path, fw.name, fw.style, file.Language, m, canonical, params))
	}

	span.SetAttributes(
		attribute.String("framework", fw.name),
		attribute.Int("endpoint_count", len(out)),
	)
	return out, nil
}
