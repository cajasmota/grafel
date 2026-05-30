// Package rust provides regex-based custom extractors for Rust source files.
// Each extractor targets a specific framework and registers via init().
package rust

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

func lineOf(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}

// rustColonParam matches a `:name` path segment param (axum 0.6 / poem / tide style).
var rustColonParam = regexp.MustCompile(`:([A-Za-z_]\w*)`)

// rustAngleParam matches a rocket-style `<name>` param, including dynamic
// segments like `<id..>` (trailing capture) and typed `<id: Type>` forms.
var rustAngleParam = regexp.MustCompile(`<\s*([A-Za-z_]\w*)(?:\s*(?:\.\.|\:[^>]*))?\s*>`)

// rustBraceParam matches an axum 0.7+ `{name}` or trailing `{*name}` param.
var rustBraceParam = regexp.MustCompile(`\{\*?\s*([A-Za-z_]\w*)\s*\}`)

// rustNormalizePath rewrites framework-specific path-param syntaxes to the
// canonical `{name}` form so the same logical route compares equal across
// frameworks and across axum/rocket version idioms. It is idempotent.
//
//	/users/:id        -> /users/{id}
//	/users/<id>       -> /users/{id}
//	/users/<id..>     -> /users/{id}
//	/users/{id}       -> /users/{id}   (already canonical)
//	/files/<path..>   -> /files/{path}
func rustNormalizePath(p string) string {
	if p == "" {
		return p
	}
	// Rocket angle params first (handles <id>, <id..>, <id: Type>).
	p = rustAngleParam.ReplaceAllString(p, "{$1}")
	// Colon params (axum 0.6, poem, tide, gotham).
	p = rustColonParam.ReplaceAllString(p, "{$1}")
	// Collapse brace params to a clean {name} (strips `*` glob marker / inner ws).
	p = rustBraceParam.ReplaceAllString(p, "{$1}")
	return p
}

// rustJoinPaths composes a prefix with a sub-path, normalising the slash seam
// and dropping a redundant trailing/leading slash. Both inputs are assumed to
// already be param-normalised. An empty prefix returns the child unchanged.
func rustJoinPaths(prefix, child string) string {
	if prefix == "" {
		return child
	}
	prefix = strings.TrimRight(prefix, "/")
	if child == "" || child == "/" {
		if prefix == "" {
			return "/"
		}
		return prefix
	}
	if !strings.HasPrefix(child, "/") {
		child = "/" + child
	}
	if prefix == "" {
		return child
	}
	return prefix + child
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
