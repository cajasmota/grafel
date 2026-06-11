// gomod.go — cached go.mod module-root reader for the Go extractor.
//
// The Go extractor stamps Properties["go_module_root"] on every IMPORTS edge
// so the resolver's in-tree import pass can strip the module prefix from an
// import path like "github.com/cajasmota/archigraph/internal/types" and
// derive the package directory "internal/types". Without this stamp the
// resolver has no way to distinguish an in-tree import (which should resolve
// to a file entity) from an external import (which resolves to an ext: node).
//
// Reading go.mod on every file extraction would be wasteful. This package
// caches the result per repo-root so the I/O cost is paid once per repo per
// process lifetime. The cache is populated lazily on first access and never
// invalidated — archigraph daemon instances are short-lived enough that a
// go.mod change always triggers a full re-index.
//
// When RepoRoot is empty or go.mod is absent/unreadable the reader returns ""
// and the stamp is silently skipped; in-tree imports are left unresolved (the
// pre-fix behaviour).
package golang

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	goModCacheMu sync.RWMutex
	goModCache   = make(map[string]string) // repoRoot → module name (or "")

	goModReplaceCacheMu sync.RWMutex
	// repoRoot → ordered list of local-path replace directives. Cached
	// alongside goModCache; populated lazily on first access.
	goModReplaceCache = make(map[string][]goReplace)
)

// goReplace captures a single go.mod `replace` directive that points at a
// LOCAL filesystem path (`=> ./internal/x`, `=> ../sibling`). Network
// replacements (`=> example.com/fork v1.2.3`) are NOT recorded — they
// resolve to a different external module, not an in-tree directory, so
// they keep their external disposition. LocalDir is repo-relative,
// slash-normalised, and may be "" when the replacement targets the repo
// root itself (`=> .`).
type goReplace struct {
	OldPath  string // the module path being replaced, e.g. "example.com/x"
	LocalDir string // repo-relative dir of the replacement, e.g. "internal/x"
}

// goModuleRoot returns the Go module name declared in <repoRoot>/go.mod.
// Returns "" when repoRoot is empty, go.mod is absent, or the module line
// cannot be parsed. Results are cached per repoRoot.
func goModuleRoot(repoRoot string) string {
	if repoRoot == "" {
		return ""
	}

	goModCacheMu.RLock()
	if name, ok := goModCache[repoRoot]; ok {
		goModCacheMu.RUnlock()
		return name
	}
	goModCacheMu.RUnlock()

	// Parse go.mod; only the first "module <name>" line matters.
	name := parseGoModModule(filepath.Join(repoRoot, "go.mod"))

	goModCacheMu.Lock()
	goModCache[repoRoot] = name
	goModCacheMu.Unlock()

	return name
}

// goModuleReplaces returns the local-path `replace` directives declared in
// <repoRoot>/go.mod, with each replacement's local path normalised to a
// repo-relative, slash-separated directory. Network/version replacements
// (those whose right-hand side is not a `./` or `../` filesystem path) are
// excluded — they redirect to a different external module and must keep
// their external disposition. Results are cached per repoRoot.
//
// Honoring these is the Go half of #4705: an `import "example.com/x"`
// backed by `replace example.com/x => ./internal/x` is an INTERNAL import
// (its source lives under internal/x) and must resolve to the local file
// entity BEFORE falling through to external_package.
func goModuleReplaces(repoRoot string) []goReplace {
	if repoRoot == "" {
		return nil
	}

	goModReplaceCacheMu.RLock()
	if reps, ok := goModReplaceCache[repoRoot]; ok {
		goModReplaceCacheMu.RUnlock()
		return reps
	}
	goModReplaceCacheMu.RUnlock()

	reps := parseGoModReplaces(filepath.Join(repoRoot, "go.mod"))

	goModReplaceCacheMu.Lock()
	goModReplaceCache[repoRoot] = reps
	goModReplaceCacheMu.Unlock()

	return reps
}

// goReplacePkgDir maps an import path to the repo-relative package
// directory implied by a local-path `replace` directive, or returns
// ("", false) when no replacement applies. An import path resolves through
// a replacement when it equals the replaced module path exactly OR is a
// sub-package of it (`old/sub` → `<localDir>/sub`). The longest matching
// OldPath wins so nested replacements stay deterministic.
func goReplacePkgDir(importPath string, replaces []goReplace) (string, bool) {
	best := goReplace{}
	bestLen := -1
	for _, rp := range replaces {
		if rp.OldPath == "" {
			continue
		}
		if importPath == rp.OldPath || strings.HasPrefix(importPath, rp.OldPath+"/") {
			if len(rp.OldPath) > bestLen {
				best = rp
				bestLen = len(rp.OldPath)
			}
		}
	}
	if bestLen < 0 {
		return "", false
	}
	suffix := strings.TrimPrefix(importPath, best.OldPath)
	suffix = strings.TrimPrefix(suffix, "/")
	switch {
	case best.LocalDir == "" && suffix == "":
		// `replace old => .` of the repo root itself: the package dir is
		// the repo root, which has no dotted form. Nothing to bind.
		return "", false
	case best.LocalDir == "":
		return suffix, true // `replace old => .` + sub-package
	case suffix == "":
		return best.LocalDir, true
	default:
		return best.LocalDir + "/" + suffix, true
	}
}

