// Package javascript — issue #505 path-alias resolution.
//
// Real-world TypeScript / JavaScript projects rarely use the long
// `../../../../../config/query-client` form. Build-tool and editor
// configurations declare short aliases — `@/components/Button` resolving
// to `src/components/Button` — and every import in the codebase uses
// those instead. archigraph's JS extractor previously treated any
// non-relative spec as an external (npm) package, which left thousands of
// project-internal IMPORTS edges bound to a bare `@/...` string that no
// resolver index could match.
//
// This file reads the five config shapes that account for almost every
// alias map seen in the wild:
//
//	tsconfig.json    — compilerOptions.paths (Microsoft TS, Next.js,
//	                   Expo / RN — all use this); follows local extends
//	                   chains and scans subdirectory tsconfigs for monorepos
//	vite.config.{js,ts}  — resolve.alias (Vite-based web apps)
//	webpack.config.{js,ts} — resolve.alias (CRA, custom Webpack setups)
//	metro.config.{js,ts} — resolver.alias / resolver.extraNodeModules
//	                       (React Native / Expo Metro bundler)
//	babel.config.{js,ts} — `module-resolver` plugin alias map
//	                       (the most common RN alias source)
//
// Parsing strategy:
//
//   - tsconfig.json is JSON5-ish (it permits comments and trailing
//     commas), but the standard json package handles the typical
//     comment-free file shipped by Expo / Vite scaffolds. A
//     comment-strip pre-pass handles the rest.
//
//   - tsconfig `extends` chains: when the extends value is a local file
//     path (starts with "." or "/") or a relative bare name that resolves
//     to a local file, the parent tsconfig is loaded and its paths are
//     merged. npm-package extends values (e.g. "expo/tsconfig.base") are
//     silently skipped — we can't read node_modules at index time.
//
//   - Monorepo nested tsconfigs: LoadAliasMap also walks one level of
//     subdirectories (skipping hidden dirs and node_modules) collecting
//     any tsconfig.json / jsconfig.json found there. The per-subdirectory
//     entries are stored in a nested map keyed by the subdirectory name
//     (repo-relative). AliasMapForFile selects the nearest ancestor
//     tsconfig before falling back to the repo-root map, so files inside
//     `frontend/src/` pick up `frontend/tsconfig.json` aliases first.
//
//   - Vite / Metro / Babel / Webpack configs are JavaScript modules whose
//     alias map is declared as a literal object. A full JS evaluator is
//     out of scope; we extract the common shape with a regex pass:
//
//     '@/foo': '...'              → key='@/foo'
//     '@/foo': path.resolve(...)  → key='@/foo'
//     '@': path.resolve(__dirname, 'src')  → key='@'
//     '@components': './src/components'    → key='@components'
//
//     The value's right-hand side is normalised down to either a
//     literal string (`./src/components`) or, when it's a
//     `path.resolve(...)` / `path.join(...)` expression, the last
//     string literal argument. That covers the dominant patterns in
//     Vite, Metro, Babel module-resolver and Webpack setups.
//
// All maps are merged into a single per-repo AliasMap. When two
// configs disagree on the same alias the merge order is:
// tsconfig < vite < webpack < metro < babel — later wins (Babel
// module-resolver is the most authoritative source on RN, and Vite
// resolve.alias on web).
//
// The map is cached per repo root so the regex parsers run at most once
// per index run.
package javascript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// aliasEntry describes a single alias prefix→targets mapping. Patterns
// ending in `/*` are treated as glob-style prefixes; `targets` lists
// every directory the prefix may resolve to (tsconfig's paths array
// can declare multiple — e.g. `@/*: ["./*", "./src/*"]` on the
// client-fixture-c project). Exact aliases (`tailwind.config`) ignore the
// trailing-slash semantics and require an equality match.
type aliasEntry struct {
	// prefix is the alias key as it appears in source code, with the
	// trailing `/*` (if any) stripped. So `@/*` becomes `@`, `@components`
	// stays `@components`, and `tailwind.config` stays `tailwind.config`.
	prefix string
	// targets is the ordered list of directories or files the alias
	// resolves to, as repo-relative POSIX paths. Order matches the
	// declaration order in the source config — at substitution time we
	// expand into one IMPORTS edge per target so the resolver's
	// per-module reverse index can bind whichever target physically
	// holds the imported symbol. Leading `./` is stripped; absolute
	// paths outside the repo are skipped at parse time.
	targets []string
	// glob is true when the alias key carried a `/*` suffix and the
	// substitution should append the remainder of the import path after
	// the prefix. False for exact aliases that do not accept a tail.
	glob bool
}

