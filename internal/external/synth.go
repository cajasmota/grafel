// Package external synthesises placeholder entities for references that
// point at code outside the indexed corpus — third-party packages
// (django, react, lodash...), language stdlib (os, json, fmt...), and
// well-known stdlib symbols (Println, print...).
//
// PORT-EXT (issue #32). After Pass 3 + the resolver (PORT-2-FIX,
// PORT-2-FIX-3) finish, a meaningful fraction of relationships still
// have stub strings as ToID — by construction, because the target
// source isn't in the corpus. They are nonetheless real graph edges
// the agent should be able to traverse and stop cleanly at. This pass
// turns each unique unresolved external into a placeholder Entity with
// id "ext:<canonical-name>" and rewrites the relationship's ToID to
// point at it.
package external

import (
	"net/url"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/types"
)

// KindExternal is the entity kind stamped on every synthesised
// placeholder. It joins the existing SCOPE.* taxonomy used elsewhere
// in the indexer. Kept as a string alias for callers; the source of
// truth is types.EntityKindExternal (Issue #77).
const KindExternal = string(types.EntityKindExternal)

// ExtIDPrefix is the deterministic prefix used by external-entity IDs.
// It is intentionally NOT a 16-char hex string so the resolver's
// isHexID heuristic continues to treat it as a stub-shaped value if a
// later pass ever encounters it.
const ExtIDPrefix = "ext:"

// Stats reports how the synthesis pass touched the document.
type Stats struct {
	// Synthesized is the number of NEW placeholder entities appended to
	// the document. Equal to UniqueExternals on a fresh run; zero on a
	// re-run because every external is already present.
	Synthesized int
	// RelationshipsResolved is the number of relationship endpoints
	// rewritten from a bare-name stub to "ext:<name>".
	RelationshipsResolved int
	// UniqueExternals is the number of distinct external names this
	// pass touched (including any that were already present from a
	// previous run).
	UniqueExternals int
}

// Synthesize scans every relationship in doc, looks for endpoints
// whose ToID is a still-unresolved string that matches an external
// reference heuristic, and appends placeholder entities for each
// unique external. The relationship's ToID is rewritten in-place to
// "ext:<canonical-name>". Idempotent: calling Synthesize twice on the
// same document is a no-op on the second call.
func Synthesize(doc *graph.Document) Stats {
	if doc == nil {
		return Stats{}
	}

	// Build a set of all known entity IDs so we don't re-synthesise an
	// external that already exists in the document. Re-runs of this
	// pass on the same document must be idempotent.
	known := make(map[string]bool, len(doc.Entities))
	// entityLang maps every entity ID to its declared language so the
	// classifier can apply per-language bare-name allowlists (issue #103
	// — Go stdlib/framework Pascal-case method names that arrive at the
	// resolver after the extractor strips the receiver). Lookups against
	// non-existent IDs return "" (the zero value), which falls back to
	// the language-agnostic stop-list — matching pre-#103 behaviour for
	// every relationship whose FromID isn't a known entity.
	entityLang := make(map[string]string, len(doc.Entities))
	// entityFile maps every entity ID to its declared SourceFile so the
	// classifier can apply file-path-gated bare-name allowlists (issue
	// #115 — Go testify helpers like Equal/NoError that are receiver-
	// stripped by the extractor and would collide trivially with user
	// methods if classified globally). Only test-file callers (paths
	// ending in `_test.go`) are eligible. Lookups against non-existent
	// IDs return "" — matching pre-#115 behaviour.
	entityFile := make(map[string]string, len(doc.Entities))
	for k := range doc.Entities {
		known[doc.Entities[k].ID] = true
		entityLang[doc.Entities[k].ID] = doc.Entities[k].Language
		entityFile[doc.Entities[k].ID] = doc.Entities[k].SourceFile
	}

	// fileImports maps every source file path to the set of import paths
	// that file declares, walking IMPORTS edges (FromID = filePath, ToID
	// = imported package). Used by the classifier to file-path-gate
	// import-aware bare-name allowlists (issue #131 — chi router methods
	// like Get/Post/Put/Delete that collide with HTTP-verb generic getter
	// names and must only classify when the source file actually imports
	// `github.com/go-chi/chi`). Lookups against non-existent paths return
	// nil — matching pre-#131 behaviour for files that import nothing.
	fileImports := make(map[string]map[string]bool)
	for k := range doc.Relationships {
		rel := &doc.Relationships[k]
		if rel.Kind != string(types.RelationshipKindImports) {
			continue
		}
		if rel.FromID == "" || rel.ToID == "" {
			continue
		}
		set, ok := fileImports[rel.FromID]
		if !ok {
			set = make(map[string]bool)
			fileImports[rel.FromID] = set
		}
		set[rel.ToID] = true
	}

	// First pass — collect every unique external name we want to
	// synthesise. The placeholder carries a subtype hint
	// ("package"/"function") but the language field is left empty:
	// we don't reliably know the source language at this layer (a name
	// like "json" or "abc" exists in multiple ecosystems), and an
	// inaccurate language tag is worse than none at all. Language can
	// be populated by a downstream enrichment pass that has more
	// context (e.g. inspecting the import statement that produced the
	// edge).
	type externalInfo struct {
		canonical string
		subtype   string
		language  string
	}
	uniques := make(map[string]externalInfo) // ext-id -> info
	resolved := 0

	for k := range doc.Relationships {
		rel := &doc.Relationships[k]
		if rel.ToID == "" || isHexID(rel.ToID) || strings.HasPrefix(rel.ToID, ExtIDPrefix) {
			continue
		}
		canonical, subtype, ok := classifyExternal(rel.ToID, rel.Kind, entityLang[rel.FromID], entityFile[rel.FromID], fileImports[entityFile[rel.FromID]])
		if !ok {
			continue
		}
		extID := ExtIDPrefix + canonical
		if _, seen := uniques[extID]; !seen {
			uniques[extID] = externalInfo{
				canonical: canonical,
				subtype:   subtype,
				language:  "",
			}
		}
		rel.ToID = extID
		resolved++
	}

	// Sort canonical names for deterministic append order — keeps
	// graph.json byte-stable across runs on the same corpus.
	keys := make([]string, 0, len(uniques))
	for k := range uniques {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	synthesised := 0
	for _, extID := range keys {
		if known[extID] {
			continue // re-run path: placeholder already present
		}
		info := uniques[extID]
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:            extID,
			Name:          info.canonical,
			QualifiedName: info.canonical,
			Kind:          KindExternal,
			Subtype:       info.subtype,
			SourceFile:    "",
			Language:      info.language,
			Metadata: map[string]interface{}{
				"is_external":    true,
				"discovered_via": "ext-synthesis",
			},
		})
		known[extID] = true
		synthesised++
	}

	// Reflect the new entities + rewritten edges in the doc-level
	// stats. Relationships count is unchanged (we rewrote endpoints,
	// not added rows) but Entities grew by len(synthesised).
	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)

	return Stats{
		Synthesized:           synthesised,
		RelationshipsResolved: resolved,
		UniqueExternals:       len(uniques),
	}
}

