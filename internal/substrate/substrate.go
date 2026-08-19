// Package substrate implements the Phase 0 static-analysis substrate (#2716).
//
// The substrate is a per-language data layer that lifts constant bindings out
// of source files into a normalised IR so the generic propagation pass in
// internal/links/constant_propagation.go can resolve identifiers across
// files via the IMPORTS edge graph.
//
// Design notes:
//   - Per-language sniffers are pure functions that take a file's source
//     content and return a slice of Binding records. They do not touch the
//     graph or the disk; they are stateless.
//   - The propagation pass owns scope (file, line, ident) → Binding
//     resolution, the IMPORTS-edge walk (max depth 3), and the property
//     stamping. Sniffers are oblivious to that infrastructure.
//   - There is intentionally no ConstantBinding entity kind. Bindings
//     are decoration on existing declaration entities; the persistent
//     graph surface is a property + a RESOLVES_TO edge (see kinds.go).
//
// Adding a new language: implement a sniffer with the SniffFn signature,
// register it via Register("<lang>", sniffFn) in an init() in the
// language file, and add capability cells per AGENTS.md.
package substrate

import "sort"

// Binding is one constant-binding fact lifted from source by a per-language
// sniffer.
//
// All fields are required except as noted.
type Binding struct {
	// Ident is the identifier being bound. For module-level constants
	// it is the bare name (e.g. "API_URL"); the propagation pass scopes
	// it by (repo, file).
	Ident string

	// Line is the 1-indexed source line of the declaration. Used by the
	// propagation pass to resolve nearest-preceding declarations when a
	// file contains multiple bindings of the same name in different
	// scopes (rare for module-level constants, but possible).
	Line int

	// Value is the resolved literal value when Provenance == ProvenanceLiteral
	// or ProvenanceEnvFallback. Empty for ProvenanceCrossFile until the
	// propagation pass fills it in by following the import.
	Value string

	// EnvVar is the OS env-var name read by getenv/os.environ/etc. Set
	// when Provenance == ProvenanceEnvFallback so downstream consumers
	// can record that a fallback was the resolved value. Empty for the
	// pure-literal case.
	EnvVar string

	// Provenance records how Value was determined. The propagation pass
	// composes the chain across import hops.
	Provenance Provenance

	// Confidence is the per-binding confidence in [0, 1]. The
	// propagation pass min()s confidences across hops.
	Confidence float64

	// ImportSource, when set, names the module path the binding was
	// re-exported from. Used by the propagation pass to follow IMPORTS
	// edges to the upstream declaration. Empty for in-file bindings.
	ImportSource string
}

// Provenance is the discrete provenance label for a binding step.
type Provenance string

const (
	// ProvenanceLiteral marks a direct string-literal binding
	// (e.g. `const API = "https://x"`). Confidence: 1.0.
	ProvenanceLiteral Provenance = "literal"

	// ProvenanceEnvFallback marks an env-var read with a literal
	// fallback (e.g. `os.Getenv("X")` with a `?? "default"`).
	// Confidence: 0.85 — the literal default may not match production.
	ProvenanceEnvFallback Provenance = "env_fallback"

	// ProvenanceCrossFile marks a re-exported binding that the
	// propagation pass must follow via IMPORTS to resolve. The
	// per-hop sniffer leaves Value empty; the pass fills it in.
	// Confidence: 0.6 baseline; the pass also min()s with upstream.
	ProvenanceCrossFile Provenance = "cross_file"
)

// SniffFn is the contract every per-language sniffer satisfies. Inputs:
// the raw file content. Output: a slice of Binding records (may be empty;
// must be nil-safe).
//
// The function MUST be deterministic: identical content → identical output
// in slice order, so byte-identical graph output across runs is preserved.
type SniffFn func(content string) []Binding

// registry holds the registered per-language sniffers. Populated by init()
// in each language file.
var registry = map[string]SniffFn{}

// Register installs a sniffer for a language slug. Idempotent: a second
// call with the same slug overwrites the prior registration. Intended for
// init()-time wiring from the per-language files.
func Register(lang string, fn SniffFn) {
	if lang == "" || fn == nil {
		return
	}
	registry[lang] = fn
}

// SnifferFor returns the registered sniffer for lang, or nil when none is
// registered. Callers must nil-check before invocation.
func SnifferFor(lang string) SniffFn {
	return registry[lang]
}

