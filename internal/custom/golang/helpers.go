// Package golang provides regex-based custom extractors for Go source files.
// Each extractor targets a specific framework and registers via init().
package golang

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

func lineOf(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}

// itoa is a local alias for strconv.Itoa, used to build collision-resistant
// synthetic entity names that fold the source line into the name.
func itoa(n int) string { return strconv.Itoa(n) }

// submatch returns the capture-group text at submatch-index pair (g, g+1)
// from a FindAllStringSubmatchIndex match, or "" when the group did not
// participate in the match (index -1).
func submatch(src string, m []int, g int) string {
	if g+1 >= len(m) || m[g] < 0 || m[g+1] < 0 {
		return ""
	}
	return src[m[g]:m[g+1]]
}

func makeEntity(name, kind, subtype, filePath, language string, lineNum int) types.EntityRecord {
	e := types.EntityRecord{
		Name:             name,
		Kind:             kind,
		Subtype:          subtype,
		SourceFile:       filePath,
		StartLine:        lineNum,
		EndLine:          lineNum,
		Language:         language,
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
		Properties: map[string]string{
			"kind":    kind,
			"subtype": subtype,
		},
	}
	e.ID = e.ComputeID()
	return e
}

func setProps(e *types.EntityRecord, kv ...string) {
	if len(kv)%2 != 0 {
		return
	}
	for i := 0; i < len(kv); i += 2 {
		e.Properties[kv[i]] = kv[i+1]
	}
}

// goClassRef returns the structural reference used as the FromID/ToID for an
// edge targeting a Go struct/model entity (e.g. a GORM model). The
// `Class:<Name>` form resolves through the resolver's byName fallback to the
// SCOPE.Schema model node the extractor emits, mirroring the JS/Python/Ruby ORM
// field-membership convention (#4328/#4366/#4367).
func goClassRef(className string) string { return "Class:" + className }

// containsFieldEdge builds the structural CONTAINS membership edge from an
// owning GORM model struct to one of its column / association field entities.
// Issue #4367: GORM `field:<Model>.<name>` and `rel:<Model>.<name>` entities
// were emitted as standalone nodes with no owning-model membership, leaving them
// as degree-0 orphans on the graph.
//
// FromID names the owner struct (`Class:<owner>`) so the resolver binds it to
// the real model entity; ToID is the field/relation entity's own ID. The edge
// is hung off the owner model node by the caller.
func containsFieldEdge(ownerModel, memberID, fieldName, framework string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: goClassRef(ownerModel),
		ToID:   memberID,
		Kind:   string(types.RelationshipKindContains),
		Properties: map[string]string{
			"framework":  framework,
			"language":   "go",
			"member":     "field",
			"field_name": fieldName,
			"provenance": "INFERRED_FROM_MODEL_FIELD_MEMBERSHIP",
		},
	}
}

// referencesClassEdge builds a REFERENCES edge from a GORM association field
// entity to the model struct it points at — the related struct type of a
// belongs_to/has_one/has_many/many2many field (`Items []Item` → `Item`). Issue
// #4367: that target model is the association field's only outbound semantic
// edge; without it the related model rings.
//
// FromID is the relation entity's own ID; ToID is the `Class:<target>` stub the
// resolver binds to the real model entity. The edge is hung off the relation
// entity by the caller.
func referencesClassEdge(memberID, targetModel, framework, fieldName string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: memberID,
		ToID:   goClassRef(targetModel),
		Kind:   string(types.RelationshipKindReferences),
		Properties: map[string]string{
			"framework":   framework,
			"language":    "go",
			"ref_kind":    "field_target_type",
			"field_name":  fieldName,
			"target_type": targetModel,
			"provenance":  "INFERRED_FROM_MODEL_FIELD_TARGET",
		},
	}
}

// ---------------------------------------------------------------------------
// Shared middleware + auth detection (issue #3213)
//
// All four well-templated Go HTTP frameworks (gin/echo/fiber/chi) register
// middleware through a `.Use(...)` call that accepts one or more middleware
// values, applied left-to-right in registration order. This shared detector
// parses a single `.Use(...)` argument list into ordered middleware
// expressions and classifies each against a heuristic auth-pattern catalog.
//
// Honesty note: detection is a heuristic substring/identifier match on the
// middleware expression text — it does NOT perform data-flow analysis to
// confirm a value actually enforces authentication. It is therefore reported
// as `partial` coverage in the registry.
// ---------------------------------------------------------------------------

// middlewareArg is one entry in a `.Use(...)` chain, in registration order.
type middlewareArg struct {
	Expr     string // the raw middleware expression, e.g. "jwt.New(cfg)"
	Name     string // the leading identifier/selector, e.g. "jwt.New"
	Order    int    // 0-based position within this .Use(...) call
	AuthKind string // non-empty when classified as auth middleware
}

// reMiddlewareCallHead extracts the leading identifier/selector of a
// middleware expression: the part before any "(" call. e.g. "jwt.New(c)" -> "jwt.New".
var reMiddlewareCallHead = regexp.MustCompile(`^[A-Za-z_][\w.]*`)

// splitTopLevelArgs splits a comma-separated argument list on top-level commas
// only — commas nested inside (), [], or {} are preserved. Quoted strings are
// skipped so commas inside string literals do not split arguments.
func splitTopLevelArgs(argList string) []string {
	var args []string
	depth := 0
	start := 0
	var quote rune
	for i, r := range argList {
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '"', '\'', '`':
			quote = r
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(argList[start:i]))
				start = i + 1
			}
		}
	}
	if last := strings.TrimSpace(argList[start:]); last != "" {
		args = append(args, last)
	}
	return args
}