// classifyExternal decides whether a stub-shaped ToID looks like an
// external reference, and if so returns the canonical name we should
// use for the placeholder entity.
//
// Heuristics, in order:
//
//  1. "Kind:Name" form where Name matches a well-known external —
//     canonicalise to Name (drop the kind prefix).
//  2. Bare names matching a stdlib stop-list (Println, print, etc.) —
//     canonicalise to the bare name.
//  3. Bare names matching a known third-party package allowlist
//     (django, react, lodash, ...).
//  4. Import-shaped paths whose first segment matches the allowlist
//     (e.g. "django.db.models" → "django").
//
// Returns ("", "", false) when the stub doesn't look external — those
// are left untouched and continue to count as "unmatched" in the
// resolver stats.
func classifyExternal(stub, relKind, lang, fromFile string, fromImports map[string]bool) (canonical, subtype string, ok bool) {
	if stub == "" {
		return "", "", false
	}

	// Issue #89 — manifest extractor emits dependency stubs as
	// "scope:component:external_dep:<package_manager>:<package>". The
	// "external_dep" tag is the extractor's explicit signal that this is
	// an external (third-party) dependency; route it to a placeholder so
	// the post-synthesis classifier lands it in external-known/external-
	// unknown rather than bug-extractor.
	if strings.HasPrefix(stub, "scope:component:external_dep:") {
		rest := stub[len("scope:component:external_dep:"):]
		// rest is "<pm>:<package>" — drop the package-manager segment.
		if i := strings.IndexByte(rest, ':'); i > 0 && i < len(rest)-1 {
			pkg := strings.TrimSpace(rest[i+1:])
			if pkg != "" && !strings.ContainsAny(pkg, "/\\") {
				root := pkg
				if dot := strings.IndexByte(pkg, '.'); dot > 0 {
					root = pkg[:dot]
				}
				return root, "package", true
			}
		}
	}

	// Issue #89 — httpclient extractor emits external HTTP API references
	// as "scope:external_api:<url>". The URL is the unresolvable identity
	// of the external service; canonicalise to the host segment when we
	// can extract one (everything between "://" and the next "/"), and
	// fall back to a synthetic "external_api" bucket otherwise. Either
	// way it leaves bug-extractor.
	if strings.HasPrefix(stub, "scope:external_api:") {
		raw := stub[len("scope:external_api:"):]
		host := externalAPIHost(raw)
		if host != "" {
			return host, "external_api", true
		}
		// Bare URL fragment / non-URL identifier — bucket under a
		// stable "external_api" placeholder rather than leaving it as
		// bug-extractor.
		return "external_api", "external_api", true
	}

	// Pass 3 cross-language extractors emit external imports as
	// "scope:<kind>:import:external:<name>" — short structural-ref
	// form that the resolver leaves untouched (it expects 6 segments).
	// Recognise it explicitly here; the trailing segment is the
	// canonical package name.
	if strings.HasPrefix(stub, "scope:") && strings.Contains(stub, ":external:") {
		if idx := strings.LastIndex(stub, ":external:"); idx >= 0 {
			ext := stub[idx+len(":external:"):]
			ext = strings.TrimSpace(ext)
			if ext == "" || strings.ContainsAny(ext, "/\\") {
				return "", "", false
			}
			root := ext
			if dot := strings.IndexByte(ext, '.'); dot > 0 {
				root = ext[:dot]
			}
			// Trust the extractor's "external" tag — emit a placeholder
			// even when the package isn't on our static allowlist. The
			// extractor has already classified it as not-local.
			return root, "package", true
		}
	}

	// Issue #82: Format A structural-refs that survived the resolver are
	// dangling by definition (the resolver rewrites resolved endpoints
	// to hex IDs). For EXTENDS edges from cross/hierarchy, the tail is
	// the parent class name — when it looks like an external import
	// (dotted, e.g. "serializers.ModelSerializer" or "rest_framework.
	// generics.ListAPIView"), synthesise a placeholder for the package
	// root. Bare-name tails are intentionally NOT handled here because
	// they could be either a local class or an external base — the
	// existing allowlist branch below already catches the well-known
	// cases.
	//
	// Format A: scope:<kind>:<subtype>:<lang>:<file_path>:<name>
	// We pull the trailing segment after the last ':' (file paths in
	// archigraph entity refs are normalised to forward slashes, so the
	// last ':' is the kind/name separator, not part of the path).
	if strings.HasPrefix(stub, "scope:") {
		if idx := strings.LastIndexByte(stub, ':'); idx >= 0 && idx < len(stub)-1 {
			tail := stub[idx+1:]
			if looksLikeExternalImport(tail) {
				root := tail
				if dot := strings.IndexByte(tail, '.'); dot > 0 {
					root = tail[:dot]
				}
				return root, "package", true
			}
		}
	}

	// Issue #101 — Rust `use foo::bar` style paths use `::` as the
	// segment separator. The Rust extractor (internal/extractors/rust)
	// emits IMPORTS edges with ToID set to the raw use-path, e.g.
	// "tokio::net::TcpListener" or brace-group forms like
	// "actix_web::{App, HttpResponse}". Without this branch the leading
	// "tokio" / "actix_web" gets misread as a "Kind:" prefix below, and
	// the residue ":net::TcpListener" never matches the allowlist —
	// every Rust use-statement lands in bug-extractor.
	//
	// Detect `::` early: take the first segment as the root crate, look
	// it up against the same allowlist used for dotted paths, and
	// collapse to a single placeholder per crate (matching the dotted-
	// path "package" subtype convention).
	if idx := strings.Index(stub, "::"); idx > 0 {
		root := stub[:idx]
		// Reject if the root contains anything that isn't a Rust ident
		// char. Path separators here mean a structural-ref or local
		// path slipped through; '@' / '.' are not legal in a Rust crate
		// name and indicate a different ecosystem.
		if isRustCrateIdent(root) && isKnownExternalPackage(root) {
			return root, "package", true
		}
	}

	// Issue #116 — Go full-import-path stubs (`net/http`,
	// `encoding/json`, `github.com/stretchr/testify/assert`,
	// `golang.org/x/sync/errgroup`, `gopkg.in/yaml.v3`) use `/` as the
	// path separator. Without this branch the path-separator rejection
	// below drops every `use`-shaped Go import into bug-extractor.
	//
	// Detect Go-shaped import paths early: split on `/`, and for stdlib
	// packages canonicalise to the root segment (allowlist match yields
	// ExternalKnown); for host-prefixed paths canonicalise to the
	// `<host>/<owner>/<repo>` triple (or `<host>/<repo>` for gopkg.in)
	// — allowlist-matched yields ExternalKnown, unknown still moves out
	// of bug-extractor as ExternalUnknown via the resolver's
	// IsKnownExternalPackage gate.
	//
	// Not lang-gated: in real corpora the relationship's FromID often
	// points at a file-scope structural-ref ("scope:component:file:
	// auth.go") that isn't in the entity map, so entityLang lookup
	// returns "" and a Go-only gate would skip every edge from a file-
	// scope source. The shape predicate is restrictive enough on its
	// own — leading char must be a-z and the path must contain `/`
	// without `:`, `\`, whitespace, or a leading `/`, which rules out
	// Unix file paths, structural-refs, and URL fragments. Mirrors the
	// Rust `::` and PHP `\` branches, which also classify on shape
	// alone (issues #101, #102).
	if isGoImportPath(stub) {
		segs := strings.Split(stub, "/")
		first := segs[0]
		// Host-prefixed: github.com/<owner>/<repo>/..., golang.org/x/<repo>/...,
		// gopkg.in/<pkg>.<vN>/...
		if isGoImportHost(first) {
			canonical := goHostCanonical(segs)
			if canonical != "" {
				return canonical, "package", true
			}
		}
		// Stdlib: root segment matched against allowlist.
		if isKnownExternalPackage(first) {
			return first, "package", true
		}
	}

	// Issue #102 — PHP `use Foo\Bar\Baz` style FQNs use `\` as the
	// namespace separator. Without this branch the path-separator
	// rejection below drops every `Symfony\Component\HttpFoundation\
	// Response`, `Doctrine\ORM\EntityManager`, etc. into bug-extractor.
	//
	// Detect `\` early: take the first segment as the root namespace,
	// gate it on the PHP-namespace ident shape (PascalCase ASCII), and
	// look up against the allowlist. Project-internal roots like
	// `App\*` are not on the allowlist and correctly fall through to
	// remain unresolved (project-aware resolution is out of scope).
	if idx := strings.IndexByte(stub, '\\'); idx > 0 {
		root := stub[:idx]
		if isPhpNamespaceIdent(root) && isKnownExternalPackage(root) {
			// Canonicalise to lowercase — the placeholder convention is
			// "ext:<lowercase>" across ecosystems (django, tokio, ...);
			// PHP namespace roots are the only ones that arrive
			// PascalCase, so we fold here rather than at the lookup site.
			return strings.ToLower(root), "package", true
		}
	}

	// Strip a leading "Kind:" prefix if present — e.g. "Module:django"
	// or "Function:Println". The remainder is what we classify.
	name := stub
	if i := strings.IndexByte(stub, ':'); i > 0 {
		// Only treat the prefix as a kind hint when it's a short
		// alphabetic word; otherwise keep the whole stub (e.g.
		// "scope:..." structural-refs were already handled by the
		// resolver and shouldn't end up here).
		prefix := stub[:i]
		if isKindLikePrefix(prefix) {
			name = stub[i+1:]
		} else {
			return "", "", false
		}
	}
	if name == "" {
		return "", "", false
	}

	// Scoped npm packages — "@scope/pkg" or "@scope/pkg/subpath" — are
	// the only legitimate external shape that contains a '/'. Detect
	// them BEFORE the path-separator rejection below so they reach the
	// allowlist; everything else with a separator is a structural-ref
	// or local file path and is dropped (issue #71).
	if scope, ok := scopedNpmRoot(name); ok {
		// Collapse to "@scope/pkg" form (drop any deeper subpath) for
		// allowlist lookup, then to the scope itself if the full form
		// isn't catalogued. Either match yields a single placeholder
		// per scoped package.
		if isKnownExternalPackage(scope) {
			return scope, "package", true
		}
		return "", "", false
	}

	// Reject obviously non-external shapes: anything containing a path
	// separator was either a structural-ref or a local file path, both
	// already handled upstream.
	if strings.ContainsAny(name, "/\\") {
		return "", "", false
	}

	// Stdlib function stop-list — bare names like "Println", "print".
	if subtype, ok := stdlibFunction(name, lang, fromFile, fromImports); ok {
		return name, subtype, true
	}

	// Dotted path → first segment is what we canonicalise to. Common
	// shape for Python imports ("django.db.models" -> "django") or
	// JS submodules ("lodash.debounce" -> "lodash").
	root := name
	if dot := strings.IndexByte(name, '.'); dot > 0 {
		root = name[:dot]
	}

	if isKnownExternalPackage(root) {
		// "package" subtype when the canonical name IS the root,
		// otherwise "module" — django.db.models is a module of the
		// django package.
		if root == name {
			return root, "package", true
		}
		// Per the PORT-EXT spec we collapse to the package level so
		// there's a single placeholder per third-party package, not
		// one per imported submodule. Submodule fan-out can be
		// re-introduced in a follow-up.
		return root, "package", true
	}

	// Issue #120 — receiver-typed Java/Kotlin call where the leading
	// segment is the simple name of an imported external class.
	// `MockMvc.perform`, `RedirectAttributes.addFlashAttribute` etc.
	// resolve to a class name that the extractor's receiver binder
	// produced from a field/parameter type. When the from-file's
	// IMPORTS edges include a full path whose leaf is that class name
	// AND the path's allowlist-matching prefix is known external,
	// fold the call into that external package. Limited to lang=="java"
	// and lang=="kotlin" — both share the dotted import shape and the
	// "PascalCase leaf identifier == class" convention. Other ecosystems
	// fall through.
	if (lang == "java" || lang == "kotlin") && fromImports != nil {
		if dot := strings.IndexByte(name, '.'); dot > 0 {
			recv := name[:dot]
			for imp := range fromImports {
				// Match either a fully-qualified import ending in
				// ".<recv>" or a bare-name import == recv.
				if imp == recv || strings.HasSuffix(imp, "."+recv) {
					if longest := longestKnownDottedPrefix(imp); longest != "" {
						return longest, "package", true
					}
					break
				}
			}
		}
	}

	// Issue #120 — multi-segment Java / Kotlin / .NET package prefixes.
	// JVM and CLR dotted paths use a multi-word root convention
	// (`org.springframework.boot`, `com.fasterxml.jackson.databind`,
	// `org.apache.commons.lang3`) that doesn't fit the
	// single-first-segment heuristic above. Walk the dot-separated
	// prefixes from longest to shortest; the first match against the
	// allowlist canonicalises to that prefix. Bias toward longer
	// matches keeps `org` (an unrelated short name) from synthesising a
	// placeholder for `org.springframework.boot.SpringApplication`
	// while still folding every Spring submodule into a single
	// `ext:org.springframework` entity.
	if longest := longestKnownDottedPrefix(name); longest != "" {
		return longest, "package", true
	}

	return "", "", false
}

// longestKnownDottedPrefix walks the dot-separated prefixes of name
// from longest to shortest and returns the first one that
// isKnownExternalPackage recognises. Returns "" when no prefix is on
// the allowlist. Used by classifyExternal to match multi-word JVM /
// .NET roots (`org.springframework`, `com.fasterxml.jackson`) without
// requiring an exact-equals match against the full dotted path.
//
// The walk skips the full path itself when it has no dots (single
// identifier) — that case is already handled by the bare-name and
// stop-list branches in classifyExternal.
func longestKnownDottedPrefix(name string) string {
	if name == "" || !strings.ContainsRune(name, '.') {
		return ""
	}
	// Build prefixes longest-first by trimming the trailing segment.
	prefix := name
	for {
		if isKnownExternalPackage(prefix) {
			return prefix
		}
		dot := strings.LastIndexByte(prefix, '.')
		if dot <= 0 {
			return ""
		}
		prefix = prefix[:dot]
	}
}

// scopedNpmRoot recognises the npm scoped-package shape "@scope/pkg"
// (optionally followed by "/subpath") and returns the "@scope/pkg"
// root. Returns ("", false) when s doesn't match the scoped-npm
// convention — typical reject cases are bare names, "./relative",
// "/absolute", or backslash-bearing paths.
//
// The scope and package segments must each be non-empty and may
// contain only word chars, '-', and '.' — the npm name grammar's
// safe subset (https://docs.npmjs.com/cli/v10/configuring-npm/package-json#name).
func scopedNpmRoot(s string) (string, bool) {
	if len(s) < 4 || s[0] != '@' {
		return "", false
	}
	slash := strings.IndexByte(s, '/')
	if slash <= 1 {
		// Need at least one char after '@' before the '/'.
		return "", false
	}
	scope := s[1:slash]
	rest := s[slash+1:]
	if !isNpmSegment(scope) {
		return "", false
	}
	// Trim any sub-path after the package name.
	pkg := rest
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		pkg = rest[:i]
	}
	if !isNpmSegment(pkg) {
		return "", false
	}
	// Backslashes are never legal in an npm name.
	if strings.ContainsRune(s, '\\') {
		return "", false
	}
	return "@" + scope + "/" + pkg, true
}

