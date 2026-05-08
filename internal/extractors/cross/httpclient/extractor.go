// Package httpclient implements the cross-language HTTP client call extractor.
//
// Scans source files for outbound HTTP client calls and emits
// SCOPE.ExternalAPI entities with CALLS(kind=external_http_call) relationships.
//
// Supported patterns:
//   - JavaScript / TypeScript: fetch('url'), axios.get('url'), axios.post(...)
//   - Python:                  requests.get('url'), httpx.post('url')
//   - Go:                      http.Get("url"), http.Post("url", ...), http.NewRequest(...)
//   - Java / Kotlin:           restTemplate.exchange("url"), URI.create("url")
//
// Entity kind: "SCOPE.ExternalAPI"
// Relationships emitted: CALLS(kind=external_http_call)
//
// OTel span: indexer.http_client_extractor.extract
// Attributes: file_path, language, calls_found, unique_urls_found,
//
//	relationships_found
//
// Registration key: "_cross_httpclient"
package httpclient

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("_cross_httpclient", &Extractor{})
}

// Extractor detects HTTP client calls across all supported languages.
type Extractor struct{}

// Language returns the registration key.
func (e *Extractor) Language() string { return "_cross_httpclient" }

// ---------------------------------------------------------------------------
// call represents a detected HTTP call site.
// ---------------------------------------------------------------------------

type call struct {
	url    string
	method string // may be empty
}

// ---------------------------------------------------------------------------
// Compiled regular expressions
// ---------------------------------------------------------------------------

// JavaScript / TypeScript: fetch('url') or fetch("url") or fetch(`url`)
// Note: RE2 (Go's regexp engine) limits repetition counts to 1000 max.
var jsFetchDoubleRE = regexp.MustCompile(`(?m)\bfetch\s*\(\s*"([^"\s]{1,500})"`)
var jsFetchSingleRE = regexp.MustCompile(`(?m)\bfetch\s*\(\s*'([^'\s]{1,500})'`)
var jsFetchBacktickRE = regexp.MustCompile("(?m)\\bfetch\\s*\\(\\s*`([^`\\s]{1,500})`")

// axios.METHOD('url') — separate patterns for single and double quotes.
var jsAxiosDoubleRE = regexp.MustCompile(
	`(?im)\baxios\.(get|post|put|patch|delete|head|options|request)\s*\(\s*"([^"\s]{1,500})"`,
)
var jsAxiosSingleRE = regexp.MustCompile(
	`(?im)\baxios\.(get|post|put|patch|delete|head|options|request)\s*\(\s*'([^'\s]{1,500})'`,
)

// Python: requests.METHOD('url') / httpx.METHOD('url')
var pyRequestsDoubleRE = regexp.MustCompile(
	`(?im)\b(?:requests|httpx)\.(get|post|put|patch|delete|head|options|request)\s*\(\s*"([^"]{1,500})"`,
)
var pyRequestsSingleRE = regexp.MustCompile(
	`(?im)\b(?:requests|httpx)\.(get|post|put|patch|delete|head|options|request)\s*\(\s*'([^']{1,500})'`,
)

// Go: http.NewRequest("METHOD", "url", body)
var goNewRequestRE = regexp.MustCompile(
	`(?m)\bhttp\.NewRequest\s*\(\s*"([A-Z]{1,10})"\s*,\s*"([^"]{1,500})"`,
)

// Go: http.Get("url") / http.Post("url", ...) / http.Head("url")
var goShorthandRE = regexp.MustCompile(
	`(?m)\bhttp\.(Get|Post|Head)\s*\(\s*"([^"]{1,500})"`,
)

// Java / Kotlin: restTemplate.METHOD("url")
var javaRestTemplateDoubleRE = regexp.MustCompile(
	`(?im)\brestTemplate\.(?:exchange|getForObject|getForEntity|postForEntity|postForObject|put|delete|headForHeaders|optionsForAllow|patchForObject)\s*\(\s*"([^"]{1,500})"`,
)
var javaRestTemplateSingleRE = regexp.MustCompile(
	`(?im)\brestTemplate\.(?:exchange|getForObject|getForEntity|postForEntity|postForObject|put|delete|headForHeaders|optionsForAllow|patchForObject)\s*\(\s*'([^']{1,500})'`,
)

// Java 11+: URI.create("url")
var javaURICreateRE = regexp.MustCompile(
	`(?m)\bURI\.create\s*\(\s*"([^"]{1,500})"`,
)

