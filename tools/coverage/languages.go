// languages.go surfaces the canonical list of languages archigraph has
// extractor support for, derived from internal/extractors/<lang>/ directories.
// This list is the source of truth for the coverage summary's pivot rows —
// languages with zero ecosystem records still appear so the matrix reflects
// extractor coverage, not just registry-record coverage.
//
// Standalone scope: stdlib only. No imports from internal/ packages — we
// only inspect directory names and the well-known utility/format excludes.
package main

import (
	"os"
	"path/filepath"
	"sort"
)

// extractorUtilityDirs are subdirectories of internal/extractors/ that
// implement shared utilities (not per-language extractors) and must be
// excluded from the supported-languages list.
var extractorUtilityDirs = map[string]bool{
	"complexity": true,
	"config":     true,
	"cross":      true,
	"references": true,
	"sresolver":  true,
}

// extractorNonLanguageFormats are subdirectories of internal/extractors/
// that extract non-language formats (build files, config, markup) and are
// represented in the coverage matrix's "multi" bucket rather than as
// standalone language rows.
var extractorNonLanguageFormats = map[string]bool{
	"bazel":      true,
	"css":        true,
	"dockerfile": true,
	"fish":       true,
	"graphql":    true,
	"hcl":        true,
	"html":       true,
	"just":       true,
	"markdown":   true,
	"proto":      true,
	"razor":      true,
	"shell":      true,
	"sql":        true,
	"yaml":       true,
}

// extractorDirAliases maps extractor directory names to the canonical
// language slug used by the registry. JavaScript and TypeScript collapse
// to "jsts" because the registry tags both under a single slug; "golang"
// is the extractor dirname but the registry uses "go"; "cpp" expands to
// "c-cpp" because archigraph's C/C++ extractor is shared across .c and
// .cpp sources (mirrors the JS/TS collapse — see #2732).
//
// Vue (.vue), Svelte (.svelte) and Astro (.astro) are JS/TS frameworks
// with custom single-file-component formats, NOT standalone languages —
// the same class as React, which #2729 collapsed into jsts. They have
// dedicated extractor directories (the SFC crackers that hand <script>
// bodies to the JS/TS pipeline) and their own runtime dispatch language
// tokens, but on the *coverage* axis they belong under jsts as frameworks
// (their registry records already live at lang.jsts.framework.{vue,
// svelte,astro}). Collapsing them here keeps them out of the by-language
// pivot and stops empty by-language/{vue,svelte,astro}.md pages being
// emitted, while leaving the runtime extraction path untouched (#2821).
var extractorDirAliases = map[string]string{
	"javascript": "jsts",
	"typescript": "jsts",
	"golang":     "go",
	"cpp":        "c-cpp",
	"vue":        "jsts",
	"svelte":     "jsts",
	"astro":      "jsts",
}

// languageDisplayOverrides maps a canonical language slug to its human
// label when the default (title-cased slug) is unsuitable. Slugs not
// listed here render via titleCase(slug).
var languageDisplayOverrides = map[string]string{
	"jsts":     "JS/TS",
	"csharp":   "C#",
	"c-cpp":    "C/C++",
	"fsharp":   "F#",
	"reasonml": "ReasonML",
	"rescript": "ReScript",
	"sml":      "Standard ML",
	"ocaml":    "OCaml",
	"vhdl":     "VHDL",
	"cobol":    "COBOL",
}

// SupportedLanguages returns the canonical, sorted, deduplicated list of
// language slugs archigraph has extractor support for. The source of
// truth is internal/extractors/<lang>/ directories under repoRoot;
// utility-only directories and non-language format extractors are
// excluded, and aliases are applied so the slugs align with the
// registry's tagging conventions.
//
// Returns an empty slice (never nil) when repoRoot does not contain an
// internal/extractors/ directory — keeps callers free of nil checks.
func SupportedLanguages(repoRoot string) []string {
	root := filepath.Join(repoRoot, "internal", "extractors")
	entries, err := os.ReadDir(root)
	if err != nil {
		return []string{}
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if extractorUtilityDirs[name] {
			continue
		}
		if extractorNonLanguageFormats[name] {
			continue
		}
		slug := name
		if a, ok := extractorDirAliases[name]; ok {
			slug = a
		}
		seen[slug] = true
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// languageDisplayName returns the human-facing label for a language
// slug. The display table is small and stable; slugs without an entry
// fall back to title-casing the slug. Used by both the summary pivot
// table and the placeholder by-language pages.
func languageDisplayName(slug string) string {
	if v, ok := languageDisplayOverrides[slug]; ok {
		return v
	}
	if slug == "" {
		return ""
	}
	return titleCase(slug)
}

// extractorDirForSlug returns the primary internal/extractors/<dir>/
// directory that backs a canonical language slug. Used by the placeholder
// page template to cite a concrete on-disk location. When multiple
// extractor directories alias to the same slug (e.g. javascript + typescript
// both map to "jsts"), this returns the canonical primary; for slugs with
// no alias the slug itself names the directory.
func extractorDirForSlug(slug string) string {
	switch slug {
	case "jsts":
		return "javascript"
	case "go":
		return "golang"
	case "c-cpp":
		return "cpp"
	}
	return slug
}

// titleCase upper-cases the first rune of slug and leaves the rest as-is.
// Slugs are already lowercase ASCII tokens by convention, so this is a
// purely cosmetic transform — not a Unicode-aware title-case operation.
func titleCase(slug string) string {
	if slug == "" {
		return ""
	}
	first := slug[0]
	if first >= 'a' && first <= 'z' {
		return string(first-('a'-'A')) + slug[1:]
	}
	return slug
}
