package engine_test

// languageRuleGap records one reviewed exception to the rule-coverage guard in
// language_rule_coverage_6537_test.go: a language the classifier routes and the
// indexer hands to Detect, for which no YAML framework rule can fire.
type languageRuleGap struct {
	// Language is the language key as the classifier produces it and the
	// detector looks it up (file.Language / d.compiled[...]).
	Language string
	// Reason is why the gap is tolerated. Required — the guard fails on an
	// entry whose Reason is empty.
	Reason string
	// Issue optionally references the issue tracking the gap's closure.
	Issue string
}

// knownLanguageRuleGaps is the reviewed list of producible languages for which
// `d.CompiledRuleCount(lang) == 0` — no YAML framework rule can fire on a file
// of that language, whatever it contains. VB.NET (#6535, #6537) was one of
// these and nobody knew, because nothing in the tree enumerated the set. This
// list is that enumeration.
//
// HOW THIS LIST WAS PRODUCED (2026-08-25, #6537 Arm A)
//
// It is not hand-collected. The guard's population is every language key an
// indexed file can carry into Pass 2.5: classifier.RoutedLanguagesForTest()
// (69 keys, derived from the classifier's own routing tables) plus the three
// keys those tables do not enumerate (swift_package, json, jsonschema), each
// re-verified against the live classifier. That is 72. Of those, 56 compile
// zero actionable rules, and only 16 compile any.
//
// The population is deliberately NOT filtered by the extractor registry.
// cmd/grafel/index.go calls Detect on every classified file (built at
// index.go:3981 from any non-empty cr.Language) without consulting the
// registry, so a language with no extractor still reaches the detector — the
// classifier says exactly this for nginx/caddy at classifier.go:648-652.
// Filtering would have hidden nine of the 56, including objective_c, perl and
// r, whose buckets load 11/4/4 rule files that emit nothing.
//
// IT IS INTENDED TO SHRINK. Deleting an entry is how a gap gets closed: once a
// language compiles a rule, the guard fails until its entry is removed.
//
// ADDING an entry is a review decision, not a formality. A new language that
// ships with no rule bucket cannot merge without appearing here, in the same
// commit, with a reason — which is precisely the step VB.NET skipped.
//
// The reasons fall into four observed shapes:
//
//	(A) BUCKET PRESENT, DOC-ONLY. internal/engine/rules/<lang>/ has a
//	    frameworks/orms/queues subdirectory whose files load, but none of them
//	    contributes a source pattern, relationship rule or file convention —
//	    most omit those schema keys entirely, a few declare them empty. The
//	    YAML documents a framework's import markers and packaging for human
//	    readers; Detect can emit nothing from it. This is the shape a "does the
//	    bucket exist?" check would have called covered — 15 of the 56 are here,
//	    which is why the guard counts actionable rules instead.
//	(B) BUCKET DIRECTORY PRESENT, NOT LOADABLE. The directory exists but has
//	    no frameworks/orms/queues subdirectory, and the loader only walks
//	    those three (loader.go ruleSubdirs).
//	(C) NO BUCKET, GO CROSS-EXTRACTORS INSTEAD. Framework recognition for the
//	    language is implemented as Go extractors registered under custom_*
//	    keys rather than as YAML rules, so a zero here does not mean the
//	    language is unrecognised. The counts quoted are registry keys; nothing
//	    here verifies those extractors fire.
//	(D) NO BUCKET, NO FRAMEWORK RECOGNITION. The genuine gaps. VB.NET is one.
var knownLanguageRuleGaps = []languageRuleGap{
	// --- (A) bucket present, no rule file contributes anything actionable ---
	{Language: "clojure", Reason: "A: rules/clojure loads 22 rule files, none contributing a source pattern, relationship rule or file convention"},
	{Language: "cpp", Reason: "A: rules/cpp loads 17 non-actionable rule files; C++ framework recognition is implemented as 30 custom_cpp_* Go extractors"},
	{Language: "css", Reason: "A: rules/css loads 12 non-actionable rule files"},
	{Language: "dart", Reason: "A: rules/dart loads 22 non-actionable rule files; see also 3 custom_dart_* Go extractors"},
	{Language: "elixir", Reason: "A: rules/elixir loads 16 non-actionable rule files; Phoenix/Ecto recognition is 13 custom_elixir_* Go extractors"},
	{Language: "groovy", Reason: "A: rules/groovy loads 8 non-actionable rule files; see also 2 custom_groovy_* Go extractors"},
	{Language: "haskell", Reason: "A: rules/haskell loads 6 non-actionable rule files"},
	{Language: "hcl", Reason: "A: rules/hcl loads 6 non-actionable rule files"},
	{Language: "lua", Reason: "A: rules/lua loads 9 non-actionable rule files; Kong/routing recognition is 7 lua_* Go extractors"},
	{Language: "objective_c", Reason: "A: rules/objective_c loads 11 non-actionable rule files (7 frameworks + orms)"},
	{Language: "perl", Reason: "A: rules/perl loads 4 non-actionable rule files (orms only)"},
	{Language: "r", Reason: "A: rules/r loads 4 non-actionable rule files (orms only)"},
	{Language: "shell", Reason: "A: rules/shell loads 9 non-actionable rule files"},
	{Language: "sql", Reason: "A: rules/sql loads 9 non-actionable rule files"},
	{Language: "zig", Reason: "A: rules/zig loads 10 non-actionable rule files"},

	// --- (B) bucket directory present but nothing loadable ------------------
	{Language: "protobuf", Reason: "B: rules/protobuf holds only root-level YAML (_manifest/complexity/extras); the loader walks frameworks/orms/queues only, so the bucket contributes nothing"},

	// --- (C) no bucket; recognition lives in Go cross-extractors ------------
	{Language: "crystal", Reason: "C: no rules/crystal bucket; ORM and test recognition is 6 custom_crystal_* Go extractors"},
	{Language: "erlang", Reason: "C: no rules/erlang bucket; recognition is 2 custom_erlang_* Go extractors"},
	{Language: "fsharp", Reason: "C: no rules/fsharp bucket; recognition is 2 custom_fsharp_* Go extractors"},
	{Language: "nim", Reason: "C: no rules/nim bucket; ORM/migration recognition is 10 custom_nim_* Go extractors"},

	// --- (D) no bucket, no framework recognition ----------------------------
	// vbnet USED to be listed here — the reported instance from #6535. Arm B of
	// #6537 measured alias-vs-own-bucket (an alias onto csharp emits 0 entities
	// on VB source) and landed rules/vbnet/frameworks/winforms.yaml, so the gap
	// is closed and the entry is gone. See vbnet_winforms_rules_6537_test.go.
	{Language: "assembly", Reason: "D: no rule bucket; no framework layer is expected for assembly"},
	{Language: "astro", Reason: "D: no rules/astro bucket; Astro is itself the framework, and its integrations are unrecognised"},
	{Language: "avro", Reason: "D: no rule bucket; schema IDL with no framework layer"},
	{Language: "bicep", Reason: "D: no rules/bicep bucket; Azure resource recognition is unimplemented"},
	{Language: "c", Reason: "D: no rules/c bucket; the cpp bucket is a distinct key and is itself non-actionable, so nothing would be gained by sharing it"},
	{Language: "caddy", Reason: "D: no rule bucket; routed with no extractor but still handed to Detect, where Caddyfile topology is parsed by the deployment-topology pass rather than by rules"},
	{Language: "cobol", Reason: "D: no rules/cobol bucket; mainframe framework recognition is unimplemented"},
	{Language: "commonlisp", Reason: "D: no rule bucket"},
	{Language: "elm", Reason: "D: no rules/elm bucket"},
	{Language: "fish", Reason: "D: no rules/fish bucket; the shell bucket is a distinct key, is not aliased onto fish, and is itself non-actionable"},
	{Language: "html", Reason: "D: no rules/html bucket; rules/html_templates exists but is deliberately NOT aliased onto any language key (see TestDormant3593_AliasMapWiring)"},
	{Language: "idris", Reason: "D: no rule bucket"},
	{Language: "jcl", Reason: "D: no rules/jcl bucket; mainframe job control has no framework layer modelled"},
	{Language: "json", Reason: "D: no rule bucket; the narrow .json routing (OpenAPI/Ocelot/Debezium) exists to feed synthesizers and the API-gateway pass, not YAML rules"},
	{Language: "jsonschema", Reason: "D: no rule bucket; *.schema.json is routed to the content-sniffing jsonschema extractor, with no framework layer"},
	{Language: "just", Reason: "D: no rule bucket; task-runner recipes carry no framework"},
	{Language: "markdown", Reason: "D: no rules/markdown bucket; docs carry no framework"},
	{Language: "nginx", Reason: "D: no rule bucket; routed with no extractor but still handed to Detect, where nginx upstream/proxy_pass topology is parsed by the deployment-topology pass rather than by rules"},
	{Language: "ocaml", Reason: "D: no rule bucket"},
	{Language: "pony", Reason: "D: no rule bucket"},
	{Language: "racket", Reason: "D: no rule bucket"},
	{Language: "razor", Reason: "D: no rules/razor bucket; Blazor/Razor recognition exists only under the csharp key, which is not aliased onto razor"},
	{Language: "reasonml", Reason: "D: no rule bucket"},
	{Language: "rescript", Reason: "D: no rules/rescript bucket"},
	{Language: "scheme", Reason: "D: no rule bucket"},
	{Language: "sml", Reason: "D: no rule bucket"},
	{Language: "solidity", Reason: "D: no rules/solidity bucket; OpenZeppelin/Hardhat recognition is unimplemented"},
	{Language: "svelte", Reason: "D: no rules/svelte bucket; SvelteKit recognition lives under javascript_typescript, which is not aliased onto svelte"},
	{Language: "swift_package", Reason: "D: no rule bucket; Package.swift is routed to its own key so the manifest extractor can claim it, and the swift bucket is not aliased onto it"},
	{Language: "systemverilog", Reason: "D: no rule bucket; HDL has no framework layer modelled"},
	{Language: "terraform", Reason: "D: no rules/terraform bucket; rules/hcl is a distinct key, is not aliased onto terraform, and is non-actionable in any case"},
	{Language: "toml", Reason: "D: no rule bucket; routed with no extractor, and TOML manifests are consumed by the cross-manifest extractor rather than by rules"},
	{Language: "verilog", Reason: "D: no rule bucket; HDL has no framework layer modelled"},
	{Language: "vhdl", Reason: "D: no rule bucket; HDL has no framework layer modelled"},
	{Language: "vue", Reason: "D: no rules/vue bucket; Vue recognition lives under javascript_typescript, which is not aliased onto vue"},
}

// closedLanguageRuleGaps is the other half of the ratchet. knownLanguageRuleGaps
// shrinks as gaps close, but once an entry leaves it, NOTHING watches that
// language any more — the guard only iterates the gap list, so a closed gap
// reopening is invisible to it.
//
// This list is where a closed gap goes. Membership is checked directly against
// d.CompiledRuleCount, not derived from the gap list, so it is reachable code:
// a language here that stops compiling rules fails
// TestLanguageRuleGaps6537_ClosedGapsStayClosed by name.
//
// (Review of PR #6600: the first attempt at this handshake inverted the
// assertion inside TestLanguageRuleCoverage6537_ListIsCurrent — "vbnet must not
// appear in stillOpen". stillOpen is built by iterating knownLanguageRuleGaps,
// so once vbnet was removed from that list the condition was unsatisfiable
// under every input and deleting the whole block survived the suite. It was a
// comment attached to dead code. This is the reachable version.)
var closedLanguageRuleGaps = []languageRuleGap{
	{Language: "vbnet", Reason: "closed by #6537 Arm B: rules/vbnet/frameworks/winforms.yaml; an alias onto csharp was measured first and emitted 0 entities on VB source", Issue: "#6537"},
}