// isNpmSegment reports whether s is a valid scope or package segment
// for the scoped-npm allowlist gate. Conservatively limited to
// [A-Za-z0-9_.-].
func isNpmSegment(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '-' || c == '.':
		default:
			return false
		}
	}
	return true
}

// stdlibFunction returns the subtype for a bare stdlib function name
// (e.g. "Println" → "function") or ("", false) when the name isn't on
// the small per-language stop-list. Kept deliberately small — v1.0
// catches the highest-volume bare-name calls without ballooning into
// a full stdlib catalogue.
func stdlibFunction(name, lang, fromFile string, fromImports map[string]bool) (string, bool) {
	if _, ok := stdlibBareNames[name]; ok {
		return "function", true
	}
	// Per-language allowlists — gated so a Go-only Pascal-case name like
	// "ServeHTTP" or "EncodeToString" doesn't shadow user-defined methods
	// in other ecosystems. Fall through to ("", false) when lang is empty
	// (relationships whose FromID isn't a known entity); the result
	// matches the pre-gating behaviour for those edges.
	if lang == "go" {
		if _, ok := goBareNames[name]; ok {
			return "function", true
		}
		// Issue #115 — testify helpers (Equal/NoError/Contains/Empty/...)
		// are receiver-stripped by the Go extractor (`assert.Equal(t, ...)`
		// → `Equal`) and collide trivially with user-defined methods on
		// any domain type. Gate the testify allowlist on BOTH lang=="go"
		// AND a `_test.go` file-path suffix on the caller — the suffix
		// rule is precise (not just "contains test"), and Go's build tool
		// already enforces that testify usage outside `_test.go` is rare
		// or wrong. A non-test caller named `Equal` falls through to the
		// generic allowlist and is left unresolved (the safer bias from
		// lesson #94 — a missed external is strictly better than a
		// synthesised placeholder shadowing a real user method).
		if strings.HasSuffix(fromFile, "_test.go") {
			if _, ok := goTestifyBareNames[name]; ok {
				return "function", true
			}
			// Issue #130 — testing.T helper methods (Helper/Cleanup/Setenv/
			// Logf/Fatal/Fatalf/Errorf/Run/...) are receiver-stripped by the
			// Go extractor (`t.Helper()` → `Helper`, `t.Run("sub", ...)` →
			// `Run`) and collide with user-defined methods. Gate on the same
			// `_test.go` suffix as testify: testing.T values exist outside
			// `_test.go` only in rare framework code, and the suffix check
			// keeps these names from shadowing user methods in production
			// `.go` files. Same safer-bias rule as #94.
			if _, ok := goTestingTBareNames[name]; ok {
				return "function", true
			}
		}
		// Issue #131 — go-chi router methods (Get/Post/Put/Delete/Mount/
		// Group/Route/Use/...) are receiver-stripped by the Go extractor
		// (`r.Get("/x", h)` → `Get`) and collide trivially with HTTP-verb
		// generic getters (`Repository.Get`, `Cache.Get`) that exist in
		// virtually every Go web codebase. Gate the chi-router allowlist
		// on BOTH lang=="go" AND a chi-package import edge from the source
		// file — the import gate is precise (the router type can only come
		// from the chi package) and falls through to the generic allowlist
		// for non-chi callers, matching the safer-bias rule from #94 (a
		// missed external is strictly better than a synthesised placeholder
		// shadowing a real user method).
		if hasGoChiImport(fromImports) {
			if _, ok := goChiRouterNames[name]; ok {
				return "function", true
			}
		}
	}
	if lang == "rust" {
		if _, ok := rustBareNames[name]; ok {
			return "function", true
		}
	}
	if lang == "java" {
		if _, ok := javaBareNames[name]; ok {
			return "function", true
		}
		// Issue #120 — JUnit/MockMvc/AssertJ/Mockito helpers that are
		// receiver-stripped by the Java extractor when the receiver
		// is the return value of a fluent-API call (e.g.
		// `mockMvc.perform(...).andExpect(status().isOk())` → bare
		// `andExpect`, `status`, `isOk`). The receiver type chain
		// can't be statically derived, so the extractor falls back to
		// the leaf method name. Gate the allowlist on a Java test-file
		// suffix (`Test.java` / `Tests.java` / `IT.java`) plus the
		// canonical Maven test source root so a same-named user
		// helper in production code does not get shadowed. Same
		// safer-bias rule the Go testify gate uses (issue #115).
		if isJavaTestFile(fromFile) {
			if _, ok := javaTestBareNames[name]; ok {
				return "function", true
			}
		}
	}
	if lang == "kotlin" {
		if _, ok := kotlinBareNames[name]; ok {
			return "function", true
		}
	}
	if lang == "ruby" {
		if _, ok := rubyBareNames[name]; ok {
			return "function", true
		}
	}
	if lang == "javascript" || lang == "typescript" {
		if _, ok := jsBareNames[name]; ok {
			return "function", true
		}
	}
	return "", false
}

// stdlibBareNames is the v1.0 stop-list of stdlib functions and
// builtins whose bare-name calls we want to surface as external
// nodes. The list is curated rather than exhaustive — only names
// that (a) appear with high frequency in real codebases and (b) are
// extremely unlikely to collide with a user-defined identifier are
// included. False positives synthesise a placeholder for a name that
// might have been a real local entity, which is worse than missing
// one.
var stdlibBareNames = map[string]struct{}{
	// Go fmt / built-in calls
	"Println": {},
	"Printf":  {},
	"Print":   {},
	"Sprintf": {},
	"Errorf":  {},
	"Fatal":   {},
	"Fatalf":  {},
	"Panic":   {},
	"Panicf":  {},
	// Python builtins (PEP 3102 / builtins module). Keep
	// alphabetical for review.
	"abs":       {},
	"all":       {},
	"any":       {},
	"bool":      {},
	"callable":  {},
	"chr":       {},
	"dict":      {},
	"enumerate": {},
	"filter":    {},
	"float":     {},
	"format":    {},
	"frozenset": {},
	// Reflection builtins (getattr/setattr/hasattr/delattr/eval/exec/
	// compile/__import__) deliberately excluded — they are dynamic-
	// dispatch primitives, not external imports. The resolver classifier
	// matches them against the per-language dynamic-pattern catalog and
	// tags them DispositionDynamic (Refs #95).
	"hash":       {},
	"id":         {},
	"int":        {},
	"isinstance": {},
	"issubclass": {},
	"iter":       {},
	"len":        {},
	"list":       {},
	"map":        {},
	"max":        {},
	"min":        {},
	"next":       {},
	"object":     {},
	"open":       {},
	"ord":        {},
	"print":      {},
	"property":   {},
	"range":      {},
	"repr":       {},
	"reversed":   {},
	"round":      {},
	"set":        {},
	"slice":      {},
	"sorted":     {},
	"str":        {},
	"sum":        {},
	"super":      {},
	"tuple":      {},
	"type":       {},
	"vars":       {},
	"zip":        {},
	// Python stdlib exceptions (extremely unlikely to collide).
	"Exception":           {},
	"ValueError":          {},
	"TypeError":           {},
	"KeyError":            {},
	"IndexError":          {},
	"AttributeError":      {},
	"RuntimeError":        {},
	"NotImplementedError": {},
	"StopIteration":       {},
	"FileNotFoundError":   {},
	// Django / DRF / Python framework symbols seen at high volume in
	// real codebases. Collisions with user code are possible but rare
	// (these are conventionally instantiated, not redefined).
	"Response":        {},
	"ValidationError": {},
	"NotFound":        {},
	"BeautifulSoup":   {},
	"BytesIO":         {},
	"StringIO":        {},
	"ObjectId":        {},
	// JS / browser
	"console": {},
	"fetch":   {},
	// Issue #89 — high-volume Python str/list/dict/set/file methods.
	// These bare-name calls arrive at the resolver after the extractor
	// strips the receiver (`s.append(x)` → `append`). Without this list
	// they all land in bug-extractor; with it they correctly classify as
	// external-known builtins.
	//
	// Issue #94 follow-up: removed names that collide with common
	// user-defined method identifiers — write/read/close/index/copy/
	// replace/items/keys/values/update/pop/clear/extend/append/remove.
	// Misclassifying a real local method as a stdlib bare-name turns a
	// genuine bug into a synthesised placeholder, hiding the real fix.
	// Kept: names that are unambiguously built-in across mainstream
	// Python codebases (no/extremely rare user-method collisions).
	"insert":     {},
	"setdefault": {},
	"startswith": {},
	"endswith":   {},
	"strip":      {},
	"lstrip":     {},
	"rstrip":     {},
	"split":      {},
	"rsplit":     {},
	"splitlines": {},
	"join":       {},
	"lower":      {},
	"upper":      {},
	"title":      {},
	"encode":     {},
	"decode":     {},
	"isdigit":    {},
	"isalpha":    {},
	"isalnum":    {},
	"readline":   {},
	"readlines":  {},
	"writelines": {},
	"flush":      {},
	"seek":       {},
	"tell":       {},
	// Python os/path/io stdlib functions seen at high volume in real
	// codebases — bare-name when accessed without a module qualifier.
	"getcwd":          {},
	"listdir":         {},
	"makedirs":        {},
	"deepcopy":        {},
	"deque":           {},
	"defaultdict":     {},
	"OrderedDict":     {},
	"Counter":         {},
	"namedtuple":      {},
	"RawConfigParser": {},
	"ConfigParser":    {},
	// Rust core builtins (Issue #91 — top Rust bare-name bug-extractors).
	// Conservative selection: only the assert_eq/assert_ne macros which
	// have no plausible collision with user identifiers in any language.
	// `Ok`/`Err`/`Some`/`None` deliberately NOT added: bare-name lookup
	// is global, and those identifiers commonly appear as user-defined
	// constants/variants in Go/JS codebases (#94 lesson — bias to misses).
	// Per-language Rust prelude allowlist filed as follow-up.
	"assert_eq": {},
	"assert_ne": {},
}

