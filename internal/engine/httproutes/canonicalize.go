// Package httproutes provides canonicalisation of HTTP route paths across
// frameworks so that synthetic http_endpoint entities use the same ID on
// both producer and consumer sides regardless of the framework conventions
// of the originating code.
//
// The canonical form uses `{name}` for path parameters (matching the
// OpenAPI / JAX-RS / FastAPI form). All paths are rooted with a leading
// `/`. Trailing slashes are stripped except for the root path itself.
//
// Convention: trailing slash is normalised AWAY. Django often writes
// `path("users/", ...)` and Flask/FastAPI/Spring usually omit it; we pick
// the shorter form so backend and frontend agree on
// `http:GET:/users/{id}` regardless of source convention.
package httproutes

import (
	"strings"
)

// Framework identifiers passed to Canonicalize.
const (
	FrameworkDjango  = "django"
	FrameworkFlask   = "flask"
	FrameworkFastAPI = "fastapi"
	FrameworkSpring  = "spring"
	FrameworkJAXRS   = "jaxrs"
	FrameworkExpress = "express"
	// FrameworkGin, FrameworkEcho, FrameworkChi (#722) share Express's
	// `:name` parameter convention; their canonicalisation reuses the
	// colon-param walker. They are listed as distinct constants so call
	// sites in per-language extractors read naturally.
	FrameworkGin  = "gin"
	FrameworkEcho = "echo"
	FrameworkChi  = "chi"
)

// Canonicalize maps a framework-specific raw path string to the canonical
// `{param}` form. The output always starts with `/` and has no trailing
// slash (except for the bare root path `/`).
//
// Recognised input forms:
//   - Django:   `<int:user_id>`, `<str:slug>`, `<uuid:pk>`, `<name>` -> `{user_id}` / `{slug}` / `{pk}` / `{name}`
//   - Flask:    `<int:id>`, `<float:x>`, `<path:rest>`, `<uuid:u>`, `<id>` -> `{id}` / `{x}` / `{rest}` / `{u}` / `{id}`
//   - FastAPI / JAX-RS / Spring: `{id}`, `{id:regex}` -> `{id}` (regex constraint stripped)
//   - Express:  `:id`, `:id?` -> `{id}` (optional marker dropped — phase 1)
func Canonicalize(framework, raw string) string {
	if raw == "" {
		return "/"
	}

	var out string
	switch framework {
	case FrameworkDjango, FrameworkFlask:
		out = canonicalizeAngleBrackets(raw)
	case FrameworkFastAPI, FrameworkSpring, FrameworkJAXRS:
		out = canonicalizeCurlyBraces(raw)
	case FrameworkExpress, FrameworkGin, FrameworkEcho, FrameworkChi:
		out = canonicalizeColonParams(raw)
	default:
		// Unknown framework: pass through but still normalise slashes.
		out = raw
	}

	return normaliseSlashes(out)
}

// canonicalizeAngleBrackets rewrites `<converter:name>` and `<name>` to
// `{name}`. Used for Django and Flask which both use angle-bracket syntax
// with optional converter prefixes (Django: `int`, `str`, `slug`, `uuid`,
// `path`; Flask: `int`, `float`, `path`, `uuid`, `string` — default).
func canonicalizeAngleBrackets(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	i := 0
	for i < len(raw) {
		c := raw[i]
		if c != '<' {
			b.WriteByte(c)
			i++
			continue
		}
		// Find matching '>'. If none, treat as literal.
		end := strings.IndexByte(raw[i+1:], '>')
		if end < 0 {
			b.WriteByte(c)
			i++
			continue
		}
		inner := raw[i+1 : i+1+end]
		// `converter:name` -> `name`. Plain `name` stays.
		if idx := strings.IndexByte(inner, ':'); idx >= 0 {
			inner = inner[idx+1:]
		}
		// Defensive: drop any embedded regex specifiers (Django `re_path` uses
		// `(?P<name>regex)` style but those don't pass through here — they're
		// handled by the django_routes AST pass).
		inner = strings.TrimSpace(inner)
		if inner == "" {
			b.WriteString("{}")
		} else {
			b.WriteByte('{')
			b.WriteString(inner)
			b.WriteByte('}')
		}
		i += 1 + end + 1
	}
	return b.String()
}

// canonicalizeCurlyBraces strips the optional regex constraint from
// `{name:regex}` forms (used by Spring MVC) and leaves bare `{name}` alone.
// FastAPI and JAX-RS already use `{name}` natively so this is mostly a
// pass-through there.
func canonicalizeCurlyBraces(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	i := 0
	for i < len(raw) {
		c := raw[i]
		if c != '{' {
			b.WriteByte(c)
			i++
			continue
		}
		end := strings.IndexByte(raw[i+1:], '}')
		if end < 0 {
			b.WriteByte(c)
			i++
			continue
		}
		inner := raw[i+1 : i+1+end]
		// Drop a `:regex` suffix if present.
		if idx := strings.IndexByte(inner, ':'); idx >= 0 {
			inner = inner[:idx]
		}
		inner = strings.TrimSpace(inner)
		if inner == "" {
			b.WriteString("{}")
		} else {
			b.WriteByte('{')
			b.WriteString(inner)
			b.WriteByte('}')
		}
		i += 1 + end + 1
	}
	return b.String()
}

// canonicalizeColonParams rewrites Express-style `:name` and `:name?` to
// `{name}`. Phase 1 drops the optional `?` marker — we treat optional and
// required path params as the same endpoint shape for cross-repo matching.
func canonicalizeColonParams(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	i := 0
	for i < len(raw) {
		c := raw[i]
		if c != ':' {
			b.WriteByte(c)
			i++
			continue
		}
		// Consume the parameter name: [A-Za-z_][A-Za-z0-9_]*.
		j := i + 1
		for j < len(raw) && isIdentChar(raw[j]) {
			j++
		}
		if j == i+1 {
			b.WriteByte(c)
			i++
			continue
		}
		name := raw[i+1 : j]
		b.WriteByte('{')
		b.WriteString(name)
		b.WriteByte('}')
		i = j
		// Drop trailing `?` (optional marker) without altering the rest.
		if i < len(raw) && raw[i] == '?' {
			i++
		}
	}
	return b.String()
}

// isIdentChar reports whether c can appear in an identifier (after the
// first character).
func isIdentChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '_':
		return true
	default:
		return false
	}
}

// normaliseSlashes ensures the path starts with exactly one `/` and has no
// trailing `/` (except for the bare root `/`). Internal duplicate slashes
// (e.g. `/api//users`) are collapsed.
func normaliseSlashes(p string) string {
	if p == "" {
		return "/"
	}
	// Ensure leading slash.
	if p[0] != '/' {
		p = "/" + p
	}
	// Collapse internal duplicate slashes.
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	// Strip trailing slash (but keep root).
	if len(p) > 1 && p[len(p)-1] == '/' {
		p = strings.TrimRight(p, "/")
		if p == "" {
			p = "/"
		}
	}
	return p
}

// SyntheticID builds the canonical synthetic-entity ID for an HTTP endpoint.
// Format: `http:<METHOD>:<canonical-path>` with METHOD upper-cased and path
// already canonicalised. Method `ANY` (case-insensitive) is preserved so
// callers can distinguish method-agnostic registrations from a specific
// verb.
func SyntheticID(method, canonicalPath string) string {
	return "http:" + strings.ToUpper(method) + ":" + canonicalPath
}
