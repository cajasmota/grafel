<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.clojure.base` — Clojure (base language)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [clojure](../by-language/clojure.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-06-12` | 4910 | `internal/extractors/clojure/clojure.go`<br>`internal/extractors/clojure/relationships_test.go` | CALLS edges are mined from each defn/defmacro body by collectCalls: the call head of every (callee args …) form is captured (callHeadRE), special forms (let/if/fn/do/-> … + def* definers) and self-recursion are filtered (clojureSpecialForms, mirroring the java/kotlin dedup set), string literals and ;-comments are scrubbed (stripStringsAndComments) so they don't pollute heads, and each surviving head is deduped and stamped Properties["line"] (1-based, counted within the body). Namespace-qualified heads (str/join, java.util.Date.) are preserved verbatim for the resolver. Honest exclusion: heads beginning with a digit and bare-data forms are dropped. |
| Core extraction | ✅ `full` | `2026-06-12` | 4910 | `internal/extractors/clojure/clojure.go`<br>`internal/extractors/clojure/clojure_test.go`<br>`internal/extractors/clojure/relationships_test.go` | Regex + hand-rolled paren-walker extractor (no tree-sitter Clojure grammar bundled in smacker/go-tree-sitter — cf. cobol/verilog precedent). Emits: (defn / defn- ...) → SCOPE.Operation(subtype=function) (defnRE); (defmacro ...) → SCOPE.Operation(subtype=macro) (#4910, defmacroRE — macros are Clojure's primary extension mechanism, modelled as operations so they carry CALLS + CONTAINS like any defn, proven by TestClojureExtractor_Macros); (defrecord / defprotocol / deftype / defmulti / definterface ...) → SCOPE.Component(subtype=class) (deftypeRE) — these ARE the Clojure type-system constructs (defprotocol = interface, defrecord/deftype = nominal types), so Type System interface/type extraction is satisfied at the base level here rather than via per-framework cells; (ns NAME ...) → SCOPE.Component(subtype=namespace) with CONTAINS edges to every top-level defn/defmacro/defrecord/… (findNsForm + BuildOperationStructuralRef). Proven by TestClojureExtractor_Functions / _TypeDefinitions / _Macros / _LineNumbers. Honest follow-ups (#4910 tail): top-level (def NAME val) vars/constants are NOT yet emitted as SCOPE.Constant (only in the special-form drop list); defmethod bodies are not attached to their defmulti; reader-conditional (.cljc) branch selection is not modelled. |
| Import resolution quality | ✅ `full` | — | 4910 | `internal/extractors/clojure/clojure.go`<br>`internal/extractors/clojure/relationships_test.go` | IMPORTS edges (#366 parity with java/kotlin/scala) are emitted file.Path → imported module for every entry in the enclosing (ns …) form's (:require …)/(:use …)/(:import …) sections (collectImports + findKeywordSections paren-balanced walk). Vector require shapes [ns :as alias] / [ns :refer [a b]] / [ns :refer :all] and bare-symbol forms are resolved to local_name/imported_name props; :use and :refer :all set wildcard=1; :import handles both bare java.util.Date and the [java.util Date Calendar] vector form (parseImportSection), expanding each class to its own edge with source_module. Proven by relationships_test.go. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.clojure.base ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