// goBareNames is the Go-language-gated bare-Pascal stop-list (issue
// #103). After the Go extractor strips the receiver from a method call
// (`w.Write(buf)` → `Write`, `r.Header().Set(...)` → `Header`), the
// resolver sees a bare PascalCase name that can't be matched to a local
// entity and lands in bug-extractor. These names are stdlib method
// identifiers from net/http, encoding/base64, encoding/hex, crypto/
// subtle, fmt, and strconv — high-volume in Go web codebases (gin/chi/
// echo) and extremely unlikely to be user-defined receiver methods on
// a Go type (PascalCase + multi-word + tied to specific stdlib APIs).
//
// Conservative selection rule: include only names that are unambiguous
// stdlib method identifiers OR have a strong stdlib idiom signature.
// Single-word framework verbs (Get, Post, Put, Delete, Use) are
// deliberately EXCLUDED — they collide trivially with user methods on
// any domain type (Repository.Get, Service.Use, etc.). When doubt
// exists about user-method collision, omit; a missed external is
// strictly better than a synthesised placeholder shadowing a real
// missing-resolution bug (lesson from #94).
var goBareNames = map[string]struct{}{
	// net/http server-side method receivers (ResponseWriter, Request,
	// Handler, Server). Multi-word PascalCase, deeply tied to net/http
	// — no plausible collision with user types.
	"ServeHTTP":      {},
	"ListenAndServe": {},
	"HandleFunc":     {},
	"WriteHeader":    {},
	// "Write" / "Header" / "Handle" deliberately omitted: they are
	// frequent user-method names (io.Writer.Write user-implementations,
	// custom Header() accessors, generic Handle handlers) and gating by
	// language alone is not enough to keep them safe.

	// encoding/base64, encoding/hex — package-level helpers commonly
	// invoked through a package-qualified call that the receiver-strip
	// reduces to a bare name.
	"EncodeToString": {},
	"DecodeString":   {},

	// crypto/subtle — single high-volume name, no plausible collision.
	"ConstantTimeCompare": {},

	// strconv — Pascal-case stdlib helpers. "Quote" is a common chi/gin
	// router-helper-adjacent call; Atoi/Itoa are stdlib-only idioms.
	"Atoi":  {},
	"Itoa":  {},
	"Quote": {},

	// Web-framework method names that are unlikely-as-user-methods
	// (multi-word PascalCase tied to gin/chi/echo handler types). Per
	// issue #103 hard rules: Get/Post/Put/Delete/Use are EXCLUDED.
	"MethodFunc":      {},
	"AbortWithStatus": {},
}

// goTestifyBareNames is the Go testify-helper bare-name stop-list (issue
// #115). The testify package (`github.com/stretchr/testify/assert` and
// `.../require`) exposes assertion helpers that are invoked through a
// receiver (`assert.Equal(t, ...)`, `require.NoError(t, err)`); the Go
// extractor strips that receiver, leaving the resolver with bare Pascal-
// case names like `Equal`, `NoError`, `Contains`. Many of these names
// (Equal in particular) collide trivially with user-defined methods on
// domain types in non-test code, so a language-only gate is not enough.
//
// Gating: lookups in this map are reached ONLY when (a) the source
// entity's language is "go" AND (b) the source entity's file path ends
// in `_test.go`. Both conditions are precise: the build tool restricts
// testify usage to `*_test.go` files in practice, and the suffix check
// (strings.HasSuffix, NOT strings.Contains) avoids false hits on paths
// like `internal/test/foo.go`.
//
// Conservative selection rule, mirroring the goBareNames spec: include
// only assertion helpers whose name is a strong testify idiom. EXCLUDED
// per the issue's hard rules: `Run` (subtest helper, but `t.Run` is the
// standard testing API and aliases trivially to user code), `New`,
// `Add`, `Set` (constructor / mutator names that collide with anything).
// `Contains` is included despite being slightly collision-prone because
// in `_test.go` context the testify identity dominates by orders of
// magnitude in real corpora.
var goTestifyBareNames = map[string]struct{}{
	// Equality and identity assertions.
	"Equal":       {},
	"NotEqual":    {},
	"EqualValues": {},
	"Same":        {},
	"NotSame":     {},

	// Error / nil assertions.
	"NoError": {},
	"Error":   {},
	"Nil":     {},
	"NotNil":  {},

	// Boolean assertions.
	"True":  {},
	"False": {},

	// Container assertions.
	"Empty":         {},
	"NotEmpty":      {},
	"Contains":      {},
	"NotContains":   {},
	"Len":           {},
	"Subset":        {},
	"ElementsMatch": {},

	// Ordering assertions.
	"Greater":        {},
	"Less":           {},
	"GreaterOrEqual": {},
	"LessOrEqual":    {},

	// Panic assertions.
	"Panics":          {},
	"NotPanics":       {},
	"PanicsWithError": {},

	// Type / interface assertions.
	"Implements": {},
	"IsType":     {},

	// Eventual / temporal assertions.
	"Eventually":     {},
	"Never":          {},
	"WithinDuration": {},

	// Encoding assertions.
	"JSONEq": {},
	"YAMLEq": {},

	// Filesystem assertions.
	"FileExists": {},
	"DirExists":  {},

	// httptest helper commonly imported alongside testify in `_test.go`.
	// Multi-word PascalCase, no plausible user-method collision.
	"NewRecorder": {},
}

// goTestingTBareNames is the Go stdlib `testing.T` helper-method bare-name
// stop-list (issue #130). The Go testing API exposes test-lifecycle helpers
// invoked through a *testing.T receiver (`t.Helper()`, `t.Cleanup(fn)`,
// `t.Setenv("K", "V")`, `t.Run("sub", subFn)`); the Go extractor strips
// the receiver and the resolver sees a bare PascalCase name (`Helper`,
// `Cleanup`, `Setenv`, `Run`, ...). Without an allowlist these names
// land in bug-extractor as unresolved CALLS edges in every Go test file.
//
// Gating: lookups in this map are reached ONLY when (a) the source
// entity's language is "go" AND (b) the source file path ends in
// `_test.go`. Both conditions are precise: `*testing.T` only exists in
// `_test.go` files in idiomatic Go, and the suffix check
// (strings.HasSuffix, NOT strings.Contains) avoids false hits on paths
// like `internal/test/foo.go` or `internal/testutil/util.go`.
//
// Conservative selection rule, mirroring goBareNames / goTestifyBareNames:
// include only names that are unambiguous testing.T method identifiers
// in test-file context. `Errorf`, `Fatal`, `Fatalf` are intentionally
// NOT duplicated here — stdlibBareNames classifies them globally before
// the lang=="go" switch. `Error` likewise classifies via
// goTestifyBareNames above. `Run` is
// collision-prone in general (`Server.Run`, `Worker.Run`) but the dual
// `_test.go` + `*testing.T`-context gate narrows the risk to the point
// where the testing.T identity dominates in real corpora.
var goTestingTBareNames = map[string]struct{}{
	// Test-lifecycle helpers — uniquely tied to the testing.TB / *testing.T
	// API. Multi-word or testing-idiom-only names with no plausible
	// collision against domain types.
	"Helper":   {},
	"Cleanup":  {},
	"Setenv":   {},
	"Parallel": {},
	"TempDir":  {},
	"Deadline": {},

	// Test-skipping helpers — testing-only verbs.
	"Skip":    {},
	"Skipf":   {},
	"SkipNow": {},

	// Test-failure helpers — `Fail` / `FailNow` are testing-only.
	// `Fatal` / `Fatalf` / `Errorf` are intentionally NOT duplicated
	// here — they already classify globally via stdlibBareNames (a
	// language-agnostic match that fires before the lang=="go" switch),
	// so adding them to this map would be dead code. Plain `Error` is
	// likewise omitted — already classified via goTestifyBareNames.
	"Fail":    {},
	"FailNow": {},

	// Logf is testing-only. Plain `Log` is collision-prone (logger.Log,
	// audit.Log) so it is INCLUDED only because the `_test.go` suffix
	// gate is strict — production loggers do not run in `_test.go` paths
	// in the dominant idiom. Same safer-bias trade-off as `Contains` in
	// goTestifyBareNames.
	"Log":  {},
	"Logf": {},

	// Subtest dispatcher. Collision-prone in general (Server.Run,
	// Command.Run) but `_test.go` + receiver-stripped from `*testing.T`
	// dominates in real corpora.
	"Run": {},

	// Misc context accessors on *testing.T.
	"Name":    {},
	"Context": {},
}

// goChiRouterNames is the go-chi router-method bare-name stop-list
// (issue #131). The chi router (`*chi.Mux` / `chi.Router`) exposes
// HTTP-verb registration methods (Get/Post/Put/Delete/...) plus
// router-composition methods (Mount/Group/Route/Use/With) that arrive
// at the resolver as bare names after the Go extractor strips the
// receiver (`r.Get("/x", h)` → `Get`). These names collide trivially
// with user-defined methods on domain types (Repository.Get,
// Service.Use, Cache.Delete) so a language-only gate is not enough.
//
// Gating: lookups in this map are reached ONLY when (a) the source
// entity's language is "go" AND (b) the source file imports the chi
// package (any of the four canonical import paths emitted by
// `hasGoChiImport`). Both conditions are precise: chi router values
// can only originate from a chi package import, and the post-#117
// allowlist already canonicalises chi imports to a known package node.
//
// The list mirrors chi's `Router` interface plus the small set of
// `Mux`-only methods (HandleFunc/Handle/NotFound/MethodNotAllowed)
// commonly invoked in routing setup. Excluded: `Method` and `MethodFunc`
// — `MethodFunc` is already covered by goBareNames as a multi-word
// PascalCase stdlib idiom (net/http via mux.HandleFunc family) and we
// don't want to widen the chi gate beyond chi-distinctive names.
var goChiRouterNames = map[string]struct{}{
	// HTTP verb registration on chi.Router. Single-word PascalCase that
	// shadows generic getters/setters in non-chi code — gated by import.
	"Get":     {},
	"Post":    {},
	"Put":     {},
	"Delete":  {},
	"Patch":   {},
	"Head":    {},
	"Options": {},
	"Connect": {},
	"Trace":   {},

	// Router composition / middleware. `Use` is especially collision-prone
	// (any plugin / middleware framework names this) so the import gate
	// is essential.
	"Mount": {},
	"Group": {},
	"Route": {},
	"Use":   {},
	"With":  {},

	// chi.Mux-specific dispatch helpers. These are less collision-prone
	// (HandleFunc is also in goBareNames as a net/http idiom) but kept
	// here so the chi gate covers the full Router-interface surface.
	"HandleFunc":       {},
	"Handle":           {},
	"NotFound":         {},
	"MethodNotAllowed": {},
}

// goChiImportPaths is the set of canonical import paths that signal a
// source file is using go-chi. The v5 path is the current default; the
// unversioned (chi v1/v2) + v3/v4/v5 paths cover legacy codebases. Note
// that chi v1.x and v2.x did not use module-path versioning, so they are
// covered transitively by the unversioned "github.com/go-chi/chi" path.
// Used by hasGoChiImport to gate goChiRouterNames lookups (issue #131).
var goChiImportPaths = map[string]bool{
	"github.com/go-chi/chi":    true,
	"github.com/go-chi/chi/v3": true,
	"github.com/go-chi/chi/v4": true,
	"github.com/go-chi/chi/v5": true,
}

// hasGoChiImport reports whether the source file's import set contains
// any of the canonical go-chi import paths. Returns false for a nil or
// empty set — falling through to the generic allowlist, which matches
// pre-#131 behaviour for files that don't use chi.
func hasGoChiImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range goChiImportPaths {
		if imports[p] {
			return true
		}
	}
	return false
}

