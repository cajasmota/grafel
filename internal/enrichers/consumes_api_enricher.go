package enrichers

// ConsumesAPIEnricher links HTTP client calls to known endpoint entities.
// Port of Python consumes_api_enricher.py (MX-686).

import (
	"net/url"
	"strings"
)

// HTTPClientCall represents an HTTP client call site extracted from source.
type HTTPClientCall struct {
	CallerServiceID string
	URLPattern      string
	Method          string
	SourceFile      string
	SourceLine      int
}

// EndpointInfo represents a known endpoint entity indexed for an org.
type EndpointInfo struct {
	ServiceID string
	EntityRef string
	Path      string
	Method    string // HTTP method or "*" for wildcard
}

// ConsumesAPIEdge is an emitted CONSUMES_API relationship.
type ConsumesAPIEdge struct {
	CallerServiceID  string
	EndpointEntityID string
	MatchedURL       string
	Method           string
	SourceFile       string
	SourceLine       int
}

// ExtractURLPath extracts the path component from a URL string.
func ExtractURLPath(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Path == "" {
		return ""
	}
	return parsed.Path
}

// MethodMatches returns true when callMethod is compatible with endpointMethod.
func MethodMatches(callMethod, endpointMethod string) bool {
	if endpointMethod == "*" {
		return true
	}
	if callMethod == "" {
		return false
	}
	return strings.EqualFold(callMethod, endpointMethod)
}

// EnrichConsumesAPI emits ConsumesAPIEdge records for matched HTTP client calls.
func EnrichConsumesAPI(calls []HTTPClientCall, endpoints []EndpointInfo) []ConsumesAPIEdge {
	if len(endpoints) == 0 {
		return nil
	}
	pathIndex := make(map[string][]EndpointInfo)
	for _, ep := range endpoints {
		pathIndex[ep.Path] = append(pathIndex[ep.Path], ep)
	}
	var edges []ConsumesAPIEdge
	for _, call := range calls {
		callPath := ExtractURLPath(call.URLPattern)
		if callPath == "" {
			continue
		}
		for _, ep := range pathIndex[callPath] {
			if !MethodMatches(call.Method, ep.Method) {
				continue
			}
			edges = append(edges, ConsumesAPIEdge{
				CallerServiceID:  call.CallerServiceID,
				EndpointEntityID: ep.EntityRef,
				MatchedURL:       call.URLPattern,
				Method:           call.Method,
				SourceFile:       call.SourceFile,
				SourceLine:       call.SourceLine,
			})
		}
	}
	return edges
}
