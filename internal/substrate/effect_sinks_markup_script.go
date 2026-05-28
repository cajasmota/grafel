// Markup-with-script effect-sink dispatcher (#2776 Phase 1A T3).
//
// Svelte (.svelte), Vue (.vue), and Astro (.astro) single-file components
// embed JS/TS inside `<script>` blocks. Effect-sink patterns inside those
// blocks are identical to plain JS/TS (fetch, axios, fs.readFile, etc.), so
// we extract every `<script>...</script>` body and run sniffEffectsJSTS over
// the concatenation. Line offsets are adjusted to match the original file.
//
// Astro also has `<script>` and server-side `---` frontmatter code blocks; we
// scan `<script>` blocks only — frontmatter effects are rare in the corpus
// and the regex surface for island-script is identical to JS.
//
// Effect applicability:
//   - http_out  : fetch / axios inside <script setup> / Svelte onMount
//   - db_write / db_read : Prisma / Drizzle calls in Vue setup / Svelte load
//   - fs_read / fs_write : Node fs calls in +server.ts / Astro API routes
//   - mutation  : this.field / store writes inside setup()
//
// Not_applicable: Verilog, VHDL, and pure-markup langs without script blocks.
package substrate

import "regexp"

func init() {
	RegisterEffectSniffer("svelte", sniffEffectsMarkupScript)
	RegisterEffectSniffer("vue", sniffEffectsMarkupScript)
	RegisterEffectSniffer("astro", sniffEffectsMarkupScript)
}

// effectScriptBlockRe matches `<script ...>` ... `</script>` blocks.
// Capture group 1 is the inner script body.
var effectScriptBlockRe = regexp.MustCompile(
	`(?si)<script\b[^>]*>(.*?)</script>`,
)

// sniffEffectsMarkupScript extracts every <script> block and runs
// sniffEffectsJSTS over each, offsetting line numbers so the recorded Line
// matches the original markup file position.
func sniffEffectsMarkupScript(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	var out []EffectMatch
	for _, m := range effectScriptBlockRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		bodyLineOffset := lineOfOffset(content, m[2]) - 1
		for _, match := range sniffEffectsJSTS(body) {
			match.Line += bodyLineOffset
			out = append(out, match)
		}
	}
	return out
}