// AliasMap is the per-repository merged alias table. Lookups are
// resolved by iterating entries longest-prefix first so `@/components`
// wins over `@` when both are declared.
type AliasMap struct {
	entries []aliasEntry
}

// Resolve returns the repo-relative POSIX path the import specifier
// substitutes to, or "" when no alias matches. The returned path has
// no leading `./` and uses forward slashes.
//
// Matching rules:
//
//   - Exact alias entry (`tailwind.config` → `tailwind.config.js`): the
//     spec must equal the prefix exactly.
//   - Glob alias entry (`@/*` → `src/`): the spec must start with the
//     prefix followed by `/`; the tail is appended to the target.
//   - Bare prefix without trailing slash (`@` → `src`): the spec must
//     equal the prefix OR start with `prefix + "/"`. We treat this as a
//     glob with an empty tail so `@` alone resolves to `src` and
//     `@/foo` resolves to `src/foo`.
//
// Resolve returns the most-specific repo-relative POSIX path the
// import spec substitutes to, or "" when no alias matches. For
// multi-target alias entries (tsconfig `paths` arrays) the LONGEST
// non-empty target is returned — `["./*", "./src/*"]` returns the
// `src/...` form. Callers that want every candidate should use
// ResolveAll directly.
func (m AliasMap) Resolve(spec string) string {
	all := m.ResolveAll(spec)
	if len(all) == 0 {
		return ""
	}
	best := all[0]
	for _, c := range all[1:] {
		if len(c) > len(best) {
			best = c
		}
	}
	return best
}

// ResolveAll returns every repo-relative POSIX path the import spec
// substitutes to under the merged alias table, in declaration order.
// Returns nil when no alias matches.
//
// Multiple targets are common for tsconfig path entries that declare
// fallback locations — e.g. `@/*: ["./*", "./src/*"]` lets the same
// `@/foo` import find a hit under either `foo` or `src/foo`. Returning
// every candidate lets the IMPORTS-edge emitter materialise one edge
// per target so the per-module reverse index can bind whichever
// candidate physically holds the symbol.
func (m AliasMap) ResolveAll(spec string) []string {
	if spec == "" || len(m.entries) == 0 {
		return nil
	}
	for _, e := range m.entries {
		if e.glob {
			if spec == e.prefix {
				return expandTargets(e.targets, "")
			}
			if strings.HasPrefix(spec, e.prefix+"/") {
				tail := strings.TrimPrefix(spec, e.prefix+"/")
				return expandTargets(e.targets, tail)
			}
			continue
		}
		// Exact entries match by equality only.
		if spec == e.prefix {
			return expandTargets(e.targets, "")
		}
	}
	return nil
}

// expandTargets concatenates every alias target with the post-prefix
// tail and runs each through cleanRepoRel. Empty targets and empty
// tails are handled per the glob substitution rules in aliasEntry.
func expandTargets(targets []string, tail string) []string {
	if len(targets) == 0 {
		return nil
	}
	out := make([]string, 0, len(targets))
	seen := make(map[string]bool, len(targets))
	for _, t := range targets {
		var combined string
		switch {
		case t == "" && tail == "":
			continue
		case t == "":
			combined = tail
		case tail == "":
			combined = t
		default:
			combined = t + "/" + tail
		}
		cleaned := cleanRepoRel(combined)
		if cleaned == "" || seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		out = append(out, cleaned)
	}
	return out
}

// cleanRepoRel normalises an alias target into a repo-relative POSIX
// path with no leading `./` and no leading `/`. Empty input passes
// through unchanged. A bare-root marker (`*` or `.`) collapses to "".
func cleanRepoRel(p string) string {
	if p == "" {
		return ""
	}
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	// Collapse `foo//bar` → `foo/bar`.
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	// Bare-root markers — `./*` (tsconfig glob "current dir") and `.`
	// (the resolved `./` minus the prefix) — represent "alias resolves
	// to the repo root with no directory prefix". Collapse to "" so
	// expandTargets concatenates only the post-prefix tail.
	if p == "*" || p == "." {
		return ""
	}
	return p
}

