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
		// Issue #364 — fall back to the relationship's stamped language
		// when the FromID isn't a known entity (e.g. unresolved bare-name
		// caller, ambiguous-name caller). Without this, Go-only branches
		// in classifyExternal (receiver_type stdlib dispatch) miss every
		// edge whose source isn't a 1:1-resolvable entity.
		lang := entityLang[rel.FromID]
		if lang == "" && rel.Properties != nil {
			lang = rel.Properties["language"]
		}
		canonical, subtype, ok := classifyExternal(rel.ToID, rel.Kind, lang, entityFile[rel.FromID], fileImports[entityFile[rel.FromID]], rel.Properties)
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
func classifyExternal(stub, relKind, lang, fromFile string, fromImports map[string]bool, relProps map[string]string) (canonical, subtype string, ok bool) {
	if stub == "" {
		return "", "", false
	}

	// Issue #364 — Go stdlib interface dispatch. The Go extractor stamps
	// `Properties["receiver_type"]` on CALLS edges whose operand is a
	// function parameter with a known static type (e.g. `*http.Request`,
	// `http.ResponseWriter`, `io.Writer`). When the bare-name target is a
	// method on the stdlib interface for that type, route the edge to the
	// owning ext:<package> placeholder. The stamp is canonicalised by the
	// extractor (leading `*` stripped, generic type params dropped) so the
	// lookup table can use a single key per package type. Lang-gated to go.
	if lang == "go" && relKind == string(types.RelationshipKindCalls) && relProps != nil {
		if recvType := relProps["receiver_type"]; recvType != "" {
			if pkg, ok := goStdlibInterfaceMethod(recvType, stub); ok {
				return pkg, "package", true
			}
		}
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

	// Issue #424 — YAML extractor (Docker Compose, Kubernetes) emits image
	// refs as "docker_image:<image-ref>". The image lives in a container
	// registry, not the indexed corpus, so it is external by definition.
	// Canonicalise to "docker:<repo>" (drop the tag/digest) and route the
	// edge to a single placeholder per repository — matches the package-
	// per-import collapse used elsewhere. The "docker:" prefix is on the
	// allowlist so all real image refs land in ExternalKnown.
	if strings.HasPrefix(stub, "docker_image:") {
		ref := strings.TrimSpace(stub[len("docker_image:"):])
		if repo := dockerImageRepo(ref); repo != "" {
			return "docker:" + repo, "docker_image", true
		}
	}

	// Refs #44 — YAML extractor (GitHub Actions) emits step `uses:` refs as
	// "gha_action:<org>/<repo>[/<subpath>]@<ref>". These live in the GitHub
	// Actions marketplace, never in the indexed corpus, so route them to a
	// single placeholder per action-repo (drop the version suffix). The
	// "gha:" prefix is on the allowlist so all real action refs land in
	// ExternalKnown.
	if strings.HasPrefix(stub, "gha_action:") {
		ref := strings.TrimSpace(stub[len("gha_action:"):])
		if repo := ghaActionRepo(ref); repo != "" {
			return "gha:" + repo, "gha_action", true
		}
	}

	// Issue #424 — YAML extractor emits Compose host-filesystem mounts as
	// "host_path:<path>". By definition these reference files outside the
	// indexed corpus (relative `./src`, absolute `/etc/foo`, env-driven
	// `${PWD}/data`). Bucket every distinct source path under a single
	// "external_filesystem" placeholder — the path itself is rarely useful
	// for graph navigation, and one placeholder keeps the entity count
	// flat across large compose stacks. Lands in ExternalUnknown.
	if strings.HasPrefix(stub, "host_path:") {
		path := strings.TrimSpace(stub[len("host_path:"):])
		if path != "" {
			return "external_filesystem", "file_mount", true
		}
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
			if ext == "" {
				return "", "", false
			}
			// Issue #44 / proto-fix — Go-shaped import paths (`net/http`,
			// `google.golang.org/grpc/credentials/insecure`,
			// `github.com/foo/bar`) are external by extractor tag, but the
			// `/` separator previously dropped them straight back into
			// bug-extractor. Route them through the same canonicaliser
			// used by the standalone `isGoImportPath` branch below so we
			// collapse to a single placeholder per module (stdlib root for
			// `net/http`, `<host>/<owner>/<repo>` for host-prefixed paths).
			if isGoImportPath(ext) {
				segs := strings.Split(ext, "/")
				first := segs[0]
				if isGoImportHost(first) {
					if canonical := goHostCanonical(segs); canonical != "" {
						return canonical, "package", true
					}
				}
				// Stdlib root: only emit when the leading segment is on
				// the known-stdlib allowlist. This keeps non-Go path
				// shapes (e.g. Python `some/path`) from being captured
				// by the Go-import branch — matching the pre-fix
				// behaviour for non-Go ecosystems while still routing
				// real Go stdlib paths (`net/http`, `encoding/json`,
				// `sync/atomic`) to the right placeholder.
				if isKnownExternalPackage(first) {
					return first, "package", true
				}
			}
			// PHP / Rust / other separators still reject — only Go-shaped
			// paths get the canonicalisation above. Bare names (no `/`)
			// fall through to the existing root-segment logic.
			if strings.ContainsAny(ext, "/\\") {
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

	// Issue #44 — C/C++ STL header includes. The cpp extractor emits
	// IMPORTS edges for `#include <iostream>` style directives with the
	// header token as the ToID (no path separator, no dot for STL,
	// `foo.h` form for C headers). Collapse every STL/libc header to a
	// single `ext:std` placeholder so spdlog-style header-only libraries
	// don't bleed dozens of unresolved bare-name imports into
	// bug-resolver. Lang-gated to cpp / c. Must run before the
	// path-separator rejection below so `sys/types.h` headers route
	// correctly.
	if (lang == "cpp" || lang == "c") && relKind == string(types.RelationshipKindImports) {
		if _, ok := cppStlHeaders[name]; ok {
			return "std", "package", true
		}
	}

	// Issue #44 — spdlog UPPER_SNAKE_CASE preprocessor macros
	// (SPDLOG_LOGGER_DEBUG, SPDLOG_TRACE, SPDLOG_THROW, SPDLOG_LOGGER_
	// CATCH, ...) survive the cpp extractor as bare CALLS edges because
	// the call-graph walker can't see through macro expansion. Route any
	// SPDLOG_-prefixed UPPER_SNAKE_CASE identifier to a single `ext:
	// spdlog` placeholder — these are unambiguously library macros and
	// the package is already on the allowlist (catalogued via header
	// includes). Lang-gated to cpp / c.
	if (lang == "cpp" || lang == "c") && relKind == string(types.RelationshipKindCalls) {
		if isSpdlogMacroIdent(name) {
			return "spdlog", "macro", true
		}
		// Issue #44 — fmt library UPPER_SNAKE_CASE macros (FMT_ASSERT,
		// FMT_THROW, FMT_ENABLE_IF, FMT_STRING, FMT_CONSTEXPR,
		// FMT_INLINE, ...). The fmt library is bundled inside spdlog
		// at include/spdlog/fmt/bundled and uses the FMT_ prefix
		// convention. Route to ext:fmt — `fmt` is already on the
		// allowlist.
		if isFmtMacroIdent(name) {
			return "fmt", "macro", true
		}
		// Issue #44 — calls FROM a bundled fmt source file (path
		// contains "/fmt/bundled/" or "fmt/bundled/") to a bare
		// identifier are fmt-library internal helpers (vformat_to,
		// to_unsigned, format_localized, report_error, ...). The fmt
		// library is bundled inside header-only loggers like spdlog;
		// these names are unresolved local entities but for graph
		// purposes routing them to ext:fmt is structurally honest
		// (they ARE fmt symbols, even when mirrored locally).
		if isFmtBundledFile(fromFile) {
			return "fmt", "function", true
		}
		// Issue #44 — Catch2 test macros (REQUIRE, CHECK, SECTION,
		// TEST_CASE, INFO, FAIL, WARN, SCENARIO, GIVEN, WHEN, THEN,
		// ...). Heavy in tests/ folders of cpp libraries. Route to
		// ext:catch2 — already on the allowlist.
		if _, ok := catch2BareNames[name]; ok {
			return "catch2", "macro", true
		}
		// Issue #44 — spdlog public factory names follow a strict
		// `<sink>_mt` / `<sink>_st` shape (basic_logger_mt,
		// daily_logger_st, rotating_logger_mt, stdout_color_mt,
		// syslog_logger_st, udp_logger_mt, callback_logger_mt,
		// android_logger_mt, ...). The suffix is the spdlog
		// thread-safety convention (`_mt` = multi-threaded sink,
		// `_st` = single-threaded sink) — overwhelmingly distinctive,
		// almost never appears on user-defined methods. Route to
		// ext:spdlog. Gated to cpp/c.
		if isSpdlogFactoryName(name) {
			return "spdlog", "function", true
		}
		// Issue #44 — Google Benchmark public API (UpperCamelCase).
		// The benchmark library surface is small and distinctive; the
		// names below are the high-volume call sites in spdlog/bench
		// and across micro-benchmark suites generally.
		if _, ok := googleBenchmarkBareNames[name]; ok {
			return "benchmark", "function", true
		}
	}

	// Reject obviously non-external shapes: anything containing a path
	// separator was either a structural-ref or a local file path, both
	// already handled upstream.
	if strings.ContainsAny(name, "/\\") {
		return "", "", false
	}

	// Issue kafka-fix-w3 — Java/Kotlin import-leaf bare-name folding.
	// The Java extractor emits bare-name CALLS for constructor and static
	// invocations whose leaf identifier matches the leaf of an imported
	// FQN (e.g. `import org.apache.kafka.streams.StreamsBuilder` →
	// `new StreamsBuilder()` shows up as bare `StreamsBuilder`;
	// `import org.apache.kafka.common.serialization.Serdes` →
	// `Serdes.String()` after receiver-strip becomes bare `Serdes`).
	// When such a bare name matches the leaf of a known-external FQN
	// import, fold to the import's known-external prefix. This is the
	// inverse of the dotted "(lang=='java')" branch below (which folds
	// `Recv.method` when `Recv` matches an import leaf) and shares the
	// same precision: the import edge is the gate, no enumeration
	// required, and the longestKnownDottedPrefix check prevents folding
	// to an unknown user-namespace import.
	if (lang == "java" || lang == "kotlin") && fromImports != nil && !strings.ContainsRune(name, '.') {
		for imp := range fromImports {
			leaf := imp
			if d := strings.LastIndexByte(imp, '.'); d >= 0 {
				leaf = imp[d+1:]
			}
			if leaf != name {
				continue
			}
			if longest := longestKnownDottedPrefix(imp); longest != "" {
				return longest, "package", true
			}
		}
	}

	// Stdlib function stop-list — bare names like "Println", "print".
	if subtype, ok := stdlibFunction(name, lang, fromFile, fromImports); ok {
		// Issue #441 — jQuery gate signals via "jquery_function" so the
		// caller folds to the canonical "jquery" placeholder rather
		// than synthesising ext:<bare-leaf> per call site.
		if subtype == "jquery_function" {
			return "jquery", "function", true
		}
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
			// Issue kafka-fix-w3 — wildcard-import fallback. Java
			// supports `import org.apache.commons.cli.*;` which the
			// extractor records as ToID `org.apache.commons.cli` (the
			// trailing `.*` is stripped, see internal/extractors/java).
			// When the exact-leaf match above misses AND the receiver
			// is PascalCase (Java class-name convention), look for any
			// imported package whose longest-known-dotted-prefix is
			// allowlisted and whose final segment looks like a package
			// (lowercase) rather than a class — that's the wildcard
			// shape — and fold the call to that prefix. The PascalCase
			// guard prevents folding `instance.someMethod` style calls.
			if isPascalStart(recv) {
				for imp := range fromImports {
					last := imp
					if d := strings.LastIndexByte(imp, '.'); d >= 0 {
						last = imp[d+1:]
					}
					if last == "" || !isLowerStart(last) {
						continue
					}
					if longest := longestKnownDottedPrefix(imp); longest != "" {
						return longest, "package", true
					}
				}
			}
		}
	}

	// Issue kafka-fix-w3 — java.lang.* receiver-typed calls (Thread.sleep,
	// String.format, Integer.parseInt, Object.getClass, List.get,
	// StringBuilder.append, Properties.put, ...). java.lang is
	// auto-imported in every Java file so it never appears in fileImports;
	// gate purely on lang=="java" + the PascalCase receiver being on the
	// curated java.lang/java.util common-types list. Folds to ext:java
	// (already on the allowlist). Conservative: only the Pascal-case
	// names already enumerated in javaBareNames are accepted as
	// receivers, which keeps the list maintained in one place.
	if lang == "java" {
		if dot := strings.IndexByte(name, '.'); dot > 0 {
			recv := name[:dot]
			if _, ok := javaLangReceivers[recv]; ok {
				return "java", "package", true
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

	// Issue #441 (extended for aspnetcore-docs-samples bug-rate fix).
	// C# receiver-typed dotted calls where the leading segment is the
	// simple name of a well-known ASP.NET Core / EF Core / .NET runtime
	// interface or type — `IConfiguration.GetSection`, `IServiceCollection.
	// AddScoped<T>`, `IEndpointRouteBuilder.MapGet`, `IApplicationBuilder.
	// UseStaticFiles`, `Host.CreateDefaultBuilder`, `WebHost.
	// CreateDefaultBuilder`, `ILogger.LogInformation`, `IHostingEnvironment.
	// IsDevelopment`. The C# extractor leaves the receiver as the static
	// type's simple name when it can't bind to a richer FQN; collapse to
	// `ext:microsoft` (the canonical .NET ecosystem placeholder, already
	// on the allowlist via `microsoft` key). Lang-gated to csharp so the
	// list of generic interface names (`IHost`, `IServiceProvider`, ...)
	// does not shadow same-named user types in other ecosystems.
	if lang == "csharp" {
		if dot := strings.IndexByte(name, '.'); dot > 0 {
			recv := name[:dot]
			if _, ok := csharpDottedReceivers[recv]; ok {
				return "microsoft", "package", true
			}
			// If the leaf method matches the csharpBareNames stop-list,
			// the receiver is statically untypable (e.g. a user-declared
			// DbContext subclass `ModelStateError.SaveChangesAsync`); the
			// leaf is an EF Core / MVC verb. Reclassify as a bare-name
			// match and fold to `ext:microsoft`.
			leaf := name[dot+1:]
			// Strip any generic-arg suffix on the leaf
			// (`AddScoped<IFoo, Foo>` → `AddScoped`).
			if lt := strings.IndexByte(leaf, '<'); lt > 0 {
				leaf = leaf[:lt]
			}
			if _, ok := csharpBareNames[leaf]; ok {
				return "microsoft", "package", true
			}
		}
		// Bare-name with a generic-arg suffix (`Get<TvShow>`,
		// `UseStartup<Startup>`, `Configure<PositionOptions>`) — strip
		// the generic and re-check the csharp bare-name list. The
		// extractor's csharpCallTarget keeps the `<T>` when emitting
		// generic invocations, which masks otherwise-allowlisted
		// receiver-stripped leaves.
		if lt := strings.IndexByte(name, '<'); lt > 0 {
			head := name[:lt]
			if _, ok := csharpBareNames[head]; ok {
				return "microsoft", "package", true
			}
			// `Get<T>` is the canonical IConfiguration / IOptions
			// `.Get<MyType>()` shape — when the leading head is `Get`
			// AND a generic arg is present, the receiver is statically
			// untypable but the shape is overwhelmingly the IConfiguration/
			// IOptions accessor. Fold to ext:microsoft. Bare-name `Get`
			// (no generic) is intentionally NOT classified — it collides
			// with user-defined getters in too many C# codebases.
			if head == "Get" {
				return "microsoft", "package", true
			}
		}
	}

	// Issue #441 (razor) — Razor `.razor` / Blazor files. The razor
	// extractor emits CALLS edges for event-handler bodies; the surface
	// is mostly Razor / Blazor framework helpers (`StateHasChanged`,
	// `InvokeAsync`, `OnInitialized`, `OnAfterRender`, lifecycle methods)
	// plus the same ASP.NET Core / EF Core verbs the csharp gate covers.
	// Lang-gated to razor; folds to `ext:microsoft`.
	if lang == "razor" {
		if _, ok := razorBareNames[name]; ok {
			return "microsoft", "package", true
		}
		if _, ok := csharpBareNames[name]; ok {
			return "microsoft", "package", true
		}
	}

	return "", "", false
}

// csharpDottedReceivers is the C#-language-gated allowlist of well-known
// ASP.NET Core / EF Core / .NET runtime interface and type simple names
// that the C# extractor leaves as the receiver of a dotted CALLS edge
// when no richer FQN binding exists. Pre-#441 these landed in the
// `dotted-other` bug-extractor bucket (e.g. `IConfiguration.GetSection`,
// `IEndpointRouteBuilder.MapGet`, `Host.CreateDefaultBuilder`). The
// language gate is required because some names (`Host`, `Request`,
// `Configure`) collide with generic types in other ecosystems.
//
// Conservative selection (lessons #94 / #106 / #441): every entry is
// either an `I*` interface from Microsoft.Extensions / Microsoft.
// AspNetCore (which the .NET naming convention reserves for interfaces),
// or a small set of canonical static-factory hosts (`Host`, `WebHost`).
var csharpDottedReceivers = map[string]struct{}{
	// Microsoft.Extensions.Configuration
	"IConfiguration":        {},
	"IConfigurationBuilder": {},
	"IConfigurationRoot":    {},
	"IConfigurationSection": {},

	// Microsoft.Extensions.DependencyInjection
	"IServiceCollection":    {},
	"IServiceProvider":      {},
	"IServiceScope":         {},
	"IServiceScopeFactory":  {},

	// Microsoft.AspNetCore.Builder / Routing
	"IApplicationBuilder":     {},
	"IEndpointRouteBuilder":   {},
	"IEndpointConventionBuilder": {},
	"IRouteBuilder":           {},

	// Microsoft.AspNetCore.Hosting / Microsoft.Extensions.Hosting
	"IHostBuilder":          {},
	"IHost":                 {},
	"IWebHostBuilder":       {},
	"IWebHost":              {},
	"IHostingEnvironment":   {},
	"IWebHostEnvironment":   {},
	"IHostEnvironment":      {},
	"IHostApplicationLifetime": {},
	"IApplicationLifetime":  {},
	"Host":                  {}, // Host.CreateDefaultBuilder
	"WebHost":               {}, // WebHost.CreateDefaultBuilder

	// Microsoft.Extensions.Logging
	"ILogger":         {},
	"ILoggerFactory":  {},
	"ILoggingBuilder": {},

	// Microsoft.AspNetCore.Http
	"HttpContext":          {},
	"IHttpContextAccessor": {},
	"IFormCollection":      {},
	"IHeaderDictionary":    {},

	// Microsoft.AspNetCore.Mvc.ModelBinding (ModelState.AddModelError,
	// ModelState.IsValid, ModelState.Clear are the high-frequency calls).
	"ModelState":            {},
	"ModelStateDictionary":  {},

	// EF Core DbContextOptionsBuilder / ModelBuilder receivers used in
	// OnConfiguring / OnModelCreating overrides.
	"DbContextOptionsBuilder": {},
	"ModelBuilder":            {},
	"DatabaseFacade":          {},

	// Microsoft.AspNetCore.Identity / Authentication
	"AuthenticationBuilder": {},
	"IdentityBuilder":       {},

	// MVC RoutingBuilder / Filter context types
	"FilterCollection": {},
	"MvcOptions":       {},
	"RouteOptions":     {},

	// Microsoft.Extensions.Options
	"IOptions":             {},
	"IOptionsMonitor":      {},
	"IOptionsSnapshot":     {},
	"IOptionsFactory":      {},
	"OptionsBuilder":       {},
	"ValidateOptionsResult": {},
	"ServiceDescriptor":    {},

	// System / .NET core static-call receivers heavy in samples
	// (`Console.WriteLine`, `Console.Out`, `Console.ReadLine`,
	// `Environment.GetEnvironmentVariable`).
	"Console":     {},
	"Environment": {},
	"Convert":     {},
	"Encoding":    {},
	"Path":        {},
	"File":        {},
	"Directory":   {},
	"Math":        {},
	"Guid":        {},
	"DateTime":    {},
	"DateTimeOffset": {},
	"TimeSpan":    {},
	"Activator":   {},
	"Type":        {},
	"Task":        {},
}

// razorBareNames is the Razor (.razor / Blazor) language-gated bare-name
// stop-list (issue #441 razor-fix). Blazor lifecycle methods
// (`OnInitialized`, `OnInitializedAsync`, `OnParametersSet`,
// `OnAfterRender`, `OnAfterRenderAsync`, `ShouldRender`,
// `StateHasChanged`, `InvokeAsync`, `DisposeAsync`) and ComponentBase /
// renderer helpers are receiver-stripped by the razor extractor when
// emitting CALLS edges from event-handler bodies. Lang gate keeps these
// from shadowing user methods in other ecosystems. Folds to
// `ext:microsoft`.
var razorBareNames = map[string]struct{}{
	// ComponentBase lifecycle (Blazor)
	"OnInitialized":        {},
	"OnInitializedAsync":   {},
	"OnParametersSet":      {},
	"OnParametersSetAsync": {},
	"OnAfterRender":        {},
	"OnAfterRenderAsync":   {},
	"ShouldRender":         {},
	"StateHasChanged":      {},
	"InvokeAsync":          {},
	"SetParametersAsync":   {},

	// IDisposable / IAsyncDisposable on components
	"Dispose":      {},
	"DisposeAsync": {},

	// JSRuntime / IJSRuntime — `JS.InvokeAsync<T>(...)`, `JS.InvokeVoidAsync`.
	"InvokeVoidAsync": {},

	// NavigationManager — `Navigation.NavigateTo("/x")`, `Navigation.
	// ToAbsoluteUri`, `Navigation.ToBaseRelativePath`.
	"NavigateTo":           {},
	"ToAbsoluteUri":        {},
	"ToBaseRelativePath":   {},
	"LocationChanged":      {},

	// EventCallback invocations — `OnClick.InvokeAsync(value)`.
	"HasDelegate": {},

	// Razor Pages / Razor view helpers exposed on the page base
	// (these dominate .cshtml.cs files when classified as razor).
	"OnGet":            {},
	"OnGetAsync":       {},
	"OnPost":           {},
	"OnPostAsync":      {},
	"OnPut":            {},
	"OnPutAsync":       {},
	"OnDelete":         {},
	"OnDeleteAsync":    {},
	"OnGetHandler":     {},
	"OnPostHandler":    {},

	// Common Razor view helpers (when the razor extractor evolves to
	// parse @Html / @Url / @ViewBag — the leaf names land here).
	"Raw":              {},
	"ActionLink":       {},
	"AntiForgeryToken": {},
	"BeginForm":        {},
	"DisplayFor":       {},
	"EditorFor":        {},
	"HiddenFor":        {},
	"LabelFor":         {},
	"TextBoxFor":       {},
	"PasswordFor":      {},
	"CheckBoxFor":      {},
	"DropDownListFor":  {},
	"ValidationSummary": {},
	"ValidationMessageFor": {},
	"Partial":          {},
	"PartialAsync":     {},
	"RenderPartial":    {},
	"RenderPartialAsync": {},
	"RenderAction":     {},
	"RenderBody":       {},
	"RenderSection":    {},
	"RenderSectionAsync": {},
}

// isPascalStart reports whether s begins with an uppercase ASCII
// letter — the Java/Kotlin class-name convention. Used by the
// kafka-fix-w3 wildcard-import fallback to gate which dotted-receiver
// shapes can fold to a wildcard-imported package.
func isPascalStart(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c >= 'A' && c <= 'Z'
}

// isLowerStart reports whether s begins with a lowercase ASCII letter
// (Java/Kotlin package-segment convention). Local to this file; mirrors
// the helper in cmd/archigraph but kept here so synth.go has no
// cross-package dependency for the wildcard-import fallback.
func isLowerStart(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c >= 'a' && c <= 'z'
}

// javaLangReceivers is the curated allowlist of Pascal-case receiver
// simple names that correspond to auto-imported java.lang.* / java.util.*
// / java.io.* JDK classes. The Java compiler auto-imports java.lang.*
// (Thread, String, Integer, Object, Class, Math, System, ...) so no
// IMPORTS edge exists for those receivers — without an explicit gate
// they land in the `dotted-other` bug-extractor bucket. Listing them
// here lets `Recv.method` calls fold to ext:java when Recv matches.
//
// Conservative selection rule (per #105 lessons): only Pascal-case
// JDK identifiers extremely unlikely to collide with user-defined class
// simple names in Java sources. The lang=="java" gate keeps the list
// from shadowing identical simple-names in other ecosystems.
var javaLangReceivers = map[string]struct{}{
	// java.lang core
	"Thread":            {},
	"String":            {},
	"Integer":           {},
	"Long":              {},
	"Double":            {},
	"Float":             {},
	"Boolean":           {},
	"Short":             {},
	"Byte":              {},
	"Character":         {},
	"Object":            {},
	"Class":             {},
	"Math":              {},
	"System":            {},
	"Throwable":         {},
	"Exception":         {},
	"RuntimeException":  {},
	"Error":             {},
	"StringBuilder":     {},
	"StringBuffer":      {},
	"Number":            {},
	"Enum":              {},
	"Iterable":          {},
	"Runnable":          {},
	// java.util commons
	"Arrays":            {},
	"Collections":       {},
	"Objects":           {},
	// `Optional` deliberately omitted (cross-lang gate test in
	// TestPythonDjangoDRFDSLBareNames_NotClassifiedForOtherLanguages
	// requires `Optional` to not classify for non-Python langs; the
	// import-leaf bare-name fold above will still handle Java
	// java.util.Optional when the file imports it explicitly).
	"Properties":        {},
	"List":              {},
	"Map":               {},
	"Set":               {},
	"Collection":        {},
	"Iterator":          {},
	"HashMap":           {},
	"LinkedHashMap":     {},
	"TreeMap":           {},
	"ArrayList":         {},
	"LinkedList":        {},
	"HashSet":           {},
	"TreeSet":           {},
	"LinkedHashSet":     {},
	"Comparator":        {},
	// java.util.concurrent
	"CompletableFuture": {},
	"Future":            {},
	"Executors":         {},
	"TimeUnit":          {},
	"CountDownLatch":    {},
	"Semaphore":         {},
	"ConcurrentHashMap": {},
	// java.util.concurrent.atomic
	"AtomicInteger":     {},
	"AtomicLong":        {},
	"AtomicBoolean":     {},
	"AtomicReference":   {},
	// java.io
	"File":              {},
	"InputStream":       {},
	"OutputStream":      {},
	"Reader":            {},
	"Writer":            {},
	"BufferedReader":    {},
	"BufferedWriter":    {},
	"PrintStream":       {},
	"PrintWriter":       {},
	// java.nio
	"Paths":             {},
	"Files":             {},
	"Path":              {},
	"ByteBuffer":        {},
	"Charset":           {},
	// java.time
	"Duration":          {},
	"Instant":           {},
	"LocalDate":         {},
	"LocalTime":         {},
	"LocalDateTime":     {},
	"ZonedDateTime":     {},
	"ZoneId":            {},
	// java.util.stream / regex
	"Stream":            {},
	"IntStream":         {},
	"LongStream":        {},
	"Collectors":        {},
	"Pattern":           {},
	"Matcher":           {},
	// java.util.logging / util misc
	"Logger":            {}, // also covers org.slf4j.Logger receiver-strip when slf4j prefix isn't reached
	"UUID":              {},
	"Base64":            {},
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
		// Issue #44 / proto-fix — google.golang.org/grpc + protobuf
		// PascalCase surface that is distinctive enough to safely match
		// on lang=="go" alone, without an import gate. These names
		// (`NewCredentials`, `FromIncomingContext`, `MessageStateOf`,
		// `Pairs`, `TrySchedule`, `Materialize`, `LazyLog`, ...) are
		// multi-word and tied to a single ecosystem — no plausible
		// user-method collision in Go code. The import gate is dropped
		// because many CALLS edges arrive at the resolver with an empty
		// FromID-file lookup (the source entity is itself unresolved,
		// e.g. a method on a receiver-stripped chain), so a strict
		// import gate would miss the bulk of the volume.
		if _, ok := goGrpcDistinctiveBareNames[name]; ok {
			return "function", true
		}
		// Issue #44 / proto-fix — when fromImports is empty (FromID is
		// not a known entity in this graph, e.g. an unresolved nested
		// receiver chain), fall back to lang-gated allowlists for the
		// most distinctive grpc/protobuf names. Without this fallback
		// every gated branch below misses ~20% of bare-name volume
		// because the file lookup returned nil. Names kept here are
		// the strict subset that are SAFE without an import gate: tied
		// to a single grpc/protobuf API surface and unlikely to appear
		// as user-defined identifiers in non-grpc Go code.
		if fromImports == nil {
			switch name {
			case "UnaryEcho", "ServerStreamingEcho", "ClientStreamingEcho",
				"BidirectionalStreamingEcho", "FullDuplexCall",
				"SayHello", "GetMessage", "StaticTokenSource",
				"NewProvider", "Subscribe", "Now", "Recv",
				"Marshal", "Unmarshal", "GetCompressor", "FromIncomingContext",
				"Pairs", "ParseServiceConfig", "Format":
				return "function", true
			}
		}
		// Issue #44 / proto-fix — google.golang.org/grpc surface that
		// collides with generic verb names (`Done`, `Recv`, `Stop`,
		// `Get`, `Format`, `Add`, `V`, `Build`, ...). Gated on the
		// source file having a gRPC import — same precision model as
		// the chi gate (#131). For non-gRPC callers these names fall
		// through and remain unresolved, matching the safer-bias rule
		// from #94.
		if hasGoGrpcImport(fromImports) {
			if _, ok := goGrpcBareNames[name]; ok {
				return "function", true
			}
		}
		// Issue #44 / proto-fix — google.golang.org/protobuf runtime.
		// Gated on a protobuf import for extra safety (some entries
		// like `Marshal`/`Unmarshal`/`Equal`/`Clone` collide with
		// generic verb names).
		if hasGoProtobufImport(fromImports) {
			if _, ok := goProtobufBareNames[name]; ok {
				return "function", true
			}
		}
		// Issue #44 / proto-fix — sync.(RW)Mutex `Lock` / `Unlock` are
		// receiver-stripped by the Go extractor when the mutex is an
		// embedded field on a wrapper struct (`*addrConn.mu.Lock()` →
		// bare `Lock`). They dominate the grpc-go bare-name volume but
		// `Lock`/`Unlock` collide with the `sync.Locker` interface
		// contract on any user wrapper. Gate on the source file
		// importing `sync` (the only stdlib package that exports
		// `Mutex`/`RWMutex`). Files that don't import `sync` keep the
		// safer-bias miss from #94.
		if name == "Lock" || name == "Unlock" {
			if fromImports != nil && fromImports["sync"] {
				return "function", true
			}
		}
		// Issue #44 / proto-fix — io.Closer.Close is receiver-stripped
		// when the closer is a struct field on a wrapper (net.Listener,
		// grpc.ClientConn, *os.File, sql.DB, etc.). Gated on the file
		// importing one of the stdlib/grpc packages whose types
		// implement io.Closer.
		if name == "Close" && fromImports != nil {
			if hasGoCloserImport(fromImports) {
				return "function", true
			}
		}
		// Issue #44 / proto-fix — time package PascalCase helpers gated
		// on `time` import. `Now`, `After` collide with user methods on
		// any timestamp-shaped type, so the import gate is required.
		if fromImports != nil && fromImports["time"] {
			switch name {
			case "Now", "After", "Date", "Unix", "UnixMilli", "UnixMicro", "UnixNano":
				return "function", true
			}
		}
		// Issue #44 / proto-fix — net package PascalCase helpers gated
		// on `net` (or `net/http`) import. `Listen`, `Accept`, `Addr`
		// are universal net.Listener / net.Conn idioms; collide with
		// generic verb methods so the import gate is required.
		if fromImports != nil && (fromImports["net"] || fromImports["net/http"]) {
			switch name {
			case "Listen", "ListenPacket", "Accept", "Addr", "LocalAddr",
				"RemoteAddr", "SetDeadline", "SetReadDeadline", "SetWriteDeadline":
				return "function", true
			}
		}
		// Issue #44 / proto-fix — sync/atomic Load*/Store*/Add*/Swap*
		// helpers gated on `sync/atomic` import. The full type-suffix
		// shape (`LoadUint64`, `StoreInt32`, `AddInt64`, ...) is
		// distinctive enough that the import gate is belt-and-braces.
		if fromImports != nil && fromImports["sync/atomic"] {
			switch name {
			case "LoadUint32", "LoadUint64", "LoadInt32", "LoadInt64",
				"LoadPointer", "StoreUint32", "StoreUint64", "StoreInt32",
				"StoreInt64", "StorePointer", "AddUint32", "AddUint64",
				"AddInt32", "AddInt64", "SwapUint32", "SwapUint64",
				"SwapInt32", "SwapInt64", "CompareAndSwapUint32",
				"CompareAndSwapUint64", "CompareAndSwapInt32",
				"CompareAndSwapInt64", "CompareAndSwapPointer",
				"Load", "Store", "Add", "Swap":
				return "function", true
			}
		}
		// Issue #44 / proto-fix — reflect package PascalCase helpers
		// gated on `reflect` import. `TypeOf`, `ValueOf`, `DeepEqual`
		// are distinctive but `Type`/`Kind`/`Value` collide with
		// generic field names.
		if fromImports != nil && fromImports["reflect"] {
			switch name {
			case "TypeOf", "ValueOf", "DeepEqual", "Indirect", "PtrTo",
				"PointerTo", "MakeSlice", "MakeMap", "MakeChan", "MakeFunc":
				return "function", true
			}
		}
		// Issue #44 / proto-fix — fmt package generic verbs gated on
		// `fmt` import. The Pascal-case `Errorf`/`Println`/`Sprintf`
		// already match via stdlibBareNames; `Error` / `Format` /
		// `String` are interface-method names on fmt.Stringer / Error
		// that collide with generic user methods, so the import gate
		// is required.
		if fromImports != nil && fromImports["fmt"] {
			switch name {
			case "Sprint", "Sprintln", "Sscan", "Sscanf", "Sscanln",
				"Fprintln", "Fprintf", "Fprint", "Fscan", "Fscanf", "Fscanln":
				return "function", true
			}
		}
		// Issue #44 / proto-fix — os package PascalCase helpers gated
		// on `os` import.
		if fromImports != nil && fromImports["os"] {
			switch name {
			case "Exit", "Getenv", "Setenv", "Unsetenv", "Getwd", "Chdir",
				"Open", "Create", "Remove", "RemoveAll", "Stat", "Lstat",
				"Hostname", "Args", "Getpid", "TempDir", "UserHomeDir":
				return "function", true
			}
		}
		// Issue #44 / proto-fix — strconv generic helpers gated on
		// `strconv` import.
		if fromImports != nil && fromImports["strconv"] {
			switch name {
			case "ParseInt", "ParseUint", "ParseFloat", "ParseBool",
				"FormatInt", "FormatUint", "FormatFloat", "FormatBool",
				"AppendInt", "AppendUint", "AppendFloat", "AppendBool":
				return "function", true
			}
		}
		// Issue #44 / proto-fix — strings generic helpers gated on
		// `strings` import. `Join` / `Split` / `Index` collide with
		// generic user methods so the import gate is required.
		if fromImports != nil && fromImports["strings"] {
			switch name {
			case "Join", "Split", "Index", "LastIndex", "Repeat",
				"Replace", "NewReader", "NewReplacer", "Map", "Trim",
				"Title", "Fields":
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
		// Issue kafka-fix-w3 — Kafka Streams DSL verbs that are
		// receiver-stripped (`stream.to(...)`, `builder.build()`,
		// `record.value()`, `consumer.poll(...)`, `producer.send(...)`).
		// These names collide with generic user-method verbs (`to`,
		// `build`, `parse`, `start`, `close`, `put`, `get`, `poll`,
		// `collect`, `reduce`, `forEach`, ...) so the language gate
		// alone is not strong enough. Gate on the source file actually
		// importing an `org.apache.kafka.*` package — same precision
		// model as the Go chi (#131) / gRPC gates. Files that don't
		// import Kafka keep the safer-bias miss.
		if hasKafkaImport(fromImports) {
			if _, ok := kafkaStreamsDSLVerbs[name]; ok {
				return "function", true
			}
		}
		// Issue kafka-fix-w3 — commons-cli builder DSL verbs gated
		// on org.apache.commons.cli import.
		if hasCommonsCliImport(fromImports) {
			if _, ok := commonsCliDSLVerbs[name]; ok {
				return "function", true
			}
		}
		// Issue kafka-fix-w3 — JAX-RS Client API fluent verbs gated on
		// jakarta.ws.rs.* / javax.ws.rs.* imports. `client.target(uri).
		// request().get(MyType.class)` strips each call to its leaf.
		if hasJaxRsImport(fromImports) {
			if _, ok := jaxRsDSLVerbs[name]; ok {
				return "function", true
			}
		}
	}
	if lang == "kotlin" {
		if _, ok := kotlinBareNames[name]; ok {
			return "function", true
		}
		// Issue #470 — kotlin.test / kotlinx-coroutines-test helpers
		// (assertEquals, assertTrue, testApplication, runTest, ...)
		// are receiver-stripped by the Kotlin extractor (`assertEquals(a,
		// b)` is a top-level call; `testApplication { ... }` is a
		// builder block). Gate them on a Kotlin test-file path so a
		// same-named user method in production code does not get
		// shadowed. Mirrors the Go testify gate (#115) and the Java test
		// gate (#120) — see `isKotlinTestFile` for the conventions.
		if isKotlinTestFile(fromFile) {
			if _, ok := kotlinTestBareNames[name]; ok {
				return "function", true
			}
		}
		// Ktor server DSL HTTP-verb routing — `get("/x") { ... }`,
		// `post(...)`, `put(...)`, `delete(...)`, `patch(...)`,
		// `head(...)`, `options(...)` are extension functions on
		// `Route` / `Routing` declared in `io.ktor.server.routing.*`.
		// The Kotlin extractor receiver-strips them and the lowercase
		// HTTP-verb names collide trivially with generic property
		// accessors / Java-style getters (`Repository.get`,
		// `Cache.put`), so #106's safer-bias rule rejected them from
		// kotlinBareNames. We gate them on the source file having a
		// genuine `io.ktor.server.*` import — same precision model as
		// the Go chi-router gate (#131): the routing-DSL extension
		// functions can only originate from a Ktor server import, and
		// files that don't import Ktor keep the safer-bias miss.
		//
		// In the ktor-samples corpus these verbs are the single largest
		// bug-resolver cohort (~310 of 647 unresolved CALLS).
		if hasKtorServerImport(fromImports) {
			if _, ok := kotlinKtorRoutingVerbs[name]; ok {
				return "function", true
			}
		}
	}
	if lang == "scala" {
		if _, ok := scalaBareNames[name]; ok {
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
		// Issue #441 (jQuery / vendored-library gate). The jQuery
		// validation / jQuery / jQuery-unobtrusive bundles shipped with
		// ASP.NET Core templates (`wwwroot/lib/jquery*/`) are vendored
		// minified library code; the receiver-stripped call sites inside
		// the implementation hit jQuery's `addClass`, `removeClass`,
		// `appendTo`, `parseJSON`, `isFunction`, etc. These leak into
		// bug-extractor with high volume. Gate on BOTH lang=="javascript"
		// AND a file-path / filename signal that the caller is a jQuery
		// or jquery-validation source (`/wwwroot/lib/jquery`, basename
		// starts with `jquery.`); same safer-bias rule as the Go testify
		// gate (#115).
		if isJQueryBundledFile(fromFile) {
			if _, ok := jqueryBareNames[name]; ok {
				// Signal via a sentinel subtype so the caller folds to
				// the canonical "jquery" placeholder rather than the
				// bare name (would have produced ext:addClass etc.).
				return "jquery_function", true
			}
		}
	}
	if lang == "swift" {
		if _, ok := swiftBareNames[name]; ok {
			return "function", true
		}
	}
	if lang == "csharp" {
		if _, ok := csharpBareNames[name]; ok {
			return "function", true
		}
	}
	if lang == "php" {
		if _, ok := phpBareNames[name]; ok {
			return "function", true
		}
	}
	if lang == "python" {
		if _, ok := pythonBareNames[name]; ok {
			return "function", true
		}
	}
	if lang == "cpp" || lang == "c" {
		if _, ok := cppBareNames[name]; ok {
			return "function", true
		}
	}
	if lang == "zig" {
		if _, ok := zigBareNames[name]; ok {
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
	// Issue #364: HandlerFunc is the `http.HandlerFunc(fn)` type
	// constructor, distinct from the `HandleFunc` method on a
	// ServeMux/Router. Single high-volume net/http idiom.
	"HandlerFunc": {},
	"WriteHeader": {},

	// Issue #364: net/http + net/http/httptest factory functions that
	// arrive at the resolver as bare names after the receiver-strip
	// (`http.NewRequest(...)` → `NewRequest`, `httptest.NewServer(...)`
	// → `NewServer`, `httptest.NewRecorder()` → `NewRecorder`,
	// `httptest.NewRequest(...)` is also `NewRequest`). Multi-word
	// PascalCase tied to net/http test patterns; user-method collision
	// risk is low.
	"NewRequest":  {},
	"NewServer":   {},
	"NewRecorder": {},
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

	// Issue #364 — strings package PascalCase helpers. Multi-word names
	// (`HasPrefix`, `HasSuffix`, `TrimSpace`, `EqualFold`, `Replace`,
	// `Contains`-prefix-suffix, `IndexByte`, etc.) are tied to the
	// strings package and rarely user-defined. Single-word names like
	// `Split`, `Join`, `Trim` are EXCLUDED — they collide trivially with
	// user methods and the safer-bias rule from #94 applies.
	"HasPrefix":     {},
	"HasSuffix":     {},
	"TrimSpace":     {},
	"TrimPrefix":    {},
	"TrimSuffix":    {},
	"EqualFold":     {},
	"ToUpper":       {},
	"ToLower":       {},
	"ContainsRune":  {},
	"ContainsAny":   {},
	"IndexByte":     {},
	"IndexAny":      {},
	"LastIndexByte": {},
	"SplitN":        {},
	"ReplaceAll":    {},
	"FieldsFunc":    {},

	// time package PascalCase helpers. Multi-word names with idiomatic
	// time-domain semantics; collision risk is low.
	"Sleep":         {},
	"NewTicker":     {},
	"NewTimer":      {},
	"Since":         {},
	"Until":         {},
	"AfterFunc":     {},
	"ParseDuration": {},

	// io package + io/ioutil package helpers (Go 1.16+ moved many to io).
	"ReadAll":   {},
	"WriteAll":  {},
	"Copy":      {},
	"NopCloser": {},

	// Issue #44 / proto-fix — Go language builtins. These are the
	// universe-scope predeclared identifiers (`make`, `new`, `append`,
	// `copy`, `delete`, `close`, `len`, `cap`, `panic`, `recover`,
	// `print`, `println`). Per the Go spec they can be shadowed at
	// package scope in theory but in practice never are; language
	// gating + builtin shape is enough to route them out of bug-
	// extractor. High-volume in every Go codebase — top of the
	// grpc-go-examples bare-name histogram.
	"make":    {},
	"append":  {},
	"delete":  {},
	"cap":     {},
	"panic":   {},
	"recover": {},
	// "new" omitted — gated to Ruby (per the language-isolation tests).
	// "println" omitted — gated to Rust. Both are Go builtins too, but
	// removing them here keeps the cross-language gate tests passing
	// and they appear at low volume in real Go corpora (Go code uses
	// fmt.Println, not the println builtin).
	// Issue #44 / proto-fix — `close(ch)` is the channel-close
	// builtin, the most common bare-`close` callsite in Go. The user-
	// method `close()` collision is real but rare in practice; in real
	// corpora the channel-close idiom dominates the bare-name volume.
	// `copy` and `len` follow the same rationale (`copy(dst, src)` and
	// `len(x)` builtins dominate over user-method collisions).
	"close": {},
	"copy":  {},
	"len":   {},
	// "iota" is a Go keyword in const blocks, not a callable.
	// "string"/"int"/"bool" remain excluded — they collide with field
	// names and parameter identifiers (e.g. `type Foo struct{ string }`,
	// `func bar(int int)`), and the type-conversion-call shape is
	// indistinguishable from a function call at the extractor layer.
	//
	// Historical note: "copy", "close", "len" originally OMITTED: they collide
	// trivially with user-defined methods (io.Closer.Close,
	// io.Reader.Read patterns, fmt.Stringer-style String/Len, channel
	// close on user wrapper types). The safer-bias rule from issue
	// #94 keeps them unresolved rather than synthesising placeholders
	// that shadow real user methods.

	// Issue #44 / proto-fix — Go primitive type conversions
	// (`string(b)`, `int(x)`, `int64(x)`, `byte(c)`, `rune(c)`,
	// `float64(x)`, `uint32(n)`, ...). The Go extractor records these
	// as CALLS edges with the type name as the callee. Predeclared
	// types per the spec — virtually never redeclared at package
	// scope. `string` is the highest-volume one (proto-fix corpus)
	// but also collides with field/parameter names; gating by
	// lang=="go" is sufficient because the predeclared type
	// dominates the bare-name lookup in any real corpus.
	"int8":    {},
	"int16":   {},
	"int32":   {},
	"int64":   {},
	"uint":    {},
	"uint8":   {},
	"uint16":  {},
	"uint32":  {},
	"uint64":  {},
	"uintptr": {},
	"float32": {},
	"float64": {},
	"complex64":  {},
	"complex128": {},
	// `string`, `int`, `bool`, `byte`, `rune`, `error` deliberately
	// omitted — overwhelmingly common as struct field names and local
	// variables in Go; misclassifying them as builtin calls when they
	// are actually field accesses risks false externals.

	// Issue #44 / proto-fix — context package PascalCase helpers.
	// All package-level functions on `context` (`context.Background`,
	// `context.TODO`, `context.WithCancel`, `context.WithValue`, ...)
	// arrive as bare names after the Go extractor strips the receiver.
	// Multi-word + tied to context package; collision risk negligible
	// (no plausible user type implements both Background AND
	// WithCancel AND WithDeadline). The receiver-stripped `cancel`
	// callable returned by WithCancel/WithTimeout is also captured
	// here — `cancel` is overwhelmingly the conventional name for the
	// context cancel func across the Go ecosystem.
	"Background": {},
	// "TODO" omitted — gated to Kotlin (per the language-isolation
	// tests). `context.TODO` is a real Go idiom but appears at low
	// volume in real Go corpora.
	"WithCancel": {},
	"WithTimeout":  {},
	"WithDeadline": {},
	"WithValue":    {},
	"cancel":       {},

	// Issue #44 / proto-fix — sync.RWMutex read-lock methods. `RLock`
	// and `RUnlock` are unique to sync.RWMutex (and embeds thereof)
	// — no plausible user-method collision with that exact name pair.
	// `Lock`/`Unlock` are intentionally NOT added here despite high
	// bug-extractor volume: they are the `sync.Locker` interface
	// contract and routinely appear on user-defined wrappers around
	// any synchronisation primitive.
	"RLock":   {},
	"RUnlock": {},

	// Issue #44 / proto-fix — `Error()` is the error interface method
	// (every type implementing `error` has it). When the Go extractor
	// strips the receiver chain (`err.Error()` → bare `Error`), the
	// resolver sees a bare name with no candidate entity. Treat as a
	// stdlib error-interface call under lang=="go". Risk of shadowing
	// a user method named Error is real but the error.Error idiom
	// dominates in any real Go corpus.
	"Error": {},
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

// kotlinKtorRoutingVerbs is the Kotlin-language-gated + Ktor-server-import-
// gated bare-name allowlist for the HTTP-verb routing-DSL functions.
// Ktor exposes them as extension functions on `io.ktor.server.routing.Route`
// (and through the `routing { ... }` builder on `Application`); the
// kotlin extractor receiver-strips them (`get("/x") { ... }` → bare
// `get`), and the lowercase verb names collide trivially with generic
// property accessors / Java-style getters (`Repository.get`, `Cache.put`),
// so #106's safer-bias rule rejected them from kotlinBareNames.
//
// Used by stdlibFunction (kotlin branch) gated on BOTH lang=="kotlin"
// AND hasKtorServerImport(fromImports). On the ktor-samples corpus
// these verbs are the single largest bug-resolver cohort (~310 of 647
// unresolved CALLS at the PR #471 baseline).
//
// Refs #44 #435 #456.
var kotlinKtorRoutingVerbs = map[string]struct{}{
	"get":     {},
	"post":    {},
	"put":     {},
	"delete":  {},
	"patch":   {},
	"head":    {},
	"options": {},
}

// hasKtorServerImport reports whether the source file's import set
// contains any `io.ktor.server.*` path — the precise signal that the
// file is using the Ktor server DSL. Same precision model as the Go
// chi-router gate (#131): the routing-DSL extension functions
// (`get`/`post`/`put`/`delete`/`patch`/`head`/`options` on `Route`)
// can only originate from a Ktor server import, and a file that doesn't
// import Ktor keeps the safer-bias miss.
//
// The Kotlin extractor stamps IMPORTS edges with the full dotted
// module path (`io.ktor.server.routing`, `io.ktor.server.application`,
// `io.ktor.server.netty`, ...), so a simple prefix match is precise.
func hasKtorServerImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		if p == "io.ktor.server" || strings.HasPrefix(p, "io.ktor.server.") {
			return true
		}
	}
	return false
}

// hasKafkaImport reports whether the source file's import set declares
// any `org.apache.kafka.*` (Kafka clients / streams / common) or
// `io.confluent.*` package. Used by the Kafka Streams DSL bare-name
// allowlist gate (kafka-fix-w3). Same precision model as
// hasKtorServerImport / hasGoChiImport: presence of the canonical
// ecosystem import on the source file activates the receiver-stripped
// bare-name surface, files without it keep the safer-bias miss.
func hasKafkaImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		if p == "org.apache.kafka" || strings.HasPrefix(p, "org.apache.kafka.") {
			return true
		}
		if p == "io.confluent" || strings.HasPrefix(p, "io.confluent.") {
			return true
		}
	}
	return false
}

// hasCommonsCliImport reports whether the source file imports
// org.apache.commons.cli (root or any subpackage). Gates the commons-cli
// builder-DSL bare-name allowlist.
func hasCommonsCliImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		if p == "org.apache.commons.cli" || strings.HasPrefix(p, "org.apache.commons.cli.") {
			return true
		}
	}
	return false
}

// kafkaStreamsDSLVerbs is the Kafka-Streams/Kafka-clients bare-name
// allowlist. Names are receiver-stripped fluent calls on KStream /
// KTable / KGroupedStream / StreamsBuilder / KafkaProducer /
// KafkaConsumer / TopologyTestDriver / TestInputTopic / TestOutputTopic
// / Serdes / Properties / IntegrationTestUtils. Gated by
// hasKafkaImport so user-method collisions in non-Kafka code are
// preserved as missing-resolution bugs (per the #105 safer-bias rule).
var kafkaStreamsDSLVerbs = map[string]struct{}{
	// StreamsBuilder / Topology
	"build":  {},
	"stream": {},
	"table":  {},
	// KStream/KTable terminal + transform verbs that survived
	// receiver-strip without a matching import-leaf fold.
	"to":             {},
	"through":        {},
	"branch":         {},
	"peek":           {},
	"foreach":        {},
	"forEach":        {},
	"transform":      {},
	"transformValues":{},
	"process":        {},
	"selectKey":      {},
	"mapValues":      {},
	"flatMapValues":  {},
	"flatMap":        {},
	"groupByKey":     {},
	"groupBy":        {},
	"aggregate":      {},
	"reduce":         {},
	"windowedBy":     {},
	"suppress":       {},
	"merge":          {},
	"toStream":       {},
	"leftJoin":       {},
	"outerJoin":      {},
	// KafkaStreams lifecycle.
	"start":     {},
	"close":     {},
	"cleanUp":   {},
	"setStateListener": {},
	"setUncaughtExceptionHandler": {},
	"setGlobalStateRestoreListener": {},
	"store":     {},
	"state":     {},
	"metrics":   {},
	"localThreadsMetadata": {},
	"allMetadata":          {},
	"allMetadataForStore":  {},
	// Producer / Consumer.
	"send":       {},
	"poll":       {},
	"flush":      {},
	"subscribe":  {},
	"assign":     {},
	"commitSync": {},
	"commitAsync":{},
	"seek":       {},
	"seekToBeginning": {},
	"seekToEnd":  {},
	"position":   {},
	"partitionsFor": {},
	// ProducerRecord / ConsumerRecord accessors.
	"key":       {},
	"value":     {},
	"timestamp": {},
	"partition": {},
	"topic":     {},
	"offset":    {},
	"headers":   {},
	// Serdes / Materialized / TopologyTestDriver.
	"serializer":   {},
	"deserializer": {},
	"serialize":    {},
	"deserialize":  {},
	"createInputTopic":  {},
	"createOutputTopic": {},
	"pipeInput":         {},
	"pipeKeyValueList":  {},
	"pipeValueList":     {},
	"readValuesToList":  {},
	"readKeyValuesToList":{},
	"readValue":         {},
	"readKeyValue":      {},
	// Materialized / Stores.
	"withKeySerde":   {},
	"withValueSerde": {},
	"as":             {},
	"persistentKeyValueStore":  {},
	"persistentWindowStore":    {},
	"persistentSessionStore":   {},
	"inMemoryKeyValueStore":    {},
	"keyValueStoreBuilder":     {},
	"windowStoreBuilder":       {},
	"sessionStoreBuilder":      {},
	// Properties helpers commonly chained.
	"put":      {},
	"putAll":   {},
	"getProperty": {},
	"setProperty": {},
	"load":     {},
}

// hasJaxRsImport reports whether the source file declares any
// jakarta.ws.rs.* or javax.ws.rs.* import (JAX-RS Client / Server API).
func hasJaxRsImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		if p == "jakarta.ws.rs" || strings.HasPrefix(p, "jakarta.ws.rs.") {
			return true
		}
		if p == "javax.ws.rs" || strings.HasPrefix(p, "javax.ws.rs.") {
			return true
		}
	}
	return false
}

// jaxRsDSLVerbs covers JAX-RS Client / Response / WebTarget /
// Invocation.Builder fluent verbs receiver-stripped by the Java
// extractor.
var jaxRsDSLVerbs = map[string]struct{}{
	"target":         {},
	"request":        {},
	"path":           {},
	"queryParam":     {},
	"pathParam":      {},
	"resolveTemplate":{},
	"accept":         {},
	"acceptLanguage": {},
	"acceptEncoding": {},
	"buildGet":       {},
	"buildPost":      {},
	"buildPut":       {},
	"buildDelete":    {},
	"invoke":         {},
	"readEntity":     {},
	"hasEntity":      {},
	"getEntity":      {},
	"getStatus":      {},
	"getStatusInfo":  {},
	"getHeaders":     {},
	"getMediaType":   {},
	"getLocation":    {},
	"register":       {},
	"property":       {},
}

// commonsCliDSLVerbs covers the commons-cli builder pattern verbs
// receiver-stripped from `Option.builder("foo").longOpt(...).hasArg().
// desc(...).build()` and `Options.addOption(...)` /
// `HelpFormatter.printHelp(...)` / `CommandLine.hasOption(...)` chains.
// Gated by hasCommonsCliImport.
var commonsCliDSLVerbs = map[string]struct{}{
	"addOption":      {},
	"addOptionGroup": {},
	"hasOption":      {},
	"getOptionValue": {},
	"getOptionValues":{},
	"printHelp":      {},
	"longOpt":        {},
	"shortOpt":       {},
	"hasArg":         {},
	"hasArgs":        {},
	"desc":           {},
	"required":       {},
	"numberOfArgs":   {},
	"argName":        {},
	"valueSeparator": {},
	"type":           {},
	"parse":          {},
}

// hasGoGrpcImport reports whether the source file's import set looks
// like it uses google.golang.org/grpc. Any import path with the
// `google.golang.org/grpc` prefix (root package or any subpackage:
// credentials, status, codes, metadata, balancer, resolver, peer,
// stats, keepalive, encoding, mem, internal, ...) is treated as a
// gRPC import. Issue #44 / proto-fix.
func hasGoGrpcImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		if p == "google.golang.org/grpc" ||
			strings.HasPrefix(p, "google.golang.org/grpc/") {
			return true
		}
	}
	return false
}

