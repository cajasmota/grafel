package engine_test

// languageRuleGap records one reviewed exception to the rule-coverage guard in
// language_rule_coverage_6537_test.go: a language the classifier routes and an
// extractor handles, for which no YAML framework rule can fire.
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

// knownLanguageRuleGaps is the reviewed list of classifier-routed,
// extractor-backed languages for which `d.CompiledRuleCount(lang) == 0` — no
// YAML framework rule can fire on a file of that language, whatever it
// contains. VB.NET (#6535, #6537) was one of these and nobody knew, because
// nothing in the tree enumerated the set. This list is that enumeration.
//
// HOW THIS LIST WAS PRODUCED (2026-08-25, #6537 Arm A)
//
// It is not hand-collected. The guard derives its population from two live
// sources — classifier.RoutedLanguagesForTest() (69 language keys the router
// can emit) intersected with extractor.List() (the registry populated by the
// blank import of internal/extractors) — giving 62 languages that an indexed
// file can actually reach Pass 2.5 as. Of those, 47 compile zero actionable
// rules. Those 47 are transcribed here with the reason each was found to have,
// and nothing else is exempt.
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
//	(A) BUCKET PRESENT, DOC-ONLY. internal/engine/rules/<lang>/frameworks/
//	    exists and loads, but every rule file in it declares
//	    `source_patterns: []`, `relationship_rules: []` and
//	    `file_conventions: []`. The YAML documents a framework's import
//	    markers and packaging for human readers; Detect can emit nothing from
//	    it. This is the shape a "does the bucket exist?" check would have
//	    called covered — 12 of the 47 are here, which is why the guard counts
//	    actionable rules instead.
//	(B) BUCKET DIRECTORY PRESENT, NOT LOADABLE. The directory exists but has
//	    no frameworks/orms/queues subdirectory, and the loader only walks
//	    those three (loader.go ruleSubdirs).
//	(C) NO BUCKET, GO CROSS-EXTRACTORS INSTEAD. Framework recognition for the
//	    language is implemented as Go extractors registered under custom_*
//	    keys rather than as YAML rules, so a zero here does not mean the
//	    language is unrecognised. The counts quoted are registry keys.
//	(D) NO BUCKET, NO FRAMEWORK RECOGNITION. The genuine gaps. VB.NET is one.
var knownLanguageRuleGaps = []languageRuleGap{
	// --- (A) bucket present, every rule file doc-only -----------------------
	{Language: "clojure", Reason: "A: rules/clojure/frameworks loads 22 rule files, all with empty source_patterns/relationship_rules/file_conventions"},
	{Language: "cpp", Reason: "A: rules/cpp loads 17 doc-only rule files; C++ framework recognition is implemented as ~30 custom_cpp_* Go extractors"},
	{Language: "css", Reason: "A: rules/css loads 12 doc-only rule files"},
	{Language: "dart", Reason: "A: rules/dart loads 22 doc-only rule files; see also 3 custom_dart_* Go extractors"},
	{Language: "elixir", Reason: "A: rules/elixir loads 16 doc-only rule files; Phoenix/Ecto recognition is 13 custom_elixir_* Go extractors"},
	{Language: "groovy", Reason: "A: rules/groovy loads 8 doc-only rule files; see also 2 custom_groovy_* Go extractors"},
	{Language: "haskell", Reason: "A: rules/haskell loads 6 doc-only rule files"},
	{Language: "hcl", Reason: "A: rules/hcl loads 6 doc-only rule files"},
	{Language: "lua", Reason: "A: rules/lua loads 9 doc-only rule files; Kong/routing recognition is 8 lua_* Go extractors"},
	{Language: "shell", Reason: "A: rules/shell loads 9 doc-only rule files"},
	{Language: "sql", Reason: "A: rules/sql loads 9 doc-only rule files"},
	{Language: "zig", Reason: "A: rules/zig loads 10 doc-only rule files"},

	// --- (B) bucket directory present but nothing loadable ------------------
	{Language: "protobuf", Reason: "B: rules/protobuf holds only root-level YAML (_manifest/complexity/extras); the loader walks frameworks/orms/queues only, so the bucket contributes nothing"},

	// --- (C) no bucket; recognition lives in Go cross-extractors ------------
	{Language: "crystal", Reason: "C: no rules/crystal bucket; ORM and test recognition is 6 custom_crystal_* Go extractors"},
	{Language: "erlang", Reason: "C: no rules/erlang bucket; recognition is 2 custom_erlang_* Go extractors"},
	{Language: "fsharp", Reason: "C: no rules/fsharp bucket; recognition is 2 custom_fsharp_* Go extractors"},
	{Language: "nim", Reason: "C: no rules/nim bucket; ORM/migration recognition is 10 custom_nim_* Go extractors"},

	// --- (D) no bucket, no framework recognition ----------------------------
	// The reported instance. Arm B of #6537 decides alias-vs-own-bucket and
	// lands the content; when it does, this entry must be deleted or the guard
	// fails.
	{Language: "vbnet", Reason: "D: no rules/vbnet bucket and no VB.NET cross-extractor; measured as 0.0% framework annotation across 45,663 entities on a real WinForms codebase", Issue: "#6537"},

	{Language: "assembly", Reason: "D: no rule bucket; no framework layer is expected for assembly"},
	{Language: "astro", Reason: "D: no rules/astro bucket; Astro is itself the framework, and its integrations are unrecognised"},
	{Language: "avro", Reason: "D: no rule bucket; schema IDL with no framework layer"},
	{Language: "bicep", Reason: "D: no rules/bicep bucket; Azure resource recognition is unimplemented"},
	{Language: "c", Reason: "D: no rules/c bucket; the cpp bucket is a distinct key and is not aliased onto c, so C files reach no rule at all"},
	{Language: "cobol", Reason: "D: no rules/cobol bucket; mainframe framework recognition is unimplemented"},
	{Language: "commonlisp", Reason: "D: no rule bucket"},
	{Language: "elm", Reason: "D: no rules/elm bucket"},
	{Language: "fish", Reason: "D: no rules/fish bucket; the shell bucket is a distinct key and is not aliased onto fish"},
	{Language: "html", Reason: "D: no rules/html bucket; rules/html_templates exists but is deliberately NOT aliased onto any language key (see TestDormant3593_AliasMapWiring)"},
	{Language: "idris", Reason: "D: no rule bucket"},
	{Language: "jcl", Reason: "D: no rules/jcl bucket; mainframe job control has no framework layer modelled"},
	{Language: "just", Reason: "D: no rule bucket; task-runner recipes carry no framework"},
	{Language: "markdown", Reason: "D: no rules/markdown bucket; docs carry no framework"},
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
	{Language: "systemverilog", Reason: "D: no rule bucket; HDL has no framework layer modelled"},
	{Language: "terraform", Reason: "D: no rules/terraform bucket; rules/hcl is a distinct key, is not aliased onto terraform, and is doc-only in any case"},
	{Language: "verilog", Reason: "D: no rule bucket; HDL has no framework layer modelled"},
	{Language: "vhdl", Reason: "D: no rule bucket; HDL has no framework layer modelled"},
	{Language: "vue", Reason: "D: no rules/vue bucket; Vue recognition lives under javascript_typescript, which is not aliased onto vue"},
}
