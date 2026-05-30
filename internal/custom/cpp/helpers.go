// Package cpp provides regex-based custom extractors for C++ source files.
// Each extractor targets a specific framework and registers via init().
package cpp

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

func lineOf(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}

// cppColonParam matches a `:name` path segment param (Pistache / RESTinio style).
var cppColonParam = regexp.MustCompile(`:([A-Za-z_]\w*)`)

// cppAngleParam matches a Crow-style angle param. Crow params are typed and
// usually unnamed — `<int>`, `<string>`, `<uint>`, `<double>`, `<path>` — but
// may also carry a name via `<int:id>`. When a name is present we keep the
// name; otherwise we keep the type token (e.g. `<int>` -> `{int}`). Trailing
// glob markers (`<path:rest>`) collapse to the inner name.
var cppAngleParam = regexp.MustCompile(`<\s*([A-Za-z_]\w*)(?:\s*:\s*([A-Za-z_]\w*))?\s*>`)

// cppBraceParam normalises an already-brace param `{ name }` / `{*name}` to a
// clean `{name}` (strips inner whitespace and a leading glob `*`). Idempotent.
var cppBraceParam = regexp.MustCompile(`\{\s*\*?\s*([A-Za-z_]\w*)\s*\}`)

// cppNormalizeRoutePath rewrites framework-specific path-param syntaxes to the
// canonical `{name}` form so the same logical route compares equal across the
// eight supported C/C++ HTTP frameworks (Crow, Drogon, oatpp, Pistache, POCO,
// Restbed, RESTinio, cpprestsdk). It is idempotent.
//
//	/users/:id          -> /users/{id}          (Pistache, RESTinio colon)
//	/users/<int>        -> /users/{int}         (Crow typed-unnamed)
//	/users/<int:id>     -> /users/{id}          (Crow typed-named)
//	/users/{id}         -> /users/{id}          (Drogon, oatpp — already canonical)
//	/files/{ path }     -> /files/{path}
//
// Sentinel paths emitted by the extractors when a literal could not be
// resolved (e.g. "<listener>", "<resource>", "*") are passed through
// untouched: the angle form there names a *variable*, not a route param, and
// "*" is a catch-all glob.
func cppNormalizeRoutePath(p string) string {
	if p == "" || p == "*" {
		return p
	}
	// Sentinel "<var>" fallback paths (no slash) are variable references, not
	// route params — leave them alone so handler_attribution stays readable.
	if strings.HasPrefix(p, "<") && strings.HasSuffix(p, ">") && !strings.Contains(p, "/") {
		return p
	}
	// Crow angle params: prefer an explicit name, else fall back to the type.
	p = cppAngleParam.ReplaceAllStringFunc(p, func(m string) string {
		sm := cppAngleParam.FindStringSubmatch(m)
		if sm[2] != "" {
			return "{" + sm[2] + "}"
		}
		return "{" + sm[1] + "}"
	})
	// Colon params (Pistache, RESTinio).
	p = cppColonParam.ReplaceAllString(p, "{$1}")
	// Collapse/clean brace params.
	p = cppBraceParam.ReplaceAllString(p, "{$1}")
	return p
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