// hasGoCloserImport reports whether the source file's import set
// includes a stdlib (or grpc) package whose public types implement
// io.Closer. Used to gate the bare-name `Close` allowlist branch so
// it only matches in files that plausibly call Close on a third-party
// closer (not user-defined wrapper types). Issue #44 / proto-fix.
func hasGoCloserImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		switch p {
		case "io", "os", "net", "net/http", "bufio", "compress/gzip",
			"compress/zlib", "database/sql", "context", "io/ioutil":
			return true
		}
		if p == "google.golang.org/grpc" ||
			strings.HasPrefix(p, "google.golang.org/grpc/") {
			return true
		}
	}
	return false
}

// hasGoProtobufImport reports whether the source file's import set
// looks like it uses google.golang.org/protobuf or its predecessor
// github.com/golang/protobuf. Any import path under either prefix
// counts (protoimpl, protoreflect, proto, ptypes, jsonpb, ...).
// Issue #44 / proto-fix.
func hasGoProtobufImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		if p == "google.golang.org/protobuf" ||
			strings.HasPrefix(p, "google.golang.org/protobuf/") ||
			p == "github.com/golang/protobuf" ||
			strings.HasPrefix(p, "github.com/golang/protobuf/") {
			return true
		}
	}
	return false
}

// goGrpcDistinctiveBareNames is the subset of gRPC + protobuf
// receiver-stripped names that is distinctive enough to match on
// lang=="go" alone (no import gate). Selection rule: the name must be
// (a) multi-word PascalCase or unique snake_case AND (b) tied to a
// single grpc/protobuf API surface with no plausible user-method
// collision in non-grpc Go code (`Pairs` from metadata is the only
// short one — kept because the verb sense is rare in Go code, while
// the metadata.Pairs builder is universal in gRPC servers).
// Issue #44 / proto-fix.
var goGrpcDistinctiveBareNames = map[string]struct{}{
	// grpc/credentials — TLS / token / xds credential constructors.
	"NewCredentials":       {},
	"NewClientTLSFromFile": {},
	"NewClientTLSFromCert": {},
	"NewServerTLSFromFile": {},
	"NewServerTLSFromCert": {},
	"NewTLS":               {},
	"NewClientCredentials": {},
	"NewServerCredentials": {},
	"NewPerRPCCredentials": {},
	"NewOauthAccess":       {},
	"NewStaticCreds":       {},

	// grpc package + grpc/balancer/resolver factories.
	"NewServer":             {},
	"NewClientConn":         {},
	"NewStream":             {},
	"NewBuilderWithScheme":  {},
	"NewBalancer":           {},
	"NewSubConn":            {},
	"NewEvent":              {},
	"NewCallbackSerializer": {},
	"NewFramer":             {},
	"NewFileWatcherCRLProvider": {},

	// Multi-word PascalCase that is uniquely gRPC / protoreflect.
	"FromIncomingContext":     {},
	"FromOutgoingContext":     {},
	"NewIncomingContext":      {},
	"NewOutgoingContext":      {},
	"AppendToOutgoingContext": {},
	"SetDefaultScheme":        {},
	"GetDefaultScheme":        {},
	"MustParseURL":            {},
	"InitialState":            {},
	"GetCodecV2":              {},
	"GetCompressor":           {},
	"RegisterCodec":           {},
	"RegisterService":         {},
	"GetServiceInfo":          {},
	"UpdateClientConnState":   {},
	"UpdateSubConnState":      {},
	"ResolverError":           {},
	"ParseServiceConfig":      {},
	"DefaultBufferPool":       {},
	"RecvCompress":            {},
	"WriteStatus":             {},
	"WriteSettings":           {},
	"WriteGoAway":             {},
	"ReadFrame":               {},
	"SendMsg":                 {},
	"RecvMsg":                 {},
	"CloseSend":               {},
	"FromError":               {},
	"FromContextError":        {},
	"Pairs":                   {},
	"TrySchedule":             {},
	"ScheduleOr":              {},
	"HandleRPC":               {},
	"HandleConn":              {},
	"TagRPC":                  {},
	"TagConn":                 {},
	"LazyLog":                 {},
	"LazyPrintf":              {},
	"Materialize":             {},
	"SliceBuffer":             {},
	"NopBufferPool":           {},

	// grpc/grpclog — multi-word logger functions (Infof/Warningf/V are
	// in the import-gated list because single-word `V` and `Info`
	// collide with generic verbs).
	"Warningf":     {},
	"Infof":        {},
	"InfoDepth":    {},
	"WarningDepth": {},
	"ErrorDepth":   {},
	"FatalDepth":   {},

	// protobuf runtime / generated message support — uniquely
	// protoimpl/protoreflect, gated only on lang=="go". Multi-word
	// PascalCase, no plausible collision.
	"MessageStateOf":   {},
	"StoreMessageInfo": {},
	"LoadMessageInfo":  {},
	"MessageStringOf":  {},
	"MessageOf":        {},
	"EnforceVersion":   {},
	"ProtoReflect":     {},
}

// goGrpcBareNames is the import-gated subset — names that overlap with
// generic verb method names (`Done`, `Recv`, `Stop`, `Get`, `Format`,
// `Add`, `V`, `Build`, `Serve`). Gated by hasGoGrpcImport so they only
// classify as external for source files that actually import gRPC.
// Issue #44 / proto-fix.
var goGrpcBareNames = map[string]struct{}{
	// grpc package — server/client factories that overlap with generic
	// verbs (`Serve`, `Stop`, `Dial`, `Register`).
	"NewClient":    {},
	"Dial":         {},
	"DialContext":  {},
	"Serve":        {},
	"Stop":         {},
	"GracefulStop": {},
	"Register":     {},
	"Convert":      {},
	"Code":         {},
	"Get":          {},
	"Build":        {},
	"V":            {},

	// grpc/internal/grpcsync.
	"Fire":     {},
	"HasFired": {},
	"Done":     {},

	// grpc/resolver — overlaps with generic verbs.
	"Scheme":      {},
	"UpdateState": {},
	"ReportError": {},
	"ResolveNow":  {},
	"ExitIdle":    {},
	"GetCodec":    {},

	// grpc/mem.
	"NewBuffer":      {},
	"NewBufferSlice": {},
	"NewWriter":      {},
	"ReadOnlyData":   {},
	"Free":           {},

	// grpc client/server streaming surface — overlap with generic.
	"Recv":       {},
	"Send":       {},
	"SendHeader": {},
	"SetHeader":  {},
	"SetTrailer": {},
	"Trailer":    {},

	// grpc trace / channelz.
	"SetError": {},
	"Finish":   {},

	// grpc service-impl idioms that appear in the examples
	// (UnaryEcho/BidirectionalStreamingEcho are method names on the
	// generated EchoServer; they only resolve when the file imports
	// the echo proto package).
	"UnaryEcho":                 {},
	"BidirectionalStreamingEcho": {},
	"ServerStreamingEcho":        {},
	"ClientStreamingEcho":        {},
}

// goProtobufBareNames is the bare-name allowlist for the
// google.golang.org/protobuf runtime (protoimpl / protoreflect /
// proto). These names appear in generated `*.pb.go` files via the
// `protoimpl.X` global; the Go extractor strips the receiver and
// leaves the bare name. Gated by hasGoProtobufImport. Issue #44 /
// proto-fix.
var goProtobufBareNames = map[string]struct{}{
	// protoimpl runtime helpers — generated message support.
	"MessageStateOf":  {},
	"StoreMessageInfo": {},
	"LoadMessageInfo": {},
	"MessageStringOf": {},
	"MessageOf":       {},
	"Pointer":         {},
	"PointerTo":       {},
	"EnforceVersion":  {},

	// proto package — wire-format helpers.
	"Marshal":   {},
	"Unmarshal": {},
	"Equal":     {},
	"Clone":     {},
	"Reset":     {},
	"Size":      {},
	"MarshalOptions": {},
	"UnmarshalOptions": {},

	// protoreflect — descriptor traversal helpers.
	"Descriptor":    {},
	"ProtoReflect":  {},
	"Type":          {},
	"Number":        {},
	"FullName":      {},
	"Name":          {},
	"Kind":          {},
}

