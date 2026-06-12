package rust

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

// Package rust DI graph extractor — #4963 (follow-up to #4921).
//
// #4921 gave us the di_injection_point: the handler-arg side of Rust
// web-framework DI (axum State<T>/Extension<T>, actix web::Data<T>). This
// extractor closes the other two DI capabilities for axum/actix:
//
//   - di_binding_extraction: the REGISTRATION site where the injected value is
//     supplied to the framework. axum `.with_state(T)` (or `.with_state(T::new
//     (...))`) binds the app-singleton state; `.layer(Extension(v))` /
//     `AddExtensionLayer::new(v)` binds a request-scoped extension value. actix
//     `App::app_data(web::Data::new(T))` binds the app-singleton data value.
//     Each emits a SCOPE.Pattern(di_binding) carrying scope + a BINDS edge from
//     the bound type to the registration site, and an INJECTED_INTO edge from
//     the bound type to every handler that extracts it (cross-file resolved by
//     the global byName index, like the Go/Python DI graphs).
//
//   - di_scope_resolution: derived from the mechanism. axum State and actix
//     Data are app-singletons (one value shared for the process lifetime);
//     axum Extension is request-scoped (a value attached per-request by the
//     AddExtensionLayer middleware). The `scope` prop carries singleton |
//     request.
//
// The injection edge is keyed off the handler the extractor type appears in,
// recovered by scanning each `fn name(...)` signature — this gives us the
// concrete consumer symbol that the di_injection_point pattern (which is
// keyed by type, not handler) could not.
func init() {
	extractor.Register("custom_rust_di_graph", &rustDIExtractor{})
}

type rustDIExtractor struct{}

func (e *rustDIExtractor) Language() string { return "custom_rust_di_graph" }

// rustWrapperOpt optionally consumes a smart-pointer wrapper constructor —
// Arc::new(/Rc::new(/Box::new(/Mutex::new(/RwLock::new( — so the captured
// type ident is the inner state/data type rather than the wrapper.
const rustWrapperOpt = `(?:(?:Arc|Rc|Box|Mutex|RwLock)::new\s*\(\s*)?`

var (
	// `fn name(` / `async fn name(` — start of a function whose signature we
	// scan for DI extractor types. parenOpen is the '(' offset.
	reRustDIFn = regexp.MustCompile(`(?m)^\s*(?:pub\s+)?(?:async\s+)?fn\s+([A-Za-z_]\w*)\s*\(`)

	// axum handler-arg extractors inside a signature.
	reRustDIState     = regexp.MustCompile(`State\s*<\s*([A-Za-z_]\w*)`)
	reRustDIExtension = regexp.MustCompile(`Extension\s*<\s*([A-Za-z_]\w*)`)
	// actix handler-arg extractor.
	reRustDIData = regexp.MustCompile(`web::Data\s*<\s*([A-Za-z_]\w*)`)

	// axum registration: .with_state(AppState) / .with_state(AppState::new(..))
	// / .with_state(Arc::new(AppState)). The optional wrapper-constructor group
	// (Arc::new(/Rc::new(/...) is skipped so we capture the inner state type.
	reAxumWithState = regexp.MustCompile(`\.with_state\s*\(\s*` + rustWrapperOpt + `([A-Za-z_][\w:]*)`)
	// axum request-scoped extension registration:
	//   .layer(Extension(value))  /  .layer(AddExtensionLayer::new(value))
	reAxumExtensionLayer = regexp.MustCompile(`\.layer\s*\(\s*(?:Extension|AddExtensionLayer::new)\s*\(\s*` + rustWrapperOpt + `([A-Za-z_][\w:]*)`)
	// actix app-data registration: .app_data(web::Data::new(T)) /
	//   .app_data(Data::new(T)) / .app_data(some_data.clone()).
	reActixAppData = regexp.MustCompile(`\.app_data\s*\(\s*(?:web::)?Data::new\s*\(\s*` + rustWrapperOpt + `([A-Za-z_][\w:]*)|\.app_data\s*\(\s*` + rustWrapperOpt + `([A-Za-z_]\w*)`)
)

// rustDILeafType reduces a registration argument expression to the bound type
// symbol the di_injection_point keys on. The wrapper constructor is already
// stripped by rustWrapperOpt in the regex, so the captured expr is either a
// bare type, a `Type::ctor` constructor path, or a value binding. We take the
// HEAD path segment: for a `Type::method` constructor the head is the type;
// for an unqualified value binding the whole ident is returned unchanged.
//
//	AppState              -> AppState
//	AppState::new         -> AppState
//	DbPool::connect       -> DbPool
//	app_state             -> app_state  (value binding — left as-is)
func rustDILeafType(expr string) string {
	expr = strings.TrimSpace(expr)
	if i := strings.Index(expr, "::"); i > 0 {
		return expr[:i]
	}
	return expr
}

// rustHandlerExtractors returns, per handler fn, the DI extractor types found
// in its parameter list. mech is "state" | "extension" | "data".
type rustInjection struct {
	handler string
	typ     string
	mech    string
	line    int
}

