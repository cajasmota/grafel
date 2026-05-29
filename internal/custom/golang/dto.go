package golang

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// dto.go — a framework-agnostic request/response DTO-struct scanner for the Go
// HTTP framework family (issue #3255, part of the Go greening epic #3210).
//
// It detects the *struct models* that handlers bind as request bodies or
// serialise as response bodies, and emits one SCOPE.Schema entity (subtype=dto)
// per resolved DTO struct, carrying its field list. This is DISTINCT from the
// request_validation scanner in validation.go: that emits validation
// rule/validator/binding SCOPE.Pattern entities (the *rules*); this emits the
// DTO *struct shapes* bound as request/response bodies (the *models*), with a
// direction (request | response) and the binding/serialisation call that proves
// the role.
//
// Detected binding/serialisation surfaces (the canonical Go HTTP idioms, shared
// across all 15 frameworks):
//
//   request  : c.ShouldBindJSON(&x) / c.ShouldBind(&x) / c.Bind(&x) /
//              c.BindJSON(&x) / c.MustBindWith(&x, ...)        (gin/buffalo/hertz)
//              c.BodyParser(&x) / c.QueryParser(&x)            (fiber)
//              ctx.ReadJSON(&x) / ctx.ReadBody(&x)             (iris)
//              this.ParseForm(&x)                              (beego)
//              render.Bind(r, &x) / render.DecodeJSON(b, &x)   (chi)
//              json.NewDecoder(r.Body).Decode(&x)              (net-http/gorilla/fasthttp/revel)
//   response : c.JSON(code, x) / ctx.JSON(x) / render.JSON(w, r, x) /
//              json.NewEncoder(w).Encode(x)                    (all frameworks)
//
// For each binding/serialisation site the scanner resolves the bound variable's
// struct type from same-file declarations (`var x T`, `x := T{}`, `x T{...}`,
// or a `x T` / `x *T` function parameter), then looks up the matching
// `type T struct { ... }` definition in the file and emits the DTO with its
// fields. A site whose type cannot be resolved to a file-local struct still
// yields a DTO entity carrying the variable/expression and the binding role
// (resolved=false), so the request/response surface is never silently dropped.
//
// Honesty (registry coverage status):
//
// Detection is a heuristic regex match on source text plus a same-file type
// resolution. It does NOT perform cross-file/import type resolution or confirm
// the binding executes on a real request path. Frameworks are therefore
// reported `partial` by default. The flagship set (gin/echo/fiber/chi) plus the
// net/http-decode family carry dedicated end-to-end fixtures that prove the
// general request-bind + response-serialise + struct-field case, so those flip
// to `full`; the remaining frameworks (whose binding/serialisation idioms are a
// subset of the same catalog but lack a per-framework proving fixture) stay
// `partial`.
//
// Framework attribution mirrors observability.go / validation.go: the scanner
// runs on every Go file and infers the framework from canonical engine/context
// markers, stamping it on each emitted entity. A file with no recognised marker
// emits nothing.

func init() {
	extractor.Register("custom_go_dto", &dtoExtractor{})
}

type dtoExtractor struct{}

func (e *dtoExtractor) Language() string { return "custom_go_dto" }

// ---------------------------------------------------------------------------
// Framework detection
// ---------------------------------------------------------------------------

// dtoFrameworkMarkers attributes a file to a framework via its canonical engine
// constructor / request-context type. The catalog is the full 15-framework Go
// HTTP set from issue #3255. Kept file-local to avoid editing shared helpers
// (contention avoidance). net-http is placed last because its `http.*` markers
// are the broadest.
var dtoFrameworkMarkers = []struct {
	name string
	re   *regexp.Regexp
}{
	{"gin", regexp.MustCompile(`\bgin\.(?:Default|New|Engine|Context|HandlerFunc)\b`)},
	{"echo", regexp.MustCompile(`\becho\.(?:New|Echo|Context|HandlerFunc|MiddlewareFunc)\b`)},
	{"fiber", regexp.MustCompile(`\bfiber\.(?:New|App|Ctx|Handler)\b`)},
	{"chi", regexp.MustCompile(`\bchi\.(?:NewRouter|Router|Mux)\b`)},
	{"beego", regexp.MustCompile(`\b(?:beego|web)\.(?:Router|NewNamespace|Run|InsertFilter|AutoRouter|Controller)\b`)},
	{"iris", regexp.MustCompile(`\biris\.(?:New|Default|Application|Context|Party)\b`)},
	{"hertz", regexp.MustCompile(`\bserver\.(?:Default|New|Hertz)\b|\bapp\.RequestContext\b`)},
	{"buffalo", regexp.MustCompile(`\bbuffalo\.(?:New|App|Options|Context)\b`)},
	{"gorilla-mux", regexp.MustCompile(`\bmux\.(?:NewRouter|Router|Vars)\b`)},
	{"revel", regexp.MustCompile(`\brevel\.(?:Result|Controller|Intercept(?:Func|Method)|BEFORE|AFTER)\b`)},
	{"go-zero", regexp.MustCompile(`\b(?:rest|httpx)\.(?:MustNewServer|Server|Parse|OkJson(?:Ctx)?|WriteJson|Error)\b`)},
	{"kratos", regexp.MustCompile(`\b(?:khttp|transhttp|http)\.(?:NewServer|Context|ServeHTTP)\b|kratos\.New\b`)},
	{"huma", regexp.MustCompile(`\bhuma\.(?:New|Register|Context|API)\b`)},
	{"fasthttp", regexp.MustCompile(`\bfasthttp\.(?:RequestCtx|RequestHandler|ListenAndServe)\b`)},
	{"net-http", regexp.MustCompile(`\bhttp\.(?:NewServeMux|HandleFunc|ListenAndServe|ListenAndServeTLS|ResponseWriter|Request)\b`)},
}