// LoadAliasMap discovers and parses every supported config file in
// repoRoot and returns the merged AliasMap for the repo root. Errors
// reading individual files are swallowed silently — alias resolution is
// a best-effort hint, and any miss falls back to the pre-#505 behaviour
// of treating the spec as external.
//
// repoRoot must be an absolute filesystem path. Empty input returns an
// empty map.
func LoadAliasMap(repoRoot string) AliasMap {
	if repoRoot == "" {
		return AliasMap{}
	}
	var entries []aliasEntry
	entries = append(entries, parseTsconfigPaths(repoRoot)...)
	entries = append(entries, parseViteAliases(repoRoot)...)
	entries = append(entries, parseWebpackAliases(repoRoot)...)
	entries = append(entries, parseMetroAliases(repoRoot)...)
	entries = append(entries, parseBabelAliases(repoRoot)...)
	// Sort by descending prefix length so longest match wins.
	sortByPrefixLen(entries)
	return AliasMap{entries: dedupAliasEntries(entries)}
}

// loadSubdirAliasMap parses configs found under a specific subdirectory
// of repoRoot. The subdirRel is the repo-relative path of the subdir
// (e.g. "frontend"). The returned AliasMap contains entries whose
// targets are expressed relative to the subdir (not the repo root),
// shifted back to repo-relative form.
func loadSubdirAliasMap(repoRoot, subdirRel string) AliasMap {
	absSubdir := filepath.Join(repoRoot, filepath.FromSlash(subdirRel))
	// Only load configs from this subdir, not recursively further.
	var entries []aliasEntry
	// For subdirectory tsconfigs, targets must be shifted to be
	// relative to repoRoot (prepend subdirRel/).
	raw := parseTsconfigPathsFromDir(absSubdir)
	for i := range raw {
		shifted := make([]string, len(raw[i].targets))
		for j, t := range raw[i].targets {
			if t == "" {
				shifted[j] = subdirRel
			} else {
				shifted[j] = cleanRepoRel(subdirRel + "/" + t)
			}
		}
		raw[i].targets = shifted
	}
	entries = append(entries, raw...)
	// Vite/Webpack/Metro/Babel in subdirs are treated like the root.
	entries = append(entries, parseViteAliases(absSubdir)...)
	entries = append(entries, parseWebpackAliases(absSubdir)...)
	entries = append(entries, parseMetroAliases(absSubdir)...)
	entries = append(entries, parseBabelAliases(absSubdir)...)
	sortByPrefixLen(entries)
	return AliasMap{entries: dedupAliasEntries(entries)}
}

// repoAliasState holds the root-level AliasMap and a subdirectory index
// for monorepo-aware nearest-tsconfig resolution.
type repoAliasState struct {
	root    AliasMap
	subdirs map[string]AliasMap // key: repo-relative subdir path (e.g. "frontend")
}

// aliasMapCache memoises LoadAliasMap by repo root so each indexing run
// pays the parse cost at most once per project. Concurrent
// goroutine-safe; the JS extractor's Extract is called in parallel.
var (
	aliasMapCache   = map[string]repoAliasState{}
	aliasMapCacheMu sync.RWMutex
)

// AliasMapFor returns the cached AliasMap for repoRoot, loading and
// caching it on first access. Empty repoRoot returns an empty map.
// For per-file nearest-tsconfig resolution use AliasMapForFile.
func AliasMapFor(repoRoot string) AliasMap {
	if repoRoot == "" {
		return AliasMap{}
	}
	state := loadRepoAliasState(repoRoot)
	return state.root
}

// AliasMapForFile returns the most specific AliasMap for filePath
// (repo-relative). It walks up from the file's directory toward the repo
// root looking for a cached subdirectory AliasMap, then falls back to the
// repo-root map. This enables monorepo packages with their own tsconfig.json
// to override root-level aliases.
func AliasMapForFile(repoRoot, filePath string) AliasMap {
	if repoRoot == "" {
		return AliasMap{}
	}
	state := loadRepoAliasState(repoRoot)
	// Walk from the file's directory up to the repo root (exclusive).
	dir := filepath.ToSlash(filepath.Dir(filePath))
	for dir != "" && dir != "." && dir != "/" {
		if m, ok := state.subdirs[dir]; ok {
			return m
		}
		parent := filepath.ToSlash(filepath.Dir(dir))
		if parent == dir {
			break
		}
		dir = parent
	}
	return state.root
}

// loadRepoAliasState loads (or returns cached) the full repoAliasState for
// repoRoot, including the root AliasMap and a subdirectory index.
func loadRepoAliasState(repoRoot string) repoAliasState {
	aliasMapCacheMu.RLock()
	s, ok := aliasMapCache[repoRoot]
	aliasMapCacheMu.RUnlock()
	if ok {
		return s
	}
	aliasMapCacheMu.Lock()
	defer aliasMapCacheMu.Unlock()
	if s, ok := aliasMapCache[repoRoot]; ok {
		return s
	}
	root := LoadAliasMap(repoRoot)
	subdirs := scanSubdirAliasMap(repoRoot)
	s = repoAliasState{root: root, subdirs: subdirs}
	aliasMapCache[repoRoot] = s
	return s
}

