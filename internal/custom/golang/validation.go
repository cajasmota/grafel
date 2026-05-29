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

// validation.go — a framework-agnostic request-validation scanner for the four
// well-templated Go HTTP frameworks (gin/echo/fiber/chi), issue #3213 cluster 2
// (the validation capability; middleware + auth already landed in helpers.go).
//
// It detects three validation surfaces that the per-framework routing and
// middleware extractors do not model:
//
//   - rule        : struct-field validation rules declared via `validate:"..."`
//                   or `binding:"..."` field tags on bound request DTOs. Each
//                   distinct (struct, field, rule-set) yields one entity.
//   - validator   : custom validator registration —
//                   `validator.New()` instances and
//                   `<v>.RegisterValidation("name", ...)` calls.
//   - binding      : request-binding call sites that trigger validation —
//                   gin   c.ShouldBind*/c.Bind* / c.MustBindWith,
//                   echo  c.Bind / c.Validate,
//                   fiber c.BodyParser / c.QueryParser / c.ParamsParser,
//                   chi   render.Bind / render.DecodeJSON.
//
// Honesty (registry coverage status):
//
// Detection is a heuristic regex/identifier match on source text. It does NOT
// resolve imports, confirm the validator is wired into a request handler, or
// link a `validate:` tag to the binding call site that enforces it. It is
// therefore reported `partial` for gin/echo/fiber/chi: a tag/call match proves
// the validation surface is present, not that every rule is enforced. There is
// no genuinely-N/A framework in this set (all four support struct-tag
// validation), so no honesty-NA cell is emitted here.
//
// Framework attribution mirrors observability.go: the scanner runs on every Go
// file (registry key custom_go_validation matches the custom_go_ dispatch
// prefix) and infers gin/echo/fiber/chi from framework-specific engine/context
// markers, stamping that framework on each emitted entity. A file with no
// recognised framework marker emits nothing.

func init() {
	extractor.Register("custom_go_validation", &validationExtractor{})
}

type validationExtractor struct{}

func (e *validationExtractor) Language() string { return "custom_go_validation" }

// valFrameworkMarkers attributes a file to a framework via its canonical engine
// constructor / request-context type. Same surface observability.go uses, kept
// file-local to avoid editing shared helpers (contention avoidance).
//
// The first four (gin/echo/fiber/chi) are the original well-templated set; the
// remaining six (beego/iris/hertz/buffalo/gorilla-mux/net-http) extend struct-
// tag / binding-call validation detection to the rest of the Go HTTP framework
// family (issue #3213). All ten support go-playground/validator `validate:`
// struct tags and a request-binding call surface.
//
// fasthttp and revel are intentionally absent: neither offers struct-tag
// request binding (fasthttp's RequestCtx exposes raw byte accessors only;
// Revel binds params positionally via controller-method signatures, not tags),
// so there is no validation surface to attribute and their request_validation
// cell is honestly marked not_applicable rather than fabricated.
//
// net-http is placed last because its `http.*` markers are the broadest.
var valFrameworkMarkers = []struct {
	name string
	re   *regexp.Regexp
}{
	{"gin", regexp.MustCompile(`\bgin\.(?:Default|New|Engine|Context|HandlerFunc)\b`)},
	{"echo", regexp.MustCompile(`\becho\.(?:New|Echo|Context|HandlerFunc|MiddlewareFunc)\b`)},
	{"fiber", regexp.MustCompile(`\bfiber\.(?:New|App|Ctx|Handler)\b`)},
	{"chi", regexp.MustCompile(`\bchi\.(?:NewRouter|Router|Mux)\b`)},
	{"beego", regexp.MustCompile(`\b(?:beego|web)\.(?:Router|NewNamespace|Run|InsertFilter|AutoRouter)\b`)},
	{"iris", regexp.MustCompile(`\biris\.(?:New|Default|Application|Context|Party)\b`)},
	{"hertz", regexp.MustCompile(`\bserver\.(?:Default|New|Hertz)\b|\bapp\.RequestContext\b`)},
	{"buffalo", regexp.MustCompile(`\bbuffalo\.(?:New|App|Options|Context)\b`)},
	{"gorilla-mux", regexp.MustCompile(`\bmux\.(?:NewRouter|Router|Vars)\b`)},
	{"net-http", regexp.MustCompile(`\bhttp\.(?:NewServeMux|HandleFunc|ListenAndServe|ListenAndServeTLS)\b`)},
}