// rustBareNames is the Rust-language-gated bare-name stop-list (issue
// #108). Rust's prelude implicitly imports a fixed set of identifiers
// (Ok, Err, Some, None, Box, Vec, Result, Option, ...) into every
// source file; the extractor sees them as bare PascalCase / snake_case
// calls without an import edge, and the resolver lands them in
// bug-extractor. They cannot live in the language-agnostic
// stdlibBareNames map because names like `Ok`, `Err`, `clone`, `vec`,
// `map`, `filter` are common user identifiers in Go/JS/Python (#94
// lesson — bias to misses over false-positive synthesis). Gating to
// lang="rust" keeps the resolution scoped to Rust source entities.
//
// Three categories are included: prelude PascalCase types & traits,
// prelude lowercase methods (post-receiver-strip from x.clone() →
// clone), and prelude macros (vec!, println!, format!).
//
// Names already covered by the language-agnostic stdlibBareNames map
// (filter, format, iter, len, map, print, zip, insert) are
// deliberately omitted here — they classify globally without needing
// the Rust gate.
var rustBareNames = map[string]struct{}{
	// Prelude PascalCase — types, enums, variants, and traits.
	"Ok":           {},
	"Err":          {},
	"Some":         {},
	"None":         {},
	"Box":          {},
	"Vec":          {},
	"Result":       {},
	"Option":       {},
	"String":       {},
	"Default":      {},
	"From":         {},
	"Into":         {},
	"TryFrom":      {},
	"TryInto":      {},
	"Iterator":     {},
	"IntoIterator": {},
	"ToString":     {},
	"ToOwned":      {},
	"Clone":        {},
	"Copy":         {},
	"Debug":        {},
	"Display":      {},
	"Send":         {},
	"Sync":         {},
	"Sized":        {},
	"Drop":         {},
	"Fn":           {},
	"FnMut":        {},
	"FnOnce":       {},

	// Prelude lowercase methods — post-receiver-strip (`opt.unwrap()` →
	// `unwrap`, `s.to_string()` → `to_string`). Risky names like
	// `clone`/`get`/`push`/`pop`/`count` are common user-method
	// identifiers in other languages, but the lang="rust" gate scopes
	// the rewrite to Rust source entities only.
	"clone":             {},
	"unwrap":            {},
	"unwrap_or":         {},
	"unwrap_or_default": {},
	"unwrap_or_else":    {},
	"expect":            {},
	"into":              {},
	"as_ref":            {},
	"as_mut":            {},
	"as_str":            {},
	"to_string":         {},
	"to_owned":          {},
	"into_iter":         {},
	"collect":           {},
	"fold":              {},
	"chain":             {},
	"count":             {},
	"is_empty":          {},
	"push":              {},
	"pop":               {},
	"remove":            {},
	"get":               {},
	"contains":          {},
	"is_some":           {},
	"is_none":           {},
	"is_ok":             {},
	"is_err":            {},
	"ok":                {},
	"err":               {},
	"take":              {},
	"replace":           {},
	"swap":              {},
	"drop":              {},
	"default":           {},

	// Prelude macros (post-`!` strip). `format`/`print` are already in
	// the language-agnostic stdlibBareNames; the rest are Rust-only
	// idioms or common-enough across languages to warrant gating.
	"vec":           {},
	"println":       {},
	"eprintln":      {},
	"eprint":        {},
	"write":         {},
	"writeln":       {},
	"panic":         {},
	"todo":          {},
	"unimplemented": {},
	"unreachable":   {},
	"dbg":           {},
	"assert":        {},
	"debug_assert":  {},
	"matches":       {},
}

// javaBareNames is the Java-language-gated bare-name stop-list (issue
// #105). After the Java extractor strips the receiver from a method
// call (`repo.findById(id)` → `findById`, `optional.orElseThrow()` →
// `orElseThrow`), the resolver sees a bare name that can't be matched
// to a local entity and lands in bug-extractor. The names below are
// JDK exception classes plus the most distinctive Spring Data /
// Spring MVC / Spring binding helper methods.
//
// Conservative selection rule (lesson from #94): include only names
// whose plausible-user-method-collision rate is low. Generic
// getters/setters (`getId`, `getName`, `getValue`, `setName`,
// `setValue`) and ubiquitous functional verbs (`map`, `filter`,
// `forEach`, `collect`, `stream`) are deliberately EXCLUDED — the
// proper resolution for those is cross-class receiver binding (the
// (A) follow-up to issue #105). When doubt exists, omit; a missed
// external is strictly better than a synthesised placeholder
// shadowing a real missing-resolution bug.
//
// Categories (curated, not exhaustive):
//   - JDK stdlib exception class names (constructed bare or referenced
//     bare in `throws`/`catch` clauses post-receiver-strip).
//   - JDK Optional helpers — only the four where collision risk is
//     low. `map`/`flatMap` excluded because every user collection
//     method named `map` would be shadowed.
//   - Spring Data JPA repository methods with distinctive shapes
//     (`findById`, `saveAndFlush`, etc.). Verbs like `delete` /
//     `find` alone are excluded; the JpaRepository names listed
//     here have a low natural-method-collision rate.
//   - Spring `BindingResult` validation helpers.
//   - Spring `Model` / `RedirectAttributes` flash-attribute helpers.
//   - Spring Data `Pageable` / `Page` accessors.
var javaBareNames = map[string]struct{}{
	// JDK stdlib exception classes — constructor and reference forms.
	"IllegalArgumentException":      {},
	"NullPointerException":          {},
	"IllegalStateException":         {},
	"UnsupportedOperationException": {},
	"RuntimeException":              {},
	"IndexOutOfBoundsException":     {},
	"ClassCastException":            {},
	"NumberFormatException":         {},
	"ArithmeticException":           {},
	"IOException":                   {},
	"FileNotFoundException":         {},
	"InterruptedException":          {},
	"Error":                         {},
	"Throwable":                     {},
	// `Exception` is already covered by the language-agnostic
	// stdlibBareNames map (it's also a Python builtin), so it does
	// NOT need to live here.

	// JDK java.util.Optional helpers — the four with low collision
	// risk. `map`/`flatMap`/`filter` are deliberately excluded:
	// every Java codebase that does anything with collections has
	// at least one user method named `map` or `filter`, and the
	// language gate alone is not strong enough.
	"orElseThrow": {},
	"orElse":      {},
	"ifPresent":   {},
	"isPresent":   {},

	// Spring Data JPA repository methods. Distinctive shapes only —
	// generic verbs (`find`, `delete`, `update`) are excluded.
	"findById":     {},
	"findAll":      {},
	"findAllById":  {},
	"save":         {},
	"saveAll":      {},
	"saveAndFlush": {},
	"deleteById":   {},
	"deleteAll":    {},
	"existsById":   {},
	"count":        {},

	// Spring BindingResult validation helpers.
	"hasErrors":     {},
	"rejectValue":   {},
	"getFieldError": {},

	// Spring Model / RedirectAttributes flash-attribute helpers.
	// `addAttribute` is generic enough to cover some user code
	// collisions but the lang="java" gate keeps the rewrite scoped
	// to Java sources only.
	"addFlashAttribute": {},
	"addAttribute":      {},

	// Spring Data Pageable / Page accessors.
	"getTotalElements": {},
	"getTotalPages":    {},
	"getNumber":        {},
	"getSize":          {},
	"hasNext":          {},
	"hasPrevious":      {},
}

// javaTestBareNames is the Java test-file-gated bare-name stop-list
// (issue #120). MockMvc, JUnit Jupiter, AssertJ, Mockito, and
// Hamcrest all expose fluent-API entry points that the Java extractor
// strips to a bare leaf identifier when the receiver is the return
// value of an upstream fluent call:
//
//	mockMvc.perform(get("/x"))
//	    .andExpect(status().isOk())
//	    .andExpect(view().name("ok"));
//
// Static-typing the receiver chain is out of scope; instead we
// allow-list the leaf names and gate them on a Java test-file path so
// production code with same-named user methods isn't shadowed.
//
// Bias toward names whose plausible-user-method-collision rate inside
// `*Tests.java` / `*IT.java` files is low — generic getters/setters
// are kept out, and only the canonical fluent-test verbs are listed.
var javaTestBareNames = map[string]struct{}{
	// MockMvc fluent API (org.springframework.test.web.servlet).
	"perform":                    {},
	"andExpect":                  {},
	"andDo":                      {},
	"andReturn":                  {},
	"status":                     {},
	"view":                       {},
	"model":                      {},
	"content":                    {},
	"header":                     {},
	"redirectedUrl":              {},
	"redirectedUrlPattern":       {},
	"forwardedUrl":               {},
	"flash":                      {},
	"jsonPath":                   {},
	"xpath":                      {},
	"cookie":                     {},
	"isOk":                       {},
	"isCreated":                  {},
	"isNoContent":                {},
	"isBadRequest":               {},
	"isUnauthorized":             {},
	"isForbidden":                {},
	"isNotFound":                 {},
	"isMethodNotAllowed":         {},
	"is3xxRedirection":           {},
	"is4xxClientError":           {},
	"is5xxServerError":           {},
	"isInternalServerError":      {},
	"attributeExists":            {},
	"attributeHasErrors":         {},
	"attributeHasFieldErrors":    {},
	"attributeHasFieldErrorCode": {},
	"attributeHasNoErrors":       {},
	"hasErrors":                  {},
	"hasNoErrors":                {},

	// MockMvcRequestBuilders / MockMvcResultMatchers shortcuts.
	"param":       {},
	"params":      {},
	"queryParam":  {},
	"flashAttr":   {},
	"sessionAttr": {},

	// JUnit Jupiter assertion façade (org.junit.jupiter.api.Assertions).
	"assertEquals":              {},
	"assertNotEquals":           {},
	"assertNull":                {},
	"assertNotNull":             {},
	"assertTrue":                {},
	"assertFalse":               {},
	"assertThrows":              {},
	"assertDoesNotThrow":        {},
	"assertSame":                {},
	"assertNotSame":             {},
	"assertArrayEquals":         {},
	"assertIterableEquals":      {},
	"assertLinesMatch":          {},
	"assertTimeout":             {},
	"assertTimeoutPreemptively": {},
	"assertAll":                 {},
	"fail":                      {},
	"assumeTrue":                {},
	"assumeFalse":               {},
	"assumingThat":              {},

	// AssertJ entry points.
	"assertThat":                         {},
	"assertThatThrownBy":                 {},
	"assertThatExceptionOfType":          {},
	"assertThatNullPointerException":     {},
	"assertThatIllegalArgumentException": {},
	"assertThatIllegalStateException":    {},
	"isEqualTo":                          {},
	"isNotEqualTo":                       {},
	"isSameAs":                           {},
	"isNotSameAs":                        {},
	"isInstanceOf":                       {},
	"isNotInstanceOf":                    {},
	"hasSize":                            {},
	"hasSizeGreaterThan":                 {},
	"hasSizeLessThan":                    {},
	"isNotEmpty":                         {},
	"containsExactly":                    {},
	"containsExactlyInAnyOrder":          {},
	"containsExactlyElementsOf":          {},
	"containsOnly":                       {},
	"doesNotContain":                     {},
	"startsWith":                         {},
	"endsWith":                           {},
	"matches":                            {},
	"isPresent":                          {},
	"isNotPresent":                       {},
	"hasValue":                           {},
	"hasMessage":                         {},
	"hasMessageContaining":               {},
	"hasRootCauseInstanceOf":             {},
	"extracting":                         {},

	// Mockito façades (org.mockito.Mockito / BDDMockito).
	"mock":                     {},
	"spy":                      {},
	"when":                     {},
	"thenReturn":               {},
	"thenThrow":                {},
	"thenAnswer":               {},
	"verify":                   {},
	"verifyNoInteractions":     {},
	"verifyNoMoreInteractions": {},
	"given":                    {},
	"willReturn":               {},
	"willThrow":                {},
	"willAnswer":               {},
	"willDoNothing":            {},
	"reset":                    {},
	"any":                      {},
	"anyString":                {},
	"anyInt":                   {},
	"anyLong":                  {},
	"anyBoolean":               {},
	"anyList":                  {},
	"anyMap":                   {},
	"anySet":                   {},
	"eq":                       {},
	"argThat":                  {},

	// MockMvc HTTP method shortcuts (collide with HTTP verbs but only
	// inside test files where they invariably mean MockMvc).
	"get":     {},
	"post":    {},
	"put":     {},
	"delete":  {},
	"patch":   {},
	"head":    {},
	"options": {},

	// Hamcrest matcher shortcuts.
	"is":           {},
	"equalTo":      {},
	"notNullValue": {},
	"nullValue":    {},
	"hasItem":      {},
	"hasItems":     {},
	"hasProperty":  {},
}

