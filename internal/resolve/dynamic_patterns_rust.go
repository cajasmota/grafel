package resolve

import "regexp"

// rustDynamicPatterns is the per-language dynamic-dispatch pattern catalog for
// Rust. Matches here tag a stub as DispositionDynamic.
//
// Rust dynamic-pattern catalog (issue #44 slice-7).
//
// The Rust extractor emits two shapes that can never be resolved
// internally and land in bug-extractor without these patterns:
//
//  1. Bare channel-constructor names.  `mpsc::channel::<String>(8)` is
//     a `generic_function > scoped_identifier` call site; the extractor
//     strips the `mpsc::` module path and emits the bare leaf `channel`.
//     The same pattern covers `oneshot::channel`, `broadcast::channel`,
//     and `watch::channel` — all tokio / std concurrency primitives with
//     no in-tree entity.
//
//  2. Generic-receiver method calls.  When a local variable `rx` is
//     declared as `Receiver<String>` (or any `Type<T>`) the extractor
//     emits the stub as `Receiver<String>.recv` — the fully-qualified
//     receiver shape including the generic argument.  No in-tree entity
//     can ever satisfy that exact to_id because the resolver's bare-name
//     lookup strips generics before matching.  These are stdlib / tokio
//     concrete-type method calls (recv, send, close, poll, next, …) that
//     are intrinsically unresolvable statically — Dynamic is correct.
//
// Both patterns are gated to lang=="rust" so they cannot fire on
// PascalCase method names in other languages (safer-bias rule #94).
var rustDynamicPatterns = []*regexp.Regexp{
	// Bare channel-constructor stubs: mpsc::channel, oneshot::channel,
	// broadcast::channel, watch::channel, etc. The extractor preserves
	// only the leaf name after the last `::`.
	regexp.MustCompile(`^channel$`),
	// Generic-receiver method stubs: `Type<T>.method` where the type
	// carries at least one generic argument.  The extractor qualifies the
	// call with the full concrete receiver type including `<...>` which
	// the resolver cannot match against any entity.  The inner generic
	// argument may itself be generic (`Arc<Mutex<State>>`), so the match
	// uses a greedy `.+` inside `<…>` rather than `[^>]+`.
	regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*<.+>\.[a-z_][A-Za-z0-9_]*$`),
}

func init() {
	dynamicPatternsByLang["rust"] = rustDynamicPatterns
}
