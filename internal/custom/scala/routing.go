// Package scala — HTTP route-path canonicalisation for Scala web frameworks.
//
// Scala frameworks express path parameters very differently from the
// `{name}` / `:name` conventions of the JS/Python/Java ecosystems:
//
//	akka-http   path("users" / LongNumber)         — positional PathMatcher
//	http4s      Root / "users" / LongVar(id)        — named extractor object
//	zio-http    Root / "users" / int("id")          — typed segment combinator
//	zio-http    Method.GET / "users" / int("id")    — Scala-3 Routes DSL
//	scalatra    get("/users/:id")                   — colon param
//	cask        @cask.get("/users/:id")             — colon param
//	finatra     @Get("/users/:id")                  — colon param
//	play        GET /users/:id  (or $id<regex>)     — colon / dollar param
//	lagom       pathCall("/users/:id", ...)         — colon param
//
// `canonicalScalaPath` normalises every one of these into the project-wide
// canonical `{name}` form (matching internal/engine/httproutes.Canonicalize)
// so Scala routes bucket identically to their cross-stack peers. Where the
// source carries no parameter name (akka-http positional matchers, anonymous
// http4s vars) a stable synthetic name is derived from the matcher type
// (`LongNumber` → `{id}`, `Segment` → `{segment}`) so the canonical path is
// deterministic and human-meaningful.
//
// All helpers are namespaced (`scalaRoute*` / `canonicalScala*`) to avoid
// collisions with the engine-level httproutes package.
package scala

import (
	"regexp"
	"strings"
)

var (
	// scalaColonParamRe rewrites Express/Play/Scalatra-style `:name` → `{name}`.
	scalaColonParamRe = regexp.MustCompile(`:([A-Za-z_]\w*)`)

	// scalaDollarParamRe rewrites Play `$name<regex>` → `{name}`.
	scalaDollarParamRe = regexp.MustCompile(`\$([A-Za-z_]\w*)<[^>]*>`)

	// scalaHttp4sVarRe matches a named http4s path-variable extractor inside a
	// DSL segment, e.g. `LongVar(id)`, `IntVar(userId)`, `UUIDVar(id)`,
	// `id @ LongVar(_)` (the name precedes the @). Group 1 = optional bound
	// name; group 2 = extractor object; group 3 = inner arg name.
	scalaHttp4sVarRe = regexp.MustCompile(
		`(?:([A-Za-z_]\w*)\s*@\s*)?((?:Long|Int|UUID|Short|Byte|BigInt)Var|Var)\s*\(\s*([A-Za-z_]\w*|_)?\s*\)`)

	// scalaZioTypedSegRe matches a zio-http typed segment combinator
	// `int("id")` / `long("id")` / `string("id")` / `uuid("id")` / `boolean("id")`.
	// Group 1 = type; group 2 = param name.
	scalaZioTypedSegRe = regexp.MustCompile(
		`\b(int|long|string|uuid|boolean)\s*\(\s*"([^"]*)"\s*\)`)
)

// akkaMatcherParamName maps an Akka-HTTP / http4s positional PathMatcher type
// to a canonical, human-meaningful parameter name. Numeric matchers (which
// idiomatically address a resource by primary key) collapse to `id`; opaque
// segment / remainder matchers keep a type-descriptive name.
func akkaMatcherParamName(matcher string) (string, bool) {
	switch matcher {
	case "LongNumber", "IntNumber", "HexIntNumber", "HexLongNumber", "DoubleNumber":
		return "id", true
	case "JavaUUID":
		return "uuid", true
	case "Segment":
		return "segment", true
	case "Segments":
		return "segments", true
	case "Remaining", "RemainingPath":
		return "remaining", true
	case "Neutral":
		// Neutral is a no-op matcher; contributes no segment.
		return "", false
	}
	return "", false
}

