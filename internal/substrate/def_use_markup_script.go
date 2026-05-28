// Markup-with-script def-use dispatcher (#2779 Phase 3C T3).
//
// Svelte (.svelte), Vue (.vue), and Astro (.astro) single-file components
// embed JS/TS inside `<script>` blocks. Def-use patterns inside those blocks
// are identical to plain JS/TS, so we extract every `<script>...</script>`
// body and run sniffDefUseJSTS over the concatenation. Line offsets are
// adjusted so that recorded lines match the original markup file position.
package substrate

func init() {
	RegisterDefUseSniffer("svelte", sniffDefUseMarkupScript)
	RegisterDefUseSniffer("vue", sniffDefUseMarkupScript)
	RegisterDefUseSniffer("astro", sniffDefUseMarkupScript)
}

// sniffDefUseMarkupScript extracts every <script> block and runs
// sniffDefUseJSTS over each, offsetting line numbers to match the original
// markup file (not the script-only slice).
func sniffDefUseMarkupScript(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	var allDefs []VarDef
	var allUses []VarUse
	for _, m := range scriptBlockRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		bodyLineOffset := lineOfOffset(content, m[2]) - 1
		defs, uses := sniffDefUseJSTS(body)
		for _, d := range defs {
			d.Line += bodyLineOffset
			allDefs = append(allDefs, d)
		}
		for _, u := range uses {
			u.Line += bodyLineOffset
			allUses = append(allUses, u)
		}
	}
	return allDefs, allUses
}
