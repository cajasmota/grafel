// Package php — regex-based API Platform (Symfony) REST-resource extractor.
//
// API Platform (api-platform/core) turns a plain PHP entity/DTO class into a
// full REST (and optionally GraphQL) API by annotating it with the
// `#[ApiResource]` attribute. From that single attribute it auto-generates the
// CRUD operation set — a collection GET + POST and an item GET + PUT + PATCH +
// DELETE — each addressable under a path derived from the short class name
// (`Book` → `/books`):
//
//	#[ApiResource]
//	class Book
//	{
//	    public int $id;
//	    public string $title;
//	}
//
// Generated endpoints: GET /books (collection), POST /books, GET /books/{id},
// PUT /books/{id}, PATCH /books/{id}, DELETE /books/{id}.
//
// The operation set can be narrowed/customised by listing explicit operation
// attributes — either as the `operations:` argument of `#[ApiResource]` or as
// per-class operation attributes #[Get], #[GetCollection], #[Post], #[Put],
// #[Patch], #[Delete] (the modern 3.x metadata style):
//
//	#[ApiResource(operations: [new Get(), new GetCollection()])]
//	#[Get]
//	#[Post(uriTemplate: '/books/publish')]
//	class Book {}
//
// And query filters are declared with `#[ApiFilter]`:
//
//	#[ApiFilter(SearchFilter::class, properties: ['title' => 'partial'])]
//
// Mapping (mirrors the symfony.go route shape — SCOPE.Operation endpoints with
// http_method / route_path):
//
//   - A bare `#[ApiResource]` on class Book emits the six default CRUD endpoints
//     for /books and /books/{id}, each a SCOPE.Operation endpoint named
//     "<METHOD> <path>" with framework=api-platform.
//   - When explicit operation attributes are present (operations: [...] list or
//     standalone #[Get]/#[Post]/… attributes on the class), ONLY those
//     operations are emitted, honouring any uriTemplate override.
//   - `#[ApiFilter]` declarations are recorded on the resource as a SCOPE.Schema
//     filter-set entity so the resource's queryability is captured.
//
// HONEST LIMIT: this is file-local and structural. The default REST path is
// derived from the class short-name via a lightweight pluraliser (Book→books,
// Category→categories) — API Platform's real inflector (and `routePrefix` /
// `uriTemplate` on #[ApiResource] itself, custom operation classes, sub-
// resources, and `shortName:` overrides) are only partially honoured: an
// explicit per-operation uriTemplate IS used, but a resource-level routePrefix
// or shortName override is not chased. GraphQL operations that #[ApiResource]
// can also generate are not emitted here (REST only). Filters are recorded by
// presence, not resolved to their target properties' types.
//
// Registration key: "custom_php_api_platform".
// Issue #3556 (epic #3505) — PHP API Platform REST-resource coverage.
package php

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_php_api_platform", &apiPlatformExtractor{})
}

type apiPlatformExtractor struct{}

func (e *apiPlatformExtractor) Language() string { return "custom_php_api_platform" }

var (
	// `#[ApiResource(...)]` — the resource attribute. Group 1 is the full
	// argument body (may be empty for a bare `#[ApiResource]`).
	reAPResource = regexp.MustCompile(`#\[ApiResource\b\s*(?:\((?P<body>(?:[^()]|\([^()]*\))*)\))?\s*\]`)
	// `class Foo` — the class the attribute decorates. Group 1 is the class name.
	reAPClass = regexp.MustCompile(`(?m)^\s*(?:final\s+|abstract\s+)*class\s+([A-Za-z_]\w*)`)
	// A standalone per-class operation attribute: #[Get], #[GetCollection],
	// #[Post(...)], #[Put], #[Patch], #[Delete]. Group 1 is the operation name,
	// group 2 the optional argument body.
	reAPOpAttr = regexp.MustCompile(`#\[(Get|GetCollection|Post|Put|Patch|Delete)\b\s*(?:\((?P<body>(?:[^()]|\([^()]*\))*)\))?\s*\]`)
	// `new Get()` / `new GetCollection(...)` — operation constructors inside an
	// `operations: [ ... ]` list. Group 1 is the operation name, group 2 body.
	reAPOpNew = regexp.MustCompile(`new\s+(Get|GetCollection|Post|Put|Patch|Delete)\s*\((?P<body>(?:[^()]|\([^()]*\))*)\)`)
	// `uriTemplate: '/path'` inside an operation body. Group 1 is the path.
	reAPUriTemplate = regexp.MustCompile(`uriTemplate\s*:\s*['"]([^'"]+)['"]`)
	// `#[ApiFilter(SearchFilter::class, ...)]` — a query filter. Group 1 is the
	// filter class short name.
	reAPFilter = regexp.MustCompile(`#\[ApiFilter\s*\(\s*([A-Za-z_]\w*)`)
)

// apOperation describes one generated REST operation: its HTTP method and
// whether it addresses the collection (/books) or an item (/books/{id}).
type apOperation struct {
	name       string // API Platform operation class name
	method     string // HTTP verb
	collection bool   // true → collection path, false → item path (/{id})
}

// apDefaultOperations is the CRUD set a bare `#[ApiResource]` generates.
var apDefaultOperations = []apOperation{
	{"GetCollection", "GET", true},
	{"Post", "POST", true},
	{"Get", "GET", false},
	{"Put", "PUT", false},
	{"Patch", "PATCH", false},
	{"Delete", "DELETE", false},
}