// canonicalScalaPathExpr canonicalises an akka-http / http4s / zio-http path
// *expression* of the form `"seg" / Matcher / "seg2" / Var(name)` into a
// `/seg/{param}/seg2/{name}` canonical string. Both string literals and
// PathMatcher / extractor tokens between `/` separators are honoured. The
// leading `Root` (http4s/zio) is expected to already be stripped by the caller.
//
// Examples:
//
//	`"users" / LongNumber`                 → /users/{id}
//	`"users" / LongVar(id) / "posts"`      → /users/{id}/posts
//	`"users" / int("id")`                  → /users/{id}
//	`"users"`                              → /users
func canonicalScalaPathExpr(expr string) string {
	parts := strings.Split(expr, "/")
	var segs []string
	for _, raw := range parts {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			continue
		}
		// 1. String literal segment: "users".
		if strings.HasPrefix(tok, `"`) {
			if lit := scalaStringLiteral(tok); lit != "" {
				segs = append(segs, lit)
			}
			continue
		}
		// 2. http4s named extractor: LongVar(id) / id @ LongVar(_).
		if m := scalaHttp4sVarRe.FindStringSubmatch(tok); m != nil {
			name := m[1]
			if name == "" {
				name = m[3]
			}
			if name == "" || name == "_" {
				name = "id"
			}
			segs = append(segs, "{"+name+"}")
			continue
		}
		// 3. zio-http typed segment: int("id") / string("name").
		if m := scalaZioTypedSegRe.FindStringSubmatch(tok); m != nil {
			name := m[2]
			if name == "" {
				name = "id"
			}
			segs = append(segs, "{"+name+"}")
			continue
		}
		// 4. Bare PathMatcher identifier: LongNumber / Segment / JavaUUID.
		if name, ok := akkaMatcherParamName(tok); ok {
			if name != "" {
				segs = append(segs, "{"+name+"}")
			}
			continue
		}
		// 5. Unknown token — keep its identifier-ish prefix as a literal so we
		// never silently drop a static segment. Strip trailing call syntax.
		if ident := scalaLeadingIdent(tok); ident != "" {
			segs = append(segs, ident)
		}
	}
	if len(segs) == 0 {
		return "/"
	}
	return "/" + strings.Join(segs, "/")
}

// canonicalScalaColonPath canonicalises a literal string path that uses colon
// (`:id`) or Play dollar (`$id<regex>`) parameter syntax into the `{name}`
// form. Used by scalatra / cask / finatra / play / lagom whose route paths are
// plain string literals.
func canonicalScalaColonPath(raw string) string {
	if q := strings.IndexByte(raw, '?'); q >= 0 {
		raw = raw[:q]
	}
	out := scalaDollarParamRe.ReplaceAllString(raw, "{$1}")
	out = scalaColonParamRe.ReplaceAllString(out, "{$1}")
	return canonicalScalaSlashes(out)
}

// scalaStringLiteral returns the inner text of a leading double-quoted literal
// token, or "" if the token does not start with a quote.
func scalaStringLiteral(tok string) string {
	if !strings.HasPrefix(tok, `"`) {
		return ""
	}
	rest := tok[1:]
	if end := strings.IndexByte(rest, '"'); end >= 0 {
		return rest[:end]
	}
	return rest
}

// scalaLeadingIdent returns the leading identifier of a token (letters, digits,
// underscore), discarding any trailing `(...)` call or other syntax. Returns
// "" when the token does not begin with an identifier character.
func scalaLeadingIdent(tok string) string {
	i := 0
	for i < len(tok) {
		c := tok[i]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			i++
			continue
		}
		break
	}
	return tok[:i]
}

// canonicalScalaSlashes ensures a single leading slash, no trailing slash
// (except root), and no internal duplicate slashes.
func canonicalScalaSlashes(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		p = strings.TrimRight(p, "/")
		if p == "" {
			p = "/"
		}
	}
	return p
}

// composeScalaPath joins an optional path prefix with a path segment, each of
// which is already canonicalised, into a single canonical path.
func composeScalaPath(prefix, seg string) string {
	switch {
	case prefix == "" || prefix == "/":
		return canonicalScalaSlashes(seg)
	case seg == "" || seg == "/":
		return canonicalScalaSlashes(prefix)
	default:
		return canonicalScalaSlashes(prefix + "/" + seg)
	}
}