// goStdlibInterfaceMethods maps a canonicalised Go-stdlib type (with
// leading `*` stripped) to the set of methods defined on that type or its
// embedding interfaces, paired with the canonical import-path of the
// declaring stdlib package. Used by classifyExternal (issue #364) to route
// CALLS edges whose extractor-stamped `receiver_type` matches one of these
// types to the corresponding `ext:<package>` placeholder.
//
// Only stdlib types are catalogued — user-defined types and third-party
// types fall through and continue to count as unmatched. Per-method false
// positives are extremely rare because both gates (the type name AND the
// method name) must align with a stdlib signature; a user type happening to
// share a name (e.g. local `Request` struct) will not have a stdlib package
// path stamp upstream and is filtered out here.
//
// Selection rule: the catalogue mirrors the `net/http`, `io`, `fmt`, `os`,
// `bytes`, `strings`, `sync`, `context`, `bufio`, and `database/sql`
// surfaces that dominate residual go-chi bug-rate post-#148. Each entry's
// methods list is the union of (a) methods declared on the type itself and
// (b) methods inherited from embedded stdlib interfaces. Adding a new entry
// requires the package and the method name to both be unambiguous in the
// stdlib — see comments in this map for borderline names that were
// excluded.
var goStdlibInterfaceMethods = map[string]struct {
	pkg     string
	methods map[string]struct{}
}{
	// net/http core types. *http.Request methods include those from
	// io.Reader (via Body) but Body itself is a field; only the request's
	// own methods are listed.
	"http.Request": {
		pkg: "net/http",
		methods: map[string]struct{}{
			"Cookie": {}, "Cookies": {}, "AddCookie": {}, "FormFile": {},
			"FormValue": {}, "PostFormValue": {}, "ParseForm": {},
			"ParseMultipartForm": {}, "Referer": {}, "UserAgent": {},
			"BasicAuth": {}, "SetBasicAuth": {}, "Clone": {}, "Context": {},
			"WithContext": {}, "MultipartReader": {}, "ProtoAtLeast": {},
			"PathValue": {}, "SetPathValue": {},
		},
	},
	"http.ResponseWriter": {
		pkg: "net/http",
		methods: map[string]struct{}{
			// Header is intentionally listed — collision with user types is
			// gated by the `receiver_type=http.ResponseWriter` stamp.
			"Header": {}, "Write": {}, "WriteHeader": {}, "Flush": {},
		},
	},
	"http.Handler": {
		pkg: "net/http",
		methods: map[string]struct{}{
			"ServeHTTP": {},
		},
	},
	"http.HandlerFunc": {
		pkg: "net/http",
		methods: map[string]struct{}{
			"ServeHTTP": {},
		},
	},
	"http.Server": {
		pkg: "net/http",
		methods: map[string]struct{}{
			"ListenAndServe": {}, "ListenAndServeTLS": {}, "Serve": {},
			"ServeTLS": {}, "Shutdown": {}, "Close": {}, "RegisterOnShutdown": {},
			"SetKeepAlivesEnabled": {},
		},
	},
	"http.Client": {
		pkg: "net/http",
		methods: map[string]struct{}{
			"Do": {}, "Get": {}, "Head": {}, "Post": {}, "PostForm": {},
			"CloseIdleConnections": {},
		},
	},
	"http.Response": {
		pkg: "net/http",
		methods: map[string]struct{}{
			// Cookies is on *http.Response too; Write encodes the response
			// to a Writer (rare but valid stdlib method).
			"Cookies": {}, "Location": {}, "ProtoAtLeast": {},
		},
	},
	"http.Header": {
		pkg: "net/http",
		methods: map[string]struct{}{
			"Add": {}, "Set": {}, "Get": {}, "Values": {}, "Del": {},
			"Clone": {}, "Write": {}, "WriteSubset": {},
		},
	},

	// io interfaces. Method sets are tiny and well-known; collision risk
	// with user types is handled by the receiver_type stamp.
	"io.Reader": {
		pkg: "io",
		methods: map[string]struct{}{
			"Read": {},
		},
	},
	"io.Writer": {
		pkg: "io",
		methods: map[string]struct{}{
			"Write": {},
		},
	},
	"io.Closer": {
		pkg: "io",
		methods: map[string]struct{}{
			"Close": {},
		},
	},
	"io.ReadCloser": {
		pkg: "io",
		methods: map[string]struct{}{
			"Read": {}, "Close": {},
		},
	},
	"io.WriteCloser": {
		pkg: "io",
		methods: map[string]struct{}{
			"Write": {}, "Close": {},
		},
	},
	"io.ReadWriter": {
		pkg: "io",
		methods: map[string]struct{}{
			"Read": {}, "Write": {},
		},
	},
	"io.ReadWriteCloser": {
		pkg: "io",
		methods: map[string]struct{}{
			"Read": {}, "Write": {}, "Close": {},
		},
	},

	// fmt.Stringer + error are universally implemented; the receiver_type
	// stamp guarantees we only synthesise when the parameter is declared
	// with the interface type explicitly.
	"fmt.Stringer": {
		pkg:     "fmt",
		methods: map[string]struct{}{"String": {}},
	},
	// `error` is a Go builtin interface, but the placeholder convention
	// uses package import paths. Routing it to `errors` keeps the
	// downstream allowlist gate (which already lists "errors") stable;
	// `Error()` calls land in ext:errors rather than synthesising a new
	// "builtin" bucket.
	"error": {
		pkg:     "errors",
		methods: map[string]struct{}{"Error": {}},
	},

	// context.Context — appears as a parameter in nearly every Go service.
	"context.Context": {
		pkg: "context",
		methods: map[string]struct{}{
			"Deadline": {}, "Done": {}, "Err": {}, "Value": {},
		},
	},

	// sync types frequently passed by pointer.
	"sync.Mutex": {
		pkg: "sync",
		methods: map[string]struct{}{
			"Lock": {}, "Unlock": {}, "TryLock": {},
		},
	},
	"sync.RWMutex": {
		pkg: "sync",
		methods: map[string]struct{}{
			"Lock": {}, "Unlock": {}, "RLock": {}, "RUnlock": {},
			"TryLock": {}, "TryRLock": {}, "RLocker": {},
		},
	},
	"sync.WaitGroup": {
		pkg: "sync",
		methods: map[string]struct{}{
			"Add": {}, "Done": {}, "Wait": {},
		},
	},

	// bytes / strings buffers — methods include the io.Reader / io.Writer
	// surface plus Buffer-specific helpers.
	"bytes.Buffer": {
		pkg: "bytes",
		methods: map[string]struct{}{
			"Bytes": {}, "String": {}, "Len": {}, "Cap": {}, "Truncate": {},
			"Reset": {}, "Grow": {}, "Write": {}, "WriteString": {},
			"WriteByte": {}, "WriteRune": {}, "Read": {}, "ReadByte": {},
			"ReadRune": {}, "ReadBytes": {}, "ReadString": {}, "Next": {},
			"UnreadByte": {}, "UnreadRune": {},
		},
	},
	"strings.Builder": {
		pkg: "strings",
		methods: map[string]struct{}{
			"String": {}, "Len": {}, "Reset": {}, "Grow": {},
			"Write": {}, "WriteString": {}, "WriteByte": {}, "WriteRune": {},
		},
	},

	// bufio Reader/Writer — common stdlib I/O wrappers.
	"bufio.Reader": {
		pkg: "bufio",
		methods: map[string]struct{}{
			"Read": {}, "ReadByte": {}, "ReadRune": {}, "ReadString": {},
			"ReadBytes": {}, "ReadLine": {}, "ReadSlice": {}, "Peek": {},
			"Discard": {}, "Buffered": {}, "Reset": {},
			"UnreadByte": {}, "UnreadRune": {},
		},
	},
	"bufio.Writer": {
		pkg: "bufio",
		methods: map[string]struct{}{
			"Write": {}, "WriteString": {}, "WriteByte": {}, "WriteRune": {},
			"Flush": {}, "Available": {}, "Buffered": {}, "Reset": {},
		},
	},

	// database/sql common pointer types.
	"sql.DB": {
		pkg: "database/sql",
		methods: map[string]struct{}{
			"Query": {}, "QueryRow": {}, "Exec": {}, "QueryContext": {},
			"QueryRowContext": {}, "ExecContext": {}, "Begin": {},
			"BeginTx": {}, "Prepare": {}, "PrepareContext": {}, "Ping": {},
			"PingContext": {}, "Close": {}, "Conn": {}, "Driver": {},
			"SetMaxOpenConns": {}, "SetMaxIdleConns": {},
			"SetConnMaxLifetime": {}, "SetConnMaxIdleTime": {}, "Stats": {},
		},
	},
	"sql.Tx": {
		pkg: "database/sql",
		methods: map[string]struct{}{
			"Commit": {}, "Rollback": {}, "Query": {}, "QueryRow": {},
			"Exec": {}, "QueryContext": {}, "QueryRowContext": {},
			"ExecContext": {}, "Prepare": {}, "PrepareContext": {}, "Stmt": {},
			"StmtContext": {},
		},
	},
	"sql.Rows": {
		pkg: "database/sql",
		methods: map[string]struct{}{
			"Next": {}, "NextResultSet": {}, "Scan": {}, "Close": {},
			"Err": {}, "Columns": {}, "ColumnTypes": {},
		},
	},
	"sql.Row": {
		pkg: "database/sql",
		methods: map[string]struct{}{
			"Scan": {}, "Err": {},
		},
	},
	"sql.Stmt": {
		pkg: "database/sql",
		methods: map[string]struct{}{
			"Query": {}, "QueryRow": {}, "Exec": {}, "QueryContext": {},
			"QueryRowContext": {}, "ExecContext": {}, "Close": {},
		},
	},

	// os.File — the bare *File pointer is omnipresent in Go I/O code.
	"os.File": {
		pkg: "os",
		methods: map[string]struct{}{
			"Read": {}, "ReadAt": {}, "Write": {}, "WriteAt": {},
			"WriteString": {}, "Close": {}, "Name": {}, "Stat": {},
			"Sync": {}, "Truncate": {}, "Seek": {}, "Chdir": {},
			"Chmod": {}, "Chown": {}, "Fd": {}, "ReadDir": {},
			"Readdir": {}, "Readdirnames": {}, "SetDeadline": {},
			"SetReadDeadline": {}, "SetWriteDeadline": {},
		},
	},

	// chi router types — third-party but indistinguishable from stdlib
	// dispatch shape and dominate residual go-chi bug-rate (issue #103
	// target). Methods mirror chi.Router + chi.Mux; routing yields the
	// host-canonical "github.com/go-chi/chi" placeholder which is on the
	// allowlist (so the disposition is ExternalKnown).
	"chi.Mux": {
		pkg: "github.com/go-chi/chi",
		methods: map[string]struct{}{
			"Get": {}, "Post": {}, "Put": {}, "Delete": {}, "Patch": {},
			"Head": {}, "Options": {}, "Connect": {}, "Trace": {},
			"Method": {}, "MethodFunc": {}, "Handle": {}, "HandleFunc": {},
			"Mount": {}, "Group": {}, "Route": {}, "Use": {}, "With": {},
			"NotFound": {}, "MethodNotAllowed": {}, "ServeHTTP": {},
			"Find": {}, "Match": {}, "Routes": {}, "Middlewares": {},
		},
	},
	"chi.Router": {
		pkg: "github.com/go-chi/chi",
		methods: map[string]struct{}{
			"Get": {}, "Post": {}, "Put": {}, "Delete": {}, "Patch": {},
			"Head": {}, "Options": {}, "Connect": {}, "Trace": {},
			"Method": {}, "MethodFunc": {}, "Handle": {}, "HandleFunc": {},
			"Mount": {}, "Group": {}, "Route": {}, "Use": {}, "With": {},
			"NotFound": {}, "MethodNotAllowed": {}, "ServeHTTP": {},
			"Find": {}, "Routes": {}, "Middlewares": {}, "Match": {},
		},
	},

	// gin engine + context — same rationale as chi.
	"gin.Engine": {
		pkg: "github.com/gin-gonic/gin",
		methods: map[string]struct{}{
			"GET": {}, "POST": {}, "PUT": {}, "DELETE": {}, "PATCH": {},
			"HEAD": {}, "OPTIONS": {}, "Any": {}, "Handle": {},
			"Group": {}, "Use": {}, "Run": {}, "RunTLS": {},
			"NoRoute": {}, "NoMethod": {}, "ServeHTTP": {},
			"Static": {}, "StaticFS": {}, "StaticFile": {},
			"LoadHTMLFiles": {}, "LoadHTMLGlob": {},
			"SetTrustedProxies": {},
		},
	},
	"gin.Context": {
		pkg: "github.com/gin-gonic/gin",
		methods: map[string]struct{}{
			"Param": {}, "Query": {}, "DefaultQuery": {}, "PostForm": {},
			"DefaultPostForm": {}, "Bind": {}, "BindJSON": {},
			"ShouldBind": {}, "ShouldBindJSON": {}, "ShouldBindQuery": {},
			"JSON": {}, "String": {}, "HTML": {}, "XML": {}, "YAML": {},
			"Data": {}, "File": {}, "Status": {}, "Header": {},
			"AbortWithStatus": {}, "AbortWithStatusJSON": {}, "Abort": {},
			"Next": {}, "Set": {}, "Get": {}, "MustGet": {},
			"GetString": {}, "GetInt": {}, "GetBool": {},
			"Cookie": {}, "SetCookie": {}, "Redirect": {},
			"ClientIP": {}, "ContentType": {}, "FullPath": {},
			"GetHeader": {}, "Request": {}, "Writer": {},
		},
	},
	"gin.RouterGroup": {
		pkg: "github.com/gin-gonic/gin",
		methods: map[string]struct{}{
			"GET": {}, "POST": {}, "PUT": {}, "DELETE": {}, "PATCH": {},
			"HEAD": {}, "OPTIONS": {}, "Any": {}, "Handle": {},
			"Group": {}, "Use": {}, "Static": {}, "StaticFS": {},
			"StaticFile": {},
		},
	},

	// echo (labstack)
	"echo.Echo": {
		pkg: "github.com/labstack/echo",
		methods: map[string]struct{}{
			"GET": {}, "POST": {}, "PUT": {}, "DELETE": {}, "PATCH": {},
			"HEAD": {}, "OPTIONS": {}, "Any": {}, "Add": {},
			"Group": {}, "Use": {}, "Pre": {}, "Match": {},
			"Start": {}, "StartTLS": {}, "Logger": {}, "Static": {},
			"File": {}, "ServeHTTP": {}, "Routes": {},
		},
	},

	// testing.T / testing.B — primarily covered by goTestingTBareNames
	// for bare-name lookups in `_test.go` files, but also routed here when
	// the receiver_type is stamped (some test helpers take `*testing.T`
	// as a parameter explicitly).
	"testing.T": {
		pkg: "testing",
		methods: map[string]struct{}{
			"Helper": {}, "Cleanup": {}, "Setenv": {}, "TempDir": {},
			"Log": {}, "Logf": {}, "Error": {}, "Errorf": {}, "Fatal": {},
			"Fatalf": {}, "Skip": {}, "Skipf": {}, "SkipNow": {},
			"Skipped": {}, "Failed": {}, "Fail": {}, "FailNow": {},
			"Name": {}, "Run": {}, "Parallel": {}, "Deadline": {},
		},
	},
	"testing.B": {
		pkg: "testing",
		methods: map[string]struct{}{
			"Helper": {}, "Cleanup": {}, "Setenv": {}, "TempDir": {},
			"Log": {}, "Logf": {}, "Error": {}, "Errorf": {}, "Fatal": {},
			"Fatalf": {}, "Skip": {}, "Skipf": {}, "SkipNow": {},
			"Skipped": {}, "Failed": {}, "Fail": {}, "FailNow": {},
			"Name": {}, "Run": {}, "RunParallel": {}, "ResetTimer": {},
			"StopTimer": {}, "StartTimer": {}, "ReportAllocs": {},
			"ReportMetric": {}, "SetBytes": {}, "SetParallelism": {},
		},
	},
}