// apOperationMeta maps an operation class name to its HTTP method and
// collection-ness. #[Get] is an item op; #[GetCollection] a collection op;
// #[Post] is the collection create.
func apOperationMeta(name string) (apOperation, bool) {
	switch name {
	case "GetCollection":
		return apOperation{name, "GET", true}, true
	case "Post":
		return apOperation{name, "POST", true}, true
	case "Get":
		return apOperation{name, "GET", false}, true
	case "Put":
		return apOperation{name, "PUT", false}, true
	case "Patch":
		return apOperation{name, "PATCH", false}, true
	case "Delete":
		return apOperation{name, "DELETE", false}, true
	}
	return apOperation{}, false
}

// apResourcePath derives the default REST collection path for a resource class
// short-name: lowercased + pluralised (Book→/books, Category→/categories,
// Status→/statuses). A lightweight pluraliser — see the HONEST LIMIT note.
func apResourcePath(className string) string {
	s := strings.ToLower(className)
	switch {
	case strings.HasSuffix(s, "y") && !endsInVowelBeforeY(s):
		s = s[:len(s)-1] + "ies"
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"),
		strings.HasSuffix(s, "z"), strings.HasSuffix(s, "ch"),
		strings.HasSuffix(s, "sh"):
		s += "es"
	default:
		s += "s"
	}
	return "/" + s
}

// endsInVowelBeforeY reports whether the char before a trailing 'y' is a vowel
// (so "day"→"days" not "daies"). Caller guarantees s ends in 'y'.
func endsInVowelBeforeY(s string) bool {
	if len(s) < 2 {
		return false
	}
	switch s[len(s)-2] {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	}
	return false
}

func (e *apiPlatformExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.api_platform_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "api-platform"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "php" {
		return nil, nil
	}

	src := string(file.Content)

	// File-signal gate: require the #[ApiResource] attribute.
	if !strings.Contains(src, "ApiResource") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	for _, rm := range reAPResource.FindAllStringSubmatchIndex(src, -1) {
		resBody := ""
		if rm[2] >= 0 {
			resBody = src[rm[2]:rm[3]]
		}

		// Find the class this #[ApiResource] decorates: the first `class Foo`
		// at or after the attribute.
		clsM := reAPClass.FindStringSubmatchIndex(src[rm[1]:])
		if clsM == nil {
			continue
		}
		className := src[rm[1]+clsM[2] : rm[1]+clsM[3]]
		classOff := rm[1] + clsM[0]
		basePath := apResourcePath(className)
		line := lineOf(src, rm[0])

		// Collect explicitly-declared operations: from `operations: [...]` in
		// the resource body AND from standalone #[Get]/#[Post]/… attributes that
		// sit between the #[ApiResource] attribute and the class declaration.
		var explicit []struct {
			op  apOperation
			uri string
		}
		addOp := func(name, body string) {
			meta, ok := apOperationMeta(name)
			if !ok {
				return
			}
			uri := ""
			if um := reAPUriTemplate.FindStringSubmatch(body); um != nil {
				uri = um[1]
			}
			explicit = append(explicit, struct {
				op  apOperation
				uri string
			}{meta, uri})
		}
		// operations: [ new Get(), new GetCollection() ] inside the resource body.
		if strings.Contains(resBody, "operations") {
			for _, om := range reAPOpNew.FindAllStringSubmatch(resBody, -1) {
				addOp(om[1], om[2])
			}
		}
		// Standalone operation attributes between #[ApiResource] and the class.
		between := src[rm[1]:classOff]
		for _, om := range reAPOpAttr.FindAllStringSubmatch(between, -1) {
			addOp(om[1], om[2])
		}

		ops := explicit
		emit := func(opName, method, path string, collection bool) {
			name := method + " " + path
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, line)
			setProps(&ent, "framework", "api-platform",
				"provenance", "INFERRED_FROM_API_PLATFORM_RESOURCE",
				"http_method", method, "verb", method,
				"route_path", path, "route_style", "attribute",
				"resource_class", className,
				"api_platform_operation", opName)
			if collection {
				setProps(&ent, "api_platform_target", "collection")
			} else {
				setProps(&ent, "api_platform_target", "item")
			}
			add(ent)
		}

		if len(ops) == 0 {
			// Bare #[ApiResource] → default CRUD set.
			for _, op := range apDefaultOperations {
				path := basePath
				if !op.collection {
					path = basePath + "/{id}"
				}
				emit(op.name, op.method, path, op.collection)
			}
		} else {
			for _, x := range ops {
				path := x.uri
				if path == "" {
					path = basePath
					if !x.op.collection {
						path = basePath + "/{id}"
					}
				}
				emit(x.op.name, x.op.method, path, x.op.collection)
			}
		}

		// #[ApiFilter] declarations → a filter-set entity on the resource.
		var filters []string
		fseen := make(map[string]bool)
		// Scan from the #[ApiResource] attribute up to (and a little past) the
		// class declaration for #[ApiFilter] attributes. Bound the end to the
		// class body's opening brace if present, else the file end.
		fEnd := len(src)
		if br := strings.IndexByte(src[classOff:], '{'); br >= 0 {
			fEnd = classOff + br
		}
		for _, fm := range reAPFilter.FindAllStringSubmatch(src[rm[0]:fEnd], -1) {
			if !fseen[fm[1]] {
				fseen[fm[1]] = true
				filters = append(filters, fm[1])
			}
		}
		if len(filters) > 0 {
			ent := makeEntity("api_platform_filters:"+className, "SCOPE.Schema", "filter_set", file.Path, file.Language, line)
			setProps(&ent, "framework", "api-platform",
				"provenance", "INFERRED_FROM_API_PLATFORM_FILTER",
				"resource_class", className,
				"api_platform_filters", strings.Join(filters, ","))
			add(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
