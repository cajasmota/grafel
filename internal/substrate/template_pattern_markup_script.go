// Markup-with-script template-pattern dispatcher (#2779 Phase 3D T3).
//
// Svelte (.svelte), Vue (.vue), and Astro (.astro) single-file components
// embed JS/TS inside `<script>` blocks. Template-pattern matches inside those
// blocks are identical to plain JS/TS (t("key"), console.log("..."), SQL
// literals), so we extract every `<script>...</script>` body and run
// sniffTemplatePatternsJSTS over each. Line offsets are adjusted to match
// the original markup file position.
package substrate

func init() {
	RegisterTemplatePatternSniffer("svelte", sniffTemplatePatternsMarkupScript)
	RegisterTemplatePatternSniffer("vue", sniffTemplatePatternsMarkupScript)
	RegisterTemplatePatternSniffer("astro", sniffTemplatePatternsMarkupScript)
}

// sniffTemplatePatternsMarkupScript extracts every <script> block and runs
// sniffTemplatePatternsJSTS over each, offsetting line numbers to match the
// original markup file (not the script-only slice).
func sniffTemplatePatternsMarkupScript(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	var out []TemplatePattern
	for _, m := range scriptBlockRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		bodyLineOffset := lineOfOffset(content, m[2]) - 1
		for _, tp := range sniffTemplatePatternsJSTS(body) {
			tp.Line += bodyLineOffset
			out = append(out, tp)
		}
	}
	return out
}
