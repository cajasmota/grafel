package golang

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
	extractor.Register("custom_go_huma", &humaExtractor{})
}

// humaExtractor extracts routing structure from Huma
// (github.com/danielgtaylor/huma) servers. Huma is OpenAPI-first: routes are
// declared by registering an Operation against an API value —
//
//	huma.Register(api, huma.Operation{Method: "GET", Path: "/users/{id}"}, handler)
//
// The Method + Path fields of the Operation literal yield an endpoint, and the
// final argument of huma.Register is the handler (handler attribution). Both
// v1 (danielgtaylor/huma) and v2 (danielgtaylor/huma/v2) share the same
// huma.Register entry point and Operation struct shape.
type humaExtractor struct{}

func (e *humaExtractor) Language() string { return "custom_go_huma" }

var (
	// huma.Register( — start token; the balanced argument span is scanned
	// forward so the Operation struct literal (with its own braces/commas)
	// is captured whole.
	reHumaRegisterHead = regexp.MustCompile(`huma\s*\.\s*Register\s*\(`)
	// Method field of an Operation literal: Method: http.MethodGet | "POST".
	reHumaMethodField = regexp.MustCompile(
		`Method\s*:\s*(?:http\.Method(\w+)|"([A-Za-z]+)")`,
	)
	// Path field of an Operation literal: Path: "/users/{id}".
	reHumaPathField = regexp.MustCompile(`Path\s*:\s*"([^"]+)"`)
)

// humaVerb resolves the HTTP verb from a Method-field match, normalising both
// the http.Method<Verb> constant form and the bare string-literal form.
func humaVerb(src string, m []int) string {
	if v := submatch(src, m, 2); v != "" { // http.MethodGet -> GET
		return strings.ToUpper(v)
	}
	if v := submatch(src, m, 4); v != "" { // "POST"
		return strings.ToUpper(v)
	}
	return ""
}

func (e *humaExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.huma_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "huma"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "go" {
		return nil, nil
	}

	src := string(file.Content)
	if !strings.Contains(src, "danielgtaylor/huma") && !strings.Contains(src, "huma.Register") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	for _, loc := range reHumaRegisterHead.FindAllStringIndex(src, -1) {
		open := loc[1] - 1 // index of the '(' that reHumaRegisterHead ends at
		args, end := balancedArgs(src, open)
		if end < 0 {
			continue // unbalanced; skip
		}
		parts := splitTopLevelArgs(args)
		if len(parts) < 3 {
			continue // need (api, Operation{...}, handler)
		}
		opLit := parts[1]
		handler := strings.TrimSpace(parts[len(parts)-1])

		verb := humaVerb(opLit, reHumaMethodField.FindStringSubmatchIndex(opLit))
		pathM := reHumaPathField.FindStringSubmatch(opLit)
		if verb == "" || pathM == nil {
			continue // incomplete Operation — would fail at huma runtime too
		}
		path := pathM[1]
		line := lineOf(src, loc[0])

		name := verb + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, line)
		setProps(&ent, "framework", "huma", "provenance", "INFERRED_FROM_HUMA_OPERATION",
			"http_method", verb, "route_path", path)
		if handler != "" {
			ent.Properties["handler"] = handler
		}
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// balancedArgs returns the argument text between the paren at index open and
// its matching close paren, plus the index of that close paren. Quoted strings
// are skipped so parens inside string literals do not affect the depth count.
// Returns ("", -1) when the parens are unbalanced.
func balancedArgs(src string, open int) (string, int) {
	depth := 0
	var quote rune
	for i := open; i < len(src); i++ {
		r := rune(src[i])
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '"', '\'', '`':
			quote = r
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return strings.TrimSpace(src[open+1 : i]), i
			}
		}
	}
	return "", -1
}
