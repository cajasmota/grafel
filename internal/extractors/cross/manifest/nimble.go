// nimble.go — Nim nimble package manifest parser (#5367, epic #5360).
//
// nimble (https://github.com/nim-lang/nimble) is the package manager for Nim. A
// package's manifest is a `*.nimble` file written in NimScript (a Nim subset).
// Dependencies are declared with the `requires` directive, which accepts one or
// more version-constraint strings:
//
//	# foo.nimble
//	version       = "0.3.1"
//	author        = "Jane Doe"
//	description   = "A Nim web service"
//	license       = "MIT"
//	srcDir        = "src"
//
//	requires "nim >= 1.6.0"
//	requires "jester >= 0.5.0"
//	requires "norm", "debby >= 0.1.0"          # several deps on one directive
//	requires("waterpark >= 0.1.6")             # call-syntax form
//
//	# task / feature blocks may declare extra (dev/test) requirements:
//	taskRequires "test", "balls >= 3.0"
//	feature "postgres":
//	  requires "db_connector"
//
// A dependency constraint string is "<name> <op> <version>" where the op/version
// is optional (a bare "jester" pins nothing). The leading identifier up to the
// first run of whitespace is the package name; the remainder is the version
// constraint. `nim` itself is a declared dependency (the compiler version floor)
// and is KEPT — it is a real edge in the dependency graph, mirroring the
// LuaRocks `lua` interpreter-floor treatment (#5365).
//
// Dev/test classification: dependencies declared via `taskRequires` (the
// per-task requirement directive, used for `test`/`docs`/etc. tasks) are flagged
// is_dev=true so they are separable from runtime `requires`, matching the
// conanfile test_requires and rockspec test_dependencies treatment.
//
// There is no nimble lockfile format in wide use (nimble pins via the `requires`
// constraints + the global package list), so only manifest_parsing is provided —
// lockfile_parsing is N/A for nimble.
package manifest

import "regexp"

// ---------------------------------------------------------------------------
// Parser: *.nimble
// ---------------------------------------------------------------------------

// nimbleRequiresRE captures the argument tail of a `requires` directive in both
// its statement form (`requires "a", "b"`) and call form (`requires("a", "b")`).
// Group 1 is the (possibly multi-string, comma-separated) argument list, which
// nimbleDepStringRE then mines for individual quoted constraint strings. The
// directive must start at a statement boundary (start of line, after leading
// indentation) so a `requires` substring inside a comment/string body never
// opens a match.
var nimbleRequiresRE = regexp.MustCompile(
	`(?m)^[ \t]*requires\b\s*\(?\s*((?:"[^"\n\r]*"\s*,?\s*)+)`)

// nimbleTaskRequiresRE captures the requirement tail of a `taskRequires "task",
// "dep"…` directive (per-task / dev requirements). Group 1 is the argument list
// AFTER the leading task-name string, which nimbleDepStringRE mines. The leading
// task-name argument is intentionally skipped by anchoring on the first comma.
var nimbleTaskRequiresRE = regexp.MustCompile(
	`(?m)^[ \t]*taskRequires\b\s*\(?\s*"[^"\n\r]*"\s*,\s*((?:"[^"\n\r]*"\s*,?\s*)+)`)

// nimbleDepStringRE matches one quoted dependency constraint string and splits
// it into the package name (leading identifier) and the trailing version
// constraint. Examples it captures:
//
//	"nim >= 1.6.0"   → name=nim,    version=">= 1.6.0"
//	"jester >= 0.5"  → name=jester, version=">= 0.5"
//	"norm"           → name=norm,   version=""
//	"debby@#head"    → name=debby,  version="@#head"
//
// nimble package names are letters, digits, `_` and `-`. The version constraint
// is whatever follows the first run of whitespace (operators >= <= == > < ^= ~=,
// the `@` git-ref form, and the version literal), trimmed.
var nimbleDepStringRE = regexp.MustCompile(
	`"([A-Za-z0-9_][A-Za-z0-9_.-]*)\s*([^"]*?)"`)

// parseNimble parses a Nim `*.nimble` manifest and returns its declared
// dependencies. Runtime deps come from `requires`; per-task (dev/test) deps come
// from `taskRequires` and are flagged is_dev=true (kind "dev"). First
// declaration of a name wins on duplicates, with a runtime declaration taking
// precedence over a later dev declaration of the same name.
func parseNimble(source string) []dep {
	var out []dep
	seen := map[string]bool{}

	emit := func(argList string, isDev bool) {
		kind := "runtime"
		if isDev {
			kind = "dev"
		}
		for _, dm := range nimbleDepStringRE.FindAllStringSubmatch(argList, -1) {
			name := dm[1]
			version := dm[2]
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, dep{name: name, version: version, isDev: isDev, kind: kind})
		}
	}

	// Runtime requires first so a name shared with a taskRequires keeps its
	// runtime classification (first-declaration-wins on the `seen` guard).
	for _, m := range nimbleRequiresRE.FindAllStringSubmatch(source, -1) {
		emit(m[1], false)
	}
	for _, m := range nimbleTaskRequiresRE.FindAllStringSubmatch(source, -1) {
		emit(m[1], true)
	}
	return out
}