// scanSubdirAliasMap walks one level of subdirectories under repoRoot
// and builds a map from repo-relative subdir → AliasMap for each subdir
// that contains a tsconfig or other config file. Hidden directories and
// node_modules are skipped.
func scanSubdirAliasMap(repoRoot string) map[string]AliasMap {
	out := map[string]AliasMap{}
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "" || name[0] == '.' || name == "node_modules" || name == "dist" || name == "build" || name == ".next" || name == "coverage" {
			continue
		}
		m := loadSubdirAliasMap(repoRoot, name)
		if len(m.entries) > 0 {
			out[name] = m
		}
	}
	return out
}

// resetAliasMapCache clears the per-repo cache. Test-only helper.
func resetAliasMapCache() {
	aliasMapCacheMu.Lock()
	defer aliasMapCacheMu.Unlock()
	aliasMapCache = map[string]repoAliasState{}
}

// dedupAliasEntries removes duplicates produced by overlapping config
// sources. The first occurrence wins (sortByPrefixLen has already put
// the longest-prefix entries first; the later-wins merge order is
// honoured by appending sources in tsconfig→vite→metro→babel order in
// LoadAliasMap, which means babel duplicates land before vite ones for
// equal prefix length — but a stable sort preserves insertion order,
// so the most-authoritative source ends up first after the sort.
func dedupAliasEntries(in []aliasEntry) []aliasEntry {
	if len(in) <= 1 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, e := range in {
		key := e.prefix + "\x00" + boolKey(e.glob)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, e)
	}
	return out
}