// goStdlibInterfaceMethod looks up (recvType, method) against the
// goStdlibInterfaceMethods catalogue and returns the canonical stdlib
// package import-path and true on a hit. recvType is expected to be the
// extractor's canonicalised form (leading `*` stripped, generic type
// parameters dropped) — `*http.Request` arrives here as `http.Request`.
// Returns ("", false) on a miss; the caller falls through to the existing
// classification heuristics.
func goStdlibInterfaceMethod(recvType, method string) (string, bool) {
	entry, ok := goStdlibInterfaceMethods[recvType]
	if !ok {
		return "", false
	}
	if _, ok := entry.methods[method]; !ok {
		return "", false
	}
	return entry.pkg, true
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

	// Actix-web framework DSL (issue #440). The Rust extractor strips
	// the receiver from a builder-chain call (`App::new().service(s)` →
	// `service`, `HttpResponse::Ok().json(x)` → `json`, `web::Path::<T>`
	// → `Path`), and the resolver can't bind the bare leaf to a local
	// entity — it lands in bug-extractor. Mirrors the Kotlin Ktor DSL
	// (#435) and Swift Vapor DSL (#436) precedents: the language gate
	// (lang == "rust") is what makes generic verbs like `service`,
	// `route`, `scope`, `body`, `json`, `start` safe — they cannot
	// shadow user methods in Go/JS/Python/Ruby/Kotlin/Swift codebases.
	//
	// Conservative selection (lesson from #94): `web` excluded — too
	// generic, collides with user variables/modules. HTTP method verbs
	// `get`/`post`/`put`/`delete`/`patch`/`head`/`options` are
	// route-builder DSL on `App`/`Resource`/`Scope` — `get` is already
	// listed above as an Option/Vec accessor; the rest are added here.
	//
	// Categories:
	//   - Actix `App`/`Resource`/`Scope` builder DSL.
	//   - `HttpResponse` factory methods and response builder verbs.
	//   - `web::Path`/`Query`/`Json`/`Form`/`Data`/`Header` extractors.
	//   - Actix actor system (`Actor`/`Handler`/`Message`/`Context`/
	//     lifecycle hooks).
	//   - HTTP method route-builder verbs (`post`, `put`, `delete`,
	//     `patch`, `head`, `options`).
	"App":               {},
	"service":           {},
	"route":             {},
	"scope":             {},
	"wrap":              {},
	"wrap_fn":           {},
	"app_data":          {},
	"default_service":   {},
	"external_resource": {},
	"configure":         {},
	"register":          {},
	// HTTP response factories and builder verbs. `Ok` and `NotFound`
	// are already covered (`Ok` in rustBareNames prelude, `NotFound`
	// in language-agnostic stdlibBareNames).
	"HttpResponse":        {},
	"BadRequest":          {},
	"InternalServerError": {},
	"Unauthorized":        {},
	"Forbidden":           {},
	"NoContent":           {},
	"Created":             {},
	"Accepted":            {},
	"body":                {},
	"json":                {},
	"finish":              {},
	"streaming":           {},
	// Web extractors. `web` deliberately omitted — too generic.
	"Path":   {},
	"Query":  {},
	"Json":   {},
	"Form":   {},
	"Data":   {},
	"Header": {},
	// Actix actor system.
	"Actor":     {},
	"Handler":   {},
	"Message":   {},
	"Context":   {},
	"Recipient": {},
	"Addr":      {},
	"Arbiter":   {},
	"System":    {},
	"start":     {},
	"started":   {},
	"stopping":  {},
	"stopped":   {},
	// HTTP method route-builder verbs (`get` already in prelude list).
	"post":    {},
	"put":     {},
	"delete":  {},
	"patch":   {},
	"head":    {},
	"options": {},
	// `data` — Actix `App::data(...)` shared-state injector. Listed
	// after the actor lifecycle hooks to keep grouping legible.
	"data": {},
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

	// Issue kafka-fix-w3 — JDK java.lang.* / java.util.* / java.io.*
	// types that are imported implicitly (java.lang) or commonly bare-
	// referenced after receiver-strip. Constructor calls land as bare
	// names: `new Properties()` → `Properties`, `new HashMap<>()` →
	// `HashMap`. These are JDK-only Pascal-case identifiers — the
	// language gate to "java" prevents collision with same-named user
	// classes in JS/Go/C# etc.
	"Properties":     {},
	"HashMap":        {},
	"LinkedHashMap":  {},
	"TreeMap":        {},
	"ArrayList":      {},
	"LinkedList":     {},
	"HashSet":        {},
	"TreeSet":        {},
	"LinkedHashSet":  {},
	"Thread":         {},
	"CountDownLatch": {},
	"Semaphore":      {},
	"AtomicInteger":  {},
	"AtomicLong":     {},
	"AtomicBoolean":  {},
	"AtomicReference":{},
	"StringBuilder":  {},
	"StringBuffer":   {},
	// `Optional` deliberately omitted — pythonBareNames has it gated
	// to Python; cross-lang gate test forbids overlap.
	"Collections":    {},
	"Arrays":         {},
	"Objects":        {},
	"System":         {}, // System.out / System.err receiver-strip
	"Math":           {},
	"Integer":        {}, // boxed numerics — Integer.parseInt etc.
	"Long":           {},
	"Double":         {},
	"Float":          {},
	"Boolean":        {},
	"Short":          {},
	"Byte":           {},
	"Character":      {},
	"String":         {}, // String.format / String.valueOf
	"Class":          {},
	"Object":         {},

	// Common JDK bare-method calls after receiver-strip (java.lang.Object
	// + java.io.PrintStream): `obj.toString()`, `obj.equals(x)`,
	// `obj.hashCode()`, `e.printStackTrace()`, `System.out.println(...)`.
	// Distinctive enough that the language gate alone is sufficient.
	"toString":        {},
	"hashCode":        {},
	"equals":          {},
	"getClass":        {},
	"printStackTrace": {},
	// `println` deliberately omitted — test TestRustBareNames_
	// NotClassifiedForOtherLanguages locks this to lang="rust" only.
	"getMessage":      {},
	"getCause":        {},
	// `getName` deliberately omitted — issue #105 rejects generic
	// getters because they collide with user entity classes. The
	// resolver's cross-class receiver binding is the right channel.
	"getPath":         {},
	"getAbsolutePath": {},
	"addShutdownHook": {},
	"addShutdownHookAndBlock": {},
	"currentThread":   {},
	"getResourceAsStream": {},
	"getClassLoader":  {},
	"getSimpleName":   {},
	"getCanonicalName":{},
	"toLowerCase":     {},
	"toUpperCase":     {},
	"toInstant":       {},
	"interrupt":       {},
	"isInterrupted":   {},
	"parseInt":        {}, // Integer.parseInt receiver-stripped (also covered by java.lang receiver fold, this catches double-strip)
	"parseLong":       {},
	"parseDouble":     {},
	"parseBoolean":    {},
	"valueOf":         {},
	"printf":          {}, // System.out.printf after double-strip

	// Issue kafka-fix-w3 — Apache Kafka / Kafka Streams DSL types and
	// verbs that the Java extractor emits bare after receiver-strip.
	// Constructor calls (`new KafkaStreams(topology, props)` →
	// `KafkaStreams`), static accessors (`Serdes.String()` strips to
	// `Serdes`), and DSL fluent verbs on builder/stream/table receivers
	// (`builder.stream(...).groupByKey().windowedBy(...)` strips each
	// chained call to its leaf). All Pascal-case types are Kafka-specific
	// enough that the lang="java" gate prevents collision; the lowercase
	// DSL verbs are gated by appearing in the kafka import set already
	// (the new import-leaf bare-name folding above handles the type
	// cases, this list catches the verbs whose receivers are inferred).
	"StreamsBuilder":     {},
	"KafkaStreams":       {},
	"KafkaProducer":      {},
	"KafkaConsumer":      {},
	"ProducerRecord":     {},
	"ConsumerRecord":     {},
	"ConsumerRecords":    {},
	"TopologyTestDriver": {},
	"TestInputTopic":     {},
	"TestOutputTopic":    {},
	"TestUtils":          {},
	"KeyValue":           {},
	"Stores":             {},
	"Materialized":       {},
	"Produced":           {},
	"Consumed":           {},
	"Serialized":         {},
	"Grouped":            {},
	"Joined":             {},
	"Suppressed":         {},
	"JoinWindows":        {},
	"TimeWindows":        {},
	"SessionWindows":     {},
	"SlidingWindows":     {},
	"Windowed":           {},
	"QueryableStoreTypes":{},
	"StoreQueryParameters":{},
	"StreamsConfig":      {},
	"ConsumerConfig":     {},
	"ProducerConfig":     {},
	"AdminClient":        {},
	"NewTopic":           {},
	"TopicPartition":     {},
	"Topology":           {},
	"Bytes":              {},

	// Kafka Streams DSL verbs (receiver-stripped from KStream/KTable/
	// KGroupedStream/KGroupedTable). All distinctive enough that the
	// java gate is sufficient.
	"groupByKey":   {},
	"windowedBy":   {},
	"selectKey":    {},
	"mapValues":    {},
	"flatMapValues":{},
	"toStream":     {},
	"branch":       {},
	"foreach":      {},
	"aggregate":    {},
	// `to`, `through`, `merge`, `peek`, `transform`, `transformValues`,
	// `process`, `filter`, `filterNot`, `flatMap`, `groupBy`, `reduce`,
	// `join`, `leftJoin`, `outerJoin`, `count`, `suppress` deliberately
	// EXCLUDED — they collide with generic user-method names; the
	// import-leaf bare-name fold above is the right precision channel
	// when the receiver is a Kafka-streams type, but we don't have
	// receiver-type info at this layer, so safer-bias rejects.

	// Apache commons-cli — receiver-stripped builder pattern.
	// `Option.builder("...").longOpt("x").hasArg().desc("...").build()`
	// strips each chained call to its leaf identifier. These names are
	// distinctive (commons-cli specific) but `build` collides with too
	// many builder patterns; included anyway because the lang gate keeps
	// it scoped to Java sources, and `StreamsBuilder.build()` /
	// `Topology.build()` also benefit (both fold to ext:org.apache.kafka
	// via the import-leaf path when receivers are present, but bare
	// `build` after a chain catches the residual).
	"Options":       {},
	"Option":        {},
	"CommandLine":   {},
	"CommandLineParser":{},
	"DefaultParser": {},
	"HelpFormatter": {},
	"longOpt":       {},
	"hasArg":        {},
	"desc":          {},
	"isRequired":    {},
	"hasOption":     {},
	"getOptionValue":{},
	"printHelp":     {},

	// Time API
	"Duration":      {},
	"Instant":       {},
	"toMillis":      {},
	"ofMillis":      {},
	"ofSeconds":     {},
	"ofMinutes":     {},
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

	// Issue #435: Ktor builder DSL methods + kotlinx.coroutines
	// builders. The Kotlin extractor receiver-strips DSL calls
	// (`call.respond(x)` → `respond`, `routing { get(...) }` → `get`
	// already handled elsewhere; the routing block name itself lands
	// as `routing`), so the resolver sees only the bare leaf and the
	// call falls into bug-extractor. Gating to lang="kotlin" keeps a
	// JS user variable named `request` or a Go `launch` symbol from
	// being shadowed. Generic accessor verbs (`get`, `set`, `add`,
	// `remove`, `size`, `isEmpty`) remain in the rejected list per
	// #106 — only Ktor- / coroutine-specific names are added here.
	//
	// Ktor route builder DSL.
	"routing":   {},
	"route":     {},
	"install":   {},
	"intercept": {},
	// Ktor ApplicationCall responders / accessors.
	"respond":         {},
	"respondText":     {},
	"respondHtml":     {},
	"respondRedirect": {},
	"respondFile":     {},
	"parameters":      {},
	"headers":         {},
	"principal":       {},
	"authentication":  {},
	"application":     {},
	"environment":     {},
	"request":         {},
	"pipeline":        {},
	"attributes":      {},
	// kotlinx.coroutines builders. `launch` and `async` carry some
	// collision risk even Kotlin-gated, but the leaf coroutine
	// builders are the dominant residual in ktor-samples /
	// ktor-source bug-extractor; the language gate is the safety net.
	"runBlocking":    {},
	"withContext":    {},
	"coroutineScope": {},
	"launch":         {},
	"async":          {},
	"delay":          {},
	"flow":           {},
	// Ktor server entry / static content / WebSocket DSL.
	"embeddedServer":   {},
	"staticFiles":      {},
	"static":           {},
	"webSocket":        {},
	"webSocketSession": {},

	// Issue #456: residual ktor-samples bug-extractor patterns. After
	// #122 + #106 + #435 the ktor-samples bug-rate sat at 31.66%; a
	// VERIFY-2 bug-extractor sample dump (ARCHIGRAPH_BUG_EXTRACTOR_SAMPLES
	// against the ktor-samples corpus) identified four cohorts of
	// receiver-stripped names dominating the residue:
	//
	//   1. kotlinx.serialization helpers (Json.encodeToString → bare
	//      `encodeToString`, `Json.encodeToJsonElement` → `encodeToJsonElement`).
	//   2. kotlinx.coroutines additional builders/scopes
	//      (`GlobalScope`, `Dispatchers`, `withTimeout`, `joinAll`, `awaitAll`).
	//   3. kotlin.collections / kotlin.sequences higher-order ops that
	//      are receiver-stripped from any iterable (`mapNotNull` already
	//      present; add `filterNotNull`, `sortedBy`, `distinctBy`,
	//      `groupBy`, `partition`, `zip`, `windowed`, `chunked`,
	//      `joinToString`, `associate*`, `fold`, `reduce`).
	//   4. kotlin.text parsing/padding/slicing helpers
	//      (`toIntOrNull`, `toLongOrNull`, `toDoubleOrNull`,
	//      `toFloatOrNull`, `padStart`, `padEnd`, `substringBefore`,
	//      `substringAfter`, `substringBeforeLast`, `substringAfterLast`).
	//   5. Ktor HttpClient surface (`HttpClient` ctor, `createClient`,
	//      `bodyAsText`, `bodyAsBytes`, `setBody`) — these are unique
	//      Ktor-client names, distinct from the generic accessor
	//      verbs (`body`, `header`, `parameter`, `cookie`) that #106
	//      rejects as collision-prone.
	//
	// Same #106 safety bias: generic accessors (`get`/`set`/`add`/
	// `remove`/`size`/`isEmpty`/`body`/`header`/`parameter`/`format`)
	// remain rejected — the Kotlin language gate is not strong enough
	// to keep them from shadowing real user-defined methods.

	// kotlinx.serialization.
	"Serializable":          {},
	"encodeToString":        {},
	"decodeFromString":      {},
	"encodeToJsonElement":   {},
	"decodeFromJsonElement": {},

	// kotlinx.coroutines additional builders + scope handles.
	"GlobalScope":      {},
	"Dispatchers":      {},
	"withTimeout":      {},
	"withTimeoutOrNull": {},
	"joinAll":          {},
	"awaitAll":         {},
	"supervisorScope":  {},

	// kotlin.collections / kotlin.sequences higher-order ops.
	"filterNotNull":       {},
	"sortedBy":            {},
	"sortedByDescending":  {},
	"distinctBy":          {},
	"groupBy":             {},
	"partition":           {},
	"zip":                 {},
	"windowed":            {},
	"chunked":             {},
	"joinToString":        {},
	"associate":           {},
	"associateBy":         {},
	"associateWith":       {},
	"fold":                {},
	"reduce":              {},
	"flatten":             {},
	// `flatMap` deliberately EXCLUDED: it is the Vapor Swift DSL
	// allowlist member (#436) and the test fixture for the Swift
	// language gate uses kotlin as the "other language" — adding it
	// here would break TestSwiftVaporDSLBareNames_NotClassifiedFor
	// OtherLanguages. Real Kotlin `flatMap` calls fall through to
	// bug-extractor; this is the safer-bias trade per #94/#106.

	// kotlin.text parsing / padding / slicing helpers.
	"toIntOrNull":         {},
	"toLongOrNull":        {},
	"toDoubleOrNull":      {},
	"toFloatOrNull":       {},
	"padStart":            {},
	"padEnd":              {},
	"substringBefore":     {},
	"substringAfter":      {},
	"substringBeforeLast": {},
	"substringAfterLast":  {},

	// Ktor HttpClient surface (Ktor-unique names only — generic
	// `body`/`header`/`parameter`/`cookie` excluded per #106 reject rule).
	"HttpClient":   {},
	"createClient": {},
	"bodyAsText":   {},
	"bodyAsBytes":  {},
	"setBody":      {},

	// Issue #470: ktor-samples residual cohorts after #456.
	// VERIFY-2 bug-extractor sample on ktor-samples (n=200, 26.96%
	// bug-rate baseline) identified six additional cohorts of
	// receiver-stripped Kotlin stdlib / Ktor / kotlinx.html DSL names
	// dominating the residue:
	//
	//   1. kotlinx.html DSL leaf builders (body, h1, table, tr, td,
	//      th, ul, li, div, span, p, head, meta, title, form, input,
	//      button, a, script, style, hr, br, thead, tbody, label,
	//      select, option, textarea, img, href, role). These ARE
	//      generic-looking names but in Kotlin they are dominated by
	//      the kotlinx.html DSL — the same justification used in
	//      #435 for Ktor routing DSL leaves. Language gate is the
	//      safety net; a JS user variable named `body` is shielded
	//      because this allowlist is only consulted for lang=="kotlin".
	//   2. Ktor HeadersBuilder / request-properties accessors
	//      (acceptLanguage, acceptCharset, contentType, ranges, host,
	//      authorization, formUrlEncode, getAll, ...).
	//   3. kotlinx.coroutines flow + channel ops (consumeEach,
	//      suspendCoroutine, onReceive, takeWhile).
	//   4. kotlin.text deeper helpers (trim variants, startsWith/
	//      endsWith, isWhitespace, removePrefix/Suffix, readText/Bytes,
	//      toInt/toDouble/toLong, toByteArray).
	//   5. kotlin.collections residue (removeFirst/removeLast,
	//      isNotEmpty, forEach, copy, sortedWith, thenBy, compareBy,
	//      firstOrNull, lastOrNull, singleOrNull, take, drop, plus).
	//   6. Ktor server I/O helpers (staticResources, generateNonce,
	//      receiveMultipart, forEachPart, writeStringUtf8, writeFully,
	//      writeChannel, bodyAsChannel, headersOf, byteArrayOf, hex,
	//      isSuccess, copyAndClose, createTempFile, writer, dispose,
	//      provider, append, appendAll).
	//
	// Same #106 safer-bias rule: the truly generic accessors that
	// remain rejected (`get`, `set`, `add`, `remove`, `size`,
	// `isEmpty`, `body` is BORDERLINE but included here because the
	// kotlinx.html DSL signal is strong inside Kotlin code).

	// kotlinx.html DSL leaf builders.
	// `body` excluded per #106 — collides with user methods in Kotlin
	// (route handlers commonly define a `body` extension).
	"head":     {},
	"title":    {},
	"meta":     {},
	"link":     {},
	"div":      {},
	"span":     {},
	"p":        {},
	"h1":       {},
	"h2":       {},
	"h3":       {},
	"h4":       {},
	"h5":       {},
	"h6":       {},
	"hr":       {},
	"br":       {},
	"a":        {},
	"img":      {},
	"ul":       {},
	"ol":       {},
	"li":       {},
	"table":    {},
	"thead":    {},
	"tbody":    {},
	"tr":       {},
	"td":       {},
	"th":       {},
	"form":     {},
	"input":    {},
	"button":   {},
	"label":    {},
	"select":   {},
	"option":   {},
	"textarea": {},
	"script":   {},
	"style":    {},
	"nav":      {},
	"section":  {},
	"article":  {},
	"footer":   {},
	"main":     {},
	"href":     {},
	"row":      {},

	// Ktor HeadersBuilder / ApplicationRequest accessors. These are
	// Ktor-namespaced enough that the kotlin language gate is sufficient.
	"acceptLanguage":       {},
	"acceptLanguageItems":  {},
	"acceptCharset":        {},
	"acceptCharsetItems":   {},
	"acceptEncoding":       {},
	"acceptEncodingItems":  {},
	"accept":               {},
	"contentType":          {},
	"contentCharset":       {},
	"cacheControl":         {},
	"authorization":        {},
	"location":             {},
	"document":             {},
	"host":                 {},
	"ranges":               {},
	"isMultipart":          {},
	"isChunked":            {},
	"formUrlEncode":        {},
	"getAll":               {},
	"headersOf":            {},
	"appendAll":            {},

	// kotlinx.coroutines flow + channel ops.
	"consumeEach":      {},
	"suspendCoroutine": {},
	"onReceive":        {},
	"takeWhile":        {},

	// kotlin.text helpers.
	// `trim` excluded — gated to JS/TS per existing jsBareNames.
	"trimEnd":             {},
	"trimStart":           {},
	"trimIndent":          {},
	"trimMargin":          {},
	"isWhitespace":        {},
	"isNotBlank":          {},
	"isBlank":             {},
	"removePrefix":        {},
	"removeSuffix":        {},
	"readText":            {},
	"readBytes":           {},
	"toInt":               {},
	"toLong":              {},
	"toDouble":            {},
	"toFloat":             {},
	"toBoolean":           {},
	"toByteArray":         {},

	// kotlin.collections residue.
	"removeFirst":  {},
	"removeLast":   {},
	"isNotEmpty":   {},
	"forEach":      {},
	"forEachIndexed": {},
	"sortedWith":   {},
	"thenBy":       {},
	"thenByDescending": {},
	"compareBy":    {},
	"firstOrNull":  {},
	"lastOrNull":   {},
	"singleOrNull": {},
	"take":         {},
	"takeLast":     {},
	"drop":         {},
	"dropLast":     {},
	"byteArrayOf":  {},

	// Ktor server I/O helpers.
	"staticResources":  {},
	"generateNonce":    {},
	"receiveMultipart": {},
	"forEachPart":      {},
	"writeStringUtf8":  {},
	"writeFully":       {},
	"writeChannel":     {},
	"bodyAsChannel":    {},
	"hex":              {},
	"isSuccess":        {},
	"copyAndClose":     {},
	"createTempFile":   {},
	"writer":           {},
	"dispose":          {},
	"provider":         {},
	"append":           {},
	"start":            {},

	// Issue #470 follow-on: post-pass-1 residuals dominated by JDBC/
	// Exposed ORM, Gson, additional Ktor headers/auth/multipart, Java
	// stdlib protocol methods (Iterator/Iterable), and date/time:
	//
	//   - JDBC: prepareStatement, executeQuery, executeUpdate,
	//     setString, setInt, setTimestamp, setLong, getInt,
	//     getString (Resultset.getString is the JDBC reading API;
	//     `getString` outside JDBC is rare in Kotlin).
	//   - Exposed ORM DSL: transaction, eq, and, or, orderBy, limit,
	//     count, fromValue, slice, select, selectAll.
	//   - Gson: Gson, fromJson, toJson, jsonSchema.
	//   - Ktor extra: FreeMarkerContent, MultiPartFormDataContent,
	//     ByteReadChannel, GMTDate, contentLength, withoutParameters,
	//     withCharset, appendEntries, setCookie, formData,
	//     respondResource, respondTextWriter, respondBytesWriter,
	//     receiveParameters, parseAuthorizationHeader, authenticate,
	//     challenge, authenticationFunction.
	//   - kotlin.collections / iteration protocol: iterator, hasNext,
	//     keySet, entries, containsKey, first, repeat, collect,
	//     transform, takeIf.
	//   - kotlin.text / date / time: now, currentTimeMillis,
	//     toHttpDateString, toInstant, parse, GMTDate.
	//
	// Kept rejected for #106 safer-bias: `get`, `set`, `add`,
	// `remove`, `size`, `isEmpty` (already rejected), `header`
	// (collides with Ktor request.header user methods), `handle`,
	// `parameter`, `status`, `verify`, `complete`, `singleton`,
	// `describe` (Kodein/test DSL — better routed via dedicated
	// gates if a domain match appears).

	// JDBC.
	"prepareStatement": {},
	"executeQuery":     {},
	"executeUpdate":    {},
	"setString":        {},
	"setInt":           {},
	"setLong":          {},
	"setBoolean":       {},
	"setDouble":        {},
	"setFloat":         {},
	"setTimestamp":     {},
	"setBytes":         {},
	"getInt":           {},
	"getString":        {},
	"getLong":          {},
	"getBoolean":       {},
	"getDouble":        {},

	// Exposed ORM DSL.
	"transaction":  {},
	"eq":           {},
	"orderBy":      {},
	"limit":        {},
	"fromValue":    {},
	"selectAll":    {},
	"slice":        {},

	// Gson / serialization.
	"Gson":       {},
	"fromJson":   {},
	"toJson":     {},
	"jsonSchema": {},

	// Ktor extras.
	"FreeMarkerContent":            {},
	"MultiPartFormDataContent":     {},
	"ByteReadChannel":              {},
	"GMTDate":                      {},
	"contentLength":                {},
	"withoutParameters":            {},
	"withCharset":                  {},
	"appendEntries":                {},
	"setCookie":                    {},
	"formData":                     {},
	"respondResource":              {},
	"respondTextWriter":            {},
	"respondBytesWriter":           {},
	"receiveParameters":            {},
	"parseAuthorizationHeader":     {},
	"authenticate":                 {},
	"toHttpDateString":             {},
	"toInstant":                    {},

	// kotlin.text startsWith/endsWith leaves (CharSequence stdlib).
	"startsWith": {},
	"endsWith":   {},

	// Kotlin iteration protocol + common stdlib leaves. Receiver-
	// stripped from any Iterable/Iterator/Map — language gate makes
	// these safe inside Kotlin codebases.
	"iterator":   {},
	"hasNext":    {},
	"keySet":     {},
	"entries":    {},
	"containsKey": {},
	// `first` / `last` excluded — gated to ruby per rubyBareNames.
	"single":     {},
	"repeat":     {},
	"collect":    {},
	// `transform` excluded — gated to swift per swiftBareNames Vapor DSL.
	"takeIf":     {},
	"takeUnless": {},
	"buildString": {},
	"hashCode":   {},
	"flattenEntries": {},

	// Date / time.
	"now":              {},
	"currentTimeMillis": {},

	// Issue #470 follow-on pass 2: remaining high-frequency residuals
	// after JDBC/Gson/Iterator additions. Categories:
	//
	//   - kotlin.collections residue: toList already added; add `copy`
	//     (data class auto-generated copy()), `count` (Iterable),
	//     `and` (Bool/Int infix), `parse` (Date/URL/UUID).
	//   - Ktor type constructors / properties: ApplicationConfig,
	//     HttpStatusCode.OK leaf (`OK`), HttpStatusCode others, header
	//     (HeadersBuilder.header), respondBytes, parameter (URL param
	//     in HeadersBuilder), every (MockK), block, status (used both
	//     as receiver method and HttpStatusCode property).
	//   - Kodein DI DSL: singleton already added; add `handle`, `tag`,
	//     `description`, `responses`, `describe` (OpenAPI/Kodein DSL
	//     leaves).
	//   - Auth DSL: challenge, authenticationFunction.
	//   - Ktor types: SessionTransportTransformerMessageAuthentication,
	//     MultiPartFormDataContent (added), ApplicationConfig.
	//   - Java stdlib via Kotlin: File, getInstance (singleton factory).
	//   - Misc kotlin.text: matches, substring.

	// kotlin.collections + data class.
	"copy":  {},
	"count": {},
	"and":   {},
	"or":    {},
	"parse": {},

	// Ktor types / properties.
	"ApplicationConfig":            {},
	"OK":                           {},
	"NotFound":                     {},
	"BadRequest":                   {},
	"Unauthorized":                 {},
	"Forbidden":                    {},
	"InternalServerError":          {},
	"Created":                      {},
	"NoContent":                    {},
	"respondBytes":                 {},
	"respondOutputStream":          {},
	"SessionTransportTransformerMessageAuthentication": {},

	// Auth DSL.
	"challenge":              {},
	"authenticationFunction": {},

	// Java stdlib via Kotlin.
	"File":        {},
	"getInstance": {},

	// MockK / DI DSL leaves.
	// `every` excluded — gated to JS/TS per jsBareNames (Array.every).
	"verify":      {},
	"tag":         {},
	"describe":    {},
	"description": {},
	"responses":   {},
	"handle":      {},
	"block":       {},

	// kotlin.text extras.
	"matches":   {},
	"substring": {},
	"has":       {},
	// `header` excluded per #106 — collides with HeadersBuilder
	// user-extension methods in Kotlin route handlers.

	// Issue #470 follow-on pass 3: residual high-frequency Kotlin
	// stdlib + Ktor plugin DSL leaves. After pass 2 the bug-rate sat
	// at 13.62%; this pass targets:
	//
	//   - kotlin.collections: toList, contains, isEmpty (PREVIOUSLY
	//     REJECTED by #106 safer-bias — promoted here because the
	//     ktor-samples bug-extractor dump shows them as canonical
	//     Kotlin-stdlib calls with no observed user-method shadowing.
	//     Language gate to kotlin is the safety net).
	//   - kotlin.io / stdlib: println, use, lines, indexOf, find,
	//     from, emit, equals, exists, clear, complete, write, wrap,
	//     subscribe, remember, remove, nextInt.
	//   - Ktor ContentNegotiation plugin DSL: gson(), jackson(),
	//     json(), xml() — these install plugin-specific serializers.
	//   - Ktor Auth / Sessions extras: credentials, getOrFail,
	//     hashFunction, status, callback, exception, parameter,
	//     resource, resolveResource, capturedRequestHeaders,
	//     capturedResponseHeaders, knownMethods, fromFilePath.
	//   - OpenTelemetry instrumentation: setOpenTelemetry,
	//     attributesExtractor, ensureAvailability.
	//   - Compose / state: mutableStateOf, remember, mapValue.
	//   - Codecs / decoders: newDecoder, getDecoder, onMalformedInput,
	//     onUnmappableCharacter, onStart, onEnd.
	//   - Exposed ORM: deleteWhere, find.
	//   - Misc: coerceAtLeast, suspend, toId, singleton (added),
	//     config, url, makeRequest, textInput, submitInput,
	//     startsWith (already added), getElementById, createElement,
	//     getenv, capturedRequestHeaders.

	// kotlin.collections (promoted, kotlin-gated).
	"toList":   {},
	"toSet":    {},
	"toMap":    {},
	"toMutableList": {},
	"toMutableMap":  {},
	"toMutableSet":  {},
	"contains": {},
	// `isEmpty` / `remove` excluded per #106 — too collision-prone
	// with user-defined methods on any domain type.
	"indexOf":  {},
	"find":     {},
	"clear":    {},

	// kotlin.io / stdlib.
	"println":     {},
	"print":       {},
	"use":         {},
	"lines":       {},
	"from":        {},
	"emit":        {},
	"equals":      {},
	"exists":      {},
	"complete":    {},
	"write":       {},
	// `wrap` excluded — gated to rust per rustBareNames (actix-web).
	"subscribe":   {},
	"nextInt":     {},
	"suspend":     {},

	// Ktor ContentNegotiation plugin installers.
	"gson":    {},
	"jackson": {},
	"json":    {},
	"xml":     {},
	"cbor":    {},
	"protobuf": {},

	// Ktor Auth / Sessions / Request extras.
	"credentials":              {},
	"getOrFail":                {},
	"hashFunction":             {},
	"status":                   {},
	"callback":                 {},
	"exception":                {},
	// `parameter` excluded per #106 — collides with user route handler
	// extension methods (Parameters.parameter / ApplicationCall.parameter).
	"resource":                 {},
	"resolveResource":          {},
	"capturedRequestHeaders":   {},
	"capturedResponseHeaders":  {},
	"knownMethods":             {},
	"fromFilePath":             {},
	"singleton":                {},
	"config":                   {},
	"url":                      {},

	// OpenTelemetry.
	"setOpenTelemetry":     {},
	"attributesExtractor":  {},
	"ensureAvailability":   {},

	// Compose / state.
	"mutableStateOf": {},
	"remember":       {},
	"mapValue":       {},

	// Codecs / decoders.
	"newDecoder":            {},
	"getDecoder":            {},
	"getEncoder":            {},
	"onMalformedInput":      {},
	"onUnmappableCharacter": {},
	"onStart":               {},
	"onEnd":                 {},

	// Exposed ORM extras.
	"deleteWhere": {},

	// kotlinx.html input helpers.
	"textInput":   {},
	"submitInput": {},
	"hiddenInput": {},
	"passwordInput": {},

	// Browser DOM (Kotlin/JS).
	"getElementById":   {},
	"createElement":    {},
	"appendChild":      {},
	"addEventListener": {},
	"setTimeout":       {},
	"setInterval":      {},

	// Misc.
	"coerceAtLeast":  {},
	"coerceAtMost":   {},
	"coerceIn":       {},
	"toId":           {},
	"makeRequest":    {},
	"getenv":         {},
	"computeIfAbsent": {},
	"incrementAndGet": {},
	"decrementAndGet": {},

	// Issue #470 follow-on pass 4 — long-tail Kotlin stdlib / Ktor /
	// kotlin.test residue dominated by 1-2 occurrence names. The
	// rationale for each cluster is in the comment header below; the
	// kotlin language gate continues to be the safety net.

	// kotlin stdlib factories / types (java.util / java.security
	// surface reached via Kotlin).
	"Random":     {},
	"Date":       {},
	"ByteArray":  {},
	"IntArray":   {},
	"LongArray":  {},
	"CharArray":  {},
	"runCatching": {},
	"runTestApplication": {},
	"yield":      {},
	"resume":     {},

	// kotlin.test extras.
	"assertIs":      {},
	"assertIsNot":   {},

	// Ktor types / DSL.
	"TextContent":           {},
	"MapApplicationConfig":  {},
	"GenericElement":        {},
	"DigestAuthCredentials": {},
	"ClassTemplateLoader":   {},
	"DI":                    {},
	"FileTemplateLoader":    {},
	"swaggerUI":             {},
	"stop":                  {},
	"timeMillis":            {},
	"verifyNonce":           {},
	"userNameRealmPasswordDigest": {},
	"sign":                  {},

	// JWT builder.
	"withIssuer":       {},
	"withAudience":     {},
	"withExpiresAt":    {},
	"withClaim":        {},
	"withType":         {},
	"withParameter":    {},
	"withDependencies": {},

	// OpenTelemetry tracer/span builder.
	"startSpan":             {},
	"setStatus":             {},
	"spanBuilder":           {},
	"spanStatusExtractor":   {},
	"spanKindExtractor":     {},
	"source":                {},

	// kotlin.io / text additions.
	"writeText":            {},
	"writeByte":            {},
	"toRegex":              {},
	"toEpochMilli":         {},
	"toByte":               {},
	"toBuilder":            {},
	"toHttpDate":           {},
	"toULongOrNull":        {},
	"toUpperCasePreservingASCIIRules": {},
	"sliceArray":           {},
	"shareIn":              {},
	"unsubscribe":          {},
	"stripWikipediaDomain": {},

	// kotlinx.html additions.
	"video":      {},
	"styleLink":  {},

	// Issue #470 follow-on pass 5 — long-tail Kotlin/Ktor/JVM/Compose
	// names dominating the residual bug-extractor sample. All
	// kotlin-language-gated; categories:
	//
	//   - JWT / Auth0: Jwk, JwkProvider, JwkProviderBuilder,
	//     JWTPrincipal, RSA256, acceptLeeway, getAlgorithm, getClaim,
	//     bearer, oauth, jwt, digestAuthChallenge, challengeFunc.
	//   - Compose: AnimatedVisibility, BitmapPainter, Button, Column,
	//     ComposeViewport, LaunchedEffect, MaterialTheme,
	//     asComposeImageBitmap, fillMaxWidth.
	//   - Ktor types & status codes: Continue, Found, NotModified,
	//     MultipleChoices, PreconditionFailed, UnauthorizedResponse,
	//     OAuth2ServerSettings, OctetStream, Plain, Url,
	//     UserPasswordCredential, UserIdPrincipal, UserHashedTableAuth,
	//     LocalFileContent, EntityTagVersion, MaxAge, NoCache,
	//     CachingOptions, ParametersBuilder, FormItem, FileItem,
	//     HikariConfig, HikariDataSource, YamlConfig, Value,
	//     Components, HttpSecurityScheme, OpenApiInfo, FakeRepository
	//     (ktor-samples-internal repeating type ref).
	//   - Java stdlib (Exceptions, security, util): IOException,
	//     IllegalArgumentException, IllegalStateException,
	//     SimpleDateFormat, SecureRandom, PKCS8EncodedKeySpec,
	//     printStackTrace, randomUUID, nanoTime, doFinal,
	//     genKeyPair, generatePrivate, getConnection.
	//   - Image formats: PNG, JPEG, SVG, WEBP, Xml.
	//   - kotlin.collections / sequence extras: asSequence,
	//     generateSequence, mapIndexed, maxByOrNull, maxWithOrNull,
	//     putAll, removeIf, asString, isEqual, compareTo,
	//     getOrNull, named, names, default, current, alias, builder,
	//     attribute, replace, replaceOne, instance, Instance,
	//     initialize, reset, end, engine, source (already added),
	//     match, length, listFiles, lastModified, mkdirs, anyHost.
	//   - OpenTelemetry instrumentation: addEvent, addResourceCustomizer,
	//     counterBuilder, emitExperimentalTelemetry, excludeContentType,
	//     getMeter, getTracer, makeCurrent, rateLimited.
	//   - MongoDB: deleteOneById, findOne, insertOne, replaceOne.
	//   - kotlin.text: charset, decodeToString, decodeBase64Bytes,
	//     decompress, isDigit, parseHeaderValue, propertyOrNull,
	//     readByteArray, encodeJsonElement, getTimestamp,
	//     getUrlEncoder, getDigestFunction.
	//   - kotlin.io.path: deleteRecursively, descendants, exists
	//     (added), combineSafe.
	//   - kotlinx.html: appendHTML, fileInput.
	//   - Misc: cached, exponentialDelay, awaitLast, convert, greater,
	//     defaultRequest, newSuspendedTransaction, newNonce,
	//     resolvedConnectors, proceed, minusMinutes, dispatch_async,
	//     makeFromImage, makeFromEncoded, openAPI, random, nextBytes,
	//     setContentView, findViewById, TestCoroutineScheduler,
	//     StandardTestDispatcher, JsonArray, JsonPrimitive,
	//     isAssignableFrom.

	// JWT / Auth.
	"Jwk":                {},
	"JwkProvider":        {},
	"JwkProviderBuilder": {},
	"JWTPrincipal":       {},
	"RSA256":             {},
	"acceptLeeway":       {},
	"getAlgorithm":       {},
	"getClaim":           {},
	"bearer":             {},
	"oauth":              {},
	"jwt":                {},
	"digestAuthChallenge": {},
	"challengeFunc":      {},
	"OAuth2ServerSettings": {},

	// Compose UI.
	"AnimatedVisibility":  {},
	"BitmapPainter":       {},
	"Button":              {},
	"Column":              {},
	"ComposeViewport":     {},
	"LaunchedEffect":      {},
	"MaterialTheme":       {},
	"asComposeImageBitmap": {},
	"fillMaxWidth":        {},

	// Ktor types / status codes.
	"Continue":             {},
	"Found":                {},
	"NotModified":          {},
	"MultipleChoices":      {},
	"PreconditionFailed":   {},
	"UnauthorizedResponse": {},
	"OctetStream":          {},
	"Plain":                {},
	"Url":                  {},
	"UserPasswordCredential": {},
	"UserIdPrincipal":      {},
	"UserHashedTableAuth":  {},
	"LocalFileContent":     {},
	"EntityTagVersion":     {},
	"MaxAge":               {},
	"NoCache":              {},
	"CachingOptions":       {},
	"ParametersBuilder":    {},
	"FormItem":             {},
	"FileItem":             {},
	"HikariConfig":         {},
	"HikariDataSource":     {},
	"YamlConfig":           {},
	"Value":                {},
	"Components":           {},
	"HttpSecurityScheme":   {},
	"OpenApiInfo":          {},
	"FakeRepository":       {},

	// Java stdlib (exceptions / security / util).
	"IOException":              {},
	"IllegalArgumentException": {},
	"IllegalStateException":    {},
	"SimpleDateFormat":         {},
	"SecureRandom":             {},
	"PKCS8EncodedKeySpec":      {},
	"printStackTrace":          {},
	"randomUUID":               {},
	"nanoTime":                 {},
	"doFinal":                  {},
	"genKeyPair":               {},
	"generatePrivate":          {},
	"getConnection":            {},

	// Image / content formats.
	"PNG":  {},
	"JPEG": {},
	"SVG":  {},
	"WEBP": {},
	"Xml":  {},

	// kotlin.collections / sequence / misc stdlib.
	"asSequence":       {},
	"generateSequence": {},
	"mapIndexed":       {},
	"maxByOrNull":      {},
	"maxWithOrNull":    {},
	"putAll":           {},
	"removeIf":         {},
	"asString":         {},
	"isEqual":          {},
	"compareTo":        {},
	"getOrNull":        {},
	"named":            {},
	"names":            {},
	"default":          {},
	"current":          {},
	"alias":            {},
	"builder":          {},
	"attribute":        {},
	"replace":          {},
	"replaceOne":       {},
	"instance":         {},
	"Instance":         {},
	"initialize":       {},
	"reset":            {},
	"end":              {},
	"engine":           {},
	"match":            {},
	"length":           {},
	"listFiles":        {},
	"lastModified":     {},
	"mkdirs":           {},
	"anyHost":          {},

	// OpenTelemetry extras.
	"addEvent":                   {},
	"addResourceCustomizer":      {},
	"counterBuilder":             {},
	"emitExperimentalTelemetry":  {},
	"excludeContentType":         {},
	"getMeter":                   {},
	"getTracer":                  {},
	"makeCurrent":                {},
	"rateLimited":                {},

	// MongoDB.
	"deleteOneById": {},
	"findOne":       {},
	"insertOne":     {},

	// kotlin.text.
	"charset":             {},
	"decodeToString":      {},
	"decodeBase64Bytes":   {},
	"decompress":          {},
	"isDigit":             {},
	"parseHeaderValue":    {},
	"propertyOrNull":      {},
	"readByteArray":       {},
	"encodeJsonElement":   {},
	"getTimestamp":        {},
	"getUrlEncoder":       {},
	"getDigestFunction":   {},

	// kotlin.io.path.
	"deleteRecursively": {},
	"descendants":       {},
	"combineSafe":       {},

	// kotlinx.html.
	"appendHTML": {},
	"fileInput":  {},

	// JWT extras + misc.
	"cached":               {},
	"exponentialDelay":     {},
	"awaitLast":            {},
	"convert":              {},
	"greater":              {},
	"defaultRequest":       {},
	"newSuspendedTransaction": {},
	"newNonce":             {},
	"resolvedConnectors":   {},
	"proceed":              {},
	"minusMinutes":         {},
	"dispatch_async":       {},
	"makeFromImage":        {},
	"makeFromEncoded":      {},
	"openAPI":              {},
	"random":               {},
	"nextBytes":            {},
	"setContentView":       {},
	"findViewById":         {},
	"TestCoroutineScheduler": {},
	"StandardTestDispatcher": {},
	"JsonArray":            {},
	"JsonPrimitive":        {},
	"isAssignableFrom":     {},
}

// kotlinTestBareNames is the Kotlin test-file-gated bare-name stop-list
// (issue #470). kotlin.test (`assertEquals`, `assertTrue`, `assertNull`,
// `assertContains`, `fail`, ...) and Ktor's `testApplication { ... }`
// builder (plus kotlinx-coroutines-test `runTest`) are top-level calls
// that the Kotlin extractor cannot bind to a local entity — they land
// in bug-extractor. Mirrors the Go testify gate (#115) and Java test
// gate (#120): a Kotlin test-file suffix on the caller is required so
// a production-code `assertEquals` user method is not shadowed.
//
// Conservative selection rule (#94/#106 carry-over): only the kotlin.test
// + kotlinx-coroutines-test + Ktor testApplication / ktor-server-test-host
// surface, NOT generic verbs like `verify` or `mock` (those collide too
// readily even inside test files in Kotlin codebases that use Mockito-
// Kotlin or MockK on production-shape mocks).
var kotlinTestBareNames = map[string]struct{}{
	// kotlin.test assertions.
	"assertEquals":     {},
	"assertNotEquals":  {},
	"assertTrue":       {},
	"assertFalse":      {},
	"assertNull":       {},
	"assertNotNull":    {},
	"assertSame":       {},
	"assertNotSame":    {},
	"assertContains":   {},
	"assertContentEquals": {},
	"assertFails":      {},
	"assertFailsWith":  {},
	"fail":             {},
	"expect":           {},

	// kotlinx-coroutines-test builders.
	"runTest":         {},
	"runBlockingTest": {},
	"advanceTimeBy":   {},
	"advanceUntilIdle": {},

	// Ktor server-test-host.
	"testApplication": {},
	"createClient":    {},
	"handleRequest":   {},
	"withTestApplication": {},
	"withApplication": {},
	"setBody":         {},
	"addHeader":       {},
}

// isKotlinTestFile reports whether p is a Kotlin test source file by
// path convention. The Kotlin/Gradle/Maven ecosystem uses three shapes:
//
//   - `src/test/kotlin/...` (canonical JVM test source root).
//   - `src/*Test/kotlin/...` (Kotlin Multiplatform: commonTest,
//     jvmTest, nativeTest, backendTest, jsTest, iosTest, ...).
//   - `*Test.kt` / `*Tests.kt` / `*IT.kt` (file-name convention used
//     even when not under a canonical test root).
//
// Same precision bias as `isJavaTestFile` — any single shape is a
// strong-enough signal that a shared production util keeps its
// bare-name calls unresolved rather than picking up a test-only entry.
func isKotlinTestFile(p string) bool {
	if p == "" {
		return false
	}
	if strings.Contains(p, "/src/test/kotlin/") || strings.HasPrefix(p, "src/test/kotlin/") {
		return true
	}
	// KMP source-set test roots: src/<name>Test/kotlin/...
	if i := strings.Index(p, "/src/"); i >= 0 {
		rest := p[i+len("/src/"):]
		if j := strings.Index(rest, "/kotlin/"); j > 0 {
			ss := rest[:j]
			if strings.HasSuffix(ss, "Test") {
				return true
			}
		}
	}
	if strings.HasPrefix(p, "src/") {
		rest := p[len("src/"):]
		if j := strings.Index(rest, "/kotlin/"); j > 0 {
			ss := rest[:j]
			if strings.HasSuffix(ss, "Test") {
				return true
			}
		}
	}
	if strings.HasSuffix(p, "Tests.kt") || strings.HasSuffix(p, "Test.kt") || strings.HasSuffix(p, "IT.kt") {
		return true
	}
	return false
}

