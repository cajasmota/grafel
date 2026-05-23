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
)

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
