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
	extractor.Register("custom_go_go_zero", &goZeroExtractor{})
}

// goZeroExtractor extracts routing structure from go-zero
// (github.com/zeromicro/go-zero) REST services. go-zero is codegen-driven: the
// `goctl` tool generates `internal/handler/routes.go` from an `.api`
// descriptor. The generated file registers routes by passing
// `[]rest.Route{...}` slices to `server.AddRoutes(...)` —
//
//	server.AddRoutes(
//		[]rest.Route{
//			{
//				Method:  http.MethodGet,
//				Path:    "/users/:id",
//				Handler: user.GetUserHandler(serverCtx),
//			},
//		},
//		rest.WithPrefix("/api/v1"),
//	)
//
// Each `rest.Route{Method, Path, Handler}` struct literal yields an endpoint
// (Method + Path) with the Handler expression attributed as the handler. A
// `rest.WithPrefix("/p")` option on the same AddRoutes call prefixes every
// route in that group.
//
// Honesty note: this targets the *generated* `routes.go` output (the committed
// goctl artifact), which is a stable statically-analysable struct-literal
// shape — the proving fixture exercises exactly this. When only the `.api`
// descriptor is present and `routes.go` has not been generated, there are no
// `rest.Route` registration sites to detect; that is an inherent limit of the
// descriptor-only layout, not a heuristic gap.
type goZeroExtractor struct{}

func (e *goZeroExtractor) Language() string { return "custom_go_go_zero" }

var (
	// server.AddRoutes( — start token. The balanced argument span is scanned
	// forward so each []rest.Route{...} slice (with nested braces) and any
	// trailing rest.WithPrefix(...) option are captured whole.
	reGoZeroAddRoutesHead = regexp.MustCompile(`(\w+)\.AddRoutes\s*\(`)
	// Method field of a rest.Route literal: Method: http.MethodGet | "GET".
	reGoZeroMethodField = regexp.MustCompile(
		`Method\s*:\s*(?:http\.Method(\w+)|"([A-Za-z]+)")`,
	)
	// Path field of a rest.Route literal: Path: "/users/:id".
	reGoZeroPathField = regexp.MustCompile(`Path\s*:\s*"([^"]+)"`)
	// Handler field of a rest.Route literal: Handler: user.GetUserHandler(ctx).
	// Captures the leading identifier/selector before the call parens.
	reGoZeroHandlerField = regexp.MustCompile(
		`Handler\s*:\s*([A-Za-z_][\w.]*)`,
	)
	// rest.WithPrefix("/api/v1") — group prefix option on an AddRoutes call.
	reGoZeroWithPrefix = regexp.MustCompile(`rest\.WithPrefix\s*\(\s*"([^"]+)"`)
)

// goZeroVerb resolves the HTTP verb from a Method-field match, normalising both
// the http.Method<Verb> constant form and the bare string-literal form.
func goZeroVerb(src string, m []int) string {
	if v := submatch(src, m, 2); v != "" { // http.MethodGet -> GET
		return strings.ToUpper(v)
	}
	if v := submatch(src, m, 4); v != "" { // "GET"
		return strings.ToUpper(v)
	}
	return ""
}

// leafBraceBlocks returns the text inside every innermost (leaf) `{...}` block
// in s — i.e. brace pairs that contain no nested brace pair. For a go-zero
// `[]rest.Route{ {Method:…, Path:…}, {…} }` argument this yields one entry per
// individual route struct literal, ignoring the enclosing slice braces. Quoted
// strings are skipped so braces inside string literals do not affect nesting.
func leafBraceBlocks(s string) []string {
	var blocks []string
	var stack []int // start indices (after '{') of currently-open blocks
	hasChild := map[int]bool{}
	var quote rune
	for i := 0; i < len(s); i++ {
		r := rune(s[i])
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '"', '\'', '`':
			quote = r
		case '{':
			if n := len(stack); n > 0 {
				hasChild[stack[n-1]] = true
			}
			stack = append(stack, i+1)
		case '}':
			if n := len(stack); n > 0 {
				start := stack[n-1]
				stack = stack[:n-1]
				if !hasChild[start] {
					blocks = append(blocks, s[start:i])
				}
				delete(hasChild, start)
			}
		}
	}
	return blocks
}

func (e *goZeroExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.go_zero_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "go_zero"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "go" {
		return nil, nil
	}

	src := string(file.Content)
	// Gate on the generated routes-registration signature.
	if !strings.Contains(src, "rest.Route") && !strings.Contains(src, "AddRoutes") {
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

	for _, loc := range reGoZeroAddRoutesHead.FindAllStringSubmatchIndex(src, -1) {
		serverVar := submatch(src, loc, 2)
		open := loc[1] - 1 // index of the '(' that the head ends at
		args, end := balancedArgs(src, open)
		if end < 0 {
			continue // unbalanced; skip
		}
		callLine := lineOf(src, loc[0])

		// Group-level server -> SCOPE.Service.
		svc := makeEntity(serverVar, "SCOPE.Service", "", file.Path, file.Language, callLine)
		setProps(&svc, "framework", "go_zero", "provenance", "INFERRED_FROM_GOZERO_SERVER",
			"server_var", serverVar)
		add(svc)

		// Optional rest.WithPrefix(...) applies to every route in this call.
		prefix := ""
		if pm := reGoZeroWithPrefix.FindStringSubmatch(args); pm != nil {
			prefix = pm[1]
			pent := makeEntity(prefix, "SCOPE.Component", "", file.Path, file.Language, callLine)
			setProps(&pent, "framework", "go_zero", "provenance", "INFERRED_FROM_GOZERO_PREFIX",
				"group_path", prefix)
			add(pent)
		}

		// Each rest.Route{...} literal in the slice is one endpoint. The route
		// literals are nested inside the []rest.Route{ ... } slice braces, so
		// scan the argument text for the innermost (leaf) brace blocks — those
		// are the individual struct literals — and parse each whose fields
		// include Method + Path.
		for _, lit := range leafBraceBlocks(args) {
			mM := reGoZeroMethodField.FindStringSubmatchIndex(lit)
			verb := goZeroVerb(lit, mM)
			pathM := reGoZeroPathField.FindStringSubmatch(lit)
			if verb == "" || pathM == nil {
				continue // not a complete route literal
			}
			path := pathM[1]
			if prefix != "" {
				path = prefix + path
			}
			name := verb + " " + path
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, callLine)
			setProps(&ent, "framework", "go_zero", "provenance", "INFERRED_FROM_GOZERO_ROUTE",
				"http_method", verb, "route_path", path)
			if hM := reGoZeroHandlerField.FindStringSubmatch(lit); hM != nil {
				ent.Properties["handler"] = hM[1]
			}
			add(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