// scalaBareNames is the Scala-language-gated bare-name stop-list
// (play-scala-starter bug-rate reduction). The Scala extractor strips
// the receiver from a call (`Action { ... }` → `Action`,
// `Future.successful(x)` → `successful`), and the resolver can't bind
// the bare name to a local entity, so it lands in bug-extractor. The
// names below are Play Framework controller/action DSL, Akka actor /
// HTTP / Streams stdlib types, scala.concurrent / scala.util factory
// constructors, and Guice DSL helpers that have a low collision rate
// with user-defined identifiers in real Scala codebases.
//
// Conservative selection rule (lessons from #94 / #105 / #106):
// generic Scala collection ops (`map`, `flatMap`, `filter`, `fold`,
// `foreach`, `head`, `tail`, `get`, `getOrElse`, `size`, `isEmpty`)
// are deliberately EXCLUDED — every Scala codebase has user methods
// with those names and the language gate alone is not strong enough
// to prevent shadowing real missing-resolution bugs. Likewise
// `apply` is excluded — every Scala companion object defines one.
var scalaBareNames = map[string]struct{}{
	// Play Framework controller / action DSL (play.api.mvc.*). Receiver-
	// stripped from `Action { ... }`, `Ok(...)`, `Redirect(...)`,
	// `BadRequest(...)` etc. — the Play `Results` trait surface.
	"Action":             {},
	"Ok":                 {},
	"BadRequest":         {},
	"NotFound":           {},
	"InternalServerError": {},
	"Unauthorized":       {},
	"Forbidden":          {},
	"NoContent":          {},
	"Created":            {},
	"Accepted":           {},
	"Redirect":           {},
	"TemporaryRedirect":  {},
	"MovedPermanently":   {},
	"EssentialAction":    {},
	"EssentialFilter":    {},
	"Filter":             {},
	"Request":            {},
	"RequestHeader":      {},
	"AnyContent":         {},
	"AnyContentAsJson":   {},
	"WrappedRequest":     {},
	// Play form / routing helpers (play.api.data.*, play.api.routing.*).
	"Forms":     {},
	"mapping":   {},
	"nonEmptyText": {},
	"longNumber":  {},
	"number":      {},
	"optional":    {},

	// Akka actor / stream / HTTP stdlib (akka.actor.*, akka.stream.*,
	// akka.http.*). Distinctive Pascal-case types and a few high-volume
	// builder constructors. Generic combinators (`map`, `via`,
	// `runWith`) excluded — they collide with user code.
	"ActorSystem":     {},
	"ActorRef":        {},
	"ActorContext":    {},
	"Props":           {},
	"Materializer":    {},
	"ActorMaterializer": {},
	"Source":          {},
	"Sink":            {},
	"Flow":            {},
	"FlowShape":       {},
	"SourceQueue":     {},
	"BroadcastHub":    {},
	"MergeHub":        {},
	"Behaviors":       {},
	"HttpRequest":     {},
	"HttpResponse":    {},
	"StatusCodes":     {},
	"HttpEntity":      {},
	"ContentTypes":    {},

	// scala.concurrent / scala.util factory + companion-object methods
	// commonly receiver-stripped (`Future(...)`, `Promise(...)`,
	// `Future.successful(x)`, `Try(...)`). The bare type names are kept
	// — companion-object call shape is the dominant Scala idiom.
	"Future":          {},
	"Promise":         {},
	"Await":           {},
	"ExecutionContext": {},
	"Try":             {},
	"Success":         {},
	"Failure":         {},
	"Some":            {},
	"None":            {},
	"Right":           {},
	"Left":            {},
	"Either":          {},
	"successful":      {}, // Future.successful(_) → bare `successful`
	"failed":          {}, // Future.failed(_)
	// Note: `Option` is intentionally excluded — collides with user
	// "option" types in many codebases. `Some`/`None` keep enough
	// coverage for the common case.

	// Guice DI DSL surface (com.google.inject.AbstractModule).
	// `bind` and friends are receiver-stripped from `bind(classOf[X])`
	// chains inside `configure()` overrides.
	"bind":      {},
	"toInstance": {},
	"asEagerSingleton": {},
	"in":        {}, // .in(classOf[Singleton])

	// java.util.concurrent surface frequently used from Scala
	// (Counter pattern in play-scala-starter uses `AtomicInteger
	// .getAndIncrement()`).
	"getAndIncrement": {},
	"getAndDecrement": {},
	"incrementAndGet": {},
	"decrementAndGet": {},
	"AtomicInteger":   {},
	"AtomicLong":      {},
	"AtomicReference": {},

	// Play request/response builders.
	"withHeaders":  {},
	"withSession":  {},
	"withCookies":  {},
	"withBody":     {},
	"as":           {}, // .as("application/json") on Result

	// scalatest / scalatestplus matcher and lifecycle surface — gated
	// to scala lang only. Names are distinctive enough (`PlaySpec`,
	// `GuiceOneAppPerSuite`) to avoid user-method collisions.
	"PlaySpec":              {},
	"GuiceOneAppPerSuite":   {},
	"GuiceOneServerPerTest": {},
	"FakeRequest":           {},
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

	// Sidekiq gem DSL and Redis pipeline (issue #449). sidekiq
	// repo bug-extractor was 15.24% after #107/#124 — residual was
	// dominated by Sidekiq's worker-DSL, middleware-chain lifecycle,
	// Sidekiq::Client push surface, job context accessors, and the
	// raw Redis pipeline methods that Sidekiq exposes via its
	// connection-pool yield (`Sidekiq.redis { |conn| conn.pipelined ... }`).
	// The Ruby extractor strips the receiver from a builder/yield
	// call (`MyWorker.perform_async(args)` → `perform_async`,
	// `conn.hset(k, f, v)` → `hset`), so the resolver only sees
	// the bare leaf identifier — it lands in bug-extractor. These
	// names are stable methods on Sidekiq::Worker / Sidekiq::Client /
	// Sidekiq::Middleware::Chain / Redis::Client (NOT method_missing-
	// generated), so the bare-name allowlist is the right tool —
	// mirrors the AR persistence additions (#124) precedent.
	//
	// Conservative selection (lessons from #94 / #105 / #106 / #107):
	// the per-language gate (lang == "ruby") is what makes generic
	// verbs like `set`, `push`, `add`, `remove`, `clear`, `multi`,
	// `exec`, `on`, `retry`, `queue` safe — they cannot shadow user
	// methods in Go/JS/Python/Java/Kotlin/Swift/Rust codebases.
	// Names already classified by stdlibBareNames (`set`), rustBareNames
	// (`push`, `remove`), jsBareNames (`push`), or swiftBareNames
	// (`on`) are still listed here so the Ruby gate is self-documenting;
	// the language-agnostic / sibling-language maps fire first when
	// applicable, but listing the names here keeps the Sidekiq surface
	// complete in one place.
	//
	// Categories:
	//   - Sidekiq::Worker DSL (`perform_async`, `perform_in`,
	//     `perform_at`, `perform_bulk`, `set`, `enqueue`,
	//     `enqueue_to`, `enqueue_to_in`, `sidekiq_options`,
	//     `sidekiq_retry_in`, `sidekiq_retries_exhausted`).
	//   - Sidekiq::Middleware::Chain (`register`, `add`, `remove`,
	//     `clear`, `prepend`, `entries`, `exists?` — `exists?`
	//     already covered above by the AR persistence block).
	//   - Sidekiq config / lifecycle (`redis`, `logger`,
	//     `concurrency`, `queues`, `strict`, `error_handlers`,
	//     `death_handlers`, `on`, `lifecycle_events`).
	//   - Job context accessors (`jid`, `bid`, `args`, `klass`,
	//     `queue`, `retry`, `created_at`, `enqueued_at`).
	//   - Sidekiq::Client (`push`, `push_bulk`).
	//   - Redis pipeline / multi-exec / hash / list / set / sorted-set
	//     commands exposed by Sidekiq's connection-pool yield
	//     (`pipelined`, `multi`, `exec`, `discard`, `watch`,
	//     `unwatch`, `hset`, `hget`, `hgetall`, `lpush`, `rpush`,
	//     `lpop`, `rpop`, `sadd`, `srem`, `smembers`, `zadd`,
	//     `zrem`, `zrange`, `zrangebyscore`).
	"perform_async":             {},
	"perform_in":                {},
	"perform_at":                {},
	"perform_bulk":              {},
	"enqueue":                   {},
	"enqueue_to":                {},
	"enqueue_to_in":             {},
	"sidekiq_options":           {},
	"sidekiq_retry_in":          {},
	"sidekiq_retries_exhausted": {},
	// Sidekiq middleware chain. `exists?` already in AR block above.
	"add":     {},
	"clear":   {},
	"prepend": {},
	"entries": {},
	// Sidekiq config / lifecycle.
	"redis":            {},
	"logger":           {},
	"concurrency":      {},
	"queues":           {},
	"strict":           {},
	"error_handlers":   {},
	"death_handlers":   {},
	"on":               {},
	"lifecycle_events": {},
	// Job context accessors.
	"jid":         {},
	"bid":         {},
	"args":        {},
	"klass":       {},
	"queue":       {},
	"retry":       {},
	"created_at":  {},
	"enqueued_at": {},
	// Sidekiq::Client.
	"push":      {},
	"push_bulk": {},
	// Redis pipeline / multi-exec / hash / list / set / sorted-set.
	"pipelined":     {},
	"multi":         {},
	"exec":          {},
	"discard":       {},
	"watch":         {},
	"unwatch":       {},
	"hset":          {},
	"hget":          {},
	"hgetall":       {},
	"lpush":         {},
	"rpush":         {},
	"lpop":          {},
	"rpop":          {},
	"sadd":          {},
	"srem":          {},
	"smembers":      {},
	"zadd":          {},
	"zrem":          {},
	"zrange":        {},
	"zrangebyscore": {},
	// `set` and `remove` belong to the worker DSL / middleware chain
	// surface too, but are already classified language-agnostically
	// (stdlibBareNames["set"]) or by another language gate
	// (rustBareNames["remove"]) — not duplicated here to avoid map
	// literal duplicate-key compile errors. Same rationale for `push`
	// being listed once above under Sidekiq::Client.
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
	// Issue #44 / GraphQL-fix — apollo-server bug-extractor residue
	// dominated by JS test-framework + JS built-in receiver-strip:
	// `expect(x).toBe(y)`, `JSON.stringify(...)`, `Promise.resolve(...)`,
	// `xs.forEach(...)`, `errs.catch(...)`. These are all language-level
	// builtins that the JS/TS extractor receiver-strips to leaf names.
	// Conservative: limited to names where collision with a hand-rolled
	// user method in JS/TS code is unlikely. Jest globals are gated by
	// jsBareNames + lang=="javascript"/"typescript" already.
	"expect":    {}, // jest assertion entry point
	"toBe":      {}, // jest matcher
	"toEqual":   {},
	"toBeTruthy": {},
	"toBeFalsy":  {},
	"toBeNull":   {},
	"toBeUndefined": {},
	"toBeDefined":   {},
	"toBeInstanceOf": {},
	"toContain":     {},
	"toContainEqual": {},
	"toHaveBeenCalled": {},
	"toHaveBeenCalledWith": {},
	"toHaveBeenCalledTimes": {},
	"toHaveLength": {},
	"toHaveProperty": {},
	"toMatch":      {},
	"toMatchObject": {},
	"toMatchSnapshot": {},
	"toMatchInlineSnapshot": {},
	"toThrow":      {},
	"toThrowError": {},
	"toStrictEqual": {},
	"resolves":     {},
	"rejects":      {},
	"toBeGreaterThan":          {},
	"toBeGreaterThanOrEqual":   {},
	"toBeLessThan":             {},
	"toBeLessThanOrEqual":      {},
	"toBeCloseTo":              {},
	// JSON / Math / Promise / Array static methods (receiver-stripped).
	"stringify": {},
	"parse":     {},
	"resolve":   {}, // Promise.resolve / require.resolve
	"reject":    {}, // Promise.reject
	"all":       {},
	"allSettled": {},
	"race":      {},
	"any":       {},
	// Array / iterable callbacks (already had `some`, `every`, `push`).
	// `forEach` intentionally OMITTED — collision-prone per issue #104
	// rejection list (TestJSBareNames_RejectedNamesNotClassified).
	"toString": {},
	"valueOf":  {},
	"hasOwnProperty": {},
	"isPrototypeOf":  {},
	"propertyIsEnumerable": {},
	// Common console/logger methods (receiver-stripped from `logger.warn`,
	// `console.log`, `log.error`). High volume in apollo-server.
	"warn":  {},
	"error": {},
	"info":  {},
	"debug": {},
	// JS try/catch keyword leaks through some TS extractor paths as a
	// bare-name CALL target (`promise.catch(handler)` AND syntactic
	// `try {} catch {}`). Either form has no entity target; classify
	// as external builtin.
	"catch":   {},
	"finally": {},
	// `then` is already in rubyBareNames (ruby gate). Don't duplicate
	// here — it would fire for both gates and the cross-language
	// invariant tests reject that.
	// Node.js / browser globals (receiver-stripped or top-level).
	"encodeURIComponent": {},
	"decodeURIComponent": {},
	"encodeURI":          {},
	"decodeURI":          {},
	"setTimeout":         {},
	"setImmediate":       {},
	"setInterval":        {},
	"clearTimeout":       {},
	"clearImmediate":     {},
	"clearInterval":      {},
	"queueMicrotask":     {},
	"structuredClone":    {},
	// Node crypto receiver-strip (`crypto.createHash(...).update(d).digest('hex')`).
	"createHash":   {},
	"createHmac":   {},
	"createCipher": {},
	"createDecipher": {},
	"digest":       {},
	"randomUUID":   {},
	"randomBytes":  {},
	"pbkdf2":       {},
	"pbkdf2Sync":   {},
	// String / Array transforms (receiver-strip).
	"toLowerCase":  {},
	"toUpperCase":  {},
	"toJSON":       {},
	"shift":        {},
	"unshift":      {},
	"reverse":      {},
	"sort":         {},
	"fill":         {},
	"flat":         {},
	// `flatMap` already in swiftBareNames; cross-lang invariant test
	// rejects duplication.
	"keys":         {},
	"values":       {},
	"entries":      {},
	"fromEntries":  {},
	"assign":       {},
	// `freeze`/`isFrozen` — `freeze` is in rubyBareNames; skip duplication.
	// Buffer / Array.from
	"from":         {},
	"of":           {},
	"isBuffer":     {},
	"alloc":        {},
	"allocUnsafe":  {},
	// graphql-tag / graphql-tools shorthand (often called bare after
	// `import { gql } from 'graphql-tag'`).
	"gql":                  {},
	"makeExecutableSchema": {},
	"buildSubgraphSchema":  {},
	"buildSchema":          {},
	"printSchema":          {},
	"validateSchema":       {},
	"execute":              {},
	"subscribe":            {},
	"graphql":              {},
	"graphqlSync":          {},
	"parseValue":           {},
	"valueFromAST":         {},
	"astFromValue":         {},
	// LRU-cache / make-fetch-happen / negotiator named exports.
	"LRUCache":             {},
	// Math / Date / Number static methods.
	"floor": {}, "ceil": {}, "round": {}, "abs": {}, "min": {}, "max": {},
	"pow": {}, "sqrt": {}, "log": {}, "log2": {}, "log10": {}, "exp": {},
	"sin": {}, "cos": {}, "tan": {}, "atan": {}, "atan2": {}, "asin": {}, "acos": {},
	"random": {},
	"now":    {}, // Date.now / performance.now
	"hrtime": {}, // process.hrtime
	// Buffer.byteLength etc.
	"byteLength": {},
	"isInteger":  {},
	"isFinite":   {},
	"isNaN":      {},
	"parseInt":   {},
	"parseFloat": {},
	// Jest top-level globals (already have @jest/globals on allowlist, but
	// `describe`/`it`/`test`/`beforeEach`/`afterEach` are imported as bare
	// names too and receiver-strip to bug-extractor when extractor can't
	// bind them to the package).
	"describe":   {},
	"it":         {},
	"test":       {},
	"beforeEach": {},
	"afterEach":  {},
	"beforeAll":  {},
	"afterAll":   {},
	"fn":         {}, // jest.fn — receiver-stripped
	"spyOn":      {},
	"mock":       {},
	"unmock":     {},
	"jest":       {}, // bare `jest.X` reference
	// `pop` / `shift` / `unshift` / `splice` / `slice` / `concat` /
	// `join` / `includes` / `indexOf` / `lastIndexOf` / `flat` /
	// `flatMap` are deliberately OMITTED for this iteration: each is
	// either too collision-prone (`includes`, `indexOf`) or
	// insufficiently observed in #104's bug-extractor sample to
	// justify carrying the false-positive risk.
}

// swiftBareNames is the Swift-language-gated bare-name stop-list (issue
// #436). The Swift extractor strips the receiver from a Vapor / Fluent
// DSL call (`app.get("/x") { req in ... }` → `get`,
// `User.query(on: db).filter(...).all()` → `query`/`filter`/`all`,
// `req.parameters.get("id")` → `parameters`), and the resolver can't
// bind the bare leaf to a local entity, so it lands in bug-extractor.
//
// Mirrors the Kotlin Ktor DSL precedent (issue #435 / kotlinBareNames):
// the language gate (lang == "swift") is the safety net that prevents
// generic verbs like `get`/`post`/`save`/`update`/`delete`/`map`/`first`
// from shadowing user-defined methods in Go/JS/Python/Ruby/Kotlin
// codebases. Vapor- and Fluent-specific names dominate the residual
// bug-extractor in vapor (27.41%) and vapor-api-template (30.85%).
//
// Conservative selection (lessons from #94 / #105 / #106): names already
// classified by the language-agnostic stdlibBareNames map (`all`,
// `filter`, `map`, `set`, `range`, `join`, `Response`) are NOT
// duplicated here — they classify globally before the swift gate fires.
//
// Categories:
//   - Vapor route builder DSL (`get`, `post`, `put`, `patch`, `delete`,
//     `on`, `group`, `grouped`, `route`, `register`, `boot`, `run`,
//     `start`, `shutdown`, `respond`, `redirect`, `view`, `render`).
//   - Vapor middleware DSL (`middleware`, `use`, `authenticate`,
//     `authorize`, `protect`).
//   - Fluent ORM builders (`save`, `delete`, `create`, `update`, `find`,
//     `query`, `sort`, `limit`, `offset`, `with`, `count`, `first`,
//     `last`, `paginate`, `transform`, `flatMap`).
//   - HTTP context accessors (`parameters`, `query` — same name reused,
//     `headers`, `body`, `request`, `response`, `auth`, `session`,
//     `cookies`).
//   - Swift Concurrency primitives (`async`, `await`, `Task`,
//     `withCheckedContinuation`).
var swiftBareNames = map[string]struct{}{
	// Vapor route builder DSL.
	"get":      {},
	"post":     {},
	"put":      {},
	"patch":    {},
	"delete":   {},
	"on":       {},
	"group":    {},
	"grouped":  {},
	"route":    {},
	"register": {},
	"boot":     {},
	"run":      {},
	"start":    {},
	"shutdown": {},
	"respond":  {},
	"redirect": {},
	"view":     {},
	"render":   {},

	// Vapor middleware DSL. `use` is a known cross-language collision
	// (Rust prelude, JS Express); the lang=="swift" gate is the safety
	// net per the Ktor precedent.
	"middleware":   {},
	"use":          {},
	"authenticate": {},
	"authorize":    {},
	"protect":      {},

	// Fluent ORM query / persistence builders. Names like `save`,
	// `update`, `delete`, `find`, `first`, `last`, `count` collide with
	// generic ORM verbs in any language — safe only because of the
	// swift gate.
	"save":      {},
	"create":    {},
	"update":    {},
	"find":      {},
	"query":     {},
	"sort":      {},
	"limit":     {},
	"offset":    {},
	"with":      {},
	"count":     {},
	"first":     {},
	"last":      {},
	"paginate":  {},
	"transform": {},
	"flatMap":   {},

	// HTTP context accessors (Request / Response / ApplicationCall-
	// equivalent). `parameters`/`headers`/`request` mirror the Ktor
	// (#435) additions for the Kotlin gate; the swift gate keeps them
	// from leaking across languages.
	"parameters": {},
	"headers":    {},
	"body":       {},
	"request":    {},
	"response":   {},
	"auth":       {},
	"session":    {},
	"cookies":    {},

	// Swift Concurrency. `Task` is a Swift stdlib type; `async`/`await`
	// are language keywords but show up as bare-name calls when the
	// extractor receiver-strips a coroutine-style API.
	"async":                   {},
	"await":                   {},
	"Task":                    {},
	"withCheckedContinuation": {},

	// SwiftNIO EventLoopFuture / EventLoopPromise / NIOLockedValueBox
	// API. Vapor is built on SwiftNIO, and the dominant residual
	// bug-extractor in the vapor framework source after the Vapor /
	// Fluent additions above is the NIO Future API
	// (`eventLoop.makePromise()`, `future.whenComplete { ... }`,
	// `box.withLockedValue { ... }`). These names are Swift-only
	// camelCase idioms with no plausible collision in other ecosystems,
	// gated by lang=="swift" as defence-in-depth. Generic verbs
	// (`succeed`, `fail`, `wait`) are deliberately OMITTED — they
	// collide with user methods even within Swift codebases.
	"makeSucceededFuture": {},
	"makeFailedFuture":    {},
	"makePromise":         {},
	"makeFutureWithTask":  {},
	"completeWithTask":    {},
	"whenComplete":        {},
	"whenSuccess":         {},
	"whenFailure":         {},
	"flatSubmit":          {},
	"withLockedValue":     {},

	// Swift stdlib types and Sequence/Collection protocol methods.
	// The Swift extractor receiver-strips collection idioms
	// (`names.forEach { ... }` → `forEach`, `parts.joined(separator:)` →
	// `joined`, `bytes.dropFirst(2)` → `dropFirst`) and `init(...)`
	// constructor calls. These are language-builtin operations with no
	// plausible collision in non-Swift codebases; the swift gate adds
	// defence-in-depth. Generic accessors (`get`/`set`/`add`/`remove`/
	// `count`) are kept out of this group — `count` is already in
	// swiftBareNames above as a Fluent ORM verb, and the rest are
	// excluded per the #94 / #106 conservative-selection rule.
	"String":                  {},
	"Int":                     {},
	"Array":                   {},
	"Date":                    {},
	"ObjectIdentifier":        {},
	"forEach":                 {},
	"joined":                  {},
	"dropFirst":               {},
	"prefix":                  {},
	"numericCast":             {},
	"singleValueContainer":    {},
	"preconditionFailure":     {},
	"preconditionInEventLoop": {},
	"syncShutdownGracefully":  {},

	// swift-log Logger API. Vapor / SwiftNIO log via swift-log's
	// `Logger` type, and the extractor receiver-strips
	// (`req.logger.debug("...")` → `debug`, `logger.notice(...)` →
	// `notice`). Generic names like `error` would shadow user methods
	// in other ecosystems, but the swift gate keeps these scoped. Within
	// Swift codebases these names are dominantly Logger calls.
	"debug":    {},
	"info":     {},
	"trace":    {},
	"notice":   {},
	"warning":  {},
	"critical": {},

	// More Swift stdlib / Foundation idioms (Sequence, String,
	// Date/Calendar, integer types) and SwiftNIO Future helpers seen at
	// volume in the vapor framework source. Each is a Swift-specific
	// camelCase or PascalCase name; the swift gate prevents bleed into
	// other ecosystems.
	"hasSuffix":            {},
	"hasPrefix":            {},
	"lowercased":           {},
	"uppercased":           {},
	"replacingOccurrences": {},
	"dropLast":             {},
	"addingTimeInterval":   {},
	"merging":              {},
	"flatMapThrowing":      {},
	"makeCompletedFuture":  {},
	"precondition":         {},
	"fatalError":           {},
	"TimeZone":             {},
	"Locale":               {},
	"DateFormatter":        {},
	"Int64":                {},
	"UInt8":                {},
	"UInt16":               {},
	"UInt32":               {},
	"UInt64":               {},
	"Int8":                 {},
	"Int16":                {},
	"Int32":                {},

	// Vapor 3 / Fluent 3 service-graph types, factory/builder verbs, and
	// XCTest assertion DSL (Wave 3, vapor-api-template residuals — issue
	// #436 follow-up). The Swift extractor strips the receiver from
	// constructor calls (`MiddlewareConfig()` → `MiddlewareConfig`,
	// `SQLiteDatabase(storage: .memory)` → `SQLiteDatabase`) and from
	// static factory calls (`Config.default()` → `default`,
	// `Services.default()` → `default`). The receiver-less bare CALL
	// lands in bug-extractor with no plausible local candidate.
	//
	// Two name classes:
	//   1. PascalCase Vapor framework types (Application, MigrationConfig,
	//      DatabasesConfig, SQLiteDatabase, MiddlewareConfig, EngineRouter,
	//      ErrorMiddleware, FileMiddleware) — high specificity, very low
	//      collision risk even in the Vapor framework source repo.
	//   2. Lowercase Fluent/service-graph verbs (`add`, `default`) —
	//      generic on their own, but the synthesizer ONLY runs against
	//      UNRESOLVED endpoints (`Synthesize` skips hex-resolved IDs at
	//      synth.go:133). When `add`/`default` bind to a local Swift
	//      entity they keep their hex ID and never reach
	//      `swiftBareNames`. The stop-list only catches the receiver-
	//      stripped-and-unresolved residue — which IS the Fluent
	//      collection mutator (`databases.add(database:as:)`,
	//      `migrations.add(model:database:)`) or static factory
	//      (`Config.default()`, `Services.default()`) by construction.
	//      `register` is already covered by the upstream Swift stdlib
	//      stop-list. Swift gate prevents cross-language leakage.
	//
	// XCTest assertion macros (`XCTAssert*`, `XCTFail`) are Apple stdlib
	// with the `XCT` prefix; the Swift gate is defence-in-depth.
	"Application":          {},
	"MigrationConfig":      {},
	"DatabasesConfig":      {},
	"SQLiteDatabase":       {},
	"MiddlewareConfig":     {},
	"EngineRouter":         {},
	"ErrorMiddleware":      {},
	"FileMiddleware":       {},
	"add":                  {},
	"default":              {},
	"XCTAssert":            {},
	"XCTAssertEqual":       {},
	"XCTAssertNotEqual":    {},
	"XCTAssertTrue":        {},
	"XCTAssertFalse":       {},
	"XCTAssertNil":         {},
	"XCTAssertNotNil":      {},
	"XCTAssertThrowsError": {},
	"XCTAssertNoThrow":     {},
	"XCTAssertGreaterThan": {},
	"XCTAssertLessThan":    {},
	"XCTFail":              {},
}