// detectValFramework returns the framework a file belongs to, or "" when no
// recognised marker is present. First match wins (gin→echo→fiber→chi→beego→
// iris→hertz→buffalo→gorilla-mux→net-http).
func detectValFramework(src string) string {
	for _, m := range valFrameworkMarkers {
		if m.re.MatchString(src) {
			return m.name
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Struct-tag validation rules
// ---------------------------------------------------------------------------

// reGoStructHead locates a `type <Name> struct {` opening so field tags can be
// attributed to the enclosing struct.
var reGoStructHead = regexp.MustCompile(`type\s+(\w+)\s+struct\s*\{`)

// reGoValidatedField matches a struct field line carrying a `validate:"..."` or
// `binding:"..."` tag, capturing the field name and the rule string. The field
// name is the first identifier on the line; binding/validate tags live in the
// backtick-quoted tag literal.
//
//	Name string `json:"name" binding:"required,min=2"`
//	Age  int    `validate:"gte=0,lte=130"`
var reGoValidatedField = regexp.MustCompile(
	"(?m)^\\s*(\\w+)\\s+[^`\\n]*`[^`]*\\b(?:binding|validate):\"([^\"]+)\"[^`]*`")

// reGoTagKind extracts which tag key (binding|validate) carried the rules, used
// for the rule_source property. binding is gin's spelling; validate is the
// go-playground/validator spelling used by echo/fiber/chi.
var reGoTagKind = regexp.MustCompile(`\b(binding|validate):"`)

// validationRule is one (struct, field) pair carrying validation rules.
type validationRule struct {
	Struct string
	Field  string
	Rules  string
	Source string // "binding" | "validate"
	Line   int
}

// findValidationRules scans struct definitions for fields carrying binding/
// validate tags. Fields are attributed to the most recently opened struct by
// source offset. "-" rule strings (explicit skip) are ignored.
func findValidationRules(src string) []validationRule {
	// Index struct heads by offset so each field can find its enclosing struct.
	type head struct {
		name string
		off  int
	}
	var heads []head
	for _, m := range reGoStructHead.FindAllStringSubmatchIndex(src, -1) {
		heads = append(heads, head{name: src[m[2]:m[3]], off: m[0]})
	}
	structAt := func(off int) string {
		name := ""
		for _, h := range heads {
			if h.off <= off {
				name = h.name
			} else {
				break
			}
		}
		return name
	}

	var rules []validationRule
	for _, m := range reGoValidatedField.FindAllStringSubmatchIndex(src, -1) {
		field := src[m[2]:m[3]]
		ruleSet := src[m[4]:m[5]]
		if ruleSet == "" || ruleSet == "-" {
			continue
		}
		source := "validate"
		if k := reGoTagKind.FindStringSubmatch(src[m[0]:m[1]]); k != nil {
			source = k[1]
		}
		rules = append(rules, validationRule{
			Struct: structAt(m[0]),
			Field:  field,
			Rules:  ruleSet,
			Source: source,
			Line:   lineOf(src, m[0]),
		})
	}
	return rules
}

// ---------------------------------------------------------------------------
// Custom validators + binding call sites
// ---------------------------------------------------------------------------

// valSignal is a non-tag validation construct detected by a single regex.
type valSignal struct {
	re      *regexp.Regexp
	kind    string // pattern sub-kind: validator | binding
	subtype string // finer detail: validator_new | register_validation | bind_call
	// nameGroup is the submatch whose text names the entity (0 = whole match).
	nameGroup int
}

var valSignals = []valSignal{
	// custom validators
	{regexp.MustCompile(`\bvalidator\.New\s*\(`), "validator", "validator_new", 0},
	{regexp.MustCompile(`\b\w+\.RegisterValidation\s*\(\s*"([^"]+)"`), "validator", "register_validation", 1},

	// binding / parse call sites that trigger validation, per framework.
	//
	// gin/echo/fiber/chi (original set):
	{regexp.MustCompile(`\bc\.(?:ShouldBind\w*|Bind\w*|MustBindWith)\s*\(`), "binding", "bind_call", 0},
	{regexp.MustCompile(`\bc\.Validate\s*\(`), "binding", "validate_call", 0},
	{regexp.MustCompile(`\bc\.(?:BodyParser|QueryParser|ParamsParser|ReqHeaderParser)\s*\(`), "binding", "parse_call", 0},
	{regexp.MustCompile(`\brender\.(?:Bind|DecodeJSON)\s*\(`), "binding", "bind_call", 0},

	// extended frameworks (issue #3213):
	//   hertz   c.BindAndValidate / c.BindJSON / c.Bind     (covered by c.Bind\w* above; explicit validate form here)
	//   iris    ctx.ReadJSON / ctx.ReadBody / ctx.ReadForm / ctx.ReadQuery
	//   beego   this.ParseForm(&dto) controller binding
	//   buffalo c.Bind(&dto)                                (covered by c.Bind\w* above)
	//   gorilla-mux / net-http  json.NewDecoder(r.Body).Decode(&dto) — the
	//           idiomatic std-lib request decode that feeds a validator.New()
	//           struct-tag check.
	{regexp.MustCompile(`\b\w+\.BindAndValidate\s*\(`), "binding", "validate_call", 0},
	{regexp.MustCompile(`\bctx\.Read(?:JSON|Body|Form|Query|XML|MsgPack|YAML|Protobuf)\s*\(`), "binding", "parse_call", 0},
	{regexp.MustCompile(`\bthis\.ParseForm\s*\(`), "binding", "parse_call", 0},
	{regexp.MustCompile(`\bjson\.NewDecoder\s*\([^)]*\)\s*\.Decode\s*\(`), "binding", "decode_call", 0},
}

func (e *validationExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.validation_extractor.extract",
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
	framework := detectValFramework(src)
	if framework == "" {
		span.SetAttributes(attribute.String("framework", ""))
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

	prov := func(detail string) string {
		return "INFERRED_FROM_" + strings.ToUpper(framework) + "_" + strings.ToUpper(detail)
	}

	// 1. struct-tag validation rules
	for _, r := range findValidationRules(src) {
		name := "validation:rule:" + r.Struct + "." + r.Field
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, r.Line)
		setProps(&ent, "framework", framework,
			"provenance", prov("VALIDATION"),
			"pattern_kind", "validation",
			"validation_kind", "rule",
			"struct_name", r.Struct,
			"field_name", r.Field,
			"rules", r.Rules,
			"rule_source", r.Source)
		add(ent)
	}

	// 2. custom validators + binding call sites
	for _, sig := range valSignals {
		for _, m := range sig.re.FindAllStringSubmatchIndex(src, -1) {
			detail := submatch(src, m, sig.nameGroup*2)
			if detail == "" {
				detail = strings.TrimSpace(src[m[0]:m[1]])
			}
			name := "validation:" + sig.kind + ":" + sig.subtype + ":" + detail
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", framework,
				"provenance", prov("VALIDATION"),
				"pattern_kind", "validation",
				"validation_kind", sig.kind,
				"validation_subtype", sig.subtype)
			if sig.subtype == "register_validation" {
				setProps(&ent, "tag", detail)
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
