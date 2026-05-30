// Package lua provides regex-based custom extractors for Lua source files.
// Each extractor targets a specific framework and registers via init().
package lua

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

func lineOf(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}

// luaColonParamRe matches an Express/Lapis-style `:name` colon path parameter.
var luaColonParamRe = regexp.MustCompile(`:([A-Za-z_]\w*)`)

// luaCanonicalPath normalises a Lapis/lua-resty-router path's `:name` colon
// parameters to the canonical `{name}` form (e.g. `/users/:id` → `/users/{id}`),
// matching the engine-level httproutes.Canonicalize convention. Literal nginx
// paths (no colon segments) pass through unchanged.
func luaCanonicalPath(raw string) string {
	return luaColonParamRe.ReplaceAllString(raw, "{$1}")
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