// isJavaTestFile reports whether p is a Java test source file by
// path convention. The Java/Maven/Gradle ecosystem uses three shapes:
//
//   - `src/test/java/...` (canonical Maven/Gradle test source root).
//   - `*Test.java` / `*Tests.java` (the JUnit naming convention).
//   - `*IT.java` (the Failsafe integration-test naming convention).
//
// Any one of these on its own is a strong signal — the canonical
// source-root rule is precise enough that a shared util living
// inside `src/main/java/` keeps its bare-name CALLS unresolved
// rather than picking up a test-only allowlist entry.
func isJavaTestFile(p string) bool {
	if p == "" {
		return false
	}
	if strings.Contains(p, "/src/test/java/") || strings.HasPrefix(p, "src/test/java/") {
		return true
	}
	if strings.HasSuffix(p, "Tests.java") || strings.HasSuffix(p, "Test.java") || strings.HasSuffix(p, "IT.java") {
		return true
	}
	return false
}

// kotlinBareNames is the Kotlin-language-gated bare-name stop-list
// (issue #106). The Kotlin extractor strips the receiver from a call
// (`flow.collect { ... }` → `collect`, `Channel(capacity)` →
// `Channel`), and the resolver can't bind the bare name to a local
// entity, so it lands in bug-extractor. The names below are
// kotlinx.coroutines / io.ktor stdlib types, kotlin.collections /
// kotlin builtins, scope functions, and contract / lazy helpers that
// have a low collision rate with user-defined identifiers in real
// Kotlin codebases.
//
// Conservative selection rule (lessons from #94 / #105): generic
// getters/setters/collection ops (`get`, `set`, `add`, `remove`,
// `size`, `isEmpty`) are deliberately EXCLUDED — every Kotlin
// codebase has user methods with those names and the language gate
// alone is not strong enough to prevent shadowing real
// missing-resolution bugs.
//
// Categories (curated, not exhaustive):
//   - kotlinx.coroutines / io.ktor common Pascal-case stdlib types.
//   - kotlin.collections / kotlin builtins (factory functions).
//   - scope functions (`let`, `also`, `apply`, `run`, `with`) — KEPT
//     Kotlin-gated because `let` could plausibly shadow a JS
//     user-variable name.
//   - kotlin.contract / lazy / require helpers.
var kotlinBareNames = map[string]struct{}{
	// kotlinx.coroutines / io.ktor stdlib types (Pascal).
	"Frame":                {},
	"CloseReason":          {},
	"CopyOnWriteArrayList": {},
	"ConcurrentHashMap":    {},
	"AtomicInteger":        {},
	"AtomicLong":           {},
	"AtomicBoolean":        {},
	"AtomicReference":      {},
	"Job":                  {},
	"Deferred":             {},
	"Channel":              {},
	"CoroutineScope":       {},
	"MutableStateFlow":     {},
	"StateFlow":            {},
	"MutableSharedFlow":    {},
	"SharedFlow":           {},
	"Flow":                 {},
	"ApplicationCall":      {},
	"Application":          {},
	"Route":                {},
	"Routing":              {},
	"WebSocketSession":     {},

	// kotlin.collections / kotlin builtins (factory functions).
	"listOf":        {},
	"mapOf":         {},
	"setOf":         {},
	"mutableListOf": {},
	"mutableMapOf":  {},
	"mutableSetOf":  {},
	"arrayOf":       {},
	"arrayListOf":   {},
	"hashMapOf":     {},
	"hashSetOf":     {},
	"linkedSetOf":   {},
	"sortedSetOf":   {},
	"emptyList":     {},
	"emptyMap":      {},
	"emptySet":      {},
	"listOfNotNull": {},
	"mapNotNull":    {},

	// Scope functions — Kotlin-gated. `let` in particular would
	// shadow JS user-variable names if added to the language-agnostic
	// list.
	"let":   {},
	"also":  {},
	"apply": {},
	"run":   {},
	"with":  {},

	// Contracts / lazy / require helpers.
	"requireNotNull": {},
	"checkNotNull":   {},
	"require":        {},
	"check":          {},
	"error":          {},
	"lazy":           {},
	"lazyOf":         {},
	"TODO":           {},
}

// rubyBareNames is the Ruby-language-gated bare-name stop-list (issue
// #107). Object/Kernel instance methods that the Ruby extractor strips
// down to the bare leaf identifier (`x.nil?` → `nil?`, `obj.to_s` →
// `to_s`) — these can't bind to a local entity and land in
// bug-extractor. Gating to lang="ruby" keeps the resolution scoped so
// JS/Python/etc. user methods named `dup`, `clone`, `freeze`,
// `respond_to?` aren't shadowed.
//
// Conservative selection rule (lessons from #94 / #105 / #106):
// generic collection ops (`each`, `map`, `select`, `find`, `count`,
// `length`, `size`) are deliberately EXCLUDED. They are user-method
// names on any class in any language and the language gate alone is
// not strong enough to keep them safe.
//
// Rails ActionController DSL (`render`/`params`/`before_action`/...)
// and ActiveRecord query builders (`where`/`order`/`has_many`/...) are
// classified by the resolver-side rubyDynamicPatterns catalog (Refs
// refs.go) as Dispositional Dynamic instead of synthesised externals,
// because those names ARE method_missing-generated rather than stable
// stdlib functions.
var rubyBareNames = map[string]struct{}{
	// Object / BasicObject lifecycle and identity.
	"new":         {},
	"nil?":        {},
	"present?":    {},
	"blank?":      {},
	"respond_to?": {},
	"class":       {},
	// `send` is intentionally OMITTED — it's classified as Dynamic by
	// the resolver-side rubyDynamicPatterns catalog (reflective
	// dispatch), which is a stronger signal than ExternalKnown.
	"tap":        {},
	"then":       {},
	"yield_self": {},
	"dup":        {},
	"clone":      {},
	"freeze":     {},
	"frozen?":    {},
	"object_id":  {},
	// Type coercion (Object#to_*).
	"to_s":   {},
	"to_str": {},
	"to_i":   {},
	"to_f":   {},
	"to_a":   {},
	"to_h":   {},
	"to_sym": {},
	// Inspection / type checks.
	"inspect":      {},
	"is_a?":        {},
	"kind_of?":     {},
	"instance_of?": {},
	// ActiveRecord persistence and validation methods (issue #124).
	// After #107 + #143, residual rails-realworld bug-extractor was
	// dominated by AR persistence calls (`record.save`, `user.update!`,
	// `model.valid_password?`). These names ARE generated/inherited from
	// ActiveRecord::Base, not user-defined — extractor strips the
	// receiver and the resolver sees only the bare leaf. The Ruby
	// language gate keeps them from polluting other ecosystems. Generic
	// collection ops (`find`, `count`) remain EXCLUDED per #107 lock-in.
	// `new` and `where` are intentionally omitted: `new` is already
	// covered above; `where` is classified by the resolver-side
	// rubyDynamicPatterns catalog as Dynamic.
	"save":              {},
	"save!":             {},
	"update":            {},
	"update!":           {},
	"destroy":           {},
	"destroy!":          {},
	"valid?":            {},
	"valid_password?":   {},
	"errors":            {},
	"persisted?":        {},
	"new_record?":       {},
	"attributes":        {},
	"reload":            {},
	"create":            {},
	"create!":           {},
	"find_or_create_by": {},
	"build":             {},
	"exists?":           {},
	"first":             {},
	"last":              {},
	"all":               {},
	// Additional AR persistence/query methods observed in
	// rails-realworld bug-extractor after the initial #124 batch. All
	// are stable AR::Base / AR::Relation methods (not method_missing).
	"find_by":                  {},
	"find_each":                {},
	"find_in_batches":          {},
	"destroy_all":              {},
	"delete_all":               {},
	"update_all":               {},
	"update_attribute":         {},
	"update_attributes":        {},
	"update_attributes!":       {},
	"toggle":                   {},
	"toggle!":                  {},
	"increment":                {},
	"increment!":               {},
	"decrement":                {},
	"decrement!":               {},
	"touch":                    {},
	"reset_counters":           {},
	"reset_column_information": {},
	// Numeric / time helpers added by ActiveSupport to Numeric
	// (`3.days`, `1.hour.ago`, `5.minutes.from_now`). Ruby-only by
	// virtue of ActiveSupport's monkey-patches; the language gate
	// keeps them from polluting non-Ruby ecosystems.
	"days":     {},
	"hours":    {},
	"minutes":  {},
	"seconds":  {},
	"weeks":    {},
	"months":   {},
	"years":    {},
	"ago":      {},
	"from_now": {},
}