// parseMiddlewareChain parses the argument text of a single `.Use(...)` call
// into ordered middleware entries with auth classification applied.
func parseMiddlewareChain(argList string) []middlewareArg {
	parts := splitTopLevelArgs(argList)
	out := make([]middlewareArg, 0, len(parts))
	order := 0
	for _, p := range parts {
		if p == "" {
			continue
		}
		// A bare string/char literal is a path-mount prefix (e.g.
		// fiber's app.Use("/api", mw)), never a middleware value — skip it
		// so it neither inflates the order index nor emits a phantom entry.
		if isStringLiteral(p) {
			continue
		}
		head := reMiddlewareCallHead.FindString(p)
		if head == "" {
			head = p
		}
		out = append(out, middlewareArg{
			Expr:     p,
			Name:     head,
			Order:    order,
			AuthKind: classifyAuthMiddleware(p),
		})
		order++
	}
	return out
}

// useCall is a single `<recv>.Use(<args>)` invocation located by scanning,
// with parentheses balanced (so nested calls like JWT([]byte("k")) are
// captured whole, which the single-level regex form cannot do).
type useCall struct {
	Recv string // receiver variable, e.g. "r" / "app"
	Args string // raw argument text between the outer parens
	Line int    // 1-based source line of the `.Use(` token
}

// reUseHead locates the `<ident>.Use(` token; the balanced argument span is
// then scanned forward from the opening paren. The optional Router/Global
// suffix covers Iris's `.UseRouter(...)` / `.UseGlobal(...)` middleware
// registration variants (gin/echo/fiber/chi/hertz/buffalo/gorilla-mux use the
// bare `.Use(...)` form, which the optional group still matches).
var reUseHead = regexp.MustCompile(`(\w+)\.Use(?:Router|Global)?\s*\(`)

// findUseCalls returns every balanced `.Use(...)` call in src. Unlike a flat
// regex, it tracks paren depth (skipping quoted strings) so arbitrarily nested
// middleware expressions are captured in full.
func findUseCalls(src string) []useCall {
	var calls []useCall
	for _, loc := range reUseHead.FindAllStringSubmatchIndex(src, -1) {
		recv := src[loc[2]:loc[3]]
		open := loc[1] - 1 // index of the '(' (reUseHead ends at it)
		depth := 0
		var quote rune
		end := -1
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
					end = i
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			continue // unbalanced; skip
		}
		calls = append(calls, useCall{
			Recv: recv,
			Args: strings.TrimSpace(src[open+1 : end]),
			Line: lineOf(src, loc[0]),
		})
	}
	return calls
}

// isStringLiteral reports whether expr is a single quoted string or rune
// literal (no surrounding operators/calls).
func isStringLiteral(expr string) bool {
	if len(expr) < 2 {
		return false
	}
	q := expr[0]
	if q != '"' && q != '\'' && q != '`' {
		return false
	}
	return expr[len(expr)-1] == q
}

// authPatterns maps a lowercased substring of a middleware expression to a
// coarse auth kind. Ordered most-specific first so e.g. "jwt" wins over the
// generic "auth". The catalog covers the common gin/echo/fiber/chi auth
// middleware surface (framework built-ins + popular community modules).
var authPatterns = []struct {
	needle string
	kind   string
}{
	{"jwtware", "jwt"},
	{"jwt", "jwt"},
	{"bearer", "jwt"},
	{"oauth2", "oauth"},
	{"oauth", "oauth"},
	{"keyauth", "api_key"},
	{"apikey", "api_key"},
	{"api_key", "api_key"},
	{"basicauth", "basic"},
	{"basic_auth", "basic"},
	{"session", "session"},
	{"casbin", "rbac"},
	{"rbac", "rbac"},
	{"authz", "authz"},
	{"authorize", "authz"},
	{"requireauth", "auth"},
	{"authrequired", "auth"},
	{"authmiddleware", "auth"},
	{"authenticate", "auth"},
	{"auth", "auth"},
}

// classifyAuthMiddleware returns a coarse auth kind for a middleware expression,
// or "" if it does not look like an auth/authorization middleware. Heuristic:
// case-insensitive substring match against authPatterns.
func classifyAuthMiddleware(expr string) string {
	low := strings.ToLower(expr)
	for _, p := range authPatterns {
		if strings.Contains(low, p.needle) {
			return p.kind
		}
	}
	return ""
}

// emitMiddlewareChain appends one SCOPE.Pattern entity per middleware in a
// `.Use(...)` call, preserving registration order via the "mw_order" property,
// and a dedicated auth SCOPE.Pattern (pattern_kind=auth) for any entry that
// classifies as auth middleware. The provenance + framework are supplied by
// the caller so this stays framework-agnostic.
//
// add is the caller's dedup-aware appender; line is the source line of the
// .Use(...) call.
func emitMiddlewareChain(
	add func(types.EntityRecord),
	args []middlewareArg,
	framework, mwProvenance, authProvenance, filePath, language string,
	line int,
) {
	for _, a := range args {
		mw := makeEntity(a.Expr, "SCOPE.Pattern", "", filePath, language, line)
		setProps(&mw, "framework", framework, "provenance", mwProvenance,
			"pattern_kind", "middleware",
			"middleware_name", a.Name,
			"mw_order", strconv.Itoa(a.Order))
		if a.AuthKind != "" {
			setProps(&mw, "is_auth", "true", "auth_kind", a.AuthKind)
		}
		add(mw)

		if a.AuthKind != "" {
			au := makeEntity("auth:"+a.Name, "SCOPE.Pattern", "", filePath, language, line)
			setProps(&au, "framework", framework, "provenance", authProvenance,
				"pattern_kind", "auth",
				"auth_kind", a.AuthKind,
				"middleware_name", a.Name,
				"middleware_expr", a.Expr)
			add(au)
		}
	}
}