// Languages returns the slugs of every registered language in sorted order.
// Used by the propagation pass to know which languages have substrate
// support enabled.
func Languages() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// LanguageForPath returns the canonical sniffer language slug for the given
// file path, based on its extension. Returns "" when no language matches.
//
// Slug list:
//   - Phase 0 T1 (#2761): jsts, python, java, go
//   - Phase 0 T2 (#2762): ruby, php, rust, csharp, kotlin, elixir, scala, c-cpp
//   - Phase 0 T3 (#2763): dart, groovy, lua, swift, clojure, crystal, elm,
//     erlang, fsharp, haskell, nim, ocaml, reasonml, rescript, sml,
//     solidity, zig, svelte, vue, astro
//
// Languages from the T3 issue scope that are intentionally not registered
// (no meaningful constant-binding shape exists):
//   - verilog / vhdl: hardware description languages — no runtime constants
//   - idris: dependently-typed niche; constant idiom too varied
//   - lisp: too generic (CL vs scheme dialects); no canonical const+env
//   - pony: actor-typed; no env-fallback convention
//   - multi: meta-language for CI / config records, not a real language
func LanguageForPath(path string) string {
	// Order matters when one extension is a suffix of another (e.g. .pyi /
	// .py). Longer suffixes first.
	switch {
	case hasSuffix(path, ".ts"), hasSuffix(path, ".tsx"),
		hasSuffix(path, ".js"), hasSuffix(path, ".jsx"),
		hasSuffix(path, ".mjs"), hasSuffix(path, ".cjs"):
		return "jsts"
	case hasSuffix(path, ".py"), hasSuffix(path, ".pyi"):
		return "python"
	case hasSuffix(path, ".java"):
		return "java"
	case hasSuffix(path, ".go"):
		return "go"
	case hasSuffix(path, ".rb"), hasSuffix(path, ".rake"):
		return "ruby"
	case hasSuffix(path, ".php"), hasSuffix(path, ".phtml"):
		return "php"
	case hasSuffix(path, ".rs"):
		return "rust"
	case hasSuffix(path, ".cs"):
		return "csharp"
	// VB.NET (#6327 S2). This is the path→slug half of the mapping only; no
	// sniffer is registered for "vbnet" yet, so SnifferFor("vbnet") returns
	// nil and Languages() does not list it. Callers nil-check, so the slug is
	// inert until S3+ registers a sniffer — it is here so the extension is
	// mapped in one place rather than two stories apart. Only `.vb`:
	// `.vbproj` is an MSBuild XML file (`.csproj` is likewise absent here) and
	// `.bas`/`.cls` are VB6/VBA — see the exclusion note in
	// internal/classifier/classifier.go.
	case hasSuffix(path, ".vb"):
		return "vbnet"
	case hasSuffix(path, ".kt"), hasSuffix(path, ".kts"):
		return "kotlin"
	case hasSuffix(path, ".ex"), hasSuffix(path, ".exs"):
		return "elixir"
	case hasSuffix(path, ".scala"), hasSuffix(path, ".sc"):
		return "scala"
	case hasSuffix(path, ".c"), hasSuffix(path, ".h"),
		hasSuffix(path, ".cc"), hasSuffix(path, ".cpp"),
		hasSuffix(path, ".cxx"), hasSuffix(path, ".hpp"),
		hasSuffix(path, ".hh"), hasSuffix(path, ".hxx"):
		return "c-cpp"
	case hasSuffix(path, ".dart"):
		return "dart"
	case hasSuffix(path, ".groovy"), hasSuffix(path, ".gradle"):
		return "groovy"
	case hasSuffix(path, ".lua"):
		return "lua"
	case hasSuffix(path, ".swift"):
		return "swift"
	case hasSuffix(path, ".clj"), hasSuffix(path, ".cljs"), hasSuffix(path, ".cljc"), hasSuffix(path, ".edn"):
		return "clojure"
	case hasSuffix(path, ".cr"):
		return "crystal"
	case hasSuffix(path, ".elm"):
		return "elm"
	case hasSuffix(path, ".erl"), hasSuffix(path, ".hrl"):
		return "erlang"
	case hasSuffix(path, ".fs"), hasSuffix(path, ".fsi"), hasSuffix(path, ".fsx"):
		return "fsharp"
	case hasSuffix(path, ".hs"), hasSuffix(path, ".lhs"):
		return "haskell"
	case hasSuffix(path, ".nim"), hasSuffix(path, ".nims"):
		return "nim"
	case hasSuffix(path, ".ml"), hasSuffix(path, ".mli"):
		return "ocaml"
	case hasSuffix(path, ".re"), hasSuffix(path, ".rei"):
		return "reasonml"
	case hasSuffix(path, ".res"), hasSuffix(path, ".resi"):
		return "rescript"
	case hasSuffix(path, ".sml"), hasSuffix(path, ".sig"):
		return "sml"
	case hasSuffix(path, ".sol"):
		return "solidity"
	case hasSuffix(path, ".zig"):
		return "zig"
	case hasSuffix(path, ".svelte"):
		return "svelte"
	case hasSuffix(path, ".vue"):
		return "vue"
	case hasSuffix(path, ".astro"):
		return "astro"
	case hasSuffix(path, ".graphql"), hasSuffix(path, ".gql"):
		return "graphql"
	}
	return ""
}

// hasSuffix is a tiny stdlib-free helper to keep the import surface minimal.
func hasSuffix(s, suf string) bool {
	if len(s) < len(suf) {
		return false
	}
	return s[len(s)-len(suf):] == suf
}