// ---------------------------------------------------------------------------
// Language gate
// ---------------------------------------------------------------------------

// normaliseLanguage maps caller-supplied language strings to internal tags.
func normaliseLanguage(language string) string {
	low := strings.ToLower(language)
	if low == "typescript" || low == "javascript_typescript" {
		return "javascript"
	}
	if low == "kotlin" {
		return "java"
	}
	return low
}

// ---------------------------------------------------------------------------
// Protocol detection
// ---------------------------------------------------------------------------

// messagingImports signals messaging protocol.
var messagingImports = map[string]bool{
	"amqp": true, "amqplib": true, "pika": true, "kafka": true,
	"kafkajs": true, "confluent_kafka": true, "aiormq": true,
}

// websocketImports signals websocket protocol.
var websocketImports = map[string]bool{
	"websockets": true, "socketio": true,
}

// importKeywordRE extracts top-level module names from import statements.
var importKeywordRE = regexp.MustCompile(`(?mi)(?:import|from)\s+["']?([\w@][\w\-./]*)["']?`)
var requireRE = regexp.MustCompile(`(?mi)\brequire\s*\(\s*["']?([\w@][\w\-./]*)["']?\s*\)`)

func extractImportedModules(source string) map[string]bool {
	mods := map[string]bool{}
	add := func(raw string) {
		if raw == "" {
			return
		}
		var token string
		if strings.HasPrefix(raw, "@") {
			parts := strings.SplitN(raw, "/", 3)
			if len(parts) >= 2 {
				token = parts[0] + "/" + parts[1]
			} else {
				token = raw
			}
		} else {
			token = strings.SplitN(raw, "/", 2)[0]
			token = strings.SplitN(token, ".", 2)[0]
		}
		mods[strings.ToLower(token)] = true
	}
	for _, m := range importKeywordRE.FindAllStringSubmatch(source, -1) {
		add(m[1])
	}
	for _, m := range requireRE.FindAllStringSubmatch(source, -1) {
		add(m[1])
	}
	return mods
}

// detectProtocol determines wire protocol for a detected call site.
// Priority: grpc > messaging > websocket > rest.
func detectProtocol(url string, importedModules map[string]bool) string {
	urlLower := strings.ToLower(url)

	if strings.HasPrefix(urlLower, "grpc://") || strings.HasPrefix(urlLower, "grpc+insecure://") {
		return "grpc"
	}

	for mod := range importedModules {
		if messagingImports[mod] {
			return "messaging"
		}
	}

	if strings.HasPrefix(urlLower, "ws://") || strings.HasPrefix(urlLower, "wss://") {
		return "websocket"
	}
	for mod := range importedModules {
		if websocketImports[mod] {
			return "websocket"
		}
	}

	return "rest"
}

// ---------------------------------------------------------------------------
// Per-language call extractors
// ---------------------------------------------------------------------------

func extractJS(source string) []call {
	var out []call

	for _, m := range jsFetchDoubleRE.FindAllStringSubmatch(source, -1) {
		out = append(out, call{url: m[1]})
	}
	for _, m := range jsFetchSingleRE.FindAllStringSubmatch(source, -1) {
		out = append(out, call{url: m[1]})
	}
	for _, m := range jsFetchBacktickRE.FindAllStringSubmatch(source, -1) {
		out = append(out, call{url: m[1]})
	}

	// axios double-quote: groups [full, method, url]
	for _, m := range jsAxiosDoubleRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 3 {
			out = append(out, call{url: m[2], method: strings.ToUpper(m[1])})
		}
	}
	// axios single-quote
	for _, m := range jsAxiosSingleRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 3 {
			out = append(out, call{url: m[2], method: strings.ToUpper(m[1])})
		}
	}

	return out
}

func extractPython(source string) []call {
	var out []call
	// double-quote: groups [full, method, url]
	for _, m := range pyRequestsDoubleRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 3 {
			out = append(out, call{url: m[2], method: strings.ToUpper(m[1])})
		}
	}
	// single-quote
	for _, m := range pyRequestsSingleRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 3 {
			out = append(out, call{url: m[2], method: strings.ToUpper(m[1])})
		}
	}
	return out
}

