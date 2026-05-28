// Markup-with-script taint-sites dispatcher (#2778 Phase 2B T3).
//
// Svelte (.svelte), Vue (.vue), and Astro (.astro) single-file components
// embed JS/TS inside `<script>` blocks. The taint primitives inside those
// blocks are identical to plain JS/TS, so we extract every `<script>...</script>`
// body and run sniffTaintJSTS over the concatenation, adjusting line offsets.
//
// In addition to the generic JS/TS primitives, these frameworks have two
// specific taint patterns worth calling out:
//
//   - SSRF via SSR fetch: an outbound `fetch(userUrl)` where the URL is
//     derived from a query param / prop / route param. This is particularly
//     risky in Vue/Nuxt SSR, SvelteKit server routes, and Astro API routes
//     because the fetch runs server-side.
//     Matched by: fetch with a non-literal URL argument inside a <script> block.
//
//   - XSS via v-html / Svelte @html / Astro set:html: setting innerHTML
//     equivalent with user content. The framework-specific directives are
//     matched in the markup (outside <script>) since they appear in the
//     template section.
//
// DOMPurify and framework-built-in escaping (Svelte auto-escapes text nodes,
// Vue auto-escapes template interpolation) are recognised as sanitizers.
//
// Sanitizers recognised:
//   - DOMPurify.sanitize (already in sniffTaintJSTS)
//   - Vue: v-html is marked as a sink; plain {{ expr }} auto-escaping is
//     recognised as a sanitizer when it replaces a v-html pattern
//   - Svelte: {@html expr} is a sink; plain {expr} auto-escaping is implicit
//   - Astro: set:html is a sink; plain {expr} injection is auto-escaped
package substrate

import "regexp"

func init() {
	RegisterTaintSniffer("svelte", sniffTaintMarkupScript)
	RegisterTaintSniffer("vue", sniffTaintMarkupScript)
	RegisterTaintSniffer("astro", sniffTaintMarkupScript)
}

// taintScriptBlockRe matches `<script ...>` ... `</script>` blocks.
// Capture group 1 is the inner script body (same as effectScriptBlockRe).
var taintScriptBlockRe = regexp.MustCompile(
	`(?si)<script\b[^>]*>(.*?)</script>`,
)

// markupSinkVHtmlRe matches Vue `v-html="expr"` / Svelte `{@html expr}` /
// Astro `set:html={expr}` in the template section — all bypass auto-escaping.
var markupSinkVHtmlRe = regexp.MustCompile(
	`\bv-html\s*=\s*["'][A-Za-z_$][\w$]*["']` +
		`|\bv-html\s*=\s*"[^"]*"` +
		`|\{@html\s+[A-Za-z_$][\w$]*\s*\}` +
		`|\bset:html\s*=\s*\{[A-Za-z_$][\w$]*\s*\}`,
)

// markupSinkSSRFetchRe matches `fetch(varName)` where the URL is a
// non-literal identifier — potential SSRF in SSR render paths.
var markupSinkSSRFetchRe = regexp.MustCompile(
	`\bfetch\s*\(\s*[A-Za-z_$][\w$]*\s*[,)]`,
)

// markupSourcePropsParamsRe matches framework-idiomatic prop / route-param /
// query-param access patterns:
//   - Vue: `route.query.<name>` / `route.params.<name>` / `useRoute().query`
//   - Svelte / SvelteKit: `$page.params.<name>` / `$page.url.searchParams` /
//     `data.params`
//   - Astro: `Astro.request.url` / `Astro.params` / `Astro.url.searchParams`
var markupSourcePropsParamsRe = regexp.MustCompile(
	`\broute\s*\.\s*(?:query|params)\b` +
		`|\buseRoute\s*\(\s*\)\s*\.\s*(?:query|params)\b` +
		`|\$page\s*\.\s*(?:params|url\s*\.\s*searchParams)\b` +
		`|\bAstro\s*\.\s*(?:params|request\s*\.\s*url|url\s*\.\s*searchParams)\b`,
)

// markupSanitizerDOMPurifyRe matches DOMPurify.sanitize (also caught by
// sniffTaintJSTS but we re-match here to ensure it's attributed correctly
// when it appears outside a script block, e.g. in an inline event handler).
var markupSanitizerDOMPurifyRe = regexp.MustCompile(
	`\bDOMPurify\s*\.\s*sanitize\s*\(`,
)

// sniffTaintMarkupScript handles taint detection for Vue/Svelte/Astro files.
// It runs the generic JS/TS sniffer on every <script> block (adjusting line
// offsets) and additionally scans the full file for markup-specific patterns.
func sniffTaintMarkupScript(content string) []TaintMatch {
	if content == "" {
		return nil
	}
	var out []TaintMatch

	// Scan <script> blocks with the JS/TS sniffer.
	for _, m := range taintScriptBlockRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		bodyLineOffset := lineOfOffset(content, m[2]) - 1
		for _, match := range sniffTaintJSTS(body) {
			match.Line += bodyLineOffset
			out = append(out, match)
		}
	}

	// Scan the full file for markup-specific taint patterns. Use empty headers
	// because these patterns appear in template sections (no function headers
	// there); Function will be left empty, which the propagation pass treats
	// as module-scope and deprioritises.
	var noHeaders []funcHeader

	// Sources: framework prop / route-param access anywhere in the file.
	out = appendTaintMatches(out, content, noHeaders, markupSourcePropsParamsRe, TaintKindSource, TaintCategoryGeneric, "route.params/Astro.params/$page.params", 1.0)
	// Sanitizers.
	out = appendTaintMatches(out, content, noHeaders, markupSanitizerDOMPurifyRe, TaintKindSanitizer, TaintCategoryXSS, "DOMPurify.sanitize", 1.0)
	// Sinks.
	out = appendTaintMatches(out, content, noHeaders, markupSinkVHtmlRe, TaintKindSink, TaintCategoryXSS, "v-html/Svelte@html/Astro set:html", 1.0)
	out = appendTaintMatches(out, content, noHeaders, markupSinkSSRFetchRe, TaintKindSink, TaintCategorySSRF, "fetch(non-literal URL) in SSR", 0.85)

	return out
}