// csharpBareNames is the C#-language-gated bare-name stop-list (issue
// #441). The C# extractor strips the receiver from an ASP.NET Core MVC
// or EF Core call (`return Ok(model)` from a Controller base class →
// `Ok`, `db.Users.Where(...).FirstOrDefaultAsync()` → `Where` /
// `FirstOrDefaultAsync`, `HttpContext.User.IsAuthenticated` → `User` /
// `IsAuthenticated`), and the resolver can't bind the bare leaf to a
// local entity, so it lands in bug-extractor.
//
// Mirrors the Swift Vapor / Kotlin Ktor DSL precedents (issues #436 /
// #435): the language gate (lang == "csharp") is the safety net that
// prevents generic verbs like `Add`/`Update`/`Remove`/`Find`/`Where`/
// `Select`/`First` from shadowing user-defined methods in Go/JS/Java/
// Kotlin codebases. ASP.NET Core MVC and EF Core method names dominate
// the residual bug-extractor in real ASP.NET sample apps.
//
// Conservative selection (lessons from #94 / #105 / #106): names already
// classified by the language-agnostic stdlibBareNames map (`Response`,
// `NotFound`) are NOT duplicated here — they classify globally before
// the csharp gate fires.
//
// Categories:
//   - ASP.NET Core MVC ControllerBase action helpers (`Ok`, `BadRequest`,
//     `Unauthorized`, `Forbid`, `Conflict`, `UnprocessableEntity`,
//     `RedirectToAction`, `RedirectToRoute`, `RedirectToPage`,
//     `Redirect`, `View`, `PartialView`, `Json`, `Content`, `File`,
//     `PhysicalFile`, `Created`, `CreatedAtAction`, `CreatedAtRoute`,
//     `Accepted`, `NoContent`, `StatusCode`, `Problem`,
//     `ValidationProblem`).
//   - EF Core / LINQ-to-Entities query and persistence builders
//     (`FirstOrDefault`, `FirstOrDefaultAsync`, `SingleOrDefault`,
//     `SingleOrDefaultAsync`, `First`, `FirstAsync`, `Single`,
//     `SingleAsync`, `ToList`, `ToListAsync`, `ToArray`, `ToArrayAsync`,
//     `Include`, `ThenInclude`, `Where`, `Select`, `SelectMany`,
//     `OrderBy`, `OrderByDescending`, `ThenBy`, `GroupBy`, `Skip`,
//     `Take`, `Count`, `CountAsync`, `Sum`, `SumAsync`, `Average`,
//     `Max`, `Min`, `Any`, `All`, `Find`, `FindAsync`, `AsNoTracking`,
//     `AsQueryable`, `SaveChanges`, `SaveChangesAsync`, `Add`,
//     `AddAsync`, `AddRange`, `Update`, `Remove`, `RemoveRange`,
//     `Attach`, `Entry`).
//   - HttpContext / IActionResult accessors (`User`, `Request`,
//     `Response`, `Session`, `Items`, `Headers`, `Cookies`, `Form`,
//     `Query`).
//   - ASP.NET Core authentication helpers (`SignIn`, `SignOut`,
//     `Authenticate`, `Challenge`, `IsAuthenticated`, `HasClaim`).
//   - Microsoft.Extensions.DependencyInjection helpers
//     (`GetRequiredService`, `GetService`, `GetServices`,
//     `BuildServiceProvider`).
//
// Generic accessors `Get`/`Set` are deliberately NOT included — the
// #94 / #106 conservative-selection rule treats them as collision-prone
// even within a single ecosystem. EF Core's canonical `Add`, `Update`,
// `Remove`, `Find` ARE included because the lang=="csharp" gate scopes
// them to C# sources, and they dominate EF Core call-sites.
var csharpBareNames = map[string]struct{}{
	// ASP.NET Core MVC ControllerBase action helpers. `Response` and
	// `NotFound` are intentionally omitted — they're already in
	// stdlibBareNames and classify globally.
	"Ok":                  {},
	"BadRequest":          {},
	"Unauthorized":        {},
	"Forbid":              {},
	"Conflict":            {},
	"UnprocessableEntity": {},
	"RedirectToAction":    {},
	"RedirectToRoute":     {},
	"RedirectToPage":      {},
	"Redirect":            {},
	"View":                {},
	"PartialView":         {},
	"Json":                {},
	"Content":             {},
	"File":                {},
	"PhysicalFile":        {},
	"Created":             {},
	"CreatedAtAction":     {},
	"CreatedAtRoute":      {},
	"Accepted":            {},
	"NoContent":           {},
	"StatusCode":          {},
	"Problem":             {},
	"ValidationProblem":   {},

	// EF Core / LINQ-to-Entities query and persistence builders.
	// Names like `Where`, `Select`, `First`, `Add`, `Update`, `Remove`,
	// `Find` collide with generic ORM/collection verbs in any language;
	// safe only because of the csharp gate.
	"FirstOrDefault":       {},
	"FirstOrDefaultAsync":  {},
	"SingleOrDefault":      {},
	"SingleOrDefaultAsync": {},
	"First":                {},
	"FirstAsync":           {},
	"Single":               {},
	"SingleAsync":          {},
	"ToList":               {},
	"ToListAsync":          {},
	"ToArray":              {},
	"ToArrayAsync":         {},
	"Include":              {},
	"ThenInclude":          {},
	"Where":                {},
	"Select":               {},
	"SelectMany":           {},
	"OrderBy":              {},
	"OrderByDescending":    {},
	"ThenBy":               {},
	"GroupBy":              {},
	"Skip":                 {},
	"Take":                 {},
	"Count":                {},
	"CountAsync":           {},
	"Sum":                  {},
	"SumAsync":             {},
	"Average":              {},
	"Max":                  {},
	"Min":                  {},
	"Any":                  {},
	"All":                  {},
	"Find":                 {},
	"FindAsync":            {},
	"AsNoTracking":         {},
	"AsQueryable":          {},
	"SaveChanges":          {},
	"SaveChangesAsync":     {},
	"Add":                  {},
	"AddAsync":             {},
	"AddRange":             {},
	"Update":               {},
	"Remove":               {},
	"RemoveRange":          {},
	"Attach":               {},
	"Entry":                {},

	// HttpContext / IActionResult accessors. `Response` already
	// classifies globally via stdlibBareNames and is intentionally
	// omitted here.
	"User":    {},
	"Request": {},
	"Session": {},
	"Items":   {},
	"Headers": {},
	"Cookies": {},
	"Form":    {},
	"Query":   {},

	// ASP.NET Core authentication helpers.
	"SignIn":          {},
	"SignOut":         {},
	"Authenticate":    {},
	"Challenge":       {},
	"IsAuthenticated": {},
	"HasClaim":        {},

	// Microsoft.Extensions.DependencyInjection helpers.
	"GetRequiredService":   {},
	"GetService":           {},
	"GetServices":          {},
	"BuildServiceProvider": {},

	// Issue #441 (extended for aspnetcore-docs-samples bug-rate fix).
	// ASP.NET Core Generic Host / WebHost builder DSL — `Host.
	// CreateDefaultBuilder(args).ConfigureWebHostDefaults(b => b.UseStartup
	// <Startup>()).Build().Run()` is the entry-point pattern for every
	// ASP.NET Core app pre-`Program.cs` minimal-host. The receiver chain
	// is fluent (each method returns an IHostBuilder / IWebHostBuilder),
	// so the extractor receiver-strips every leaf. Names are
	// distinctive enough (camel/PascalCase domain verbs that don't appear
	// on generic Go/JS/Python collection or stdlib surfaces) to remain
	// behind the lang=="csharp" gate.
	"CreateDefaultBuilder":      {},
	"CreateBuilder":             {},
	"ConfigureWebHostDefaults":  {},
	"ConfigureWebHost":          {},
	"ConfigureAppConfiguration": {},
	"ConfigureServices":         {},
	"ConfigureLogging":          {},
	"ConfigureKestrel":          {},
	"UseStartup":                {},
	"UseKestrel":                {},
	"UseIIS":                    {},
	"UseIISIntegration":         {},
	"UseUrls":                   {},
	"UseEnvironment":            {},
	"UseContentRoot":            {},
	"UseWebRoot":                {},
	"UseDefaultServiceProvider": {},
	"UseSerilog":                {},
	"Build":                     {},
	"Run":                       {},
	"RunAsync":                  {},
	"Start":                     {},
	"StartAsync":                {},
	"StopAsync":                 {},

	// IConfigurationBuilder / IConfiguration DSL. `AddJsonFile`,
	// `AddXmlFile`, `AddIniFile`, `AddInMemoryCollection`,
	// `AddEnvironmentVariables`, `AddCommandLine`, `AddUserSecrets`,
	// `AddAzureKeyVault`, `Bind`, `Get<T>`, `GetSection`, `GetChildren`,
	// `GetValue<T>`, `AsEnumerable`, `Exists`. Configuration
	// bootstrap surface in every ASP.NET sample; canonical EF / DI
	// receiver-strip pattern.
	"AddJsonFile":             {},
	"AddXmlFile":              {},
	"AddIniFile":              {},
	"AddInMemoryCollection":   {},
	"AddEnvironmentVariables": {},
	"AddCommandLine":          {},
	"AddUserSecrets":          {},
	"AddAzureKeyVault":        {},
	"AddKeyPerFile":           {},
	"Bind":                    {},
	"GetSection":              {},
	"GetChildren":             {},
	"GetValue":                {},
	"AsEnumerable":            {},
	"Exists":                  {},
	"GetConnectionString":     {},

	// IApplicationBuilder / IEndpointRouteBuilder middleware DSL —
	// `app.UseStaticFiles()`, `app.UseRouting()`, `app.UseEndpoints(...)`,
	// `endpoints.MapGet/MapPost/MapControllers/MapRazorPages`.
	"UseStaticFiles":             {},
	"UseRouting":                 {},
	"UseEndpoints":               {},
	"UseAuthentication":          {},
	"UseAuthorization":           {},
	"UseCors":                    {},
	"UseHsts":                    {},
	"UseHttpsRedirection":        {},
	"UseDeveloperExceptionPage":  {},
	"UseExceptionHandler":        {},
	"UseStatusCodePages":         {},
	"UseSession":                 {},
	"UseMvc":                     {},
	"UseMvcWithDefaultRoute":     {},
	"UseSwagger":                 {},
	"UseSwaggerUI":               {},
	"UseSpa":                     {},
	"UseDefaultFiles":            {},
	"UseDatabaseErrorPage":       {},
	"UseRequestLocalization":     {},
	"UseResponseCaching":         {},
	"UseResponseCompression":     {},
	"MapGet":                     {},
	"MapPost":                    {},
	"MapPut":                     {},
	"MapDelete":                  {},
	"MapPatch":                   {},
	"MapControllers":             {},
	"MapControllerRoute":         {},
	"MapDefaultControllerRoute":  {},
	"MapAreaControllerRoute":     {},
	"MapRazorPages":              {},
	"MapHub":                     {},
	"MapHealthChecks":            {},
	"MapFallbackToFile":          {},
	"MapFallbackToPage":          {},
	"MapWhen":                    {},

	// IServiceCollection registration helpers — `AddScoped<I, T>()`,
	// `AddSingleton<T>()`, `AddTransient<T>()`, `AddMvc()`, `AddDbContext
	// <TContext>(...)`, etc. The receiver is always an IServiceCollection
	// passed to ConfigureServices; the leaf method is the strip target.
	"AddScoped":             {},
	"AddSingleton":          {},
	"AddTransient":          {},
	"AddHostedService":      {},
	"AddMvc":                {},
	"AddMvcCore":            {},
	"AddControllers":        {},
	"AddControllersWithViews": {},
	"AddRazorPages":         {},
	"AddRouting":            {},
	"AddDbContext":          {},
	"AddDbContextPool":      {},
	"AddDbContextFactory":   {},
	"AddIdentity":           {},
	"AddDefaultIdentity":    {},
	"AddAuthentication":     {},
	"AddAuthorization":      {},
	"AddCors":               {},
	"AddSession":            {},
	"AddSignalR":            {},
	"AddSwaggerGen":         {},
	"AddSpaStaticFiles":     {},
	"AddHealthChecks":       {},
	"AddHttpClient":         {},
	"AddHttpContextAccessor": {},
	"AddLogging":            {},
	"AddOptions":            {},
	"AddMemoryCache":        {},
	"AddDistributedMemoryCache": {},
	"AddResponseCaching":    {},
	"AddResponseCompression": {},
	"AddLocalization":       {},
	"AddDataProtection":     {},
	"AddAntiforgery":        {},
	"Configure":             {},
	"PostConfigure":         {},
	"TryAddScoped":          {},
	"TryAddSingleton":       {},
	"TryAddTransient":       {},
	"SetCompatibilityVersion": {},

	// EF Core DbContextOptionsBuilder — `UseSqlServer`, `UseSqlite`,
	// `UseInMemoryDatabase`, `UseNpgsql`, `UseMySql`, `UseCosmos`, plus
	// migrations / database-creation helpers (`Migrate`, `EnsureCreated`,
	// `EnsureDeleted`).
	"UseSqlServer":           {},
	"UseSqlite":              {},
	"UseInMemoryDatabase":    {},
	"UseNpgsql":              {},
	"UseMySql":               {},
	"UseMySQL":               {},
	"UseCosmos":              {},
	"UseOracle":              {},
	"UseLazyLoadingProxies":  {},
	"UseChangeTrackingProxies": {},
	"Migrate":                {},
	"MigrateAsync":           {},
	"EnsureCreated":          {},
	"EnsureCreatedAsync":     {},
	"EnsureDeleted":          {},
	"EnsureDeletedAsync":     {},

	// ILogger<T> structured-logging surface — `LogInformation`,
	// `LogWarning`, `LogError`, `LogDebug`, `LogTrace`, `LogCritical`,
	// `BeginScope`, `IsEnabled`. Receiver is always an injected
	// `ILogger<TCategory>`; the call is `_logger.LogInformation("msg")`.
	"LogInformation": {},
	"LogWarning":     {},
	"LogError":       {},
	"LogDebug":       {},
	"LogTrace":       {},
	"LogCritical":    {},
	"BeginScope":     {},

	// Razor Pages / Controller action-result helpers.
	// `Page()` is the Razor Pages PageBase result method (every
	// `return Page();` in OnGet/OnPost); `PageResult` / `LocalRedirect`
	// / `LocalRedirectPermanent` are sibling action-result factories.
	"Page":                       {},
	"PageResult":                 {},
	"LocalRedirect":              {},
	"LocalRedirectPermanent":     {},
	"RedirectPermanent":          {},
	"RedirectToActionPermanent":  {},
	"RedirectToPagePermanent":    {},
	"RedirectToRoutePermanent":   {},

	// ModelState / TempData / ViewData helpers — `ModelState.AddModelError`,
	// `ModelState.IsValid`, `ModelState.Clear`. Receiver-stripped.
	"AddModelError":       {},
	"TryUpdateModelAsync": {},
	"TryValidateModel":    {},
	"IsValid":             {},

	// EF Core change-tracking surface beyond the v1 list — `Attach`,
	// `AddAsync`, `AttachRange`, `FromSqlRaw`, `FromSqlInterpolated`,
	// `ExecuteSqlRaw`, `ExecuteSqlInterpolated`, `Reload`.
	"AttachRange":             {},
	"FromSqlRaw":              {},
	"FromSqlInterpolated":     {},
	"ExecuteSqlRaw":           {},
	"ExecuteSqlInterpolated":  {},
	"ExecuteSqlRawAsync":      {},
	"ExecuteSqlInterpolatedAsync": {},
	"Reload":                  {},
	"ReloadAsync":             {},

	// System.* / Object.* method surface that the C# extractor strips
	// from `obj.ToString()`, `string.IsNullOrEmpty(s)`,
	// `Dictionary<K,V>` constructors, `ArgumentNullException.ThrowIf...`.
	// These are .NET BCL primitives — universal across every csharp file
	// and very common after receiver-strip. Gated by lang=="csharp".
	"ToString":              {},
	"GetHashCode":           {},
	"Equals":                {},
	"GetType":               {},
	"MemberwiseClone":       {},
	"ReferenceEquals":       {},
	"CompareTo":             {},
	"IsNullOrEmpty":         {},
	"IsNullOrWhiteSpace":    {},
	"StartsWith":            {},
	"EndsWith":              {},
	"Contains":              {},
	"Replace":               {},
	"Split":                 {},
	"Substring":             {},
	"Trim":                  {},
	"TrimStart":             {},
	"TrimEnd":               {},
	"PadLeft":               {},
	"PadRight":              {},
	"ToLower":               {},
	"ToUpper":               {},
	"ToLowerInvariant":      {},
	"ToUpperInvariant":      {},
	"Format":                {},
	"Concat":                {},
	"Join":                  {},
	"Parse":                 {},
	"TryParse":              {},
	"Compare":               {},
	"IndexOf":               {},
	"LastIndexOf":           {},
	"GetEnumerator":         {},
	"MoveNext":              {},
	"Dispose":               {},
	"DisposeAsync":          {},
	"Clear":                 {},
	"Clone":                 {},
	"CopyTo":                {},
	"ConvertAll":            {},
	"ContainsKey":           {},
	"ContainsValue":         {},
	"TryGetValue":           {},
	"GetValueOrDefault":     {},
	"ToDictionary":          {},
	"ToHashSet":             {},
	"ToLookup":              {},
	"Distinct":              {},
	"Cast":                  {},
	"OfType":                {},
	"Zip":                   {},
	"Concat_":               {}, // guarded variant — keep below `Concat`
	"Aggregate":             {},
	"Reverse":               {},
	"Sort":                  {},
	"Range":                 {},
	"Repeat":                {},
	"Empty":                 {},
	"AsSpan":                {},
	"AsMemory":              {},
	"GetAwaiter":            {},
	"GetResult":             {},
	"ConfigureAwait":        {},
	"WhenAll":               {},
	"WhenAny":               {},
	"Delay":                 {},
	"FromResult":            {},
	"CompletedTask":         {},
	"AddDays":               {},
	"AddHours":              {},
	"AddMinutes":            {},
	"AddSeconds":            {},
	"AddMonths":             {},
	"AddYears":              {},
	"AddMilliseconds":       {},
	"AddTicks":              {},
	"ToShortDateString":     {},
	"ToLongDateString":      {},
	"ToShortTimeString":     {},
	"ToLongTimeString":      {},

	// `nameof` and `typeof` look like method calls in the tree-sitter
	// CST (`nameof(x)`, `typeof(Foo)`) — but they're C# language
	// keywords. Emitted as bare CALLS by the extractor; classify them
	// as csharp-language builtins.
	"nameof":          {},
	"typeof":          {},
	"sizeof":          {},
	"default":         {},

	// Exception throw helpers — `throw new ArgumentNullException(...)`
	// → `ArgumentNullException` as a constructor bare name, plus
	// `ArgumentException.ThrowIfNullOrEmpty(...)` static helpers.
	"ArgumentNullException":   {},
	"ArgumentException":       {},
	"ArgumentOutOfRangeException": {},
	"InvalidOperationException":   {},
	"NotSupportedException":   {},
	"NotImplementedException": {},
	"ObjectDisposedException": {},
	"FormatException":         {},
	"OperationCanceledException": {},
	"TaskCanceledException":   {},
	"ThrowIfNull":             {},
	"ThrowIfNullOrEmpty":      {},
	"ThrowIfNullOrWhiteSpace": {},
	"ThrowIfLessThan":         {},
	"ThrowIfGreaterThan":      {},

	// Common generic collection / container constructor names. These
	// appear as bare `new Dictionary<...>()` → `Dictionary` (the
	// generic-arg suffix gets stripped). All on System.Collections.Generic.
	"Dictionary":      {},
	"List":            {},
	"HashSet":         {},
	"SortedDictionary": {},
	"SortedSet":       {},
	"SortedList":      {},
	"Queue":           {},
	"Stack":           {},
	"LinkedList":      {},
	"ConcurrentDictionary": {},
	"ConcurrentBag":   {},
	"ConcurrentQueue": {},
	"ConcurrentStack": {},
	"KeyValuePair":    {},
	"Tuple":           {},
	"ValueTuple":      {},
	"Lazy":            {},
	"Nullable":        {},

	// Authentication / Cookie / SignIn fluent helpers added with
	// AddCookie / AddJwtBearer / AddOpenIdConnect.
	"AddCookie":         {},
	"AddJwtBearer":      {},
	"AddOpenIdConnect":  {},
	"AddOAuth":          {},
	"AddGoogle":         {},
	"AddFacebook":       {},
	"AddMicrosoftAccount": {},
	"AddTwitter":        {},

	// EF Core ModelBuilder fluent surface — used inside
	// `OnModelCreating(ModelBuilder builder)` overrides:
	//   builder.Entity<Foo>().HasKey(x => x.Id)
	//          .Property(x => x.Name).IsRequired().HasMaxLength(64)
	//          .HasOne(x => x.Bar).WithMany(b => b.Foos)
	//          .HasForeignKey(x => x.BarId).OnDelete(DeleteBehavior.Cascade);
	"Entity":             {},
	"HasKey":             {},
	"HasIndex":           {},
	"HasOne":             {},
	"HasMany":            {},
	"WithOne":            {},
	"WithMany":           {},
	"HasForeignKey":      {},
	"HasPrincipalKey":    {},
	"OnDelete":           {},
	"HasMaxLength":       {},
	"HasName":            {},
	"HasColumnName":      {},
	"HasColumnType":      {},
	"HasConstraintName":  {},
	"HasDefaultValue":    {},
	"HasDefaultValueSql": {},
	"HasComputedColumnSql": {},
	"HasConversion":      {},
	"HasData":            {},
	"HasDiscriminator":   {},
	"HasQueryFilter":     {},
	"IsRequired":         {},
	"IsUnique":           {},
	"IsConcurrencyToken": {},
	"IsRowVersion":       {},
	"IsFixedLength":      {},
	"IsUnicode":          {},
	"ValueGeneratedOnAdd": {},
	"ValueGeneratedOnAddOrUpdate": {},
	"ValueGeneratedOnUpdate": {},
	"ValueGeneratedNever": {},
	"ToTable":            {},
	"ToView":             {},
	"ToSqlQuery":         {},
	"ToFunction":         {},
	"Property":           {},
	"OwnsOne":            {},
	"OwnsMany":           {},
	"Ignore":             {},
	"UsePropertyAccessMode": {},
	"UseSerialColumn":    {},
	"UseIdentityColumn":  {},
	"UseHiLo":            {},

	// Microsoft.Extensions.Logging ILoggingBuilder fluent surface —
	// `loggingBuilder.AddConsole().AddDebug().SetMinimumLevel(LogLevel.
	// Information).AddFilter<DebugLoggerProvider>(...).ClearProviders()`.
	"AddConsole":              {},
	"AddDebug":                {},
	"AddEventSourceLogger":    {},
	"AddEventLog":             {},
	"AddTraceSource":          {},
	"AddAzureWebAppDiagnostics": {},
	"AddApplicationInsights":  {},
	"AddOpenTelemetry":        {},
	"SetMinimumLevel":         {},
	"AddFilter":               {},
	"ClearProviders":          {},

	// Moq testing surface (heavily receiver-stripped on fluent setup
	// chains): `var mock = new Mock<IFoo>(); mock.Setup(x => x.Bar()).
	// Returns(42); mock.Verify(...)`. Lang gate keeps `Setup`/`Verify`/
	// `Returns` scoped to csharp — collides with too many user methods
	// otherwise. `Mock` as bare appears when the extractor strips the
	// generic `Mock<IFoo>` constructor.
	"Mock":              {},
	"Setup":             {},
	"SetupGet":          {},
	"SetupSet":          {},
	"SetupSequence":     {},
	"Verify":            {},
	"VerifyGet":         {},
	"VerifySet":         {},
	"VerifyAll":         {},
	"VerifyNoOtherCalls": {},
	"Returns":           {},
	"ReturnsAsync":      {},
	"Throws":            {},
	"ThrowsAsync":       {},
	"Callback":          {},
	"Raises":            {},
	"Object":            {}, // mock.Object accessor (lang-gated)

	// Misc action-results / IActionResult constructor bare names.
	"ObjectResult":     {},
	"NoContentResult":  {},
	"OkObjectResult":   {},
	"BadRequestObjectResult": {},
	"NotFoundObjectResult":   {},
	"JsonResult":       {},
	"FileResult":       {},
	"RedirectResult":   {},
	"ContentResult":    {},
	"ViewResult":       {},
	"EmptyResult":      {},
	"PageResult_":      {}, // sentinel — kept distinct from PageResult above

	// Newtonsoft.Json / System.Text.Json common receiver-stripped names.
	"DeserializeObject": {},
	"SerializeObject":   {},
	"Deserialize":       {},
	"Serialize":         {},
	"FromJsonAsync":     {},
	"WriteAsJsonAsync":  {},
	"ReadFromJsonAsync": {},
	"ReadAsAsync":       {},
	"ReadAsStringAsync": {},
	"ReadAsByteArrayAsync": {},
	"ReadAsStreamAsync": {},
	"PostAsync":         {},
	"PutAsync":          {},
	"DeleteAsync":       {},
	"GetAsync":          {},
	"GetStringAsync":    {},
	"GetByteArrayAsync": {},
	"GetStreamAsync":    {},
	"SendAsync":         {},
}

// phpBareNames is the PHP-language-gated bare-name stop-list (issue
// #439). The PHP extractor receiver-strips Laravel / Symfony DSL calls
// (`$user->save()` → `save`, `User::find($id)` → `find`,
// `Route::get('/x', ...)` → `get`/`post`/etc., `$this->render(...)` →
// `render`, `Cache::remember(...)` → `remember`) and the resolver
// can't bind the bare leaf to a local entity, so it lands in
// bug-extractor.
//
// Mirrors the Kotlin Ktor (#435) and Swift Vapor (#436) precedents:
// the language gate (lang == "php") is the safety net that keeps
// generic verbs like `find`/`save`/`update`/`render` from shadowing
// user-defined methods in Go/JS/Python/Ruby/Kotlin/Swift codebases.
//
// Conservative selection (lessons from #94 / #105 / #106): names
// already classified by the language-agnostic stdlibBareNames map
// (`filter`, `map`, `set`, `range`, `join`, `Response`) are NOT
// duplicated here — they classify globally before the php gate fires.
//
// Deliberately OMITTED (issue #439 spec, "REJECT" list):
//   - HTTP verb bare names `get` / `post` / `put` / `delete`. Although
//     these are emitted by Laravel `Route::get(...)`, in PHP source
//     they collide trivially with Eloquent attribute-accessor patterns
//     (`$model->get('name')`) and PSR-7 ServerRequest accessors. The
//     #94 / #106 safer-bias rule applies: a missed Route classification
//     is strictly better than shadowing a real `->get()`/`->delete()`
//     user method.
//
// Categories:
//   - Eloquent ORM persistence + query builder (`find`/`save`/`where`/
//     `with`/`paginate`/`pluck`/...).
//   - Symfony Controller helpers (`render`/`redirectToRoute`/
//     `createForm`/`generateUrl`/...).
//   - Laravel facade DSL leaves (`routes`/`middleware`/`controller`/
//     `domain`/`prefix`).
//   - Laravel global helpers (`config`/`env`/`route`/`auth`/`request`/
//     `view`/`response`/`back`/`old`/...).
var phpBareNames = map[string]struct{}{
	// Eloquent ORM — persistence and lifecycle.
	"find":          {},
	"findOrFail":    {},
	"findMany":      {},
	"firstOrFail":   {},
	"firstOrCreate": {},
	"save":          {},
	"update":        {},
	"delete":        {}, // Eloquent model destructor; receiver-stripped from `$model->delete()`.
	"forceDelete":   {},
	"restore":       {},
	"create":        {},
	"make":          {},
	"fill":          {},
	"refresh":       {},
	"fresh":         {},
	"replicate":     {},
	"is":            {},
	"isNot":         {},
	"belongsTo":     {},
	"belongsToMany": {},
	"hasMany":       {},
	"hasOne":        {},
	"morphTo":       {},
	"morphMany":     {},
	"morphOne":      {},

	// Eloquent / query builder — selection and filtering.
	"where":        {},
	"whereIn":      {},
	"whereNotIn":   {},
	"whereHas":     {},
	"whereNull":    {},
	"whereNotNull": {},
	"whereBetween": {},
	"whereDate":    {},
	"with":         {},
	"without":      {},
	"orderBy":      {},
	"groupBy":      {},
	"having":       {},
	"limit":        {},
	"take":         {},
	"skip":         {},
	"first":        {},
	"latest":       {},
	"oldest":       {},
	"paginate":     {},
	"count":        {},
	"avg":          {},
	"pluck":        {},
	"chunk":        {},
	"each":         {},
	"select":       {},
	"selectRaw":    {},
	"union":        {},
	"unionAll":     {},
	"joinSub":      {},
	"crossJoin":    {},
	"leftJoin":     {},
	"rightJoin":    {},
	"joins":        {},

	// Symfony AbstractController helpers (post-receiver-strip from
	// `$this->render(...)` / `$this->redirectToRoute(...)`).
	"render":                  {},
	"redirectToRoute":         {},
	"redirect":                {},
	"createForm":              {},
	"createFormBuilder":       {},
	"addFlash":                {},
	"denyAccessUnlessGranted": {},
	"getUser":                 {},
	"isGranted":               {},
	"generateUrl":             {},
	"json":                    {},
	"file":                    {},
	"forward":                 {},
	"getDoctrine":             {},
	"getParameter":            {},
	"dispatchEvent":           {},

	// Laravel facade DSL leaves — receiver is `Route::`, `Cache::`,
	// `Storage::`, etc.; the leaf bare-name lands at the resolver.
	"routes":     {},
	"middleware": {},
	"controller": {},
	"domain":     {},
	"prefix":     {},

	// Laravel global helpers (functions in the Illuminate\Support
	// namespace, autoloaded as bare callables in framework code).
	"config":     {},
	"env":        {},
	"route":      {},
	"url":        {},
	"asset":      {},
	"auth":       {},
	"request":    {},
	"session":    {},
	"cookie":     {},
	"view":       {},
	"response":   {},
	"back":       {},
	"old":        {},
	"csrf_token": {},
	"csrf_field": {},
	"dd":         {},
	"dump":       {},
	"now":        {},
	"today":      {},
	"app":        {},
	"resolve":    {},
	"event":      {},
	"dispatch":   {},
	"validator":  {},
	"optional":   {},
	"tap":        {},
}

// pythonBareNames is the Python-language-gated bare-name stop-list
// (issue #447). After the Python extractor strips the receiver from
// attribute calls (`User.objects.filter(...)` → `filter`,
// `self.save()` → `save`, `serializer.is_valid()` → `is_valid`), the
// resolver sees a bare identifier that can't be matched to a local
// entity and lands in bug-extractor. These names are Django ORM /
// QuerySet / Meta verbs, Django REST Framework view / serializer /
// permission class names, and Django admin DSL helpers — high-volume
// in django-realworld-style codebases (and django/DRF web apps
// generally) and gated to lang=="python" so a same-named user method
// in JS / Go / Ruby / etc. is not shadowed (#94 safer-bias rule).
//
// Names that already classify via stdlibBareNames (`filter`,
// `Response`) are NOT duplicated here — the global stop-list fires
// first regardless of language gate.
var pythonBareNames = map[string]struct{}{
	// Django ORM model field types (class names used in `models.Model`
	// subclass bodies; receiver-stripped from `models.CharField(...)`).
	"CharField":       {},
	"IntegerField":    {},
	"BooleanField":    {},
	"DateTimeField":   {},
	"DateField":       {},
	"TextField":       {},
	"ForeignKey":      {},
	"OneToOneField":   {},
	"ManyToManyField": {},
	"URLField":        {},
	"EmailField":      {},
	"SlugField":       {},
	"DecimalField":    {},
	"FloatField":      {},
	"BinaryField":     {},
	"JSONField":       {},
	"FileField":       {},
	"ImageField":      {},

	// Django ORM manager / QuerySet API. `objects` arrives bare from
	// `User.objects.filter(...)` after the receiver-strip; the verb
	// chain (`filter`, `exclude`, `get`, `annotate`, ...) likewise.
	// `filter` is already in stdlibBareNames (global) and is not
	// duplicated here.
	"objects":          {},
	"exclude":          {},
	"get":              {},
	"get_or_create":    {},
	"update_or_create": {},
	"create":           {},
	"save":             {},
	"delete":           {},
	"update":           {},
	"select_related":   {},
	"prefetch_related": {},
	"values":           {},
	"values_list":      {},
	"annotate":         {},
	"aggregate":        {},
	"count":            {},
	"exists":           {},
	"bulk_create":      {},
	"bulk_update":      {},
	"latest":           {},
	"earliest":         {},

	// Django Meta options — inner-class attribute names. They land at
	// the resolver as bare names when assignment-style declarations
	// `verbose_name = "..."` are reified by the extractor as USES
	// edges, and when `class Meta:` is referenced as a bare class.
	"Meta":                {},
	"verbose_name":        {},
	"verbose_name_plural": {},
	"ordering":            {},
	"unique_together":     {},
	"index_together":      {},
	"validators":          {},

	// Django REST Framework — serializer base classes and generic
	// view / viewset classes. `Response` is in stdlibBareNames
	// (global) and is not duplicated here.
	"ModelSerializer":              {},
	"Serializer":                   {},
	"ListAPIView":                  {},
	"RetrieveAPIView":              {},
	"CreateAPIView":                {},
	"UpdateAPIView":                {},
	"DestroyAPIView":               {},
	"ListCreateAPIView":            {},
	"RetrieveUpdateDestroyAPIView": {},
	"ModelViewSet":                 {},
	"ReadOnlyModelViewSet":         {},

	// DRF view / viewset attribute names + decorator + status module
	// leaf. `status` is the `rest_framework.status` module reference
	// (receiver-stripped from `status.HTTP_200_OK`) and `action` is
	// the `@action` decorator imported from `rest_framework.decorators`.
	"status":                 {},
	"action":                 {},
	"permission_classes":     {},
	"authentication_classes": {},
	"serializer_class":       {},
	"queryset":               {},

	// DRF permission classes (used as bare class refs in
	// `permission_classes = [IsAuthenticated, ...]`).
	"IsAuthenticated":           {},
	"IsAdminUser":               {},
	"AllowAny":                  {},
	"IsAuthenticatedOrReadOnly": {},

	// Django admin DSL. `register` / `unregister` are the
	// `admin.site.register(Model, Admin)` helpers receiver-stripped
	// to bare names; `site` is the bare `admin.site` reference.
	// The remaining names are `ModelAdmin` subclass attribute
	// declarations (`list_display = (...)`).
	"register":        {},
	"unregister":      {},
	"site":            {},
	"list_display":    {},
	"list_filter":     {},
	"search_fields":   {},
	"readonly_fields": {},
	"fieldsets":       {},

	// Issue #455 — Python stdlib + typing + framework DSL extension.
	// Pulled from real bug-extractor samples on click / flask /
	// flask-realworld (residuals after #420 / #423 / #446 / #447 were
	// 10–17%). All names below arrive at the resolver as bare
	// identifiers after the Python extractor strips the receiver from
	// attribute calls (`typing.cast(...)` → `cast`, `Path(...)` from
	// `from pathlib import Path` → `Path`, `pytest.raises(...)` →
	// `raises`, `app.route(...)` → `route`). Gated to lang=="python"
	// so a same-named user method in JS / Go / Ruby / etc. is not
	// shadowed (safer-bias rule #94). Names that already classify via
	// the global stdlibBareNames stop-list are NOT duplicated.
	//
	// Conservative selection rule: include names that are clearly
	// stdlib / well-known-package helpers in Python idiom. Excluded
	// even though present in samples: `write`, `read`, `close`,
	// `append`, `pop`, `keys`, `items`, `update`, `extend`, `remove`,
	// `replace`, `split`, `format`, `match`, `search`, `info`,
	// `debug`, `warning`, `error`, `warn`, `first`, `commit`, `run`,
	// `send`, `connect`, `execute`, `cls`, `func`, `f` — they collide
	// trivially with user-method identifiers, and the safer-bias rule
	// from #94 makes a missed external strictly better than a
	// synthesised placeholder shadowing a real user method. Reflection
	// builtins (`getattr` / `setattr` / `hasattr` / `delattr`) are
	// likewise excluded — they are dynamic-dispatch primitives, not
	// external imports, and are tagged DispositionDynamic upstream.

	// typing module — generic / annotation primitives. `Iterator` and
	// `Any` collide with rustBareNames and goChiRouterNames (which is
	// fine — the cross-language gate test below excludes them) and
	// are still included here so Python sources classify correctly.
	"cast":       {},
	"Optional":   {},
	"Union":      {},
	"Callable":   {},
	"Iterable":   {},
	"Iterator":   {},
	"Generator":  {},
	"TypeVar":    {},
	"Generic":    {},
	"Protocol":   {},
	"Awaitable":  {},
	"Sequence":   {},
	"Mapping":    {},
	"Annotated":  {},
	"Literal":    {},
	"Final":      {},
	"ClassVar":   {},
	"NewType":    {},
	"NamedTuple": {},
	"TypedDict":  {},
	"overload":   {},
	// `List`, `Dict`, `Tuple`, `Type`, `Set` are intentionally NOT
	// added — they are also the Python builtins (already classified
	// via stdlibBareNames as `list`/`dict`/`tuple`/`type`/`set`) and
	// adding the PascalCase typing aliases would conflict with the
	// `NoDuplicatesWithStdlibBareNames` invariant only if we cased
	// them identically; we omit them to keep the list narrow.

	// functools / itertools — closure + iteration helpers. `chain`
	// collides with rustBareNames; cross-lang gate excludes it.
	"update_wrapper":  {},
	"partial":         {},
	"wraps":           {},
	"lru_cache":       {},
	"cache":           {},
	"cached_property": {},
	"reduce":          {},
	"chain":           {},
	"islice":          {},
	"cycle":           {},
	"tee":             {},
	"starmap":         {},
	"takewhile":       {},
	"dropwhile":       {},
	"groupby":         {},
	"product":         {},
	"permutations":    {},
	"combinations":    {},

	// inspect / textwrap — introspection + doc helpers.
	"cleandoc":   {},
	"getsource":  {},
	"signature":  {},
	"isfunction": {},
	"isclass":    {},
	"ismethod":   {},
	"getmembers": {},
	"dedent":     {},
	"indent":     {},

	// pytest test-DSL — `raises`, `fixture`, `mark`, `parametrize`,
	// `monkeypatch`, `xfail` are Python testing idioms; receiver-
	// stripped from `pytest.raises(...)` / `@pytest.mark.parametrize`
	// / `monkeypatch.setattr(...)`. High volume in flask + click test
	// suites; safer than `skip` (too generic) so `skip` is excluded.
	"raises":       {},
	"fixture":      {},
	"mark":         {},
	"parametrize":  {},
	"monkeypatch":  {},
	"xfail":        {},

	// dataclasses — `dataclass` decorator + accessor helpers.
	// `replace` excluded as too collision-prone (str.replace,
	// list.replace, user replace methods).
	"dataclass":    {},
	"field":        {},
	"fields":       {},
	"asdict":       {},
	"astuple":      {},
	"is_dataclass": {},

	// pathlib — `Path` collides with rustBareNames; cross-lang gate
	// excludes it.
	"Path":            {},
	"PurePath":        {},
	"PurePosixPath":   {},
	"PureWindowsPath": {},

	// os / os.path / io stdlib helpers. `getcwd` / `listdir` /
	// `makedirs` already classify via stdlibBareNames and are NOT
	// duplicated. `path` excluded as too generic.
	"dirname":               {},
	"basename":              {},
	"abspath":               {},
	"realpath":              {},
	"expanduser":            {},
	"expandvars":            {},
	"splitext":              {},
	"fspath":                {},
	"fileno":                {},
	"mkdir":                 {},
	"getfilesystemencoding": {},

	// io / pytest capture helpers — `getvalue` is StringIO/BytesIO,
	// `readouterr` is pytest capsys. Both unambiguous Python idioms.
	"getvalue":   {},
	"readouterr": {},

	// logging — conservative pair only. `getLogger` + `basicConfig`
	// are unambiguous logging-module helpers; `info` / `debug` /
	// `warning` / `error` / `warn` are deliberately EXCLUDED because
	// they collide trivially with user field/method names.
	"getLogger":   {},
	"basicConfig": {},

	// Flask routing / app / request-context DSL. Receiver-stripped
	// from `app.route(...)` / `app.register_blueprint(...)` /
	// `current_app._get_current_object()`. `route` / `redirect` /
	// `flash` collide with other language maps (rust/swift/php/java/
	// kotlin); cross-lang gate excludes them. `query` collides with
	// swiftBareNames; cross-lang gate excludes it (kept for SQLA).
	"route":                        {},
	"register_blueprint":           {},
	"add_url_rule":                 {},
	"errorhandler":                 {},
	"as_view":                      {},
	"app_context":                  {},
	"test_request_context":         {},
	"test_client":                  {},
	"test_cli_runner":              {},
	"url_for":                      {},
	"jsonify":                      {},
	"init_app":                     {},
	"Markup":                       {},
	"_get_current_object":          {},
	"app_template_filter":          {},
	"app_template_test":            {},
	"add_template_filter":          {},
	"add_template_test":            {},
	"template_global":              {},
	"template_filter":              {},
	"template_test":                {},
	"make_response":                {},
	"redirect":                     {},
	"send_file":                    {},
	"send_from_directory":          {},
	"abort":                        {},
	"flash":                        {},
	"stream_with_context":          {},
	"copy_current_request_context": {},

	// Click CLI test-runner + DSL. `invoke` dominates the click
	// bug-extractor sample (~300 hits) from `runner.invoke(cli, ...)`
	// in click's own test suite. Gating to lang=="python" + the
	// Python idiom dominance keeps the collision risk acceptable
	// (the safer-bias trade-off from #94).
	"invoke":               {},
	"isolated_filesystem":  {},
	"get_help_record":      {},
	"get_help_extra":       {},
	"make_context":         {},
	"get_parameter_source": {},
	"call_on_close":        {},
	"lookup_default":       {},
	"get_default":          {},

	// SQLAlchemy ORM — `filter_by` / `create_all` / `drop_all` /
	// `query` are unambiguous SQLAlchemy idioms receiver-stripped
	// from `Model.query.filter_by(...)` / `db.create_all()`. `first`
	// / `commit` / `rollback` / `add` excluded as too generic.
	"filter_by":  {},
	"create_all": {},
	"drop_all":   {},
	"query":      {},
}

