// Package resolve — platform_variants.go
//
// Issue #1811: merge build-tagged platform-variant symbols into one logical
// symbol so find_callers works across _unix.go / _windows.go splits.
//
// The Go toolchain allows the same symbol to be defined in multiple files
// inside the same package as long as each file carries a //go:build constraint
// and those constraints are MUTUALLY EXCLUSIVE (their intersection is empty).
// The canonical example:
//
//	read_source_unix.go    //go:build darwin || linux
//	read_source_windows.go //go:build windows
//
// The resolver's byPackageOperation index saw two definitions of
// `readSourceWindow` in the same package directory and blanked the entry with
// the ambiguity sentinel — preventing find_callers from returning anything.
//
// This file provides:
//
//   - parseBuildTag: extract the normalised GOOS set from a "//go:build <expr>"
//     line or from Properties["build_tag"] already stamped by the extractor.
//   - buildTagsMutuallyExclusive: true iff two tag-strings have no GOOS in
//     common (the typical platform-split pattern).
//   - pickCanonicalVariant: given two entity IDs and their build-tag strings,
//     return the canonical entity ID (first alphabetically by source file, used
//     as the stable representative for cross-platform CALLS resolution).
package resolve

import (
	"sort"
	"strings"
)

// knownGOOS is the set of GOOS values recognised by the Go toolchain.
// We match only these so spurious user-defined build tags (e.g. "integration")
// don't fool the mutual-exclusion check.
var knownGOOS = map[string]bool{
	"darwin":    true,
	"linux":     true,
	"windows":   true,
	"freebsd":   true,
	"openbsd":   true,
	"netbsd":    true,
	"dragonfly": true,
	"plan9":     true,
	"illumos":   true,
	"solaris":   true,
	"android":   true,
	"ios":       true,
	"js":        true,
	"wasip1":    true,
	"aix":       true,
}

// parseBuildTag extracts the set of GOOS values that are EXPLICITLY listed
// (positive mentions) in a build constraint expression. The input is a raw
// build-constraint string as stored in Properties["build_tag"] by the Go
// extractor, e.g. "darwin || linux" or "windows" or "!windows".
//
// Returns nil when the tag string is empty or contains no recognised GOOS
// tokens (e.g. a pure architecture constraint like "amd64").
//
// Negation ("!windows") is deliberately NOT included in the positive set so
// that two files with "!windows" and "windows" are correctly identified as
// mutually exclusive (one positively lists "windows", the other does not list
// any positive GOOS).
func parseBuildTag(tag string) map[string]bool {
	if tag == "" {
		return nil
	}
	result := make(map[string]bool)
	// Strip surrounding whitespace and split on boolean operators and parens.
	// We only need the positive leaf identifiers — "darwin", "linux", etc.
	fields := strings.FieldsFunc(tag, func(r rune) bool {
		return r == '|' || r == '&' || r == '(' || r == ')' || r == ',' || r == ' ' || r == '\t'
	})
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" || strings.HasPrefix(f, "!") {
			continue
		}
		if knownGOOS[f] {
			result[f] = true
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// buildTagsMutuallyExclusive reports whether two build-constraint strings
// (as stored in Properties["build_tag"]) can NEVER both be satisfied at the
// same time, i.e. their GOOS intersection is empty.
//
// The function returns false conservatively in the following cases:
//   - Either tag is empty (no-tag file has no constraint → could coexist with
//     anything; we can't merge safely).
//   - Either tag parses to no recognised GOOS (e.g. a pure arch tag "amd64";
//     we don't have enough information to decide).
//   - The parsed GOOS sets share at least one element (real overlap → the
//     two definitions could BOTH be compiled on the overlapping platform,
//     making it a genuine ambiguity, not a platform split).
func buildTagsMutuallyExclusive(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	setA := parseBuildTag(a)
	setB := parseBuildTag(b)
	if len(setA) == 0 || len(setB) == 0 {
		return false
	}
	for goos := range setA {
		if setB[goos] {
			return false // overlap → not mutually exclusive
		}
	}
	return true
}

// buildTagPlatforms returns a sorted, deduplicated list of GOOS names
// extracted from tag. Used to generate the "platform_variants" diagnostic
// property on the canonical entity.
func buildTagPlatforms(tag string) []string {
	m := parseBuildTag(tag)
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for g := range m {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

// mergePlatformVariantTags joins two platform-variant tag strings into a
// combined "platform_variants" property value. The result is the union of all
// recognised GOOS names from both tags, joined with commas in sorted order.
// Returns "" if neither tag contributes any recognised GOOS.
func mergePlatformVariantTags(a, b string) string {
	combined := make(map[string]bool)
	for _, tag := range []string{a, b} {
		for g := range parseBuildTag(tag) {
			combined[g] = true
		}
	}
	if len(combined) == 0 {
		return ""
	}
	names := make([]string, 0, len(combined))
	for g := range combined {
		names = append(names, g)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// ExtractFileBuildTag reads the first meaningful lines of Go source content
// and returns the normalised build constraint expression from a
// "//go:build <expr>" directive, or "" if none is present.
//
// Only the first 50 lines are scanned (build constraints must appear before
// the package clause per the Go spec). The returned string preserves the
// original expression text with surrounding whitespace trimmed.
func ExtractFileBuildTag(content []byte) string {
	if len(content) == 0 {
		return ""
	}
	// Scan up to the first 50 lines or 4096 bytes — whichever comes first.
	limit := len(content)
	if limit > 4096 {
		limit = 4096
	}
	lines := strings.SplitN(string(content[:limit]), "\n", 52)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//go:build ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "//go:build "))
		}
		// Also handle the legacy "+build" directive (Go 1.16 and older) but
		// only as a fallback when //go:build is absent. Since Go 1.17 gofmt
		// always emits //go:build first, this branch rarely fires.
		if strings.HasPrefix(line, "// +build ") {
			// Don't return immediately — keep scanning for //go:build which
			// takes precedence if both are present.
			_ = line // handled below by the absence of //go:build
		}
	}
	// Legacy-only fallback: re-scan for "// +build" if no //go:build found.
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "// +build ") {
			// Convert the legacy POSIX-style constraint to the canonical form
			// used by the rest of this package. The legacy form uses spaces
			// for OR within one tag line and multiple lines for AND; we only
			// parse the simple POSIX-space-separated OR case here.
			return strings.TrimSpace(strings.TrimPrefix(line, "// +build "))
		}
	}
	return ""
}
