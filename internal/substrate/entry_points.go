// Phase 1B reachability substrate (#2766). Per-language entry-point
// sniffers feed the generic reachability pass at
// internal/links/reachability.go.
//
// An entry-point is a symbol that the language runtime, framework, or
// build system can invoke without an in-graph edge pointing at it: a
// CLI main, a library re-export, a test entry, a framework lifecycle
// hook. The reachability pass starts a BFS from every entry-point and
// stamps `reachable: true` on every entity it can reach.
//
// Design notes (mirroring Phase 0 — see substrate.go):
//   - Per-language sniffers are pure functions over file content.
//     Stateless, deterministic, nil-safe.
//   - The reachability pass owns graph traversal and property stamping.
//   - Recognised entry-point shapes are intentionally narrow per #2766:
//     CLI main, library exports, test entries, framework lifecycle
//     names. Framework-handler reachability (HTTP routes, signal
//     handlers, navigation targets) is already encoded as graph edges
//     and is recognised by the reachability pass directly — sniffers
//     do not need to re-emit those.
package substrate

import "sort"

// EntryPoint is one entry-point fact lifted from source by a per-language
// sniffer.
type EntryPoint struct {
	// Ident is the bare symbol name (e.g. "main", "test_login"). The
	// reachability pass scopes it by (repo, file) when matching against
	// entity Names.
	Ident string

	// Line is the 1-indexed source line of the entry. Disambiguates
	// when multiple matches share the same name in one file (rare for
	// the recognised shapes, but possible for nested test functions).
	Line int

	// Kind labels the entry-point shape so callers can filter or
	// telemetry-bucket them. One of the EntryKind* constants below.
	Kind EntryKind
}

// EntryKind is the discrete kind label for an entry-point.
type EntryKind string

const (
	// EntryKindCLIMain marks a process entry (Go `func main`, Python
	// `if __name__ == "__main__"` block, Java `public static void
	// main`, JS/TS top-level CLI script).
	EntryKindCLIMain EntryKind = "cli_main"

	// EntryKindLibraryExport marks a re-exported public symbol that an
	// external consumer may call without an in-graph CALLS edge. Covers
	// `export` (JS/TS), `__all__` membership (Python), `public` top-
	// level methods (Java), and capitalised package-level identifiers
	// (Go — every capitalised top-level identifier is public).
	EntryKindLibraryExport EntryKind = "library_export"

	// EntryKindTestEntry marks a test function whose name follows the
	// language test-runner convention (`Test*` Go, `test_*` / pytest
	// fn, `@Test` Java, `it`/`describe`/`test` JS).
	EntryKindTestEntry EntryKind = "test_entry"

	// EntryKindFrameworkLifecycle marks a name like `setup`, `init`,
	// `start`, `bootstrap` invoked by a runtime / framework / DI
	// container without an in-graph caller.
	EntryKindFrameworkLifecycle EntryKind = "framework_lifecycle"
)

// EntryPointSniffFn is the contract every per-language entry-point
// sniffer satisfies. Deterministic.
type EntryPointSniffFn func(content string) []EntryPoint

// entryRegistry holds the registered per-language sniffers.
var entryRegistry = map[string]EntryPointSniffFn{}

// RegisterEntryPoints installs a sniffer for a language slug. Last-wins
// on duplicate registration.
func RegisterEntryPoints(lang string, fn EntryPointSniffFn) {
	if lang == "" || fn == nil {
		return
	}
	entryRegistry[lang] = fn
}

// EntryPointSnifferFor returns the registered sniffer for lang, or nil
// when none is registered.
func EntryPointSnifferFor(lang string) EntryPointSniffFn {
	return entryRegistry[lang]
}

// EntryPointLanguages returns the slugs of every registered language in
// sorted order.
func EntryPointLanguages() []string {
	out := make([]string, 0, len(entryRegistry))
	for k := range entryRegistry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