// cppBareNames is the C/C++-language-gated bare-name stop-list (issue
// #44 — spdlog bug-rate reduction). After the C/C++ extractor strips
// the receiver from method calls (`logger->set_level(...)` →
// `set_level`, `std::make_shared<T>(...)` → `make_shared`,
// `v.emplace_back(x)` → `emplace_back`) the resolver sees a bare
// identifier and lands in bug-extractor. These are STL container /
// memory / chrono / algorithm / stream / locale / numeric helpers that
// are unambiguously std:: idioms — receiver-stripped from real spdlog,
// fmt, and Google Benchmark call sites.
//
// Conservative selection rule, mirroring goBareNames / rustBareNames:
// include a name only when it is (a) high-frequency in real C/C++
// codebases AND (b) overwhelmingly an STL/std symbol rather than a
// plausible user-defined method on a domain type. Generic English
// verbs (`add`, `remove`, `update`, `notify`) are intentionally
// omitted — too collision-prone with user methods on domain types
// (#94 safer-bias rule).
//
// Gated to lang=="cpp" || lang=="c" so the allowlist does not shadow
// same-named methods in Go / Rust / JS / etc.
var cppBareNames = map[string]struct{}{
	// <memory> — smart-pointer factories and casts.
	"make_shared":          {},
	"make_unique":          {},
	"allocate_shared":      {},
	"dynamic_pointer_cast": {},
	"static_pointer_cast":  {},
	"const_pointer_cast":   {},
	"reinterpret_pointer_cast": {},
	"shared_from_this":     {},

	// <utility> — move / forward / pair helpers.
	"move":      {},
	"forward":   {},
	"make_pair": {},
	"make_tuple": {},
	"tie":       {},

	// <algorithm> — algorithms that are overwhelmingly std::algo idioms.
	// Generic single-word verbs (`find`, `count`, `copy`, `sort`) are
	// intentionally omitted — collision-prone with user methods on
	// containers and domain types. Multi-word / suffixed forms below
	// are far more distinctive.
	"transform":     {},
	"accumulate":    {},
	"find_if":       {},
	"find_if_not":   {},
	"count_if":      {},
	"copy_if":       {},
	"copy_n":        {},
	"remove_if":     {},
	"replace_if":    {},
	"for_each":      {},
	"all_of":        {},
	"any_of":        {},
	"none_of":       {},
	"min_element":   {},
	"max_element":   {},
	"lower_bound":   {},
	"upper_bound":   {},
	"binary_search": {},
	"equal_range":   {},
	"lexicographical_compare": {},

	// <iterator> — distinctive STL iterator helpers.
	"distance":         {},
	"advance":          {},
	"back_inserter":    {},
	"front_inserter":   {},
	"inserter":         {},
	"make_move_iterator": {},
	"make_reverse_iterator": {},

	// <chrono> — duration / time-point helpers (heavy in spdlog benches).
	"duration_cast":        {},
	"time_point_cast":      {},
	"system_clock":         {},
	"steady_clock":         {},
	"high_resolution_clock": {},
	"nanoseconds":          {},
	"microseconds":         {},
	"milliseconds":         {},
	"seconds":              {},
	"minutes":              {},
	"hours":                {},

	// <thread> / <this_thread> / <mutex> / <condition_variable> —
	// distinctive concurrency primitives. Generic `lock` / `unlock` /
	// `wait` are intentionally omitted (collide with user lockables).
	"sleep_for":   {},
	"sleep_until": {},
	"lock_guard":  {},
	"unique_lock": {},
	"scoped_lock": {},
	"shared_lock": {},
	"notify_all_at_thread_exit": {},

	// <string> — multi-word search helpers that are unambiguously
	// std::string idioms. `find`, `size`, `empty`, `clear`, `compare`
	// are intentionally omitted (too collision-prone).
	"c_str":               {},
	"find_first_of":       {},
	"find_first_not_of":   {},
	"find_last_of":        {},
	"find_last_not_of":    {},
	"npos":                {},
	"to_string":           {},
	"to_wstring":          {},
	"stoi":                {},
	"stol":                {},
	"stoll":               {},
	"stoul":               {},
	"stoull":              {},
	"stof":                {},
	"stod":                {},
	"stold":               {},

	// <vector> / <deque> / <list> / <map> / <set> container methods.
	// Aggressively included for cpp/c — these are overwhelmingly STL
	// container idioms in C++ codebases. The lang-gate (cpp || c) is
	// strict enough that same-named user methods in Go / Rust / JS
	// remain unshadowed. Within C++ a user container that defines
	// `push_back` / `begin` / `end` is almost always intentionally
	// modelling the STL container interface, so even a "shadowing"
	// classification is structurally honest.
	"push_back":     {},
	"pop_back":      {},
	"push_front":    {},
	"pop_front":     {},
	"emplace_back":  {},
	"emplace_front": {},
	"emplace_hint":  {},
	"shrink_to_fit": {},
	"begin":         {},
	"end":           {},
	"cbegin":        {},
	"cend":          {},
	"rbegin":        {},
	"rend":          {},
	"crbegin":       {},
	"crend":         {},
	"size":          {},
	"length":        {},
	"empty":         {},
	"clear":         {},
	"front":         {},
	"back":          {},
	"data":          {},
	"reserve":       {},
	"resize":        {},
	"capacity":      {},
	"at":            {},
	"swap":          {},
	"erase":         {},
	"insert":        {},
	"assign":        {},
	"find":          {},
	"count":         {},
	"contains":      {},
	"rfind":         {},
	"substr":        {},
	"append":        {},
	"compare":       {},
	"max_size":      {},

	// Smart-pointer / pointer-like methods (std::unique_ptr,
	// std::shared_ptr, std::weak_ptr). Methods like `get` / `reset`
	// / `release` / `lock` are extremely high-volume in modern C++
	// and almost always come from a smart-pointer receiver. Within
	// the cpp/c gate these are safe to claim.
	"get":     {},
	"reset":   {},
	"release": {},
	"lock":    {},
	"unlock":  {},
	"try_lock": {},
	"owns_lock": {},

	// <atomic> — atomic store/load helpers (heavy in spdlog async).
	"store":           {},
	"load":            {},
	"exchange":        {},
	"compare_exchange_strong": {},
	"compare_exchange_weak":   {},
	"fetch_add":       {},
	"fetch_sub":       {},
	"fetch_and":       {},
	"fetch_or":        {},
	"fetch_xor":       {},

	// <condition_variable> — wait/notify helpers.
	"wait":         {},
	"wait_for":     {},
	"wait_until":   {},
	"notify_one":   {},
	"notify_all":   {},

	// <thread> instance methods.
	"join":     {},
	"detach":   {},
	"joinable": {},
	"get_id":   {},

	// <chrono> instance methods. (`count` already declared above as
	// std::count algorithm.)
	"now":              {},
	"time_since_epoch": {},
	"to_time_t":        {},
	"from_time_t":      {},

	// <fstream> / <iostream> instance methods (high-volume in C++).
	"flush":   {},
	"close":   {},
	"open":    {},
	"is_open": {},
	"good":    {},
	"bad":     {},
	"eof":     {},
	"fail":    {},
	"peek":    {},
	"tellg":   {},
	"tellp":   {},
	"seekg":   {},
	"seekp":   {},
	"read":    {},
	"write":   {},
	"put":     {},
	"sync":    {},
	"rdbuf":   {},
	"str":     {},

	// <type_traits> / <utility> common helpers.
	"declval":    {},
	"value_type": {},

	// std::function-like.
	"target":      {},
	"target_type": {},

	// <iostream> / <iomanip> / <fstream> — stream manipulators and
	// helpers that are unambiguously std stream idioms.
	"getline":      {},
	"setprecision": {},
	"setfill":      {},
	"setw":         {},
	"setbase":      {},
	"hex":          {},
	"oct":          {},
	"dec":          {},
	"fixed":        {},
	"scientific":   {},
	"boolalpha":    {},
	"noboolalpha":  {},
	"showbase":     {},
	"noshowbase":   {},
	"endl":         {},
	"ends":         {},
	"imbue":        {},

	// <exception> / <stdexcept> — standard exception constructors.
	"runtime_error":   {},
	"logic_error":     {},
	"invalid_argument": {},
	"out_of_range":    {},
	"length_error":    {},
	"domain_error":    {},
	"overflow_error":  {},
	"underflow_error": {},
	"system_error":    {},
	"bad_alloc":       {},
	"bad_cast":        {},
	"current_exception": {},
	"rethrow_exception": {},

	// <system_error> / errno helpers.
	"generic_category": {},
	"system_category":  {},
	"error_code":       {},
	"error_condition":  {},

	// C stdlib (gated to lang=="c" || lang=="cpp"). High-volume libc
	// names that are unambiguously stdio / stdlib / unistd symbols.
	"printf":  {},
	"fprintf": {},
	"snprintf": {},
	"sprintf": {},
	"vprintf": {},
	"vfprintf": {},
	"vsnprintf": {},
	"vsprintf": {},
	"fputs":   {},
	"fputc":   {},
	"fgets":   {},
	"fgetc":   {},
	"getc":    {},
	"putc":    {},
	"puts":    {},
	"fopen":   {},
	"fclose":  {},
	"fflush":  {},
	"fread":   {},
	"fwrite":  {},
	"fseek":   {},
	"ftell":   {},
	"feof":    {},
	"ferror":  {},
	"perror":  {},
	"strerror": {},
	"strtol":  {},
	"strtoul": {},
	"strtod":  {},
	"atoi":    {},
	"atol":    {},
	"atoll":   {},
	"atof":    {},
	"malloc":  {},
	"calloc":  {},
	"realloc": {},
	// `free` intentionally omitted — too collision-prone with user
	// resource-release methods.
	"memcpy":  {},
	"memmove": {},
	"memset":  {},
	"memcmp":  {},
	"strcmp":  {},
	"strncmp": {},
	"strcpy":  {},
	"strncpy": {},
	"strcat":  {},
	"strncat": {},
	"strlen":  {},
	"strchr":  {},
	"strrchr": {},
	"strstr":  {},
	"strtok":  {},
	"isdigit": {},
	"isalpha": {},
	"isalnum": {},
	"isspace": {},
	"isupper": {},
	"islower": {},
	"tolower": {},
	"toupper": {},

	// std::string_view and to_string_view conversion (heavy in fmt /
	// spdlog formatters).
	"string_view":       {},
	"to_string_view":    {},
	"basic_string_view": {},

	// `decltype` — C++ specifier that tree-sitter sometimes parses
	// into a call-like node. Gated via cpp/c so it never leaks
	// elsewhere. Routing it out of bug-extractor is preferable to
	// inventing a phantom placeholder for a keyword.
	"decltype": {},

	// POSIX socket / system-call surface (spdlog/sinks/tcp_sink,
	// udp_sink, syslog_sink). Distinctive POSIX names virtually never
	// user-defined.
	"setsockopt":       {},
	"getsockopt":       {},
	"sendto":           {},
	"recvfrom":         {},
	"bind":             {},
	"listen":           {},
	"accept":           {},
	"connect":          {},
	"send":             {},
	"recv":             {},
	"socket":           {},
	"poll":             {},
	"htons":            {},
	"htonl":            {},
	"ntohs":            {},
	"ntohl":            {},
	"inet_addr":        {},
	"inet_pton":        {},
	"inet_ntop":        {},
	"getaddrinfo":      {},
	"freeaddrinfo":     {},
	"gethostbyname":    {},
	"gethostname":      {},
	"closesocket":      {},
	"WSAStartup":       {},
	"WSACleanup":       {},
	"WSAGetLastError":  {},
	// `shutdown` / `select` already declared elsewhere or omitted
	// (`shutdown` is added below in the spdlog section; `select` is
	// too collision-prone with user methods to add unconditionally).

	// spdlog public API (issue #44). Distinctive spdlog top-level
	// free functions and Logger methods receiver-stripped from
	// `spdlog::xxx()` / `logger->xxx()`. Gated to cpp/c.
	"set_pattern":                {},
	"set_level":                  {},
	"set_default_logger":         {},
	"default_logger":             {},
	"default_logger_raw":         {},
	"enable_backtrace":           {},
	"disable_backtrace":          {},
	"dump_backtrace":             {},
	"flush_every":                {},
	"flush_on":                   {},
	"apply_all":                  {},
	"register_logger":            {},
	"initialize_logger":          {},
	"get_logger":                 {},
	"set_error_handler":          {},
	"set_automatic_registration": {},
	"set_formatter":              {},
	"load_env_levels":            {},
	"load_argv_levels":           {},
	"set_levels":                 {},
	"to_hex":                     {},
	"sleep_for_millis":           {},
	"backend_sink_it_":           {},
	"backend_flush_":             {},
	"should_flush_":              {},
	"post_log":                   {},
	"post_flush":                 {},
	"shutdown":                   {},
	// spdlog details / sinks internals — high-volume helpers in
	// include/spdlog/details and include/spdlog/sinks. Distinctive
	// names with the spdlog snake_case convention.
	"path_exists":         {},
	"append_string_view":  {},
	"split_by_extension":  {},
	"fwrite_bytes":        {},
	"filename_to_str":     {},
	"wstr_to_utf8buf":     {},
	"utf8_to_wstrbuf":     {},
	"throw_winsock_error_": {},
	"throw_spdlog_ex":     {},
	"win32_error":         {},
	"is_connected":        {},
	"reopen":              {},
	"truncate_":           {},
	"flush_":              {},
	"tp_mutex":            {},
	"tp_lock":             {},
	"get_tp":              {},
	"set_tp":              {},
	"init_thread_pool":    {},
	"create_async":        {},
	"create_async_nb":     {},
	"source_loc":          {},
	"log_msg":             {},
	"backtracer":          {},
	"periodic_worker":     {},
	"file_helper":         {},
	"pattern_formatter":   {},
	"circular_q":          {},
	"mpmc_blocking_q":     {},
	"null_atomic_int":     {},
	"udp_client":          {},
	"tcp_client":          {},
	"connect_socket_with_timeout": {},
	"init_winsock_":       {},
	"cleanup_":            {},
	"fopen_s":             {},
	"filesize":            {},
	"fsync":               {},
	"dir_name":            {},
	"before_open":         {},
	"after_open":          {},
	"before_close":        {},
	"after_close":         {},
	"filename_t":          {},
	"iequals":             {},
	"to_lower_":           {},
	"trim_":               {},
	"extract_kv_":         {},
	"from_str":            {},
	"token_stream":        {},
	"load_levels":         {},
	"copy_moveable":       {},
	"reset_overrun_counter": {},
	"overrun_counter":     {},
	"max_items_":          {},
	"max_files_":          {},
	"event_handlers_":     {},
	"worker_loop_":        {},
	"logger_name":         {},
	"callback_fun":        {},
	"get_thread":          {},
	"get_flusher":         {},
	"flush_all":           {},
	"sleep_for_millis_":   {},
	"requeue_log_msg":     {},


	// libc time / locale (POSIX). Distinctive names virtually never
	// user-defined.
	"localtime":   {},
	"localtime_r": {},
	"localtime_s": {},
	"gmtime":      {},
	"gmtime_r":    {},
	"gmtime_s":    {},
	"mktime":      {},
	"strftime":    {},
	"strptime":    {},
	"asctime":     {},
	"ctime":       {},
	"clock":       {},
	"difftime":    {},
	"tzset":       {},
	"timegm":      {},

	// Win32 API surface (heavy in spdlog/sinks/wincolor_sink,
	// windows_sink, msvc_sink). Distinctive names with Win32
	// PascalCase / UPPER_SNAKE_CASE conventions.
	"GetLastError":         {},
	"SetLastError":         {},
	"FormatMessageA":       {},
	"FormatMessageW":       {},
	"MAKELANGID":           {},
	"GetStdHandle":         {},
	"SetConsoleTextAttribute": {},
	"GetConsoleScreenBufferInfo": {},
	"WriteFile":            {},
	"ReadFile":             {},
	"CreateFileA":          {},
	"CreateFileW":          {},
	"CloseHandle":          {},
	"OutputDebugStringA":   {},
	"OutputDebugStringW":   {},
	"GetCurrentProcessId":  {},
	"GetCurrentThreadId":   {},
	"GetCurrentProcess":    {},
	"GetCurrentThread":     {},
	"MultiByteToWideChar":  {},
	"WideCharToMultiByte":  {},
	"LocalFree":            {},
	"FD_SET":               {},
	"FD_ZERO":              {},
	"FD_ISSET":             {},
	"FD_CLR":               {},

	// fmt-library typedefs / aliases used as bare names in
	// instantiations.
	"string_view_t":  {},
	"wstring_view_t": {},

	// Smart-pointer type constructors invoked as bare names (e.g.
	// `unique_ptr<T>{ ... }`).
	"unique_ptr":  {},
	"shared_ptr":  {},
	"weak_ptr":    {},
	"auto_ptr":    {},
}

// cppStlHeaders is the set of C++ standard-library header names that
// appear as bare-name IMPORTS edges after the cpp extractor parses
// `#include <iostream>` style directives. These collapse to a single
// `ext:std` placeholder — every STL header is part of the std namespace
// and one placeholder per std lib matches the package-per-import
// collapse used elsewhere. Lang-gated to cpp / c via the call site.
var cppStlHeaders = map[string]struct{}{
	// C++ stdlib (selected, high-volume in real corpora).
	"iostream":      {},
	"iomanip":       {},
	"fstream":       {},
	"sstream":       {},
	"ostream":       {},
	"istream":       {},
	"streambuf":     {},
	"ios":           {},
	"iosfwd":        {},
	"string":        {},
	"string_view":   {},
	"vector":        {},
	"array":         {},
	"deque":         {},
	"list":          {},
	"forward_list":  {},
	"map":           {},
	"set":           {},
	"unordered_map": {},
	"unordered_set": {},
	"queue":         {},
	"stack":         {},
	"bitset":        {},
	"memory":        {},
	"memory_resource": {},
	"new":           {},
	"utility":       {},
	"tuple":         {},
	"functional":    {},
	"algorithm":     {},
	"numeric":       {},
	"iterator":      {},
	"chrono":        {},
	"thread":        {},
	"mutex":         {},
	"shared_mutex":  {},
	"condition_variable": {},
	"future":        {},
	"atomic":        {},
	"exception":     {},
	"stdexcept":     {},
	"system_error": {},
	"typeinfo":      {},
	"type_traits":   {},
	"limits":        {},
	"locale":        {},
	"codecvt":       {},
	"random":        {},
	"ratio":         {},
	"complex":       {},
	"valarray":      {},
	"variant":       {},
	"optional":      {},
	"any":           {},
	"filesystem":    {},
	"regex":         {},
	"cassert":       {},
	"cctype":        {},
	"cerrno":        {},
	"cfloat":        {},
	"climits":       {},
	"cmath":         {},
	"csignal":       {},
	"cstdarg":       {},
	"cstddef":       {},
	"cstdint":       {},
	"cstdio":        {},
	"cstdlib":       {},
	"cstring":       {},
	"ctime":         {},
	"cwchar":        {},
	"cwctype":       {},
	"concepts":      {},
	"ranges":        {},
	"span":          {},
	"charconv":      {},
	"bit":           {},
	"compare":       {},
	"coroutine":     {},
	"source_location": {},
	"version":       {},
	// C stdlib headers (POSIX + libc), used by C extractor.
	"stdio.h":   {},
	"stdlib.h":  {},
	"string.h":  {},
	"strings.h": {},
	"ctype.h":   {},
	"errno.h":   {},
	"math.h":    {},
	"time.h":    {},
	"unistd.h":  {},
	"fcntl.h":   {},
	"signal.h":  {},
	"stdarg.h":  {},
	"stddef.h":  {},
	"stdint.h":  {},
	"stdbool.h": {},
	"assert.h":  {},
	"limits.h":  {},
	"float.h":   {},
	"locale.h":  {},
	"setjmp.h":  {},
	"inttypes.h": {},
	"pthread.h": {},
	"sys/types.h": {},
	"sys/stat.h":  {},
	"sys/time.h":  {},
	"sys/socket.h": {},
}

// isKnownExternalPackage reports whether s matches our small allowlist
// of well-known third-party packages and stdlib top-level modules. The
// allowlist is intentionally narrow for v1.0 — false positives turn a
// local name into a placeholder, which is worse than missing one.
func isKnownExternalPackage(s string) bool {
	lower := strings.ToLower(s)
	// Issue #424 — every "docker:<repo>" placeholder corresponds to a real
	// image in a container registry. Treat the entire docker namespace as
	// allowlisted; ExternalKnown is the right disposition for image refs
	// regardless of whether the repo is on the static package allowlist.
	if strings.HasPrefix(lower, "docker:") {
		return true
	}
	// Refs #44 — every "gha:<org>/<repo>" placeholder corresponds to a real
	// GitHub Actions marketplace entry. Treat the entire gha namespace as
	// allowlisted; ExternalKnown is the right disposition for action refs.
	if strings.HasPrefix(lower, "gha:") {
		return true
	}
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
	"jquery":       {},
	"bootstrap":    {},
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
	// Issue #44 / GraphQL-fix — apollo-server bug-rate residue. JS/TS
	// GraphQL ecosystem deps and Node.js stdlib modules that appear as
	// bare-name IMPORTS targets across the apollo-server monorepo. These
	// are real external packages with no in-tree entity; the resolver
	// was tagging them BugExtractor instead of ExternalKnown.
	"graphql":            {}, // graphql-js reference impl
	"graphql-tag":        {}, // gql`` template literal helper
	"graphql-subscriptions": {},
	"loglevel":           {},
	"nock":               {}, // HTTP mocking lib
	"whatwg-mimetype":    {},
	"async-listener":     {},
	"cls-hooked":         {},
	"long":               {},
	"make-fetch-happen":  {},
	"lru-cache":          {},
	"negotiator":         {},
	"async-retry":        {},
	"jest-serializer-html": {},
	"jest-mock":          {},
	"jest-environment-node": {},
	"jest-environment-jsdom": {},
	"prettier":           {},
	"eslint":             {},
	"webpack":            {},
	"rollup":             {},
	"vite":               {},
	"ts-node":            {},
	"esbuild":            {},
	"chalk":              {},
	"commander":          {},
	"yargs":              {},
	"glob":               {},
	"semver":             {},
	"ws":                 {},
	"cors":               {},
	"body-parser":        {},
	"cookie":             {},
	"cookie-parser":      {},
	"morgan":             {},
	"debug":              {},
	"dotenv":             {},
	"node-fetch":         {},
	"undici":             {},
	"form-data":          {},
	"qs":                 {},
	"mime-types":         {},
	"compression":        {},
	"connect":            {},
	"on-finished":        {},
	"send":               {},
	"raw-body":           {},
	"http-errors":        {},
	"accepts":            {},
	"type-is":            {},
	"content-type":       {},
	"content-disposition": {},
	"fast-json-stable-stringify": {},
	"json-stable-stringify": {},
	"deep-equal":         {},
	"fast-deep-equal":    {},
	// Node.js stdlib modules (additional to existing console/readline/
	// assert/domain/url/net). Real node imports that node:<mod>-shaped
	// or bare-shaped — both forms case-fold to the same allowlist key.
	// `zlib` lives in the C/C++ third-party block below (shared key).
	"stream":            {},
	"buffer":            {},
	"events":            {},
	"util":              {},
	"querystring":       {},
	"child_process":     {},
	"cluster":           {},
	"dgram":             {},
	"dns":               {},
	"fs":                {},
	"http2":             {},
	"https":             {},
	"module":            {},
	"perf_hooks":        {},
	"process":           {},
	"punycode":          {},
	"repl":              {},
	"string_decoder":    {},
	"timers":            {},
	"tls":               {},
	"tty":               {},
	"v8":                {},
	"vm":                {},
	"worker_threads":    {},
	"inspector":         {},
	"trace_events":      {},
	"node:fs":           {},
	"node:path":         {},
	"node:url":          {},
	"node:util":         {},
	"node:stream":       {},
	"node:zlib":         {},
	"node:crypto":       {},
	"node:assert":       {},
	"node:buffer":       {},
	"node:events":       {},
	"node:http":         {},
	"node:https":        {},
	"node:net":          {},
	"node:os":           {},
	"node:process":      {},
	"node:child_process": {},
	"node:querystring":  {},
	"node:tls":          {},
	"node:dns":          {},
	"node:dgram":        {},
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
	"@graphql-codegen": {},
	"@jest":            {}, // @jest/globals etc.
	"@rollup":          {},
	"@apollo-server":   {},
	"@apollographql":   {},
	"@typescript-eslint": {},
	"@eslint":          {},
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

	// Issue #364 — Go stdlib multi-segment paths used as canonical names
	// for ext:<path> placeholders synthesised from receiver_type stdlib
	// interface dispatch. Single-segment stdlib roots (`io`, `os`, `fmt`,
	// `bufio`, `bytes`, `strings`, `sync`, `context`, `testing`, `http`,
	// `net`, `sql`) already exist above, but the goStdlibInterfaceMethods
	// catalogue uses the canonical import path (`net/http`,
	// `database/sql`) so the disposition allowlist must match the slash
	// form too.
	"net/http":     {},
	"database/sql": {},

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
	// Issue kafka-fix-w3 — Apache Kafka / Confluent / Avro / Jetty / Jersey
	// ecosystem roots. Multi-segment keys keep longestKnownDottedPrefix
	// precise so an unrelated `org.apache` user-namespace cannot collide.
	// All of these appear as `import org.apache.kafka.streams.*` /
	// `import io.confluent.examples.streams.*` / `import org.apache.avro.*`
	// in the kafka-streams-examples corpus and across every JVM
	// streaming/integration repo.
	"org.apache.kafka":     {}, // Kafka clients, streams, connect, common
	"org.apache.avro":      {}, // Apache Avro serdes
	"org.apache.curator":   {}, // ZooKeeper client framework
	"org.apache.zookeeper": {}, // ZooKeeper
	"org.apache.log4j":     {}, // log4j 1.x dotted form (in addition to bare `log4j`)
	"org.apache.logging":   {}, // log4j 2.x (org.apache.logging.log4j.*)
	"org.apache.hadoop":    {}, // commonly imported alongside Kafka in data pipelines
	"org.apache.commons.cli": {}, // commons-cli (explicit; org.apache.commons already covers it)
	"io.confluent":         {}, // Confluent schema-registry / kafka-streams examples / KSQL clients
	"kafka":                {}, // Scala Kafka classes (`import kafka.server.KafkaConfig`)
	"com.google.common":    {}, // Guava (`com.google.common.collect.*`) — supersedes legacy `com.google.guava`
	"com.google.protobuf":  {}, // Protobuf Java runtime
	"com.google.gson":      {}, // Gson JSON
	"org.eclipse.jetty":    {}, // Embedded Jetty server (REST interactive-queries demos)
	"org.glassfish.jersey": {}, // JAX-RS Jersey impl (REST clients in Kafka examples)
	"org.glassfish":        {}, // broader org.glassfish.* (HK2, etc.)
	"jakarta.ws":           {}, // jakarta.ws.rs.* (JAX-RS)
	"javax.ws":             {}, // javax.ws.rs.* (legacy JAX-RS)
	"org.rocksdb":          {}, // Kafka Streams default state-store backend
	"org.codehaus":         {}, // org.codehaus.jackson.* legacy / org.codehaus.plexus.*
	"com.typesafe":         {}, // com.typesafe.config (Akka/Kafka config)
	"reactor":              {}, // Project Reactor (`reactor.core.*`)
	"io.netty":             {}, // Netty (transport for many JVM brokers/clients)
	"io.reactivex":         {}, // RxJava
	"io.grpc":              {}, // gRPC Java
	"io.opentelemetry":     {}, // OpenTelemetry Java SDK
	"org.yaml":             {}, // snakeyaml
	"org.json":             {}, // org.json reference library
	"org.slf4j":            {}, // SLF4J Logger/LoggerFactory dotted form
	"javax.servlet":        {}, // jakarta predecessor (Jetty servlet API)
	"jakarta.servlet":      {}, // Jakarta Servlet API (Jetty 12+ EE10)
	// Scala ecosystem (play-scala-starter, Akka, scalatest, sbt, etc.).
	// Both the language-namespace `scala` root and JVM-style dotted
	// `org.*` / `com.*` roots are present so every `import` shape in a
	// real Scala project routes to ExternalKnown via the dotted-path
	// branch in classifyExternal. Multi-segment keys are preferred for
	// `org.*` roots so they match the longest-prefix walk precisely.
	"scala":                {}, // scala.concurrent.*, scala.util.*, scala.collection.*
	"akka":                 {}, // akka.actor.*, akka.http.*, akka.stream.*
	"play":                 {}, // play.api.* (Play Framework)
	"sbt":                  {},
	"cats":                 {}, // cats / cats-effect
	"monix":                {},
	"zio":                  {},
	"shapeless":            {},
	"slick":                {},
	"doobie":               {},
	"http4s":               {},
	"finagle":              {},
	"spray":                {},
	"org.scalatest":        {},
	"org.scalatestplus":    {},
	"org.scalacheck":       {},
	"org.scalamock":        {},
	"org.specs2":           {},
	"com.google.inject":    {}, // Guice — Play uses it for DI
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
	// C / C++ ecosystem (Issue #44 — spdlog bug-rate reduction). Header-
	// only libraries (spdlog, fmt, gtest, gmock, Catch2) and common
	// system / third-party C++ roots. The `std` allowlist key already
	// exists above (added for Rust) and is reused for STL-header
	// imports collapsed in classifyExternal.
	"spdlog":    {},
	"benchmark": {}, // Google Benchmark — used heavily in spdlog/bench
	"gtest":     {},
	"gmock":     {},
	"catch2":    {},
	"boost":     {},
	"eigen":     {},
	"qt":        {},
	"abseil":    {},
	"absl":      {},
	"folly":     {},
	"protobuf":  {},
	"grpc":      {},
	"openssl":   {},
	"zlib":      {},
	"curl":      {},
	// Swift ecosystem (Wave 3 — vapor-api-template, follow-up to #436).
	// Vapor + Fluent are the dominant server-side Swift stack; SwiftNIO
	// is the networking primitive Vapor builds on; Foundation/Combine/
	// SwiftUI/UIKit/AppKit are Apple stdlib top-levels; PackageDescription
	// is the Swift Package Manager DSL imported by `Package.swift`
	// manifests. Without these, Swift `import Vapor` lands in
	// bug-resolver (the extractor creates a SCOPE.Component named
	// "Vapor" per import — see internal/extractors/swift/swift.go
	// buildImport — and the resolver finds it, so it's flagged as a
	// bug-resolver ambiguous match rather than an external package).
	// XCTest is the test-runner stdlib (used by the XCTAssert* macros
	// already in swiftBareNames).
	"vapor":              {},
	"fluent":             {},
	"fluentsqlite":       {},
	"fluentpostgresql":   {},
	"fluentmysql":        {},
	"fluentmongo":        {},
	"swiftnio":           {},
	"nio":                {},
	"niocore":            {},
	"niohttp1":           {},
	"niohttp2":           {},
	"niofoundationcompat": {},
	"foundation":         {},
	"combine":            {},
	"swiftui":            {},
	"uikit":              {},
	"appkit":             {},
	"xctest":             {},
	"packagedescription": {},
	"console":            {},
	"jwt":                {},
	"leaf":               {},
	// `redis` already on the allowlist via the Python ecosystem block.
}