// jsBareNames is the JS/TS-language-gated bare-name stop-list (issue
// #104). Two families covered:
//
//  1. Prisma ORM client method names. The JS extractor strips the
//     receiver (`prisma.user.findMany(...)` → bare `findMany`), so
//     the resolver only sees the leaf identifier. These collide with
//     user methods in OTHER ecosystems (Ruby/Java/Go all have classes
//     with their own `update`/`delete`/`create` methods) so the
//     language gate is required.
//  2. JS/TS array & util builtins (`some`, `every`, `push`, `trim`,
//     `isArray`) that bare-call after receiver-strip and reach the
//     resolver as leaf identifiers.
//
// Conservative selection rule (lessons from #94 / #105 / #106 / #107):
// generic collection ops (`map`, `filter`, `forEach`, `reduce`,
// `find`, `length`, `size`) are deliberately EXCLUDED. They are
// user-method names on any class in any language and the language
// gate alone is not strong enough to keep them safe — JS/TS share
// the `map`/`filter`/`forEach` namespace with hand-rolled domain
// methods on user classes too readily.
var jsBareNames = map[string]struct{}{
	// Prisma ORM client surface (https://www.prisma.io/docs/orm/reference/prisma-client-reference)
	"findUnique":        {},
	"findUniqueOrThrow": {},
	"findFirst":         {},
	"findFirstOrThrow":  {},
	"findMany":          {},
	"createMany":        {},
	"updateMany":        {},
	"deleteMany":        {},
	"upsert":            {},
	"aggregate":         {},
	"groupBy":           {},
	"executeRaw":        {},
	"executeRawUnsafe":  {},
	"queryRaw":          {},
	"queryRawUnsafe":    {},
	// Prisma `$`-prefixed top-level client methods. `$` is rare in
	// user-defined identifier names so these are unambiguous.
	"$connect":     {},
	"$disconnect":  {},
	"$transaction": {},
	"$queryRaw":    {},
	"$executeRaw":  {},
	"$on":          {},
	"$use":         {},
	// `create`, `update`, `delete`, `count` are intentionally OMITTED.
	// They overlap heavily with non-Prisma user methods (controllers,
	// services, factories) and the per-language gate isn't enough.
	// They land in bug-extractor instead — acceptable trade-off vs
	// false positives that hide real local entities.

	// JS/TS array & util builtins. Names that bare-call after
	// receiver-strip (`xs.some(p)` → `some`).
	"some":    {},
	"every":   {},
	"push":    {},
	"trim":    {},
	"isArray": {},
	// `pop` / `shift` / `unshift` / `splice` / `slice` / `concat` /
	// `join` / `includes` / `indexOf` / `lastIndexOf` / `flat` /
	// `flatMap` are deliberately OMITTED for this iteration: each is
	// either too collision-prone (`includes`, `indexOf`) or
	// insufficiently observed in #104's bug-extractor sample to
	// justify carrying the false-positive risk.
}

// isKnownExternalPackage reports whether s matches our small allowlist
// of well-known third-party packages and stdlib top-level modules. The
// allowlist is intentionally narrow for v1.0 — false positives turn a
// local name into a placeholder, which is worse than missing one.
func isKnownExternalPackage(s string) bool {
	lower := strings.ToLower(s)
	if _, ok := knownExternalPackages[lower]; ok {
		return true
	}
	// Scoped npm fallback: a full "@scope/pkg" key matches if the bare
	// "@scope" key is on the allowlist. This lets us keep the existing
	// scope-level entries (@radix-ui, @tanstack, ...) functional for
	// every package they ship without enumerating each one. The scope
	// must be non-empty and start with '@' (issue #71).
	if strings.HasPrefix(lower, "@") {
		if slash := strings.IndexByte(lower, '/'); slash > 1 {
			scope := lower[:slash]
			if _, ok := knownExternalPackages[scope]; ok {
				return true
			}
		}
	}
	return false
}

// IsKnownExternalPackage is the exported form of the allowlist check.
// VERIFY-2-PREP / issue #56: the resolver consults this to decide
// whether an "ext:<pkg>" placeholder should be tagged ExternalKnown
// (allowlisted, expected) or ExternalUnknown (real external dep we
// haven't catalogued yet). Comparison is case-folded.
func IsKnownExternalPackage(s string) bool {
	return isKnownExternalPackage(s)
}

// knownExternalPackages is the v1.1 allowlist. Lowercase keys; lookups
// are case-folded. Curated from real codebases — Python web/data
// stacks, JS/TS frontend + node, Go services, JVM enterprise. False
// positives synthesise a placeholder for what might have been a local
// name; bias toward names extremely unlikely to collide.
var knownExternalPackages = map[string]struct{}{
	// Python ecosystem (third-party)
	"django":            {},
	"rest_framework":    {},
	"drf":               {},
	"flask":             {},
	"fastapi":           {},
	"sqlalchemy":        {},
	"alembic":           {},
	"pydantic":          {},
	"celery":            {},
	"requests":          {},
	"httpx":             {},
	"numpy":             {},
	"pandas":            {},
	"scipy":             {},
	"pytest":            {},
	"mypy":              {},
	"attrs":             {},
	"click":             {},
	"redis":             {},
	"boto3":             {},
	"awswrangler":       {},
	"typing_extensions": {},
	// Python stdlib top-level
	"os":              {},
	"sys":             {},
	"json":            {},
	"re":              {},
	"typing":          {},
	"datetime":        {},
	"collections":     {},
	"asyncio":         {},
	"concurrent":      {},
	"multiprocessing": {},
	"threading":       {},
	"queue":           {},
	"weakref":         {},
	"logging":         {},
	"pathlib":         {},
	"functools":       {},
	"itertools":       {},
	"operator":        {},
	"builtins":        {},
	"unittest":        {},
	"abc":             {},
	"enum":            {},
	"uuid":            {},
	"hashlib":         {},
	"dataclasses":     {},
	"contextlib":      {},
	"warnings":        {},
	"tempfile":        {},
	"subprocess":      {},
	"argparse":        {},
	"socket":          {},
	"ssl":             {},
	"urllib":          {},
	// JS / TS ecosystem (unscoped)
	"react":        {},
	"vue":          {},
	"angular":      {},
	"lodash":       {},
	"ramda":        {},
	"immer":        {},
	"dayjs":        {},
	"date-fns":     {},
	"axios":        {},
	"ky":           {},
	"express":      {},
	"next":         {},
	"jest":         {},
	"vitest":       {},
	"mocha":        {},
	"chai":         {},
	"sinon":        {},
	"supertest":    {},
	"typescript":   {},
	"zod":          {},
	"prisma":       {},
	"redux":        {},
	"rxjs":         {},
	"tanstack":     {},
	"nodemailer":   {},
	"bcrypt":       {},
	"jsonwebtoken": {},
	"helmet":       {},
	"multer":       {},
	"faker":        {},
	// JS / TS scoped packages (kept lowercase per case-folded lookup;
	// only the leading "@scope" segment is matched).
	"@radix-ui":        {},
	"@tanstack":        {},
	"@reduxjs":         {},
	"@testing-library": {},
	"@types":           {},
	"@nestjs":          {},
	"@prisma":          {},
	"@faker-js":        {},
	"@apollo":          {},
	"@mui":             {},
	"@emotion":         {},
	"@chakra-ui":       {},
	"@headlessui":      {},
	"@hookform":        {},
	"@trpc":            {},
	"@storybook":       {},
	"@vitejs":          {},
	"@babel":           {},
	"@swc":             {},
	"@sentry":          {},
	"@auth0":           {},
	"@aws-sdk":         {},
	"@azure":           {},
	"@google-cloud":    {},
	"@graphql-tools":   {},
	"@vue":             {},
	"@angular":         {},
	// Go stdlib top-level
	"fmt":           {},
	"strings":       {},
	"strconv":       {},
	"errors":        {},
	"context":       {},
	"net":           {},
	"http":          {},
	"io":            {},
	"bytes":         {},
	"sort":          {},
	"sync":          {},
	"time":          {},
	"path":          {},
	"regexp":        {},
	"testing":       {},
	"encoding/json": {},
	// Issue #116: Go stdlib root segments — populated so full-import-
	// path stubs (`net/http`, `encoding/json`, `crypto/tls`) can be
	// classified by their root segment after the `/`-split branch in
	// classifyExternal. Each root is the top-level directory in the
	// Go stdlib tree.
	"encoding":  {},
	"crypto":    {},
	"bufio":     {},
	"database":  {},
	"compress":  {},
	"archive":   {},
	"image":     {},
	"text":      {},
	"html":      {},
	"mime":      {},
	"hash":      {},
	"math":      {},
	"runtime":   {},
	"reflect":   {},
	"unicode":   {},
	"flag":      {},
	"container": {},
	"plugin":    {},
	"embed":     {},
	"expvar":    {},
	"syscall":   {},
	"unsafe":    {},
	// "log" / "hash" already present (Rust crates / Python builtins
	// blocks) and serve double-duty for Go stdlib roots via case-
	// folded lookup.
	// "os" / "sys" / "json" / "queue" / "abc" / "enum" are already in
	// the Python stdlib block above — case-folded lookup makes them
	// accept Go's `os`/`sort`/etc. as well. "io"/"net"/"sort"/"sync"/
	// "time"/"path"/"errors"/"strings"/"strconv"/"context"/"bytes"/
	// "regexp"/"testing"/"hash" likewise serve both ecosystems.

	// Issue #116: Go third-party host-prefixed roots (3-segment
	// "<host>/<owner>/<repo>" canonical form). These are matched by
	// goHostCanonical after the slash-split branch in classifyExternal.
	"github.com/stretchr/testify":         {},
	"github.com/gin-gonic/gin":            {},
	"github.com/go-chi/chi":               {},
	"github.com/labstack/echo":            {},
	"github.com/sirupsen/logrus":          {},
	"github.com/spf13/cobra":              {},
	"github.com/spf13/viper":              {},
	"github.com/spf13/pflag":              {},
	"github.com/pkg/errors":               {},
	"github.com/google/uuid":              {},
	"github.com/golang/protobuf":          {},
	"github.com/golang/mock":              {},
	"github.com/jmoiron/sqlx":             {},
	"github.com/jinzhu/gorm":              {},
	"github.com/gorilla/mux":              {},
	"github.com/gorilla/websocket":        {},
	"github.com/prometheus/client_golang": {},
	"github.com/uber-go/zap":              {},
	"github.com/davecgh/go-spew":          {},
	"github.com/stretchr/objx":            {},
	"golang.org/x/sync":                   {},
	"golang.org/x/crypto":                 {},
	"golang.org/x/net":                    {},
	"golang.org/x/text":                   {},
	"golang.org/x/sys":                    {},
	"golang.org/x/oauth2":                 {},
	"golang.org/x/exp":                    {},
	"golang.org/x/tools":                  {},
	"google.golang.org/grpc":              {},
	"google.golang.org/protobuf":          {},
	"gopkg.in/yaml.v3":                    {},
	"gopkg.in/yaml.v2":                    {},
	// Go third-party (legacy single-segment keys; left in place so any
	// pre-#116 caller hitting the Pascal/dotted branch still resolves).
	"testify": {},
	"viper":   {},
	"cobra":   {},
	"zap":     {},
	"logrus":  {},
	"sqlx":    {},
	"gorm":    {},
	"gorilla": {},
	// Java / Kotlin
	"java":                  {},
	"javax":                 {},
	"kotlin":                {},
	"kotlinx":               {},
	"io.ktor":               {}, // io.ktor.* server / client / websockets (Issue #106)
	"org.springframework":   {},
	"com.fasterxml.jackson": {},
	"com.google.guava":      {},
	"org.apache.commons":    {},
	"junit":                 {},
	"mockito":               {},
	"slf4j":                 {},
	"log4j":                 {},
	"lombok":                {},
	// Java test stack (Issue #120 — spring-petclinic test imports).
	// Multi-segment keys keep the longest-prefix matcher precise so an
	// unrelated `org.junit` user-namespace would not collide.
	"org.junit":          {}, // covers org.junit / org.junit.jupiter.* / org.junit.platform.*
	"org.mockito":        {},
	"org.assertj":        {},
	"org.hamcrest":       {},
	"org.testcontainers": {},
	"io.micrometer":      {}, // metrics/observability used by Spring Boot
	"ch.qos.logback":     {}, // default Spring Boot logger
	// Ruby
	"rails":        {},
	"activerecord": {},
	// C# / .NET (Issue #91 — top non-Python language by import-bug)
	"system":    {}, // System.*, System.Text.*, System.Collections.*
	"microsoft": {}, // Microsoft.EntityFrameworkCore, Microsoft.AspNetCore.*
	// Java EE / Jakarta (Issue #91 — Spring/JPA imports)
	"jakarta": {}, // jakarta.persistence, jakarta.validation
	// Rust crates (Issue #91 — top Rust import-bug roots)
	"tokio":              {},
	"actix_web":          {},
	"actix":              {},
	"serde":              {},
	"serde_json":         {},
	"anyhow":             {},
	"thiserror":          {},
	"tracing":            {},
	"tracing_subscriber": {},
	"clap":               {},
	"reqwest":            {},
	"futures":            {},
	"async_trait":        {},
	"opentelemetry":      {},
	// Rust stdlib + extended ecosystem (Issue #101 — bug-rate
	// reduction targets for mini-redis / actix-examples). `std` is
	// the Rust standard library; every `use std::...` path leaks
	// without it. The actix_* / async_* ecosystem and Diesel ORM
	// are the highest-volume third-party leak roots in the
	// actix-examples corpus.
	"std":                  {},
	"core":                 {}, // libcore (rare in user code but legit)
	"alloc":                {}, // liballoc
	"actix_files":          {},
	"actix_identity":       {},
	"actix_session":        {},
	"actix_cors":           {},
	"actix_multipart":      {},
	"actix_rt":             {},
	"actix_service":        {},
	"actix_codec":          {},
	"actix_http":           {},
	"actix_router":         {},
	"actix_test":           {},
	"actix_web_actors":     {},
	"actix_protobuf":       {},
	"awc":                  {}, // actix web client
	"diesel":               {},
	"diesel_migrations":    {},
	"sea_orm":              {},
	"casbin":               {},
	"chrono":               {},
	"regex":                {},
	"rand":                 {},
	"hyper":                {},
	"tower":                {},
	"tower_http":           {},
	"axum":                 {},
	"rocket":               {},
	"warp":                 {},
	"once_cell":            {},
	"lazy_static":          {},
	"parking_lot":          {},
	"crossbeam":            {},
	"rayon":                {},
	"log":                  {},
	"env_logger":           {},
	"opentelemetry_aws":    {},
	"opentelemetry_otlp":   {},
	"opentelemetry_jaeger": {},
	"async_stream":         {},
	"tokio_stream":         {},
	"tokio_util":           {},
	"derive_more":          {},
	// PHP ecosystem (Issue #102 — symfony-demo bug-rate reduction).
	// PHP namespace roots reach this allowlist via the `\`-separator
	// branch in classifyExternal, gated on isPhpNamespaceIdent. Keys
	// are lowercase here because isKnownExternalPackage case-folds the
	// lookup; the on-disk root is "Symfony", "Doctrine", etc. App\* is
	// intentionally absent — it's the project-local convention in
	// Symfony/Laravel layouts and must not be promoted to a placeholder.
	"symfony":    {},
	"doctrine":   {},
	"twig":       {},
	"laravel":    {},
	"illuminate": {},
	"psr":        {},
}

