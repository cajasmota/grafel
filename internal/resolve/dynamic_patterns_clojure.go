package resolve

import "regexp"

// clojureDynamicPatterns are per-language patterns for Clojure.
// Registered via init() into dynamicPatternsByLang.
//
// Clojure dynamic-pattern catalog (issue #44 — Clojure resolver slice).
//
// The Clojure extractor (internal/extractors/clojure/clojure.go) uses a
// hand-rolled paren walker (no tree-sitter grammar exists for Clojure in
// smacker/go-tree-sitter). It emits CALLS edges whose ToID is the first
// symbol in any s-expression that is NOT a known special form.
//
// Three categories drive the bulk of unresolved Clojure edges:
//
//  1. Clojure core BIFs — functions that are part of clojure.core and
//     are never indexed as in-tree entities. Examples: get-in, assoc,
//     swap!, count, filter, filterv, first, reduce-kv, map?, boolean,
//     re-matches. These appear in every Clojure codebase and are
//     statically unresolvable because clojure.core is an external
//     dependency, not source code indexed by archigraph.
//
//  2. Ring / HTTP response helpers — ring.util.response functions
//     (response, status, not-found, created, redirect) that are brought
//     into scope via :refer and appear as bare symbols. The Ring library
//     is a Clojars dependency, never indexed in-tree.
//
//  3. Namespace-qualified stdlib calls — dotted forms like
//     `codec/form-decode-map`, `req/character-encoding`,
//     `log/error`, `str/split`, `str/join` where the extractor
//     emits the full qualified name (alias/fn). The receiver alias
//     is included in ToID (unlike the Elixir extractor which strips
//     it), so patterns must match the full qualified form.
//
// All patterns are gated to lang=="clojure" so they cannot fire on stubs
// from Go, Python, Ruby, TypeScript, or any other language (safer-bias
// rule #94). Cross-language collisions that motivated this gate:
// `filter` (Python builtin), `first` (Rust Iterator), `count` (SQL/Go),
// `assoc` (Python dict), `status` (HTTP framework method in any language),
// `response` (HTTP framework in any language), `boolean` (Java primitive).
var clojureDynamicPatterns = []*regexp.Regexp{
	// ── Clojure core BIFs: map/sequence operations ──────────────────
	// These are clojure.core functions emitted as bare CALLS targets.
	// The graph never contains an entity named "map" or "filter" because
	// clojure.core is a Clojars dependency, not indexed source.
	regexp.MustCompile(`^map$`),
	regexp.MustCompile(`^filter$`),
	regexp.MustCompile(`^filterv$`),
	regexp.MustCompile(`^reduce$`),
	regexp.MustCompile(`^reduce-kv$`),
	regexp.MustCompile(`^mapv$`),
	regexp.MustCompile(`^keep$`),
	regexp.MustCompile(`^keep-indexed$`),
	regexp.MustCompile(`^remove$`),
	regexp.MustCompile(`^into$`),
	regexp.MustCompile(`^concat$`),
	regexp.MustCompile(`^flatten$`),
	regexp.MustCompile(`^distinct$`),
	regexp.MustCompile(`^sort$`),
	regexp.MustCompile(`^sort-by$`),
	regexp.MustCompile(`^group-by$`),
	regexp.MustCompile(`^partition$`),
	regexp.MustCompile(`^partition-by$`),
	regexp.MustCompile(`^take$`),
	regexp.MustCompile(`^take-while$`),
	regexp.MustCompile(`^drop$`),
	regexp.MustCompile(`^drop-while$`),
	regexp.MustCompile(`^first$`),
	regexp.MustCompile(`^second$`),
	regexp.MustCompile(`^last$`),
	regexp.MustCompile(`^rest$`),
	regexp.MustCompile(`^next$`),
	regexp.MustCompile(`^nth$`),
	regexp.MustCompile(`^count$`),
	regexp.MustCompile(`^empty\?$`),
	regexp.MustCompile(`^seq$`),
	regexp.MustCompile(`^vec$`),
	regexp.MustCompile(`^set$`),
	regexp.MustCompile(`^list$`),
	regexp.MustCompile(`^vector$`),
	regexp.MustCompile(`^hash-map$`),
	regexp.MustCompile(`^zipmap$`),
	regexp.MustCompile(`^frequencies$`),
	regexp.MustCompile(`^interleave$`),
	regexp.MustCompile(`^interpose$`),
	regexp.MustCompile(`^range$`),
	regexp.MustCompile(`^repeat$`),
	regexp.MustCompile(`^repeatedly$`),
	regexp.MustCompile(`^iterate$`),

	// ── Clojure core BIFs: map/assoc operations ─────────────────────
	regexp.MustCompile(`^assoc$`),
	regexp.MustCompile(`^assoc-in$`),
	regexp.MustCompile(`^dissoc$`),
	regexp.MustCompile(`^update$`),
	regexp.MustCompile(`^update-in$`),
	regexp.MustCompile(`^merge$`),
	regexp.MustCompile(`^merge-with$`),
	regexp.MustCompile(`^get$`),
	regexp.MustCompile(`^get-in$`),
	regexp.MustCompile(`^contains\?$`),
	regexp.MustCompile(`^keys$`),
	regexp.MustCompile(`^vals$`),
	regexp.MustCompile(`^select-keys$`),
	regexp.MustCompile(`^rename-keys$`),
	regexp.MustCompile(`^map\?$`),
	regexp.MustCompile(`^vector\?$`),
	regexp.MustCompile(`^sequential\?$`),
	regexp.MustCompile(`^coll\?$`),
	regexp.MustCompile(`^set\?$`),

	// ── Clojure core BIFs: atom/state operations ────────────────────
	// swap!, reset!, deref, atom — unresolvable because clojure.core
	// is external. The `!` suffix makes these Clojure-specific enough
	// that cross-language gate risk is negligible.
	regexp.MustCompile(`^swap!$`),
	regexp.MustCompile(`^reset!$`),
	regexp.MustCompile(`^atom$`),
	regexp.MustCompile(`^deref$`),
	regexp.MustCompile(`^compare-and-set!$`),

	// ── Clojure core BIFs: arithmetic / type predicates ─────────────
	regexp.MustCompile(`^boolean$`),
	regexp.MustCompile(`^int$`),
	regexp.MustCompile(`^long$`),
	regexp.MustCompile(`^double$`),
	regexp.MustCompile(`^float$`),
	regexp.MustCompile(`^str$`),
	regexp.MustCompile(`^keyword$`),
	regexp.MustCompile(`^symbol$`),
	regexp.MustCompile(`^name$`),
	regexp.MustCompile(`^namespace$`),
	regexp.MustCompile(`^type$`),
	regexp.MustCompile(`^class$`),
	regexp.MustCompile(`^nil\?$`),
	regexp.MustCompile(`^some\?$`),
	regexp.MustCompile(`^true\?$`),
	regexp.MustCompile(`^false\?$`),
	regexp.MustCompile(`^number\?$`),
	regexp.MustCompile(`^string\?$`),
	regexp.MustCompile(`^keyword\?$`),
	regexp.MustCompile(`^symbol\?$`),
	regexp.MustCompile(`^fn\?$`),
	regexp.MustCompile(`^ifn\?$`),
	regexp.MustCompile(`^pos\?$`),
	regexp.MustCompile(`^neg\?$`),
	regexp.MustCompile(`^zero\?$`),
	regexp.MustCompile(`^even\?$`),
	regexp.MustCompile(`^odd\?$`),
	regexp.MustCompile(`^re-matches$`),
	regexp.MustCompile(`^re-find$`),
	regexp.MustCompile(`^re-seq$`),
	regexp.MustCompile(`^re-pattern$`),
	regexp.MustCompile(`^format$`),
	regexp.MustCompile(`^printf$`),
	regexp.MustCompile(`^println$`),
	regexp.MustCompile(`^prn$`),
	regexp.MustCompile(`^print$`),
	regexp.MustCompile(`^slurp$`),
	regexp.MustCompile(`^spit$`),
	regexp.MustCompile(`^read-string$`),
	regexp.MustCompile(`^pr-str$`),
	regexp.MustCompile(`^apply$`),
	regexp.MustCompile(`^partial$`),
	regexp.MustCompile(`^comp$`),
	regexp.MustCompile(`^juxt$`),
	regexp.MustCompile(`^memoize$`),
	regexp.MustCompile(`^identity$`),
	regexp.MustCompile(`^constantly$`),
	regexp.MustCompile(`^fnil$`),
	regexp.MustCompile(`^nthrest$`),
	regexp.MustCompile(`^split-at$`),
	regexp.MustCompile(`^split-with$`),
	regexp.MustCompile(`^flatten$`),
	regexp.MustCompile(`^conj$`),
	regexp.MustCompile(`^cons$`),
	regexp.MustCompile(`^disj$`),
	regexp.MustCompile(`^peek$`),
	regexp.MustCompile(`^pop$`),
	regexp.MustCompile(`^max$`),
	regexp.MustCompile(`^min$`),
	regexp.MustCompile(`^inc$`),
	regexp.MustCompile(`^dec$`),
	regexp.MustCompile(`^abs$`),
	regexp.MustCompile(`^mod$`),
	regexp.MustCompile(`^quot$`),
	regexp.MustCompile(`^rem$`),
	regexp.MustCompile(`^gcd$`),
	regexp.MustCompile(`^rand$`),
	regexp.MustCompile(`^rand-int$`),
	regexp.MustCompile(`^rand-nth$`),
	regexp.MustCompile(`^shuffle$`),
	regexp.MustCompile(`^not=$`),
	regexp.MustCompile(`^compare$`),

	// ── Ring / Pedestal HTTP helpers ─────────────────────────────────
	// ring.util.response functions referred in via :refer [...].
	// The Ring library is a Clojars dependency not indexed in-tree.
	// `response`, `status`, `not-found`, `created`, `redirect` are the
	// top Ring helpers emitted as bare CALLS targets in corpus data.
	regexp.MustCompile(`^response$`),
	regexp.MustCompile(`^not-found$`),
	regexp.MustCompile(`^created$`),
	regexp.MustCompile(`^ok$`),
	regexp.MustCompile(`^bad-request$`),
	regexp.MustCompile(`^unauthorized$`),
	regexp.MustCompile(`^forbidden$`),
	regexp.MustCompile(`^conflict$`),
	regexp.MustCompile(`^redirect$`),
	regexp.MustCompile(`^redirect-after-post$`),
	regexp.MustCompile(`^header$`),
	regexp.MustCompile(`^content-type$`),
	regexp.MustCompile(`^charset$`),
	// ring.middleware.* helpers injected by wrap-* middleware wrappers.
	regexp.MustCompile(`^wrap-params$`),
	regexp.MustCompile(`^wrap-json-body$`),
	regexp.MustCompile(`^wrap-json-response$`),
	regexp.MustCompile(`^wrap-defaults$`),
	regexp.MustCompile(`^wrap-cors$`),
	regexp.MustCompile(`^wrap-auth$`),
	regexp.MustCompile(`^wrap-error-handler$`),
	regexp.MustCompile(`^wrap-reload$`),
	regexp.MustCompile(`^wrap-resource$`),
	regexp.MustCompile(`^wrap-content-type$`),
	regexp.MustCompile(`^wrap-not-modified$`),
	regexp.MustCompile(`^wrap-head$`),
	regexp.MustCompile(`^wrap-session$`),

	// ── Namespace-qualified stdlib calls ────────────────────────────
	// The Clojure extractor emits the full `alias/fn` form as ToID.
	// These are the most common qualified calls observed in the corpus.
	// All are from standard Clojure namespaces required with :as aliases.
	// str/* — clojure.string
	regexp.MustCompile(`^str/split$`),
	regexp.MustCompile(`^str/join$`),
	regexp.MustCompile(`^str/trim$`),
	regexp.MustCompile(`^str/upper-case$`),
	regexp.MustCompile(`^str/lower-case$`),
	regexp.MustCompile(`^str/starts-with\?$`),
	regexp.MustCompile(`^str/ends-with\?$`),
	regexp.MustCompile(`^str/includes\?$`),
	regexp.MustCompile(`^str/replace$`),
	regexp.MustCompile(`^str/blank\?$`),
	// log/* — clojure.tools.logging
	regexp.MustCompile(`^log/debug$`),
	regexp.MustCompile(`^log/info$`),
	regexp.MustCompile(`^log/warn$`),
	regexp.MustCompile(`^log/error$`),
	regexp.MustCompile(`^log/fatal$`),
	// codec/* — ring.util.codec
	regexp.MustCompile(`^codec/form-decode-map$`),
	regexp.MustCompile(`^codec/form-encode$`),
	regexp.MustCompile(`^codec/url-encode$`),
	regexp.MustCompile(`^codec/url-decode$`),
	// req/* — ring.util.request
	regexp.MustCompile(`^req/character-encoding$`),
	regexp.MustCompile(`^req/urlencoded-form\?$`),
	regexp.MustCompile(`^req/body-string$`),
	// json/* — cheshire / clojure.data.json
	regexp.MustCompile(`^json/generate-string$`),
	regexp.MustCompile(`^json/parse-string$`),
	regexp.MustCompile(`^json/encode$`),
	regexp.MustCompile(`^json/decode$`),

	// ── core.async channels ─────────────────────────────────────────
	// core.async is a standard Clojure library for CSP-style concurrency.
	// chan, go, <!, >!, put!, take!, close!, alt!, alts! are macros/fns
	// emitted as bare CALLS targets. They are unresolvable because
	// clojure.core.async is a Clojars dependency not indexed in-tree.
	// `chan`, `go`, `close!` are Clojure-specific enough for the gate.
	regexp.MustCompile(`^chan$`),
	regexp.MustCompile(`^close!$`),
	regexp.MustCompile(`^put!$`),
	regexp.MustCompile(`^take!$`),
	regexp.MustCompile(`^alt!$`),
	regexp.MustCompile(`^alts!$`),
	regexp.MustCompile(`^alt!!$`),
	regexp.MustCompile(`^alts!!$`),
	regexp.MustCompile(`^go-loop$`),
	regexp.MustCompile(`^pipeline$`),
	regexp.MustCompile(`^pipeline-async$`),
	regexp.MustCompile(`^onto-chan!$`),
	regexp.MustCompile(`^to-chan!$`),
	regexp.MustCompile(`^<!!$`),
	regexp.MustCompile(`^>!!$`),

	// ── Java interop BIFs ────────────────────────────────────────────
	// Java interop forms that reach the resolver as CALLS stubs.
	// Integer/parseInt, Double/parseDouble are qualified Java-class calls.
	// The Clojure extractor preserves the Class/method form in ToID.
	regexp.MustCompile(`^Integer/parseInt$`),
	regexp.MustCompile(`^Integer/valueOf$`),
	regexp.MustCompile(`^Long/parseLong$`),
	regexp.MustCompile(`^Double/parseDouble$`),
	regexp.MustCompile(`^System/currentTimeMillis$`),
	regexp.MustCompile(`^System/getenv$`),
	regexp.MustCompile(`^System/exit$`),
	regexp.MustCompile(`^Math/floor$`),
	regexp.MustCompile(`^Math/ceil$`),
	regexp.MustCompile(`^Math/abs$`),
	regexp.MustCompile(`^Math/max$`),
	regexp.MustCompile(`^Math/min$`),
	regexp.MustCompile(`^Math/round$`),
	regexp.MustCompile(`^Thread/sleep$`),
	regexp.MustCompile(`^UUID/randomUUID$`),
}

func init() {
	dynamicPatternsByLang["clojure"] = clojureDynamicPatterns
}