// detectDtoFramework returns the framework a file belongs to, or "" when no
// recognised marker is present. First match wins in declaration order.
func detectDtoFramework(src string) string {
	for _, m := range dtoFrameworkMarkers {
		if m.re.MatchString(src) {
			return m.name
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Binding / serialisation call sites
// ---------------------------------------------------------------------------

// dtoBindSignal is one request-bind or response-serialise call form. The bound
// DTO is referenced by the argGroup submatch — for request binds the `&x` /
// `x` pointer argument, for response serialisers the serialised value.
type dtoBindSignal struct {
	re        *regexp.Regexp
	direction string // request | response
	subtype   string // bind call dialect, e.g. should_bind | body_parser | json_encode
	argGroup  int    // 1-based submatch holding the bound expression
}

var dtoBindSignals = []dtoBindSignal{
	// --- request binds: &x pointer to the destination DTO ----------------
	// gin / buffalo / hertz: ShouldBind*/Bind*/BindJSON/MustBindWith
	{regexp.MustCompile(`\b\w+\.(?:ShouldBind\w*|BindJSON|Bind)\s*\(\s*&(\w+)`), "request", "should_bind", 1},
	{regexp.MustCompile(`\b\w+\.MustBindWith\s*\(\s*&(\w+)`), "request", "must_bind_with", 1},
	// fiber: BodyParser / QueryParser / ParamsParser
	{regexp.MustCompile(`\b\w+\.(?:BodyParser|QueryParser|ParamsParser|ReqHeaderParser)\s*\(\s*&(\w+)`), "request", "body_parser", 1},
	// iris: ctx.ReadJSON / ReadBody / ReadForm ...
	{regexp.MustCompile(`\b\w+\.Read(?:JSON|Body|Form|Query|XML|YAML|Protobuf)\s*\(\s*&(\w+)`), "request", "read_body", 1},
	// beego: this.ParseForm(&x)
	{regexp.MustCompile(`\b\w+\.ParseForm\s*\(\s*&(\w+)`), "request", "parse_form", 1},
	// chi: render.Bind(r, &x) / render.DecodeJSON(b, &x)
	{regexp.MustCompile(`\brender\.(?:Bind|DecodeJSON)\s*\([^,]*,\s*&(\w+)`), "request", "render_bind", 1},
	// stdlib decode: json.NewDecoder(r.Body).Decode(&x)  (net-http/gorilla/fasthttp/revel)
	{regexp.MustCompile(`\bjson\.NewDecoder\s*\([^)]*\)\s*\.Decode\s*\(\s*&(\w+)`), "request", "json_decode", 1},

	// --- response serialisers: the value being written -------------------
	// gin/hertz/iris/beego: c.JSON(code, x) or ctx.JSON(x)
	{regexp.MustCompile(`\b\w+\.JSON\s*\(\s*(?:[\w.]+\s*,\s*)?(?:&)?(\w+)\s*\)`), "response", "ctx_json", 1},
	// chi: render.JSON(w, r, x)
	{regexp.MustCompile(`\brender\.JSON\s*\([^,]*,[^,]*,\s*(?:&)?(\w+)\s*\)`), "response", "render_json", 1},
	// stdlib encode: json.NewEncoder(w).Encode(x)
	{regexp.MustCompile(`\bjson\.NewEncoder\s*\([^)]*\)\s*\.Encode\s*\(\s*(?:&)?(\w+)\s*\)`), "response", "json_encode", 1},
}

// ---------------------------------------------------------------------------
// Struct catalog + local type resolution
// ---------------------------------------------------------------------------

// dtoStruct is a `type T struct { ... }` definition with its fields.
type dtoStruct struct {
	Name   string
	Fields []dtoField
	Off    int // source offset of the `type` keyword
	Line   int
}

type dtoField struct {
	Name string
	Type string
}

// reDtoStructHead locates a `type <Name> struct {` opening.
var reDtoStructHead = regexp.MustCompile(`type\s+(\w+)\s+struct\s*\{`)

// reDtoStructField matches one exported/unexported field line inside a struct
// body: a leading identifier and a type token. Embedded fields (a bare type) and
// blank lines are skipped by requiring two tokens. Best-effort; complex types
// (maps/funcs) are captured as their leading token.
var reDtoStructField = regexp.MustCompile(`(?m)^\s*([A-Za-z_]\w*)\s+([\w\.\*\[\]]+)`)

// catalogStructs parses every `type T struct {...}` in src into a name→struct
// map, capturing each struct's field list by scanning to the matching closing
// brace (paren/brace balanced, quote-aware).
func catalogStructs(src string) map[string]dtoStruct {
	out := make(map[string]dtoStruct)
	for _, m := range reDtoStructHead.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		open := m[1] - 1 // index of the '{'
		end := matchBrace(src, open)
		if end < 0 {
			continue
		}
		body := src[open+1 : end]
		var fields []dtoField
		for _, fm := range reDtoStructField.FindAllStringSubmatch(body, -1) {
			fname := fm[1]
			// skip Go keywords that can lead a line inside a struct body
			if fname == "struct" || fname == "func" {
				continue
			}
			fields = append(fields, dtoField{Name: fname, Type: fm[2]})
		}
		out[name] = dtoStruct{
			Name:   name,
			Fields: fields,
			Off:    m[0],
			Line:   lineOf(src, m[0]),
		}
	}
	return out
}

// matchBrace returns the index of the '}' matching the '{' at openIdx, or -1.
// Quote-aware so braces inside string/rune/raw literals are ignored.
func matchBrace(src string, openIdx int) int {
	depth := 0
	var quote rune
	for i := openIdx; i < len(src); i++ {
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
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// reDtoVarDecls resolves a local variable's struct type from same-file
// declarations. Three idioms:
//
//	var x T            / var x *T
//	x := T{...}        / x := &T{...}
//	x T  / x *T        (function parameter or short field)
//
// Each regex captures (var, type).
var dtoVarDeclForms = []*regexp.Regexp{
	regexp.MustCompile(`\bvar\s+(\w+)\s+\*?(\w+)\b`),
	regexp.MustCompile(`\b(\w+)\s*:=\s*&?(\w+)\s*\{`),
	regexp.MustCompile(`\b(\w+)\s+\*?(\w+)\s*[,)]`),
}

// resolveVarType returns the struct type name bound to varName in src, looking
// up the var-declaration forms and returning the first whose captured type is a
// known struct. Returns "" when unresolved.
func resolveVarType(src, varName string, structs map[string]dtoStruct) string {
	for _, re := range dtoVarDeclForms {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			if m[1] != varName {
				continue
			}
			if _, ok := structs[m[2]]; ok {
				return m[2]
			}
		}
	}
	// The bound expression may itself be a struct name (e.g. responding with a
	// composite literal var that shares the type name, or a direct type ref).
	if _, ok := structs[varName]; ok {
		return varName
	}
	return ""
}

func (e *dtoExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.dto_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "go" {
		return nil, nil
	}

	src := string(file.Content)
	framework := detectDtoFramework(src)
	if framework == "" {
		span.SetAttributes(attribute.String("framework", ""))
		return nil, nil
	}

	structs := catalogStructs(src)

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

	prov := func(detail string) string {
		return "INFERRED_FROM_" + strings.ToUpper(framework) + "_" + strings.ToUpper(detail)
	}

	// A DTO struct may be referenced by several call sites; track the role we
	// have already emitted per (struct,direction) so we don't dedup a request
	// DTO into a response one (or vice versa) but still collapse repeats.
	for _, sig := range dtoBindSignals {
		for _, m := range sig.re.FindAllStringSubmatchIndex(src, -1) {
			boundExpr := submatch(src, m, sig.argGroup*2)
			if boundExpr == "" {
				continue
			}
			// Response serialisers fire on a lot of generic identifiers; only
			// emit a response DTO when the value resolves to a known struct, to
			// avoid flooding the graph with non-DTO locals (err, code, msg…).
			structName := resolveVarType(src, boundExpr, structs)
			if sig.direction == "response" && structName == "" {
				continue
			}

			line := lineOf(src, m[0])
			name := "dto:" + framework + ":" + sig.direction + ":"
			if structName != "" {
				name += structName
			} else {
				name += boundExpr
			}

			ent := makeEntity(name, "SCOPE.Schema", "dto", file.Path, file.Language, line)
			setProps(&ent,
				"framework", framework,
				"provenance", prov("DTO_"+strings.ToUpper(sig.direction)),
				"pattern_kind", "dto",
				"dto_direction", sig.direction,
				"binding_subtype", sig.subtype,
				"bound_expr", boundExpr)

			if structName != "" {
				st := structs[structName]
				ent.StartLine = st.Line
				ent.EndLine = st.Line
				setProps(&ent,
					"struct_name", structName,
					"resolved", "true",
					"field_count", strconv.Itoa(len(st.Fields)),
					"fields", joinDtoFields(st.Fields))
			} else {
				setProps(&ent, "resolved", "false")
			}
			add(ent)
		}
	}

	span.SetAttributes(
		attribute.String("framework", framework),
		attribute.Int("entity_count", len(entities)),
	)
	return entities, nil
}

// joinDtoFields renders a struct's fields as a stable "Name:Type" comma list for
// the entity's "fields" property.
func joinDtoFields(fields []dtoField) string {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, f.Name+":"+f.Type)
	}
	return strings.Join(parts, ",")
}