func boolKey(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// sortByPrefixLen sorts entries in-place by descending prefix length
// using a stable sort so equal-length entries keep their insertion
// order (which encodes the source-precedence merge rule above).
func sortByPrefixLen(in []aliasEntry) {
	// Simple insertion sort — alias tables are tiny (typically <50
	// entries), and the stable property is easier to guarantee here than
	// via sort.SliceStable + a lt closure.
	for i := 1; i < len(in); i++ {
		j := i
		for j > 0 && len(in[j-1].prefix) < len(in[j].prefix) {
			in[j-1], in[j] = in[j], in[j-1]
			j--
		}
	}
}

// ---------------------------------------------------------------------
// tsconfig.json parsing
// ---------------------------------------------------------------------

// parseTsconfigPaths reads <repoRoot>/tsconfig.json (or jsconfig.json)
// and returns alias entries derived from compilerOptions.paths. Returns
// nil on any IO/parse failure. Follows local `extends` chains up to
// maxTsconfigExtendsDepth levels deep.
//
// Shape:
//
//	{
//	  "compilerOptions": {
//	    "baseUrl": ".",
//	    "paths": {
//	      "@/*": ["./*", "./src/*"],
//	      "tailwind.config": ["./tailwind.config.js"]
//	    }
//	  }
//	}
//
// Each ts-paths entry can have multiple targets; we keep the FIRST one
// (the canonical TypeScript path-resolver behaviour). The map is
// returned as alias entries with prefix and target derived per the
// `*`-suffix rules described on aliasEntry.
func parseTsconfigPaths(repoRoot string) []aliasEntry {
	return parseTsconfigPathsFromDir(repoRoot)
}

// parseTsconfigPathsFromDir reads tsconfig.json / jsconfig.json from
// configDir and returns alias entries. configDir is the directory
// where the tsconfig resides (used as the base for relative extends
// and target resolution).
func parseTsconfigPathsFromDir(configDir string) []aliasEntry {
	for _, name := range []string{"tsconfig.json", "jsconfig.json"} {
		configPath := filepath.Join(configDir, name)
		data, err := os.ReadFile(configPath)
		if err != nil {
			continue
		}
		entries := parseTsconfigPathsBytesWithDir(data, configDir, 0)
		if len(entries) > 0 {
			return entries
		}
	}
	return nil
}

// maxTsconfigExtendsDepth caps the number of parent tsconfig files we
// follow to prevent cycles and infinite recursion on pathological setups.
const maxTsconfigExtendsDepth = 5

// parseTsconfigPathsBytes parses the raw tsconfig.json bytes. Exposed
// for direct unit-testing without filesystem fixtures. Uses the repo
// root as the configDir (no extends resolution, as no filesystem path
// is available).
func parseTsconfigPathsBytes(data []byte) []aliasEntry {
	return parseTsconfigPathsBytesWithDir(data, "", 0)
}

// parseTsconfigPathsBytesWithDir parses raw tsconfig.json bytes with
// knowledge of configDir so that local extends paths and relative target
// paths can be resolved. depth guards against extends cycles.
func parseTsconfigPathsBytesWithDir(data []byte, configDir string, depth int) []aliasEntry {
	cleaned := stripJSONComments(data)
	var raw struct {
		Extends         string `json:"extends"`
		CompilerOptions struct {
			BaseURL string              `json:"baseUrl"`
			Paths   map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(cleaned, &raw); err != nil {
		return nil
	}

	baseURL := strings.TrimPrefix(strings.TrimPrefix(raw.CompilerOptions.BaseURL, "./"), "/")
	var out []aliasEntry

	// Child paths take precedence over parent (child is appended last;
	// sortByPrefixLen+dedupAliasEntries in LoadAliasMap will keep the first
	// occurrence at equal prefix length — so we append parent first).
	if raw.Extends != "" && depth < maxTsconfigExtendsDepth {
		out = append(out, followTsconfigExtends(raw.Extends, configDir, depth+1)...)
	}

	for key, targets := range raw.CompilerOptions.Paths {
		if len(targets) == 0 {
			continue
		}
		entry := tsPathEntry(key, targets, baseURL)
		if entry.prefix == "" {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// followTsconfigExtends resolves a tsconfig `extends` value and returns
// alias entries from the parent config. Only local-file references are
// followed; npm package extends (e.g. "expo/tsconfig.base") are silently
// ignored because we cannot read node_modules at index time.
//
// Local references: any value that starts with ".", "/", or resolves as
// a path relative to configDir that exists on disk.
func followTsconfigExtends(extendsVal, configDir string, depth int) []aliasEntry {
	if configDir == "" || depth > maxTsconfigExtendsDepth {
		return nil
	}
	// Skip npm-package references (no "." or "/" prefix, contains "/")
	// unless the path actually exists — e.g. "expo/tsconfig.base" won't.
	isLocal := strings.HasPrefix(extendsVal, ".") || strings.HasPrefix(extendsVal, "/")
	if !isLocal {
		// Attempt to treat as relative bare name (e.g. "tsconfig.base").
		candidate := filepath.Join(configDir, extendsVal)
		// Try with and without .json extension.
		if !fileExists(candidate) {
			candidate = candidate + ".json"
			if !fileExists(candidate) {
				// npm package or non-existent — skip.
				return nil
			}
		}
		data, err := os.ReadFile(candidate)
		if err != nil {
			return nil
		}
		return parseTsconfigPathsBytesWithDir(data, filepath.Dir(candidate), depth)
	}
	// Local path (starts with "." or "/").
	candidate := extendsVal
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(configDir, candidate)
	}
	if !fileExists(candidate) {
		candidate = candidate + ".json"
		if !fileExists(candidate) {
			return nil
		}
	}
	data, err := os.ReadFile(candidate)
	if err != nil {
		return nil
	}
	return parseTsconfigPathsBytesWithDir(data, filepath.Dir(candidate), depth)
}

// fileExists reports whether path exists as a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// tsPathEntry converts a single TypeScript paths declaration (key plus
// candidate-target list) into an aliasEntry. baseURL is applied as a
// directory prefix to each target (tsc resolves paths relative to
// baseUrl). All candidates are preserved so the substitution step
// emits one IMPORTS edge per target.
func tsPathEntry(key string, targets []string, baseURL string) aliasEntry {
	glob := strings.HasSuffix(key, "/*")
	prefix := strings.TrimSuffix(key, "/*")
	resolved := make([]string, 0, len(targets))
	for _, t := range targets {
		stripped := cleanRepoRel(strings.TrimSuffix(t, "/*"))
		if baseURL != "" && baseURL != "." && stripped != "" {
			stripped = cleanRepoRel(baseURL + "/" + stripped)
		}
		resolved = append(resolved, stripped)
	}
	return aliasEntry{
		prefix:  prefix,
		targets: resolved,
		glob:    glob,
	}
}

// stripJSONComments removes // and /* */ comments from a JSON document
// so the standard json package can parse a tsconfig-flavour file. The
// implementation is a single-pass scanner that respects string
// boundaries — comment markers inside strings are left intact.
func stripJSONComments(in []byte) []byte {
	out := make([]byte, 0, len(in))
	inString := false
	escape := false
	i := 0
	for i < len(in) {
		c := in[i]
		if inString {
			out = append(out, c)
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				inString = false
			}
			i++
			continue
		}
		if c == '"' {
			inString = true
			out = append(out, c)
			i++
			continue
		}
		if c == '/' && i+1 < len(in) {
			next := in[i+1]
			if next == '/' {
				// Line comment — skip until newline.
				i += 2
				for i < len(in) && in[i] != '\n' {
					i++
				}
				continue
			}
			if next == '*' {
				// Block comment — skip until closing */.
				i += 2
				for i+1 < len(in) {
					if in[i] == '*' && in[i+1] == '/' {
						i += 2
						break
					}
					i++
				}
				continue
			}
		}
		out = append(out, c)
		i++
	}
	return out
}

// ---------------------------------------------------------------------
// Vite / Metro / Babel parsing (regex-based extraction)
// ---------------------------------------------------------------------

// pathStringRe extracts the LAST single- or double-quoted string from a
// path.resolve / path.join argument list. The greedy match ensures the
// final string literal (the directory name) wins over earlier ones
// like `__dirname`-shaped arguments that may also be quoted.
var pathStringRe = regexp.MustCompile(`['"]([^'"]+)['"]\s*\)?\s*$`)

// parseViteAliases finds vite.config.{js,ts,mjs,cjs} in repoRoot and
// extracts the resolve.alias object. The function scans the file for a
// `resolve` block, then for an `alias` object literal inside it.
func parseViteAliases(repoRoot string) []aliasEntry {
	for _, name := range []string{"vite.config.ts", "vite.config.js", "vite.config.mjs", "vite.config.cjs"} {
		path := filepath.Join(repoRoot, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		entries := extractAliasBlock(data, "resolve", "alias")
		if len(entries) > 0 {
			return entries
		}
	}
	return nil
}

// parseWebpackAliases finds webpack.config.{js,ts,mjs,cjs} in repoRoot
// and extracts the resolve.alias object. Webpack is the bundler used by
// Create React App and many custom setups.
func parseWebpackAliases(repoRoot string) []aliasEntry {
	for _, name := range []string{"webpack.config.js", "webpack.config.ts", "webpack.config.mjs", "webpack.config.cjs"} {
		path := filepath.Join(repoRoot, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		entries := extractAliasBlock(data, "resolve", "alias")
		if len(entries) > 0 {
			return entries
		}
	}
	return nil
}

// parseMetroAliases finds metro.config.{js,ts} and extracts the
// resolver.alias / resolver.extraNodeModules object.
func parseMetroAliases(repoRoot string) []aliasEntry {
	for _, name := range []string{"metro.config.js", "metro.config.ts", "metro.config.mjs", "metro.config.cjs"} {
		path := filepath.Join(repoRoot, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		entries := extractAliasBlock(data, "resolver", "alias")
		entries = append(entries, extractAliasBlock(data, "resolver", "extraNodeModules")...)
		if len(entries) > 0 {
			return entries
		}
	}
	return nil
}

// parseBabelAliases finds babel.config.{js,ts} (and .babelrc-shaped
// fallbacks) and extracts the alias object from the `module-resolver`
// plugin configuration. Babel plugin configs are array literals of the
// form `['module-resolver', { alias: { ... } }]`, so we look for the
// plugin name first, then scan forward for the alias object literal.
func parseBabelAliases(repoRoot string) []aliasEntry {
	for _, name := range []string{"babel.config.js", "babel.config.ts", "babel.config.cjs", "babel.config.mjs", ".babelrc.js", ".babelrc.json", ".babelrc"} {
		path := filepath.Join(repoRoot, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		entries := extractBabelModuleResolverAliases(data)
		if len(entries) > 0 {
			return entries
		}
	}
	return nil
}

// extractAliasBlock locates an object literal at `<outer>.<inner>` and
// extracts the key/value pairs as alias entries. Both `outer` and
// `inner` are scanned non-strictly — the function does not require the
// outer block to be syntactically well-formed JavaScript, only that
// the inner object literal's first `{` appears after the inner key.
//
// Returns nil when the configured keys aren't present.
func extractAliasBlock(src []byte, outer, inner string) []aliasEntry {
	// Look for "outer:" anywhere in the source. We don't bother
	// distinguishing top-level vs nested — the alias map is unique
	// enough in practice. If outer is empty, scan from byte 0.
	body := string(src)
	start := 0
	if outer != "" {
		idx := indexOfKey(body, outer)
		if idx < 0 {
			return nil
		}
		start = idx
	}
	if inner == "" {
		return nil
	}
	innerIdx := indexOfKeyAfter(body, inner, start)
	if innerIdx < 0 {
		return nil
	}
	obj := extractObjectLiteral(body[innerIdx:])
	if obj == "" {
		return nil
	}
	return parseAliasObjectLiteral(obj)
}

// extractBabelModuleResolverAliases is the babel-specific variant. It
// finds the `module-resolver` plugin name, then looks for the next
// `alias` key after it.
func extractBabelModuleResolverAliases(src []byte) []aliasEntry {
	body := string(src)
	pluginIdx := strings.Index(body, "module-resolver")
	if pluginIdx < 0 {
		return nil
	}
	innerIdx := indexOfKeyAfter(body, "alias", pluginIdx)
	if innerIdx < 0 {
		return nil
	}
	obj := extractObjectLiteral(body[innerIdx:])
	if obj == "" {
		return nil
	}
	return parseAliasObjectLiteral(obj)
}

// indexOfKey returns the byte index of an unquoted key occurrence
// `<key>:` in body. Skips matches inside string literals.
func indexOfKey(body, key string) int {
	return indexOfKeyAfter(body, key, 0)
}

// indexOfKeyAfter is indexOfKey with a starting offset.
func indexOfKeyAfter(body, key string, after int) int {
	if after < 0 {
		after = 0
	}
	if after >= len(body) {
		return -1
	}
	needle := key
	for i := after; i+len(needle) < len(body); i++ {
		// Match either "key:" or "'key':" / "\"key\":" but require a
		// word boundary before the key so `resolve` doesn't match
		// `resolved` and `alias` doesn't match `aliasMap`.
		if !startsWith(body, i, needle) {
			continue
		}
		// Word boundary before.
		if i > 0 {
			prev := body[i-1]
			if isIdent(prev) || prev == '_' {
				continue
			}
		}
		// Find the next non-space character after the key — must be
		// `:` to count as an object key.
		j := i + len(needle)
		// Allow optional quote consumption when the key is quoted:
		// the loop above matched the unquoted key text; an immediately
		// preceding quote with a matching quote at position j is fine.
		for j < len(body) && (body[j] == ' ' || body[j] == '\t' || body[j] == '"' || body[j] == '\'') {
			j++
		}
		if j < len(body) && body[j] == ':' {
			return i
		}
	}
	return -1
}

func startsWith(s string, at int, prefix string) bool {
	if at+len(prefix) > len(s) {
		return false
	}
	return s[at:at+len(prefix)] == prefix
}

func isIdent(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '$'
}

// extractObjectLiteral returns the substring of body starting at the
// first `{` and ending at the matching `}`. Brace nesting is respected
// and braces inside string literals are ignored. Returns "" when the
// first `{` cannot be found or the source is truncated before the
// matching close.
func extractObjectLiteral(body string) string {
	start := strings.IndexByte(body, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	var quote byte
	escape := false
	for i := start; i < len(body); i++ {
		c := body[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == quote {
				inString = false
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			inString = true
			quote = c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return body[start : i+1]
			}
		}
	}
	return ""
}

// parseAliasObjectLiteral extracts every `key: value` pair from an
// object literal. Values are reduced to a literal string via
// extractAliasStringValue. Bare-identifier values and complex
// expressions we can't reduce are skipped.
//
// The splitter is paren-aware: a value like
// `path.resolve(__dirname, 'src')` contains a comma that a naive split
// would treat as a key separator. We walk the body character-by-character
// tracking string and paren depth, and only treat a comma at
// (paren=0, brace=0, bracket=0, no-string) as a pair terminator.
func parseAliasObjectLiteral(obj string) []aliasEntry {
	body := strings.TrimSpace(obj)
	body = strings.TrimPrefix(body, "{")
	body = strings.TrimSuffix(body, "}")
	pairs := splitTopLevelCommas(body)
	out := make([]aliasEntry, 0, len(pairs))
	for _, p := range pairs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		colon := findTopLevelColon(p)
		if colon < 0 {
			continue
		}
		keyRaw := strings.TrimSpace(p[:colon])
		valRaw := strings.TrimSpace(p[colon+1:])
		key := unquoteIdentOrString(keyRaw)
		if key == "" {
			continue
		}
		target := extractAliasStringValue(valRaw)
		if target == "" {
			continue
		}
		hadGlob := strings.HasSuffix(key, "/*")
		prefix := strings.TrimSuffix(key, "/*")
		if prefix == "" {
			continue
		}
		// Vite / Metro / Babel module-resolver aliases are
		// semantically prefix-matches even when written without an
		// explicit `/*` suffix — `'@': './src'` resolves
		// `@/components/Foo` to `./src/components/Foo`. The single
		// known exception is the `tailwind.config` style dotted-key
		// alias used by NativeWind / Tailwind tooling, which IS an
		// exact-only spec (a project named `tailwind.config.something`
		// would clash). We treat any key containing a `.` as exact,
		// matching how those keys appear only in module-id form, and
		// every other key as a glob.
		glob := hadGlob || !strings.Contains(prefix, ".")
		out = append(out, aliasEntry{
			prefix:  prefix,
			targets: []string{cleanRepoRel(strings.TrimSuffix(target, "/*"))},
			glob:    glob,
		})
	}
	return out
}

// splitTopLevelCommas splits body on commas that are NOT inside a
// string literal or bracketed expression. Brackets tracked:
// `(` `)`, `{` `}`, `[` `]`. String literals: `'`, `"`, “ ` “.
func splitTopLevelCommas(body string) []string {
	var out []string
	depthParen, depthBrace, depthBracket := 0, 0, 0
	inString := false
	var quote byte
	escape := false
	last := 0
	for i := 0; i < len(body); i++ {
		c := body[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == quote {
				inString = false
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			inString = true
			quote = c
		case '(':
			depthParen++
		case ')':
			depthParen--
		case '{':
			depthBrace++
		case '}':
			depthBrace--
		case '[':
			depthBracket++
		case ']':
			depthBracket--
		case ',':
			if depthParen == 0 && depthBrace == 0 && depthBracket == 0 {
				out = append(out, body[last:i])
				last = i + 1
			}
		}
	}
	if last < len(body) {
		out = append(out, body[last:])
	}
	return out
}

// findTopLevelColon returns the index of the first `:` outside of any
// string literal or bracketed expression in s, or -1 when no such
// colon exists.
func findTopLevelColon(s string) int {
	depthParen, depthBrace, depthBracket := 0, 0, 0
	inString := false
	var quote byte
	escape := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == quote {
				inString = false
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			inString = true
			quote = c
		case '(':
			depthParen++
		case ')':
			depthParen--
		case '{':
			depthBrace++
		case '}':
			depthBrace--
		case '[':
			depthBracket++
		case ']':
			depthBracket--
		case ':':
			if depthParen == 0 && depthBrace == 0 && depthBracket == 0 {
				return i
			}
		}
	}
	return -1
}

// unquoteIdentOrString returns the unquoted form of a key token. JS
// object keys may be bare identifiers (`foo:`), single-quoted, or
// double-quoted; tsconfig/JSON keys are always double-quoted. Empty
// input or unrecognised shape returns "".
func unquoteIdentOrString(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '\'' || first == '"' || first == '`') && first == last {
			return s[1 : len(s)-1]
		}
	}
	// Bare identifier — accept if it parses as one, including dots and
	// hyphens that JS object keys allow when unquoted... actually JS
	// forbids those unquoted, but be permissive: if no whitespace, take it.
	if !strings.ContainsAny(s, " \t\n") {
		return s
	}
	return ""
}

// extractAliasStringValue reduces an alias-value RHS to a literal
// string. Handles:
//
//	'./src'                          → "./src"
//	"src"                            → "src"
//	path.resolve(__dirname, 'src')   → "src"
//	path.join(__dirname, './src')    → "./src"
//	require.resolve('react')         → "" (we skip these — they
//	                                       describe node_modules, not
//	                                       project paths)
//
// Anything not matching one of the above shapes returns "".
func extractAliasStringValue(val string) string {
	val = strings.TrimSpace(val)
	if val == "" {
		return ""
	}
	// Skip require.resolve — those describe node_modules.
	if strings.HasPrefix(val, "require.resolve") {
		return ""
	}
	// Bare literal: 'foo' or "foo".
	if len(val) >= 2 {
		first, last := val[0], val[len(val)-1]
		if (first == '\'' || first == '"' || first == '`') && first == last {
			return val[1 : len(val)-1]
		}
	}
	// path.resolve / path.join — pull the last string literal.
	if strings.HasPrefix(val, "path.resolve") || strings.HasPrefix(val, "path.join") ||
		strings.HasPrefix(val, "resolve(") || strings.HasPrefix(val, "join(") {
		if m := pathStringRe.FindStringSubmatch(val); len(m) >= 2 {
			return m[1]
		}
	}
	return ""
}
