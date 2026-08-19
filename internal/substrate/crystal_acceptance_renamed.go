// Crystal constant-binding sniffer (#2763 Phase 0 T3).
//
// Recognises module-level binding shapes:
//   - `NAME = "literal"` (Crystal constants are SCREAMING_CASE — same as Ruby)
//   - `NAME = ENV["Y"]? || "default"` / `NAME = ENV.fetch("Y", "default")`
//   - `require "module"`
//
// Crystal's constant rules require an uppercase first letter at the
// top-level; we accept that pattern explicitly.
package substrate

import "regexp"

func init() { Register("crystal", sniffCrystal) }

// crystalLiteralRe matches `NAME = "value"` at the start of a line.
// Capture groups: 1=name, 2=value.
var crystalLiteralRe = regexp.MustCompile(
	`(?m)^([A-Z][A-Z0-9_]*)\s*=\s*"([^"\n\r]{0,512})"\s*$`,
)

// crystalEnvOrRe matches `NAME = ENV["Y"]? || "default"`.
// Capture groups: 1=name, 2=env-var, 3=default.
var crystalEnvOrRe = regexp.MustCompile(
	`(?m)^([A-Z][A-Z0-9_]*)\s*=\s*ENV\s*\[\s*"([^"\n]{1,128})"\s*\]\?\s*\|\|\s*"([^"\n\r]{0,512})"`,
)

// crystalEnvFetchRe matches `NAME = ENV.fetch("Y", "default")`.
// Capture groups: 1=name, 2=env-var, 3=default.
var crystalEnvFetchRe = regexp.MustCompile(
	`(?m)^([A-Z][A-Z0-9_]*)\s*=\s*ENV\.fetch\s*\(\s*"([^"\n]{1,128})"\s*,\s*"([^"\n\r]{0,512})"\s*\)`,
)

// crystalRequireRe matches `require "module"`. The local binding name is
// the module path (Crystal exposes all top-level definitions on require).
var crystalRequireRe = regexp.MustCompile(
	`(?m)^\s*require\s+"([^"\n]+)"`,
)

func sniffCrystal(content string) []Binding {
	if content == "" {
		return nil
	}
	var out []Binding
	seen := map[int]bool{}

	emit := func(m []int, prov Provenance, conf float64, valIdx, envIdx int) {
		out = append(out, Binding{
			Ident:      content[m[2]:m[3]],
			Line:       lineOfOffset(content, m[2]),
			Value:      content[m[valIdx]:m[valIdx+1]],
			EnvVar:     content[m[envIdx]:m[envIdx+1]],
			Provenance: prov,
			Confidence: conf,
		})
		seen[m[2]] = true
	}

	for _, m := range crystalEnvOrRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		emit(m, ProvenanceEnvFallback, 0.85, 6, 4)
	}
	for _, m := range crystalEnvFetchRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 || seen[m[2]] {
			continue
		}
		emit(m, ProvenanceEnvFallback, 0.85, 6, 4)
	}
	for _, m := range crystalLiteralRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 || seen[m[2]] {
			continue
		}
		out = append(out, Binding{
			Ident:      content[m[2]:m[3]],
			Line:       lineOfOffset(content, m[2]),
			Value:      content[m[4]:m[5]],
			Provenance: ProvenanceLiteral,
			Confidence: 1.0,
		})
	}
	for _, m := range crystalRequireRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		module := content[m[2]:m[3]]
		out = append(out, Binding{
			Ident:        module,
			Line:         lineOfOffset(content, m[0]),
			Provenance:   ProvenanceCrossFile,
			Confidence:   0.6,
			ImportSource: module,
		})
	}
	return out
}