// isKindLikePrefix reports whether s is a short, alphabetic kind name
// like "Module" or "Function" — used to decide whether a "Foo:Bar"
// stub should be treated as Kind:Name. The structural-ref shape
// "scope:..." has multiple ':'s and a long prefix; this filter avoids
// claiming those.
//
// '.' is intentionally allowed in the prefix character class to admit
// dotted-kind shapes like "a.b.c:Symbol" that some extractors emit
// (e.g. fully-qualified Python or JVM kind hints). The trade-off is
// that "java.util.Map:put" looks kind-like and gets treated as a
// Kind:Name pair — that's what we want for these external lookups.
func isKindLikePrefix(s string) bool {
	if len(s) == 0 || len(s) > 24 {
		return false
	}
	for _, c := range s {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '.') {
			return false
		}
	}
	return true
}

// looksLikeExternalImport reports whether a bare name has the shape of
// a dotted external import — at least one dot, terminal segment is a
// capitalised identifier, and no path separators. Used by Pass 4.5
// (issue #82) to synthesise placeholders for dangling EXTENDS targets
// like "serializers.ModelSerializer" that cross/hierarchy emits as
// structural-refs without an :external: marker.
func looksLikeExternalImport(s string) bool {
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, "/\\ \t") {
		return false
	}
	dot := strings.IndexByte(s, '.')
	if dot <= 0 || dot >= len(s)-1 {
		return false
	}
	// Every segment must be a valid identifier (letters, digits, '_'),
	// non-empty, and not start with a digit.
	for _, seg := range strings.Split(s, ".") {
		if !isIdentSegment(seg) {
			return false
		}
	}
	// Terminal segment must start with an uppercase letter — the
	// convention for class/type names that show up as EXTENDS targets.
	last := s[strings.LastIndexByte(s, '.')+1:]
	if last == "" {
		return false
	}
	c := last[0]
	if !(c >= 'A' && c <= 'Z') {
		return false
	}
	return true
}

// isRustCrateIdent reports whether s has the shape of a Rust crate
// name — ASCII letters, digits, and '_', non-empty, not starting with
// a digit. Issue #101: gates the `::` separator branch so we only
// trust the leading segment of a use-path when it looks like a crate
// name (and not, e.g., a bracketed/spaced fragment that slipped in).
func isRustCrateIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c == '_':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// isPhpNamespaceIdent reports whether s has the shape of a PHP
// top-level namespace segment — ASCII letters, digits, and '_',
// non-empty, not starting with a digit, and starting with an
// uppercase letter (PHP convention for vendor/namespace roots:
// Symfony, Doctrine, Twig, Psr, App, ...). Issue #102: gates the
// `\` separator branch so we only trust the leading segment of a
// use-statement when it looks like a namespace root, not a stray
// fragment with backslashes that slipped in from elsewhere.
func isPhpNamespaceIdent(s string) bool {
	if s == "" {
		return false
	}
	if c := s[0]; !(c >= 'A' && c <= 'Z') {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c == '_':
		case c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

// isIdentSegment reports whether s is a non-empty identifier segment
// (ASCII letters/digits/underscore, not starting with a digit).
func isIdentSegment(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c == '_':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// externalAPIHost extracts the host segment from a URL-shaped string.
// Returns "" when raw doesn't look like a URL with a recognisable host.
// Issue #89.
//
// Issue #94 follow-up: the original byte-scanning implementation broke
// on IPv6 hosts ("https://[::1]:8080" canonicalised to "[" because the
// port-stripping ran before the bracket-balanced host was extracted).
// Switched to net/url which understands bracketed IPv6 hosts and gives
// a clean Hostname() without brackets or port. Falls back to "" on
// parse error or any URL without a host.
func externalAPIHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Require an explicit scheme to keep behaviour close to the prior
	// "://" gate; net/url is permissive about scheme-less inputs.
	if !strings.Contains(raw, "://") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		return ""
	}
	// Reject obviously malformed hosts (e.g. percent-encoded garbage
	// that survived parsing).
	for _, r := range host {
		if r == '%' {
			return ""
		}
	}
	return host
}

// isGoImportPath reports whether s has the shape of a Go import path
// — slash-separated, no backslashes, no colons (rules out URLs and
// "Kind:Name" forms), no whitespace, and the first segment is a
// lowercase ASCII identifier (Go package names are conventionally
// lowercase; host prefixes like "github.com" are also lowercase).
// Issue #116: gates the `/` separator branch in classifyExternal so
// only Go-shaped paths trigger split-and-lookup, not Unix file paths.
func isGoImportPath(s string) bool {
	if s == "" {
		return false
	}
	if !strings.Contains(s, "/") {
		return false
	}
	if strings.ContainsAny(s, ":\\ \t") {
		return false
	}
	// Reject leading slash — that's a Unix absolute path, not a Go
	// import path.
	if s[0] == '/' {
		return false
	}
	// First segment must be a lowercase identifier (letters, digits,
	// '_', '-', '.'). Go stdlib packages are single-word lowercase;
	// host prefixes like "github.com" / "golang.org" / "gopkg.in" are
	// also lowercase ASCII with dots.
	slash := strings.IndexByte(s, '/')
	first := s[:slash]
	if first == "" {
		return false
	}
	if c := first[0]; !(c >= 'a' && c <= 'z') {
		return false
	}
	for _, c := range first {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '-' || c == '.':
		default:
			return false
		}
	}
	return true
}

// isGoImportHost reports whether s looks like a host prefix in a Go
// import path — contains a '.' (e.g. "github.com", "golang.org",
// "gopkg.in", "google.golang.org"). Stdlib package roots like "net"
// or "encoding" never contain a dot.
func isGoImportHost(s string) bool {
	return strings.IndexByte(s, '.') > 0
}

// goHostCanonical returns the canonical "<host>/<owner>/<repo>" (or
// "<host>/<pkg>" for gopkg.in's two-segment shape, or "<host>/x/<repo>"
// for golang.org/x). Returns "" when the segment count is too short
// to identify a package.
func goHostCanonical(segs []string) string {
	if len(segs) < 2 {
		return ""
	}
	host := segs[0]
	// gopkg.in uses a two-segment shape: gopkg.in/<pkg>.<vN> (the
	// version is encoded in the package segment, not as a separate
	// directory). Collapse to "<host>/<pkg>" — full key on the
	// allowlist, no trailing import path.
	if host == "gopkg.in" {
		return host + "/" + segs[1]
	}
	// golang.org/x/<repo>/<subpath>... → "<host>/x/<repo>". The "x"
	// owner segment is universal for the golang.org/x staging-grounds
	// modules.
	if host == "golang.org" {
		if len(segs) >= 3 {
			return host + "/" + segs[1] + "/" + segs[2]
		}
		return ""
	}
	// google.golang.org/<module>/<subpath>... → "<host>/<module>"
	// (two-segment shape — modules like grpc, protobuf, api).
	if host == "google.golang.org" {
		return host + "/" + segs[1]
	}
	// Default host shape: "<host>/<owner>/<repo>" (github.com,
	// gitlab.com, bitbucket.org, ...). Subpaths are dropped so a
	// single placeholder represents the module.
	if len(segs) >= 3 {
		return host + "/" + segs[1] + "/" + segs[2]
	}
	return ""
}

// isHexID mirrors resolve.isHexID — a 16-char lower-hex string is
// already an entity ID and must never be treated as a stub.
func isHexID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
