// Reaching-definitions / def-use chain substrate (#2774 Phase 3C).
//
// Per-language sniffers lift two facts per source file:
//
//   - VarDef:  a write to a local identifier (assignment, declaration).
//   - VarUse:  a read of a local identifier.
//
// Each fact is pinned to (function, line). The generic pass in
// internal/links/def_use_pass.go composes intra-procedural reaching-
// definitions: for every (function, use) it finds the set of (function,
// def) records whose Line ≤ use.Line and Var == use.Var with no
// intervening def of the same name (last-write-wins). The result is a
// list of DefUseChain edges stamped onto the function entity's
// Properties as a compact "uses=<n>;defs=<n>;chains=v@def_line→use_line,..."
// string and surfaced via the archigraph_def_use MCP tool.
//
// Out of scope (intentionally) per the issue body:
//   - Inter-procedural def-use (requires alias analysis — Phase 4).
//   - Object-field / attribute chains (use plain identifiers only).
//   - Loop-back / SSA-phi precision (last-write-wins is sufficient for
//     the "where does this value come from at this point" query).
//
// Design mirrors Phase 0/1A: per-language sniffers are pure functions
// over file content, stateless and deterministic.
package substrate

import "sort"

// VarDef is one identifier write (definition site).
type VarDef struct {
	// Function is the declaring function/method name the def occurs in.
	// Empty when the def is at module/file scope; the pass skips those.
	Function string
	// Line is the 1-indexed source line of the def site.
	Line int
	// Var is the bare identifier name being defined.
	Var string
}

// VarUse is one identifier read (use site).
type VarUse struct {
	// Function is the declaring function/method name the use occurs in.
	// Empty when the use is at module scope; the pass skips those.
	Function string
	// Line is the 1-indexed source line of the use site.
	Line int
	// Var is the bare identifier name being read.
	Var string
}

// DefUseSnifferFn is the contract for per-language def-use sniffers.
// Returns every def and use site in the file in source order. Must be
// deterministic so the pass's output is byte-stable across runs.
type DefUseSnifferFn func(content string) (defs []VarDef, uses []VarUse)

var defUseRegistry = map[string]DefUseSnifferFn{}

// RegisterDefUseSniffer installs a per-language def-use sniffer.
func RegisterDefUseSniffer(lang string, fn DefUseSnifferFn) {
	if lang == "" || fn == nil {
		return
	}
	defUseRegistry[lang] = fn
}

// DefUseSnifferFor returns the registered sniffer for lang, or nil.
func DefUseSnifferFor(lang string) DefUseSnifferFn {
	return defUseRegistry[lang]
}

// DefUseLanguages returns the slugs of every registered def-use sniffer.
func DefUseLanguages() []string {
	out := make([]string, 0, len(defUseRegistry))
	for k := range defUseRegistry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
