package resolve

import "regexp"

// astroDynamicPatterns are per-language patterns for Astro (.astro) files.
// Registered via init() into dynamicPatternsByLang.
//
// Three groups of callee shapes appear as unresolvable stubs without this catalog:
//
//  1. Astro global object accessors — Astro.url, Astro.params, Astro.props,
//     Astro.request, Astro.cookies, Astro.redirect, Astro.glob, Astro.site,
//     Astro.generator, Astro.self, Astro.slots, Astro.locals.
//     These are injected by the Astro runtime; they are never graph entities.
//
//  2. Content collection helpers — getCollection, getEntry, getEntries,
//     defineCollection, reference.  Imported from "astro:content".
//
//  3. View transition directives — transition:name, transition:animate,
//     transition:persist. These appear as template attributes, not callable
//     functions; resolving them prevents false CALLS stubs.
//
//  4. Astro.fetchContent (deprecated v1 API) and Image/Picture built-ins
//     from "astro:assets".
//
// All patterns are gated to lang=="astro" to prevent collisions with other
// languages.
var astroDynamicPatterns = []*regexp.Regexp{
	// ── Astro global accessors ────────────────────────────────────────────
	regexp.MustCompile(`^Astro\.url$`),
	regexp.MustCompile(`^Astro\.params$`),
	regexp.MustCompile(`^Astro\.props$`),
	regexp.MustCompile(`^Astro\.request$`),
	regexp.MustCompile(`^Astro\.cookies$`),
	regexp.MustCompile(`^Astro\.redirect$`),
	regexp.MustCompile(`^Astro\.glob$`),
	regexp.MustCompile(`^Astro\.site$`),
	regexp.MustCompile(`^Astro\.generator$`),
	regexp.MustCompile(`^Astro\.self$`),
	regexp.MustCompile(`^Astro\.slots$`),
	regexp.MustCompile(`^Astro\.locals$`),
	// Catch-all for any remaining Astro.* property access.
	regexp.MustCompile(`^Astro\.[A-Za-z][A-Za-z0-9_]*$`),

	// ── Content collection helpers ────────────────────────────────────────
	// Imported from "astro:content".
	regexp.MustCompile(`^getCollection$`),
	regexp.MustCompile(`^getEntry$`),
	regexp.MustCompile(`^getEntries$`),
	regexp.MustCompile(`^defineCollection$`),
	regexp.MustCompile(`^reference$`),
	regexp.MustCompile(`^z$`), // zod re-export from astro:content

	// ── View transition directives ────────────────────────────────────────
	// Template attributes treated as dynamic call targets by the resolver.
	regexp.MustCompile(`^transition:name$`),
	regexp.MustCompile(`^transition:animate$`),
	regexp.MustCompile(`^transition:persist$`),

	// ── Astro assets ──────────────────────────────────────────────────────
	// Imported from "astro:assets".
	regexp.MustCompile(`^Image$`),
	regexp.MustCompile(`^Picture$`),
	regexp.MustCompile(`^getImage$`),

	// ── Deprecated v1 API ─────────────────────────────────────────────────
	regexp.MustCompile(`^Astro\.fetchContent$`),

	// ── Astro middleware ──────────────────────────────────────────────────
	// Imported from "astro:middleware".
	regexp.MustCompile(`^defineMiddleware$`),
	regexp.MustCompile(`^sequence$`),
}

func init() {
	dynamicPatternsByLang["astro"] = astroDynamicPatterns
}
