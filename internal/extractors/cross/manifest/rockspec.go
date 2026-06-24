// rockspec.go — LuaRocks manifest + lockfile parsers (#5365, epic #5360).
//
// LuaRocks is the package manager and build system for Lua. Two file shapes
// are parsed here, both of which are Lua source (table literals):
//
//	*.rockspec — the package manifest. Declares package/version/source/build
//	  and three dependency lists, each a Lua array of version-constraint
//	  strings:
//
//	    package = "luafilesystem"
//	    version = "1.8.0-1"
//	    dependencies = {
//	       "lua >= 5.1",
//	       "lpeg >= 1.0.0",
//	    }
//	    build_dependencies = { "luarocks-build-cpp" }
//	    test_dependencies  = { "busted >= 2.0" }
//
//	  A dependency string is "<name> <op> <version>" (the op/version is
//	  optional: a bare "lpeg" pins nothing). The leading token up to the first
//	  space is the rock name; the remainder is the version constraint. `lua`
//	  itself is a declared dependency (the interpreter version floor) and is
//	  kept — it is a real edge in the dependency graph.
//
//	luarocks.lock — the pinned lockfile (`luarocks install --lock`,
//	  LuaRocks 3.3+). A Lua table literal of exactly-resolved versions:
//
//	    return {
//	       dependencies = {
//	          ["lua"] = "5.4-1",
//	          ["lpeg"] = "1.0.2-1",
//	       },
//	    }
//
//	  Every entry is a fully-resolved transitive dependency → emitted with
//	  kind="locked" so downstream queries can distinguish the resolved tree
//	  from the declared (range-versioned) rockspec deps, mirroring the
//	  package-lock.json / pubspec.lock contract (#2865).
package manifest

import "regexp"

// ---------------------------------------------------------------------------
// Parser: *.rockspec
// ---------------------------------------------------------------------------

// rockspecDepBlockRE captures the array body assigned to one of the three
// LuaRocks dependency lists. The body runs from the opening `{` to the matching
// `}` on the (possibly multi-line) RHS; the inner string entries are mined by
// rockspecDepEntryRE.
var rockspecDepBlockRE = regexp.MustCompile(
	`(?s)\b(dependencies|build_dependencies|test_dependencies)\s*=\s*\{([^}]*)\}`)

// rockspecDepEntryRE matches one quoted dependency constraint string and splits
// it into the rock name (leading identifier) and the trailing version
// constraint. Examples it captures:
//
//	"lua >= 5.1"        → name=lua,  version=">= 5.1"
//	"lpeg >= 1.0.0"     → name=lpeg, version=">= 1.0.0"
//	"luafilesystem"     → name=luafilesystem, version=""
//	"luasocket == 3.0"  → name=luasocket, version="== 3.0"
//
// Rock names are LuaRocks-legal: letters, digits, `_` and `-`. The version
// constraint is whatever follows the first run of whitespace (operators
// >= <= == ~> < > and the version literal), trimmed.
var rockspecDepEntryRE = regexp.MustCompile(
	`["']([A-Za-z0-9_][A-Za-z0-9_.-]*)\s*([^"']*?)["']`)

// parseRockspec parses a LuaRocks `.rockspec` manifest and returns its declared
// dependencies across the runtime (`dependencies`), build (`build_dependencies`)
// and test (`test_dependencies`) lists. Build- and test-only deps are flagged
// isDev=true (kind "dev") so they are separable from runtime deps, matching the
// conanfile build_requires/test_requires and gradle testImplementation
// treatment. First declaration of a name wins on duplicates.
func parseRockspec(source string) []dep {
	var out []dep
	seen := map[string]bool{}

	for _, bm := range rockspecDepBlockRE.FindAllStringSubmatch(source, -1) {
		listName := bm[1]
		body := bm[2]
		isDev := listName == "build_dependencies" || listName == "test_dependencies"
		kind := "runtime"
		if isDev {
			kind = "dev"
		}
		for _, em := range rockspecDepEntryRE.FindAllStringSubmatch(body, -1) {
			name := em[1]
			version := em[2]
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, dep{name: name, version: version, isDev: isDev, kind: kind})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: luarocks.lock
// ---------------------------------------------------------------------------

// rockspecLockDepsBlockRE captures the `dependencies = { … }` table inside a
// luarocks.lock return-table. The lock may also carry a `dependencies` key
// nested under sub-tables, but the top-level resolved set is what we enumerate.
var rockspecLockDepsBlockRE = regexp.MustCompile(
	`(?s)\bdependencies\s*=\s*\{(.*?)\n\s*\}`)

// rockspecLockEntryRE matches one pinned `["name"] = "version"` entry of a
// luarocks.lock dependencies table. Both bracketed (`["lpeg"]`) and bare-key
// (`lpeg =`) forms are accepted.
var rockspecLockEntryRE = regexp.MustCompile(
	`(?m)(?:\[\s*["']([A-Za-z0-9_][A-Za-z0-9_.-]*)["']\s*\]|^\s*([A-Za-z0-9_][A-Za-z0-9_.-]*))\s*=\s*["']([^"']+)["']`)

// parseLuarocksLock parses a `luarocks.lock` lockfile and returns the
// fully-resolved (pinned-version) dependency set. Every entry is marked
// kind="locked" — these are the exact resolved versions including the
// transitive closure the rockspec never names, the whole point of
// lockfile_parsing (#2865).
func parseLuarocksLock(source string) []dep {
	var out []dep
	seen := map[string]bool{}

	block := source
	if m := rockspecLockDepsBlockRE.FindStringSubmatch(source); m != nil {
		block = m[1]
	}

	for _, em := range rockspecLockEntryRE.FindAllStringSubmatch(block, -1) {
		name := em[1]
		if name == "" {
			name = em[2]
		}
		version := em[3]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, dep{name: name, version: version, kind: "locked"})
	}
	return out
}
