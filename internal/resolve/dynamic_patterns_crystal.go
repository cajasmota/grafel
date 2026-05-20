package resolve

import "regexp"

// crystalDynamicPatterns are per-language patterns for Crystal.
// Registered via init() into dynamicPatternsByLang.
//
// Categories:
//
//  1. Relative-path require ToIDs — the Crystal extractor emits the raw
//     string literal from `require "..."` as the IMPORTS ToID.
//     Paths starting with `./`, `../` or bare names like `"http/server"`
//     are not in-tree entity names; mark Dynamic.
//
//  2. Kemal web-framework route helpers — `get`, `post`, `put`, `patch`,
//     `delete`, `ws` are top-level Kemal DSL calls that appear inside
//     route blocks. The extractor strips the block to a bare CALLS edge;
//     static binding to a Kemal framework entity is impossible.
//
//  3. Crystal stdlib method names stripped from receivers — called on
//     Array / Hash / String / Int / IO objects after the extractor drops
//     the receiver. These are Crystal built-in methods, not user code.
//
//  4. Concurrency / fiber primitives — `spawn`, `Channel::Unbuffered`,
//     `select` and similar concurrency idioms that appear as bare calls.
var crystalDynamicPatterns = []*regexp.Regexp{
	// ── 1. Require path ToIDs ─────────────────────────────────────────────
	// Relative imports: `require "./foo"`, `require "../bar/baz"`
	regexp.MustCompile(`^\.{1,2}/`),
	// Standard-library shards: `require "http/server"`, `require "json"`,
	// `require "yaml"`, etc. — no in-tree entity.
	regexp.MustCompile(`^[a-z][a-z0-9_]*/`), // path-style shard require
	regexp.MustCompile(`^[a-z][a-z0-9_]*$`), // bare shard name

	// ── 2. Kemal DSL route helpers ────────────────────────────────────────
	// `get "/path" do |env| ... end` — HTTP verb handlers.
	regexp.MustCompile(`^get$`),
	regexp.MustCompile(`^post$`),
	regexp.MustCompile(`^put$`),
	regexp.MustCompile(`^patch$`),
	regexp.MustCompile(`^delete$`),
	regexp.MustCompile(`^ws$`),    // WebSocket handler
	regexp.MustCompile(`^error$`), // Kemal error handler
	// `before_get`, `after_all`, etc. — Kemal middleware callbacks.
	regexp.MustCompile(`^(?:before|after)_(?:get|post|put|patch|delete|all)$`),
	// `Kemal.run` / `add_handler` — framework lifecycle.
	regexp.MustCompile(`^run$`),
	regexp.MustCompile(`^add_handler$`),
	regexp.MustCompile(`^serve_static$`),

	// ── 3. Amber framework helpers (common alternative to Kemal) ─────────
	// `pipeline`, `scope`, `routes` — Amber router DSL.
	regexp.MustCompile(`^pipeline$`),
	regexp.MustCompile(`^scope$`),
	regexp.MustCompile(`^routes$`),
	// `plug` — Amber pipeline plug.
	regexp.MustCompile(`^plug$`),
	// `render` — controller render call.
	regexp.MustCompile(`^render$`),
	// `redirect_to` — controller redirect.
	regexp.MustCompile(`^redirect_to$`),
	// `params` — controller params accessor.
	regexp.MustCompile(`^params$`),
	// `respond_with` — Amber content-type helper.
	regexp.MustCompile(`^respond_with$`),

	// ── 4. Crystal stdlib Array / Enumerable methods ──────────────────────
	// Called on Array(T) / Enumerable(T) receivers; receiver stripped.
	regexp.MustCompile(`^map$`),
	regexp.MustCompile(`^flat_map$`),
	regexp.MustCompile(`^select$`), // also Crystal's concurrency keyword
	regexp.MustCompile(`^reject$`),
	regexp.MustCompile(`^each$`),
	regexp.MustCompile(`^each_with_index$`),
	regexp.MustCompile(`^each_with_object$`),
	regexp.MustCompile(`^reduce$`),
	regexp.MustCompile(`^inject$`),
	regexp.MustCompile(`^find$`),
	regexp.MustCompile(`^first$`),
	regexp.MustCompile(`^last$`),
	regexp.MustCompile(`^any\?$`),
	regexp.MustCompile(`^all\?$`),
	regexp.MustCompile(`^none\?$`),
	regexp.MustCompile(`^count$`),
	regexp.MustCompile(`^sum$`),
	regexp.MustCompile(`^min$`),
	regexp.MustCompile(`^max$`),
	regexp.MustCompile(`^min_by$`),
	regexp.MustCompile(`^max_by$`),
	regexp.MustCompile(`^sort$`),
	regexp.MustCompile(`^sort_by$`),
	regexp.MustCompile(`^uniq$`),
	regexp.MustCompile(`^flatten$`),
	regexp.MustCompile(`^compact$`),
	regexp.MustCompile(`^zip$`),
	regexp.MustCompile(`^push$`),
	regexp.MustCompile(`^pop$`),
	regexp.MustCompile(`^shift$`),
	regexp.MustCompile(`^unshift$`),
	regexp.MustCompile(`^concat$`),
	regexp.MustCompile(`^include\?$`),
	regexp.MustCompile(`^empty\?$`),
	regexp.MustCompile(`^size$`),
	regexp.MustCompile(`^length$`),

	// ── 5. Crystal stdlib Hash methods ───────────────────────────────────
	regexp.MustCompile(`^fetch$`),
	regexp.MustCompile(`^has_key\?$`),
	regexp.MustCompile(`^has_value\?$`),
	regexp.MustCompile(`^keys$`),
	regexp.MustCompile(`^values$`),
	regexp.MustCompile(`^merge$`),
	regexp.MustCompile(`^merge!$`),
	regexp.MustCompile(`^delete$`),
	regexp.MustCompile(`^each_key$`),
	regexp.MustCompile(`^each_value$`),
	regexp.MustCompile(`^each_with_object$`),
	regexp.MustCompile(`^to_a$`),

	// ── 6. Crystal stdlib String methods ─────────────────────────────────
	regexp.MustCompile(`^split$`),
	regexp.MustCompile(`^join$`),
	regexp.MustCompile(`^strip$`),
	regexp.MustCompile(`^lstrip$`),
	regexp.MustCompile(`^rstrip$`),
	regexp.MustCompile(`^upcase$`),
	regexp.MustCompile(`^downcase$`),
	regexp.MustCompile(`^capitalize$`),
	regexp.MustCompile(`^chars$`),
	regexp.MustCompile(`^bytes$`),
	regexp.MustCompile(`^lines$`),
	regexp.MustCompile(`^gsub$`),
	regexp.MustCompile(`^sub$`),
	regexp.MustCompile(`^includes\?$`),
	regexp.MustCompile(`^starts_with\?$`),
	regexp.MustCompile(`^ends_with\?$`),
	regexp.MustCompile(`^index$`),
	regexp.MustCompile(`^rindex$`),
	regexp.MustCompile(`^to_i$`),
	regexp.MustCompile(`^to_f$`),
	regexp.MustCompile(`^to_s$`),
	regexp.MustCompile(`^to_json$`),
	regexp.MustCompile(`^from_json$`),
	regexp.MustCompile(`^to_yaml$`),
	regexp.MustCompile(`^from_yaml$`),
	regexp.MustCompile(`^not_nil!$`),
	regexp.MustCompile(`^as$`),
	regexp.MustCompile(`^as\?$`),
	regexp.MustCompile(`^is_a\?$`),
	regexp.MustCompile(`^responds_to\?$`),
	regexp.MustCompile(`^nil\?$`),

	// ── 7. IO / print methods ─────────────────────────────────────────────
	regexp.MustCompile(`^puts$`),
	regexp.MustCompile(`^print$`),
	regexp.MustCompile(`^p$`),
	regexp.MustCompile(`^pp$`),
	regexp.MustCompile(`^gets$`),
	regexp.MustCompile(`^read$`),
	regexp.MustCompile(`^write$`),
	regexp.MustCompile(`^flush$`),

	// ── 8. Concurrency / fiber primitives ────────────────────────────────
	regexp.MustCompile(`^spawn$`),
	regexp.MustCompile(`^sleep$`),
	regexp.MustCompile(`^send$`),
	regexp.MustCompile(`^receive$`),
	regexp.MustCompile(`^receive\?$`),
	regexp.MustCompile(`^close$`),
	regexp.MustCompile(`^closed\?$`),
	// `Fiber.yield` / `Fiber.current` stripped to bare `yield`/`current`.
	regexp.MustCompile(`^yield$`),

	// ── 9. Crystal macro / annotation helpers ────────────────────────────
	// These appear as CALLS targets from def bodies that invoke macro methods.
	regexp.MustCompile(`^getter$`),
	regexp.MustCompile(`^setter$`),
	regexp.MustCompile(`^property$`),
	regexp.MustCompile(`^getter!$`),
	regexp.MustCompile(`^property!$`),
	regexp.MustCompile(`^delegate$`),
	regexp.MustCompile(`^forward_missing_to$`),
	regexp.MustCompile(`^record$`),
	regexp.MustCompile(`^class_getter$`),
	regexp.MustCompile(`^class_setter$`),
	regexp.MustCompile(`^class_property$`),

	// ── 10. Exception / error handling ───────────────────────────────────
	regexp.MustCompile(`^raise$`),
	regexp.MustCompile(`^rescue$`),
	regexp.MustCompile(`^ensure$`),
	regexp.MustCompile(`^message$`),
	regexp.MustCompile(`^backtrace$`),
}

func init() {
	dynamicPatternsByLang["crystal"] = crystalDynamicPatterns
}