func extractGo(source string) []call {
	var out []call

	for _, m := range goNewRequestRE.FindAllStringSubmatch(source, -1) {
		out = append(out, call{url: m[2], method: strings.ToUpper(m[1])})
	}

	for _, m := range goShorthandRE.FindAllStringSubmatch(source, -1) {
		out = append(out, call{url: m[2], method: strings.ToUpper(m[1])})
	}

	return out
}

func extractJava(source string) []call {
	var out []call

	for _, m := range javaRestTemplateDoubleRE.FindAllStringSubmatch(source, -1) {
		out = append(out, call{url: m[1]})
	}
	for _, m := range javaRestTemplateSingleRE.FindAllStringSubmatch(source, -1) {
		out = append(out, call{url: m[1]})
	}

	for _, m := range javaURICreateRE.FindAllStringSubmatch(source, -1) {
		out = append(out, call{url: m[1]})
	}

	return out
}

// ---------------------------------------------------------------------------
// All-language scan (when language is empty)
// ---------------------------------------------------------------------------

func extractAll(source string) []call {
	var out []call
	out = append(out, extractJS(source)...)
	out = append(out, extractPython(source)...)
	out = append(out, extractGo(source)...)
	out = append(out, extractJava(source)...)
	return out
}

// ---------------------------------------------------------------------------
// Ref builders
// ---------------------------------------------------------------------------

func callerRef(filePath string) string {
	return "scope:component:http_caller:" + filePath
}

func apiRef(url string) string {
	return "scope:external_api:" + url
}

// ---------------------------------------------------------------------------
// Entity / relationship builders
// ---------------------------------------------------------------------------

func buildEntitiesAndRels(filePath string, calls []call, importedModules map[string]bool) []types.EntityRecord {
	var out []types.EntityRecord
	cRef := callerRef(filePath)
	seenURLs := map[string]bool{}

	for _, c := range calls {
		aRef := apiRef(c.url)

		if !seenURLs[c.url] {
			seenURLs[c.url] = true
			out = append(out, types.EntityRecord{
				Name:       c.url,
				Kind:       "SCOPE.ExternalAPI",
				SourceFile: filePath,
				Properties: map[string]string{
					"url":        c.url,
					"ref":        aRef,
					"provenance": "INFERRED_FROM_HTTP_CLIENT_CALL",
				},
				QualityScore: 0.8,
			})
		}

		protocol := detectProtocol(c.url, importedModules)
		relProps := map[string]string{
			"kind":     "external_http_call",
			"url":      c.url,
			"protocol": protocol,
		}
		if c.method != "" {
			relProps["http_method"] = c.method
		}

		relName := "CALLS:" + cRef + "->" + aRef
		if c.method != "" {
			relName += ":" + c.method
		}
		out = append(out, types.EntityRecord{
			Name:       relName,
			Kind:       "relationship",
			Subtype:    "calls",
			SourceFile: filePath,
			Relationships: []types.RelationshipRecord{
				{
					FromID:     cRef,
					ToID:       aRef,
					Kind:       "CALLS",
					Properties: relProps,
				},
			},
			QualityScore: 0.8,
		})
	}

	return out
}

// ---------------------------------------------------------------------------
// Extract implements extractor.Extractor
// ---------------------------------------------------------------------------

// Extract scans a source file for HTTP client calls and emits External_API entities.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor._cross_httpclient")
	ctx, span := tracer.Start(ctx, "indexer.http_client_extractor.extract")
	defer span.End()
	_ = ctx

	span.SetAttributes(
		attribute.String("file_path", file.Path),
		attribute.String("language", file.Language),
	)

	source := string(file.Content)
	langTag := normaliseLanguage(file.Language)

	var calls []call
	switch langTag {
	case "javascript":
		calls = extractJS(source)
	case "python":
		calls = extractPython(source)
	case "go":
		calls = extractGo(source)
	case "java":
		calls = extractJava(source)
	default:
		// Empty language: evaluate all patterns (cross-language fallback).
		calls = extractAll(source)
	}

	importedModules := extractImportedModules(source)
	entities := buildEntitiesAndRels(file.Path, calls, importedModules)

	// Count unique URLs (entity records with Kind=SCOPE.ExternalAPI).
	uniqueURLs := 0
	for _, rec := range entities {
		if rec.Kind == "SCOPE.ExternalAPI" {
			uniqueURLs++
		}
	}

	span.SetAttributes(
		attribute.Int("calls_found", len(calls)),
		attribute.Int("unique_urls_found", uniqueURLs),
		attribute.Int("relationships_found", len(calls)),
	)

	return entities, nil
}