// parseGoModModule reads path and returns the module name from the first
// "module <name>" directive. Returns "" on any error.
func parseGoModModule(path string) string {
	f, err := os.Open(filepath.FromSlash(path))
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "module ") {
			continue
		}
		// "module github.com/owner/repo" — strip the keyword and any
		// trailing comment or whitespace.
		name := strings.TrimPrefix(line, "module ")
		if idx := strings.IndexByte(name, ' '); idx >= 0 {
			name = name[:idx] // drop inline comment
		}
		if idx := strings.IndexByte(name, '\t'); idx >= 0 {
			name = name[:idx]
		}
		name = strings.TrimSpace(name)
		if name != "" {
			return name
		}
	}
	return ""
}

// parseGoModReplaces reads path and returns every local-path `replace`
// directive. Both the single-line form
//
//	replace example.com/x => ./internal/x
//
// and the block form
//
//	replace (
//	    example.com/x => ./internal/x
//	    example.com/y v1.0.0 => ../y
//	)
//
// are supported. Only replacements whose right-hand side is a filesystem
// path (begins with "./", "../", "/", or is exactly ".") are recorded;
// version/network replacements are skipped. Returns nil on any read error.
func parseGoModReplaces(path string) []goReplace {
	f, err := os.Open(filepath.FromSlash(path))
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []goReplace
	inBlock := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := stripGoModComment(strings.TrimSpace(scanner.Text()))
		if line == "" {
			continue
		}
		switch {
		case inBlock && line == ")":
			inBlock = false
			continue
		case !inBlock && line == "replace (":
			inBlock = true
			continue
		case !inBlock && strings.HasPrefix(line, "replace "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "replace "))
		case inBlock:
			// directive line inside the block — fall through to parse.
		default:
			continue
		}
		if rp, ok := parseGoModReplaceDirective(line); ok {
			out = append(out, rp)
		}
	}
	return out
}

// parseGoModReplaceDirective parses a single `old [v..] => new [v..]`
// directive body (the `replace` keyword and any block indentation already
// stripped). It returns a goReplace only when the right-hand side is a
// LOCAL filesystem path; version/network replacements yield ok=false.
func parseGoModReplaceDirective(line string) (goReplace, bool) {
	arrow := strings.Index(line, "=>")
	if arrow < 0 {
		return goReplace{}, false
	}
	lhs := strings.Fields(strings.TrimSpace(line[:arrow]))
	rhs := strings.Fields(strings.TrimSpace(line[arrow+2:]))
	if len(lhs) == 0 || len(rhs) == 0 {
		return goReplace{}, false
	}
	oldPath := lhs[0]
	target := rhs[0]
	dir, ok := cleanGoLocalReplaceDir(target)
	if !ok {
		// Network/version replacement, or a path that escapes the indexed
		// tree (absolute, or a `../` sibling outside the repo). Either way
		// it cannot bind to an in-repo entity — keep external disposition.
		return goReplace{}, false
	}
	return goReplace{OldPath: oldPath, LocalDir: dir}, true
}

// cleanGoLocalReplaceDir normalises a local replace target into a
// repo-relative, slash-separated directory. Returns ok=false when the
// target is NOT an in-repo local path:
//   - module+version replacements (no leading "./", "../", "/", or ".");
//   - absolute paths and `../` escapes that leave the indexed tree.
//
// "." (the repo root) yields ("", true): a sub-package import still binds
// via the path suffix. A leading "./" is stripped and trailing slashes
// trimmed.
func cleanGoLocalReplaceDir(target string) (string, bool) {
	switch {
	case target == ".":
		return "", true
	case strings.HasPrefix(target, "/"):
		return "", false // absolute path — outside the indexed tree
	case target == ".." || strings.HasPrefix(target, "../"):
		return "", false // escapes the repo root
	case strings.HasPrefix(target, "./"):
		return strings.TrimRight(strings.TrimPrefix(target, "./"), "/"), true
	default:
		return "", false // module+version replacement — keep external
	}
}

// stripGoModComment removes a trailing `// ...` line comment from a go.mod
// line, preserving the directive body.
func stripGoModComment(line string) string {
	if idx := strings.Index(line, "//"); idx >= 0 {
		return strings.TrimSpace(line[:idx])
	}
	return line
}
