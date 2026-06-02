// Taint-flow substrate (#2772 Phase 2B).
//
// Taint analysis tags every function declaration with the union of
// taint-relevant primitives that occur within it:
//
//   - sources    : untrusted-input primitives (HTTP request body / query
//     / headers / cookies, env vars, file reads of
//     user-controlled paths, JSON deserialisation from an
//     untrusted byte slice).
//   - sinks      : security-sensitive operations (raw SQL exec with
//     string concatenation, shell exec, eval, regex
//     construction from a non-literal, file writes with a
//     non-literal path, HTML output without escaping).
//   - sanitizers : primitives that statically prove the value flowing
//     through them is safe at this point (parameterised
//     SQL, html.escape / bleach / DOMPurify, validation
//     library schema declarations — zod / joi /
//     marshmallow / pydantic).
//
// The per-language sniffer is the only language-aware piece; the
// generic propagation pass at internal/links/taint_flow.go consumes
// TaintMatch records to compute SecurityFinding records.
//
// Design choices echo the Phase 1A effect substrate (see effects.go):
//
//   - Sniffers are stateless, deterministic, nil-safe.
//   - Sniffers attribute each match to the nearest preceding function
//     header so the propagation pass can bind matches to graph entities
//     via (repo, file, function-name) without a per-language symbol
//     table.
//   - Confidence is set by the sniffer per kind: well-known primitives
//     (e.g. cursor.execute with a literal SELECT) are 1.0; heuristic
//     matches drop to 0.7-0.85.
//
// Hard rule per the issue spec: validation libraries count as
// sanitizers ONLY when the call shape is a schema declaration (zod's
// `z.object({...})`, marshmallow's `class FooSchema(Schema)`, etc.).
// A bare `parse()` call without a paired schema does NOT count — that
// is enforced inside each per-language sniffer.
package substrate

import "sort"

// TaintKind labels the role a primitive plays in the source/sink/
// sanitizer lattice.
type TaintKind string

const (
	// TaintKindSource marks an untrusted-input primitive.
	TaintKindSource TaintKind = "source"
	// TaintKindSink marks a security-sensitive operation that should
	// not receive tainted input without an intervening sanitizer.
	TaintKindSink TaintKind = "sink"
	// TaintKindSanitizer marks a primitive that proves its input is
	// safe at the call site — taint flowing into the call is
	// considered cleansed downstream.
	TaintKindSanitizer TaintKind = "sanitizer"
)

// TaintCategory is the finer-grained classification used by the
// propagation pass to label SecurityFinding records (sql_injection,
// command_injection, path_traversal, xss, redos, deserialisation,
// open_redirect — broader than the OWASP top-10 but unified across
// languages so the MCP tool can rank cross-language).
type TaintCategory string

const (
	// TaintCategorySQL — raw SQL exec / query without parameter binding.
	TaintCategorySQL TaintCategory = "sql_injection"
	// TaintCategoryCommand — shell / process exec, eval, dynamic code.
	TaintCategoryCommand TaintCategory = "command_injection"
	// TaintCategoryPath — file open / read / write with a path derived
	// from untrusted input (path traversal).
	TaintCategoryPath TaintCategory = "path_traversal"
	// TaintCategoryXSS — HTML / template output without escaping.
	TaintCategoryXSS TaintCategory = "xss"
	// TaintCategoryReDoS — regex constructed from a non-literal pattern.
	TaintCategoryReDoS TaintCategory = "redos"
	// TaintCategoryDeserialization — pickle.loads / yaml.load /
	// ObjectInputStream.readObject style untrusted deserialisation.
	TaintCategoryDeserialization TaintCategory = "deserialization"
	// TaintCategorySSRF — outbound HTTP request to a user-controlled URL.
	TaintCategorySSRF TaintCategory = "ssrf"
	// TaintCategoryGeneric — primitive that is sensitive but does not
	// fit the named categories cleanly (still emitted, ranked lower).
	TaintCategoryGeneric TaintCategory = "generic"
)

// TaintMatch is one detected taint-relevant primitive inside a function
// body. Mirrors EffectMatch's shape so reviewers reading both files see
// the same skeleton.
type TaintMatch struct {
	// Function is the declaring function/method name that owns the
	// primitive. Empty when the primitive occurs at module scope; the
	// propagation pass drops module-scope matches because security
	// findings without a containing function are not actionable.
	Function string
	// Line is the 1-indexed source line of the primitive.
	Line int
	// Kind is the lattice role of this match.
	Kind TaintKind
	// Category narrows the kind (sinks and a few sources carry one).
	// Sanitizers carry the category they cleanse (e.g. parameterised
	// SQL is TaintCategorySQL) so the propagation pass can pair them.
	Category TaintCategory
	// Primitive is a short tag identifying the matched primitive
	// (e.g. "cursor.execute(non-literal)", "subprocess.run(shell=True)",
	// "html.escape", "z.object"). Surfaced by archigraph_security_findings
	// so the agent can see why a function is flagged.
	Primitive string
	// Confidence is the per-match confidence in [0, 1]. Direct
	// matches against well-known primitives are 1.0; heuristic
	// matches drop to 0.7-0.85.
	Confidence float64
}

// TaintSnifferFn is the contract for per-language taint-site sniffers.
// Input: raw file content. Output: every detected TaintMatch in source
// order (deterministic — identical content yields identical slices, so
// graph output stays byte-identical across runs).
type TaintSnifferFn func(content string) []TaintMatch

// taintRegistry holds the registered per-language taint sniffers.
// Populated via init() in each per-language taint_sites_*.go file.
var taintRegistry = map[string]TaintSnifferFn{}

// RegisterTaintSniffer installs a per-language taint sniffer. Mirrors
// RegisterEffectSniffer; the two registries are independent so a
// language can ship one without the other (T1 ships both; T2 ships
// effects first, then taint in a follow-up issue).
func RegisterTaintSniffer(lang string, fn TaintSnifferFn) {
	if lang == "" || fn == nil {
		return
	}
	taintRegistry[lang] = fn
}

// TaintSnifferFor returns the per-language taint sniffer, or nil when
// none is registered. Callers must nil-check before invocation.
func TaintSnifferFor(lang string) TaintSnifferFn {
	return taintRegistry[lang]
}

// TaintLanguages returns the slugs of every registered taint-sniffer
// language in sorted order.
func TaintLanguages() []string {
	out := make([]string, 0, len(taintRegistry))
	for k := range taintRegistry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