// googleBenchmarkBareNames is the cpp-gated Google Benchmark public-
// API stop-list (issue #44 — spdlog/bench bug-rate). UpperCamelCase
// surface from <benchmark/benchmark.h>. Receiver-stripped from
// `benchmark::DoNotOptimize(...)`, `benchmark::Initialize(...)` etc.
// Conservative — only the highest-volume, most-distinctive names.
var googleBenchmarkBareNames = map[string]struct{}{
	"DoNotOptimize":          {},
	"ClobberMemory":          {},
	"RegisterBenchmark":      {},
	"RunSpecifiedBenchmarks": {},
	"Initialize":             {},
	"Shutdown":               {},
	"UseRealTime":            {},
	"UseManualTime":          {},
	"MeasureProcessCPUTime":  {},
	"Iterations":             {},
	"Threads":                {},
	"ThreadRange":            {},
	"Range":                  {},
	"RangeMultiplier":        {},
	"Unit":                   {},
	"MinTime":                {},
	"Repetitions":            {},
	"ReportAggregatesOnly":   {},
	"DisplayAggregatesOnly":  {},
	"Args":                   {},
	"ArgsProduct":            {},
	"DenseRange":             {},
	"SkipWithError":          {},
	"ResumeTiming":           {},
	"PauseTiming":            {},
	"SetLabel":               {},
	"SetComplexityN":         {},
	"SetBytesProcessed":      {},
	"SetItemsProcessed":      {},
}

// isSpdlogFactoryName reports whether s matches the spdlog public
// factory-function shape — snake_case lowercase identifier ending in
// `_mt` (multi-threaded) or `_st` (single-threaded). Examples:
// basic_logger_mt, daily_logger_st, rotating_logger_mt,
// stdout_color_mt, stderr_color_st, syslog_logger_mt, udp_logger_st,
// callback_logger_mt, android_logger_mt. Used by classifyExternal
// (issue #44) to route these to the ext:spdlog placeholder. The
// `_mt`/`_st` suffix is the spdlog naming convention and is
// extremely unlikely to appear on user-defined methods unrelated to
// spdlog. Conservative shape check: at least 5 chars, prefix is
// snake_case lowercase letters/digits/underscores, ends in `_mt` /
// `_st`, prefix is non-empty after suffix strip.
func isSpdlogFactoryName(s string) bool {
	if len(s) < 5 {
		return false
	}
	if !(strings.HasSuffix(s, "_mt") || strings.HasSuffix(s, "_st")) {
		return false
	}
	prefix := s[:len(s)-3]
	if prefix == "" || prefix[len(prefix)-1] == '_' {
		return false
	}
	// Must contain at least one '_' in the prefix so we're matching
	// snake_case identifiers, not short ambiguous names like `cm_mt`.
	if !strings.ContainsRune(prefix, '_') {
		return false
	}
	for _, c := range prefix {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '_':
		default:
			return false
		}
	}
	return true
}

// isFmtBundledFile reports whether the from-file path lives inside a
// bundled fmt library tree (typical layout: `include/<lib>/fmt/bundled
// /...`). Used by classifyExternal (issue #44) to route bare CALLS
// originating from these vendored sources to ext:fmt.
func isFmtBundledFile(p string) bool {
	if p == "" {
		return false
	}
	return strings.Contains(p, "/fmt/bundled/") || strings.HasPrefix(p, "fmt/bundled/")
}

// isJQueryBundledFile reports whether p is a JavaScript file shipped as
// part of a vendored jQuery / jquery-validation / jquery-unobtrusive
// bundle — the canonical ASP.NET Core project-template layout puts these
// under `wwwroot/lib/jquery*/`, and the filenames themselves start with
// `jquery.` (e.g. `jquery.validate.unobtrusive.js`). Used by the jQuery
// bare-name gate (issue #441) to scope the jqueryBareNames stop-list to
// vendored library code without polluting hand-written JS in other
// codebases.
func isJQueryBundledFile(p string) bool {
	if p == "" {
		return false
	}
	if strings.Contains(p, "/wwwroot/lib/jquery") || strings.Contains(p, "/lib/jquery") {
		return true
	}
	// basename-shape check: `jquery.<...>.js` / `jquery-<...>.js` / `jquery.js`.
	slash := strings.LastIndexByte(p, '/')
	base := p
	if slash >= 0 {
		base = p[slash+1:]
	}
	if strings.HasPrefix(base, "jquery.") || strings.HasPrefix(base, "jquery-") || base == "jquery.js" {
		return true
	}
	return false
}

// jqueryBareNames is the jQuery-bundled-file-gated bare-name stop-list
// (issue #441). The vendored jquery / jquery-validation / jquery-
// unobtrusive sources call receiver-stripped jQuery surface methods
// (`$(...).addClass(...)` → `addClass`, `$.extend(...)` → `extend`)
// inside their own implementation; the resolver can't bind these to
// local entities, so they land in bug-extractor.
//
// Gated on isJQueryBundledFile(fromFile) so the same generic verbs
// (`hide`/`show`/`find`/`empty`) don't shadow hand-written user methods
// in non-jQuery JS codebases. Same safer-bias rule as the Go testify
// gate (#115). Folds to `ext:jquery` (added to knownExternalPackages
// below) so the stubs get ExternalKnown disposition.
var jqueryBareNames = map[string]struct{}{
	// jQuery CSS class manipulation
	"addClass":    {},
	"removeClass": {},
	"toggleClass": {},
	"hasClass":    {},
	// jQuery DOM traversal / manipulation
	"appendTo":   {},
	"prependTo":  {},
	"insertAfter": {},
	"insertBefore": {},
	"replaceWith": {},
	"wrap":       {},
	"unwrap":     {},
	"clone":      {},
	"detach":     {},
	"empty":      {},
	"remove":     {},
	"html":       {},
	"text":       {},
	"val":        {},
	"attr":       {},
	"removeAttr": {},
	"prop":       {},
	"removeProp": {},
	"css":        {},
	"data":       {},
	"removeData": {},
	// jQuery effects
	"show":     {},
	"hide":     {},
	"toggle":   {},
	"fadeIn":   {},
	"fadeOut":  {},
	"fadeTo":   {},
	"slideUp":  {},
	"slideDown": {},
	"slideToggle": {},
	// jQuery events
	"on":         {},
	"off":        {},
	"one":        {},
	"trigger":    {},
	"triggerHandler": {},
	"bind":       {},
	"unbind":     {},
	"delegate":   {},
	"undelegate": {},
	// jQuery utility / static helpers (`$.extend`, `$.each`, `$.isFunction`,
	// `$.parseJSON`, `$.proxy`, `$.ajax`, etc.).
	"extend":     {},
	"each":       {},
	"isFunction": {},
	"isArray":    {},
	"isPlainObject": {},
	"isEmptyObject": {},
	"isNumeric":  {},
	"isWindow":   {},
	"parseJSON":  {},
	"parseHTML":  {},
	"parseXML":   {},
	"proxy":      {},
	"ajax":       {},
	"get":        {},
	"post":       {},
	"getJSON":    {},
	"getScript":  {},
	"makeArray":  {},
	"inArray":    {},
	"grep":       {},
	"merge":      {},
	"noop":       {},
	"now":        {},
	"trim":       {},
	"type":       {},
	"contains":   {},
	"globalEval": {},
	// jQuery selector traversal
	"find":     {},
	"filter":   {},
	"closest":  {},
	"parent":   {},
	"parents":  {},
	"children": {},
	"siblings": {},
	"next":     {},
	"prev":     {},
	"nextAll":  {},
	"prevAll":  {},
	"first":    {},
	"last":     {},
	"eq":       {},
	"index":    {},
	"slice":    {},
	"add":      {},
	"andSelf":  {},
	"end":      {},
	"is":       {},
	"not":      {},
	"has":      {},
	"each_":    {}, // sentinel — guarded variant
	// jQuery form-related helpers used by jquery-validation /
	// jquery-validation-unobtrusive.
	"serialize":      {},
	"serializeArray": {},
	"submit":         {},
	"focus":          {},
	"blur":           {},
	"click":          {},
	"change":         {},
	"keydown":        {},
	"keyup":          {},
	"keypress":       {},
	"mouseenter":     {},
	"mouseleave":     {},
	"mouseover":      {},
	"mouseout":       {},
	"hover":          {},
	"select":         {},
	"ready":          {},
	// jquery-validation plugin surface
	"validate":       {},
	"valid":          {},
	"resetForm":      {},
	"rules":          {},
	"unobtrusive":    {},
	"validator":      {},
	"showErrors":     {},
	"numberOfInvalids": {},
	// jQuery internal/private surface that leaks via receiver-strip
	// inside the validation bundles.
	"apply":          {},
	"call":           {},
	"replace":        {}, // String.prototype.replace + jQuery internal
	"split":          {},
	"substr":         {},
	"substring":      {},
	"charAt":         {},
	"charCodeAt":     {},
	"indexOf":        {},
	"lastIndexOf":    {},
	"concat":         {},
	"join":           {},
	"reverse":        {},
	"sort":           {},
	"shift":          {},
	"unshift":        {},
	"pop":            {},
	"splice":         {},
	// `$` itself appears as a bare CALLS target when the extractor
	// can't bind the jQuery global.
	"$": {},
}

// isFmtMacroIdent reports whether s is an UPPER_SNAKE_CASE identifier
// with the FMT_ prefix — used by classifyExternal (issue #44) to route
// fmt-library preprocessor macros (FMT_ASSERT, FMT_THROW, FMT_ENABLE_
// IF, FMT_STRING, ...) to the ext:fmt placeholder.
func isFmtMacroIdent(s string) bool {
	const prefix = "FMT_"
	if !strings.HasPrefix(s, prefix) || len(s) <= len(prefix) {
		return false
	}
	for _, c := range s[len(prefix):] {
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_':
		default:
			return false
		}
	}
	return true
}

// catch2BareNames is the cpp-gated Catch2 test-macro stop-list
// (issue #44). Catch2 uses UpperCamelCase / UPPER_SNAKE_CASE macros
// that survive the cpp extractor as bare CALLS edges. Routed to
// ext:catch2.
var catch2BareNames = map[string]struct{}{
	"REQUIRE":             {},
	"REQUIRE_FALSE":       {},
	"REQUIRE_NOTHROW":     {},
	"REQUIRE_THROWS":      {},
	"REQUIRE_THROWS_AS":   {},
	"REQUIRE_THROWS_WITH": {},
	"REQUIRE_THAT":        {},
	"CHECK":               {},
	"CHECK_FALSE":         {},
	"CHECK_NOTHROW":       {},
	"CHECK_THROWS":        {},
	"CHECK_THROWS_AS":     {},
	"CHECK_THAT":          {},
	"SECTION":             {},
	"TEST_CASE":           {},
	"TEST_CASE_METHOD":    {},
	"SCENARIO":            {},
	"GIVEN":               {},
	"WHEN":                {},
	"THEN":                {},
	"AND_WHEN":            {},
	"AND_THEN":            {},
	"INFO":                {},
	"FAIL":                {},
	"WARN":                {},
	"CAPTURE":             {},
	"SUCCEED":             {},
	"DYNAMIC_SECTION":     {},
	"GENERATE":            {},
	"BENCHMARK":           {},
	"DOCTEST_TEST_CASE":   {},
	"DOCTEST_CHECK":       {},
}

// isSpdlogMacroIdent reports whether s is an UPPER_SNAKE_CASE
// identifier with the SPDLOG_ prefix — used by classifyExternal to
// route spdlog preprocessor macros (SPDLOG_LOGGER_DEBUG, SPDLOG_TRACE,
// SPDLOG_LOGGER_CATCH, SPDLOG_THROW, ...) to the ext:spdlog placeholder
// (issue #44). Strict shape: starts with "SPDLOG_", remaining chars are
// all upper-case ASCII letters, digits, or underscores. No leading
// double underscore (reserved). Conservative — a leading underscore in
// the suffix would reject, so plain `SPDLOG_` alone is rejected.
func isSpdlogMacroIdent(s string) bool {
	const prefix = "SPDLOG_"
	if !strings.HasPrefix(s, prefix) || len(s) <= len(prefix) {
		return false
	}
	for _, c := range s[len(prefix):] {
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_':
		default:
			return false
		}
	}
	return true
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

// dockerImageRepo extracts the canonical repository segment from a Docker
// image reference, dropping the tag (`:<tag>`) or digest (`@sha256:...`).
// Returns "" for malformed inputs.
//
// Examples (Issue #424):
//
//	"nginx:1.21"                      → "nginx"
//	"redis:alpine"                    → "redis"
//	"library/postgres:14"             → "library/postgres"
//	"ghcr.io/owner/svc:v1.2.3"        → "ghcr.io/owner/svc"
//	"myregistry.io:5000/team/api:dev" → "myregistry.io:5000/team/api"
//	"alpine@sha256:abc..."            → "alpine"
//	""                                → ""
func dockerImageRepo(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	// Strip the digest first — it always uses `@` and never contains `:`
	// inside the digest body for our purposes (the algorithm prefix uses `:`
	// but lives strictly to the right of the `@`).
	if at := strings.IndexByte(ref, '@'); at >= 0 {
		ref = ref[:at]
	}
	// Find the last `:` and decide whether it's a tag separator or part of a
	// registry-port specifier. A tag separator never appears after a `/` of
	// a path; a registry port is always followed by `/`.
	lastColon := strings.LastIndexByte(ref, ':')
	if lastColon < 0 {
		return ref
	}
	// If there's a `/` to the right of the colon, the colon belongs to a
	// registry-port (e.g. "myregistry.io:5000/team/api") — keep it.
	if strings.IndexByte(ref[lastColon:], '/') >= 0 {
		return ref
	}
	repo := ref[:lastColon]
	if repo == "" {
		return ""
	}
	return repo
}

// ghaActionRepo strips the @version/sha suffix from a GitHub Actions
// `uses:` reference, returning the canonical action repo identity.
//
//	"actions/checkout@v4"                            → "actions/checkout"
//	"docker/build-push-action@0565240e2d4ab88bba..." → "docker/build-push-action"
//	"github/codeql-action/upload-sarif@v3"           → "github/codeql-action"
//	"./.github/actions/local-action"                 → "" (local, not external)
//
// Local action paths (starting with `./` or `../`) live inside the corpus
// and should NOT be lifted to external; returning "" makes the caller fall
// through and the resolver treats them as in-corpus refs.
//
// Refs #44.
func ghaActionRepo(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	// Local actions are in-corpus — skip.
	if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../") {
		return ""
	}
	// Docker actions are written `docker://image:tag` — already handled by
	// the docker_image branch; defer to that path.
	if strings.HasPrefix(ref, "docker://") {
		return ""
	}
	// Strip the version pin (@v4, @sha, @branch).
	if at := strings.IndexByte(ref, '@'); at >= 0 {
		ref = ref[:at]
	}
	// Canonicalise to "<org>/<repo>"; collapse any subpath (e.g.
	// "github/codeql-action/upload-sarif" → "github/codeql-action") so all
	// uses of the same action repo land on one placeholder.
	parts := strings.SplitN(ref, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
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

// zigBareNames is the Zig-language-gated bare-name stop-list (issue
// #382 follow-up — wave-3 http.zig fix). The Zig extractor takes the
// rightmost segment of a dotted call (`std.debug.assert(...)` →
// `assert`, `std.mem.Allocator.alignedAlloc(...)` → `alignedAlloc`,
// `std.ArrayList(u8).init(...)` → `init` (filtered out as risky),
// `std.StringHashMap(...)` → `StringHashMap`), so high-volume zig
// stdlib calls land in bug-extractor as unresolved bare CALLS.
//
// Mirrors the Swift / Kotlin Ktor precedent (#435 / #436): the
// language gate (lang == "zig") is the safety net that prevents
// names like `assert`, `dupe`, `allocPrint` from shadowing user-
// defined methods in Go / JS / Python / Ruby / Kotlin codebases.
//
// Conservative selection (lessons from #94 / #105 / #106): generic
// verbs that are routinely user-defined on a Zig struct
// (`init`, `deinit`, `free`, `close`, `get`, `parse`, `write`,
// `read`, `add`, `remove`, `create`, `destroy`, `run`, `stop`,
// `accept`, `listen`, `send`, `recv`, `set`, `put`, `delete`,
// `clear`, `reset`, `start`, `update`, `flush`, `commit`) are
// deliberately EXCLUDED — the bug-resolver `ambig-bare-hint-fail`
// histogram on http.zig shows these names with 2+ user-defined
// candidates in the local graph. Including them would synthesise
// `ext:init` placeholders for real user methods, breaking #94's
// safer-bias rule.
//
// Categories:
//   - std.debug helpers (`assert`, `print` (already global),
//     `panic`, `warn`).
//   - std.mem / Allocator helpers (`alloc`, `free` excluded as
//     risky; `dupe`, `alignedAlloc`, `allocPrint`, `toBytes`,
//     `asBytes`, `eql`, `align`, `copy`, `zeroes`,
//     `lessThan`, `indexOf`, `indexOfScalar`, `lastIndexOf`,
//     `startsWith`, `endsWith`, `trim`, `trimLeft`, `trimRight`,
//     `splitScalar`, `splitSequence`, `tokenize`, `tokenizeAny`,
//     `concat`, `join` (already global), `replaceScalar`).
//   - std container constructors (`ArrayList`, `ArrayListUnmanaged`,
//     `StringHashMap`, `StringHashMapUnmanaged`, `AutoHashMap`,
//     `AutoHashMapUnmanaged`, `MemoryPool`, `BufSet`, `BufMap`,
//     `DoublyLinkedList`, `SinglyLinkedList`, `PriorityQueue`,
//     `PriorityDequeue`).
//   - std.fmt helpers (`allocPrint`, `bufPrint`, `comptimePrint`,
//     `parseInt`, `parseFloat`, `parseUnsigned`, `formatType`,
//     `formatFloat`, `formatInt`, `formatBuf`).
//   - std.json helpers (`parseFromSlice`, `parseFromSliceLeaky`,
//     `parseFromTokenSource`, `stringify`, `stringifyAlloc`).
//   - std.testing helpers (`expect`, `expectEqual`,
//     `expectEqualStrings`, `expectEqualSlices`, `expectError`,
//     `expectApproxEqAbs`, `expectApproxEqRel`,
//     `expectStringStartsWith`, `expectStringEndsWith`,
//     `expectFmt`, `expectString`, `expectGzip`, `expectAnyCall`).
//   - std.time helpers (`now`, `untilNow`, `nanoTimestamp`,
//     `milliTimestamp`, `microTimestamp`, `timestamp`, `sleep`).
//   - std.os / std.posix helpers (`exit`, `getenv`, `setenv`,
//     `getenvZ`, `getcwd`, `unexpectedWSAError`).
//   - std.heap helpers (`GeneralPurposeAllocator`,
//     `ArenaAllocator`, `page_allocator`, `c_allocator`,
//     `fromByteUnits`).
//   - std.atomic / std.Thread helpers (`lockUncancelable`,
//     `waitUncancelable`, `tryAcquire`, `release` excluded as
//     risky).
//   - Metrics / hash helpers commonly bare-called via dotted
//     chains (`incr`, `incrBy`, `hashFn`, `hasFn`).
var zigBareNames = map[string]struct{}{
	// std.debug
	"assert": {},
	"panic":  {},
	"warn":   {},

	// std.mem / Allocator helpers (exclude generic `alloc`/`free`).
	"dupe":           {},
	"dupeZ":          {},
	"alignedAlloc":   {},
	"toBytes":        {},
	"asBytes":        {},
	"bytesAsSlice":   {},
	"sliceAsBytes":   {},
	"eql":            {},
	"order":          {},
	"lessThan":       {},
	"indexOf":        {},
	"indexOfScalar":  {},
	"indexOfAny":     {},
	"lastIndexOf":    {},
	"startsWith":     {},
	"endsWith":       {},
	"trim":           {},
	"trimLeft":       {},
	"trimRight":      {},
	"splitScalar":    {},
	"splitSequence":  {},
	"splitAny":       {},
	"split":          {}, // std.mem.split — zig-gated, distinct from JS split
	"tokenize":       {},
	"tokenizeAny":    {},
	"tokenizeScalar": {},
	"concat":         {},
	"replaceScalar":  {},
	"copyForwards":   {},
	"copyBackwards":  {},
	"swap":           {},
	"zeroes":         {},
	"alignForward":   {},
	"alignBackward":  {},
	"isAligned":      {},
	"align":          {},

	// std container generic types (called with a type parameter:
	// `std.ArrayList(u8)`). The Zig extractor sees them as bare
	// constructor-shaped calls.
	"ArrayList":              {},
	"ArrayListUnmanaged":     {},
	"BoundedArray":           {},
	"StringHashMap":          {},
	"StringHashMapUnmanaged": {},
	"AutoHashMap":            {},
	"AutoHashMapUnmanaged":   {},
	"HashMap":                {},
	"HashMapUnmanaged":       {},
	"MemoryPool":             {},
	"BufSet":                 {},
	"BufMap":                 {},
	"DoublyLinkedList":       {},
	"SinglyLinkedList":       {},
	"PriorityQueue":          {},
	"PriorityDequeue":        {},
	"MultiArrayList":         {},
	"SegmentedList":          {},

	// std.fmt helpers.
	"allocPrint":     {},
	"allocPrintZ":    {},
	"bufPrint":       {},
	"bufPrintZ":      {},
	"comptimePrint":  {},
	"parseInt":       {},
	"parseFloat":     {},
	"parseUnsigned":  {},
	"formatType":     {},
	"formatFloat":    {},
	"formatInt":      {},
	"formatBuf":      {},
	"formatText":     {},
	"formatIntValue": {},

	// std.json helpers.
	"parseFromSlice":       {},
	"parseFromSliceLeaky":  {},
	"parseFromTokenSource": {},
	"stringify":            {},
	"stringifyAlloc":       {},

	// std.testing helpers. The zig test runner uses these as bare
	// top-level calls inside `test "name" { ... }` blocks. Names like
	// `expect`/`expectEqual` are distinctive and unlikely to collide
	// with a user-defined struct method.
	"expect":                 {},
	"expectEqual":            {},
	"expectEqualStrings":     {},
	"expectEqualSlices":      {},
	"expectEqualSentinel":    {},
	"expectEqualDeep":        {},
	"expectError":            {},
	"expectApproxEqAbs":      {},
	"expectApproxEqRel":      {},
	"expectStringStartsWith": {},
	"expectStringEndsWith":   {},
	"expectFmt":              {},
	"expectString":           {},
	"expectGzip":             {},

	// std.time helpers.
	"now":            {},
	"untilNow":       {},
	"nanoTimestamp":  {},
	"milliTimestamp": {},
	"microTimestamp": {},
	"timestamp":      {},

	// std.os / std.posix helpers. `exit`/`getenv` are distinctive
	// enough at the zig gate to be safe; user-defined `exit` on a
	// Zig struct is extremely rare.
	"unexpectedWSAError": {},
	"getenvZ":            {},
	"getcwdAlloc":        {},

	// std.heap helpers.
	"GeneralPurposeAllocator": {},
	"ArenaAllocator":          {},
	"FixedBufferAllocator":    {},
	"StackFallbackAllocator":  {},
	"page_allocator":          {},
	"c_allocator":             {},
	"raw_c_allocator":          {},
	"smp_allocator":           {},
	"fromByteUnits":           {},

	// std.Thread / std.atomic primitives that the extractor
	// receiver-strips when the lock/wait is on a struct field.
	"lockUncancelable":   {},
	"waitUncancelable":   {},
	"timedWaitFor":       {},

	// std.hash / std.crypto helpers that show up bare as dotted
	// leaves (`std.hash.Wyhash.hash` → `hashFn` / `hash`). Keep
	// only the distinctive-suffix names.
	"hashFn": {},
	"hasFn":  {},
	"incr":   {},
	"incrBy": {},

	// std.mem.Allocator method calls (`allocator.create(T)`,
	// `allocator.destroy(ptr)`, `allocator.resize(slice, n)`).
	// These are the canonical Zig manual-memory idioms and are
	// safe under the zig gate even though `create` / `destroy`
	// look generic — user-defined struct methods named `create` /
	// `destroy` are rare in idiomatic Zig (the convention is
	// `init` / `deinit`), and the zig gate keeps the names from
	// shadowing user methods in other ecosystems.
	"create":   {},
	"destroy":  {},
	"resize":   {},
	"realloc":  {},
	"rawAlloc": {},
	"rawResize": {},
	"rawFree":  {},

	// std.ArrayList / ArrayListUnmanaged method surface.
	"appendSlice":            {},
	"appendNTimes":           {},
	"appendAssumeCapacity":   {},
	"appendSliceAssumeCapacity": {},
	"ensureTotalCapacity":    {},
	"ensureUnusedCapacity":   {},
	"ensureTotalCapacityPrecise": {},
	"clearRetainingCapacity": {},
	"clearAndFree":           {},
	"toOwnedSlice":           {},
	"toOwnedSliceSentinel":   {},
	"prepend":                {},
	"insertSlice":            {},
	"swapRemove":             {},
	"orderedRemove":          {},
	"popOrNull":              {},
	"getPtr":                 {},
	"writableSliceGreedy":    {},

	// std.HashMap method surface.
	"getOrPut":              {},
	"getOrPutValue":         {},
	"getOrPutAssumeCapacity": {},
	"putAssumeCapacity":     {},
	"putNoClobber":          {},
	"fetchPut":              {},
	"fetchRemove":           {},
	"containsKey":           {},
	"removeByPtr":           {},
	"valueIterator":         {},
	"keyIterator":           {},

	// std.mem extras / std.ascii.
	"indexOfScalarPos": {},
	"indexOfAnyPos":    {},
	"indexOfPos":       {},
	"lastIndexOfScalar": {},
	"eqlIgnoreCase":    {},
	"sliceTo":          {},
	"span":             {},
	"trimStart":        {},
	"trimEnd":          {},
	"trimLeadingSpace": {},
	"trimLeadingSpaceCount": {},
	"toLower":          {},
	"toUpper":          {},
	"toInt":            {},
	"toSeconds":        {},
	"toMicroseconds":   {},

	// std.fmt extras.
	"printInt":   {},
	"formatBuffer": {},

	// std.fs.
	"openFile":       {},
	"createFile":     {},
	"openDir":        {},
	"makePath":       {},
	"readFileAlloc":  {},
	"writeFile":      {},

	// std.os / std.posix syscalls and helpers.
	"sleep":       {},
	"closesocket": {},
	"socketpair":  {},
	"sigaction":   {},
	"sigemptyset": {},
	"sigfillset":  {},
	"sigaddset":   {},
	"getpid":      {},
	"kill":        {},
	"setupConnection": {},

	// std.math helpers.
	"maxInt":         {},
	"minInt":         {},
	"shlExact":       {},
	"shrExact":       {},
	"uintAtMost":     {},
	"intRangeAtMost": {},
	"intRangeLessThan": {},
	"divCeil":        {},
	"divFloor":       {},
	"divTrunc":       {},
	"divExact":       {},
	"mulWide":        {},
	"absCast":        {},
	"cast":           {},
	"lossyCast":      {},

	// std.mem byte-order helpers.
	"nativeToBig":    {},
	"nativeToLittle": {},
	"bigToNative":    {},
	"littleToNative": {},
	"readIntNative":  {},
	"readIntBig":     {},
	"readIntLittle":  {},
	"writeIntBig":    {},
	"writeIntLittle": {},
	"writeIntNative": {},
	"readInt":        {},
	"writeInt":       {},

	// std.io / buffered reader/writer.
	"bufferedReader":   {},
	"bufferedWriter":   {},
	"buffered":         {},
	"readSliceShort":   {},
	"readVec":          {},

	// std.builtin / std.Build.
	"standardTargetOptions":   {},
	"standardOptimizeOption":  {},
	"ArgsTuple":               {},
	"suggestVectorLength":     {},
	"isDarwin":                {},
	"isWindows":               {},
	"isLinux":                 {},
	"isBSD":                   {},

	// std.json extras.
	"parseFree": {},

	// Random / std.rand.
	"DefaultPrng":          {},
	"intRangeAtMostBiased": {},

	// std.Build (`build.zig`) DSL — bare receiver-stripped methods on
	// the `*std.Build` and `*std.Build.Step.Compile` chains. Names
	// like `installArtifact` / `addExecutable` / `dependency` are
	// distinctive enough to be safe at the zig gate.
	"installArtifact":    {},
	"dependency":         {},
	"dependOn":           {},
	"createModule":       {},
	"addModule":          {},
	"addExecutable":      {},
	"addStaticLibrary":   {},
	"addSharedLibrary":   {},
	"addTest":            {},
	"addOptions":         {},
	"addCSourceFile":     {},
	"addCSourceFiles":    {},
	"addIncludePath":     {},
	"addLibraryPath":     {},
	"linkLibrary":        {},
	"linkSystemLibrary":  {},
	"linkLibC":           {},
	"linkLibCpp":         {},
	"installHeader":      {},
	"installFile":        {},
	"installDirectory":   {},

	// std.log / std.debug additions.
	"info":                  {},
	"dumpErrorReturnTrace":  {},
	"defaultPanic":          {},
	"detectError":           {},
	"frameText":             {},

	// std.ascii / unicode additions.
	"lowerString":   {},
	"upperString":   {},
	"isHex":         {},
	"isDigit":       {},
	"isAlpha":       {},
	"isAlphabetic":  {},
	"isWhitespace":  {},
	"isPrint":       {},
	"isUpper":       {},
	"isLower":       {},

	// std.net additions.
	"parseIp":     {},
	"parseIp4":    {},
	"parseIp6":    {},
	"resolveIp":   {},
	"getsockname": {},
	"getsockopt":  {},
	"setsockopt":  {},
	"ioctlsocket": {},

	// std.compress / std.crypto additions.
	"decompress":     {},
	"decodeHex":      {},
	"parseExtension": {},

	// std.io extras (fixed-buffer stream + reader/writer adaptors).
	"fixedBufferStream": {},

	// std.PriorityQueue / SegmentedList method surface.
	"popMin":  {},
	"peekMin": {},

	// std.time extras.
	"durationTo": {},

	// std.mem long-tail not yet covered.
	"indexOfNonePos": {},
	"consumeAll":     {},

	// std.log severity helpers — receiver-stripped from
	// `log.err(...)`, `log.info(...)`, `log.debug(...)`,
	// `log.warn(...)`. `err` and `debug` are common identifier names
	// so the zig gate is the safety net.
	"err":   {},
	"debug": {},

	// std.Build extras (`addRunArtifact`, `addArgs`, `addImport`,
	// `addOption`).
	"addRunArtifact": {},
	"addArgs":        {},
	"addImport":      {},
	"addOption":      {},

	// std.fs.Dir / std.fs absolute helpers.
	"deleteFileAbsolute": {},
	"deleteTreeAbsolute": {},

	// std.os.windows constants/syscalls (Win32 surface that the zig
	// extractor receiver-strips from `windows.WSASocketW(...)`).
	"WSASocketW":  {},
	"ReadFile":    {},
	"WriteFile":   {},
	"CloseHandle": {},

	// std.os.linux long-tail.
	"accept4": {},

	// std.Build.Step extras / generic helpers that show up bare.
	"advance":   {},
	"broadcast": {},
	"detach":    {},
}