func rustHandlerInjections(src string) []rustInjection {
	var out []rustInjection
	fns := reRustDIFn.FindAllStringSubmatchIndex(src, -1)
	for _, fm := range fns {
		name := src[fm[2]:fm[3]]
		parenOpen := fm[1] - 1
		sig := rustBalancedParen(src, parenOpen)
		if sig == "" {
			continue
		}
		line := lineOf(src, fm[0])
		for _, sm := range reRustDIState.FindAllStringSubmatch(sig, -1) {
			out = append(out, rustInjection{handler: name, typ: sm[1], mech: "state", line: line})
		}
		for _, sm := range reRustDIExtension.FindAllStringSubmatch(sig, -1) {
			out = append(out, rustInjection{handler: name, typ: sm[1], mech: "extension", line: line})
		}
		for _, sm := range reRustDIData.FindAllStringSubmatch(sig, -1) {
			out = append(out, rustInjection{handler: name, typ: sm[1], mech: "data", line: line})
		}
	}
	return out
}

// rustBalancedParen returns the text between the '(' at openIdx and its
// matching ')', honouring nested parens/angle brackets. Returns "" if
// unbalanced.
func rustBalancedParen(src string, openIdx int) string {
	if openIdx < 0 || openIdx >= len(src) || src[openIdx] != '(' {
		return ""
	}
	depth := 0
	for i := openIdx; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[openIdx+1 : i]
			}
		}
	}
	return ""
}

// rustDIScope derives the DI scope from the mechanism. axum State + actix Data
// are app-singletons; axum Extension is request-scoped.
func rustDIScope(mech string) string {
	switch mech {
	case "extension":
		return "request"
	default: // state, data
		return "singleton"
	}
}

func (e *rustDIExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_di_graph.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
		return nil, nil
	}
	src := string(file.Content)
	var out []types.EntityRecord
	seen := map[string]bool{}

	// Map bound type -> mechanism, for the binding sites discovered below, so
	// the INJECTED_INTO edge can carry the correct scope even when only the
	// handler side names the type.
	injections := rustHandlerInjections(src)

	// addEdge emits a thin owner SCOPE.Pattern(di_binding) carrying one DI
	// relationship. The owner Name is synthetic so it never collides with — and
	// therefore never replaces, via MergeWithCustom — a rich base entity. The
	// semantic from/to symbols live on the relationship endpoints, resolved by
	// the global byName index (Go/Python DI-graph precedent).
	addEdge := func(subtype string, line int, props map[string]string, rel types.RelationshipRecord) {
		owner := "di:" + rel.Kind + ":" + rel.FromID + "->" + rel.ToID + "@" + strconv.Itoa(line)
		if seen[owner] {
			return
		}
		seen[owner] = true
		ent := makeEntity(owner, "SCOPE.Pattern", subtype, file.Path, file.Language, line)
		for k, v := range props {
			ent.Properties[k] = v
		}
		ent.Relationships = append(ent.Relationships, rel)
		out = append(out, ent)
	}

	// --- Registration / binding sites ------------------------------------

	// axum .with_state(T) — app-singleton state binding.
	emitBinding := func(re *regexp.Regexp, framework, mech, provenance string) {
		for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
			// First non-empty capture group is the bound expression.
			var raw string
			for gi := 1; gi*2 < len(m); gi++ {
				if m[gi*2] >= 0 {
					raw = src[m[gi*2]:m[gi*2+1]]
					break
				}
			}
			if raw == "" {
				continue
			}
			typ := rustDILeafType(raw)
			if typ == "" {
				continue
			}
			line := lineOf(src, m[0])
			scope := rustDIScope(mech)
			site := framework + "_registration"
			// BINDS: bound type -> the registration site (the framework wiring
			// that supplies it). ToID is a synthetic registration-site token so
			// the edge is self-contained and never dangles.
			addEdge("di_binding", line,
				map[string]string{
					"framework":     framework,
					"di_framework":  framework,
					"di_role":       "binding",
					"injected_type": typ,
					"mechanism":     mech,
					"scope":         scope,
					"provenance":    provenance,
					"registration":  site,
				},
				types.RelationshipRecord{
					FromID: typ,
					ToID:   site + ":" + typ,
					Kind:   string(types.RelationshipKindBinds),
					Properties: map[string]string{
						"framework": framework,
						"mechanism": mech,
						"scope":     scope,
						"via":       provenance,
					},
				})

			// INJECTED_INTO: bound type -> every handler that extracts it with
			// the matching mechanism. This is the concrete provider->consumer
			// edge the di_injection_point pattern (type-keyed) could not form.
			for _, inj := range injections {
				if inj.typ != typ || inj.mech != mech {
					continue
				}
				addEdge("di_injection", inj.line,
					map[string]string{
						"framework":     framework,
						"di_framework":  framework,
						"di_role":       "injection",
						"injected_type": typ,
						"mechanism":     mech,
						"scope":         scope,
						"consumer":      inj.handler,
						"provenance":    provenance,
					},
					types.RelationshipRecord{
						FromID: typ,
						ToID:   inj.handler,
						Kind:   string(types.RelationshipKindInjectedInto),
						Properties: map[string]string{
							"framework": framework,
							"mechanism": mech,
							"scope":     scope,
							"consumer":  inj.handler,
							"via":       provenance,
						},
					})
			}
		}
	}

	emitBinding(reAxumWithState, "axum", "state", "INFERRED_FROM_AXUM_WITH_STATE")
	emitBinding(reAxumExtensionLayer, "axum", "extension", "INFERRED_FROM_AXUM_EXTENSION_LAYER")
	emitBinding(reActixAppData, "actix_web", "data", "INFERRED_FROM_ACTIX_APP_DATA")

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
