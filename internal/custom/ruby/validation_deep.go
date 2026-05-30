// validation_deep.go — deep Rails validation + strong-params extraction.
//
// Raises lang.ruby.framework.rails Validation/dto_extraction and
// Validation/request_validation from partial to full (TS/JS bar parity).
//
// Part of issue #3340.
//
// Deepens the existing validation.go heuristics in two ways:
//
//  1. Per-validator rule entities (request_validation):
//     validates :name, presence: true, length: { minimum: 2, maximum: 50 }
//     → railsval:name:presence  (props: field=name, validator=presence, options={})
//     → railsval:name:length    (props: field=name, validator=length, options=minimum:2,maximum:50)
//     Also handles validates_presence_of / validates_length_of / validates_format_of /
//     validates_uniqueness_of / validates_numericality_of with trailing option capture.
//     validate :custom_method → railsval_custom:custom_method (unchanged).
//     with_options block → all validates inside inherit the block's options.
//
//  2. Per-field strong-params entities (dto_extraction):
//     params.require(:user).permit(:name, :email, roles: [])
//     → sp_field:user:name   (props: param=user, field=name, permit_type=scalar)
//     → sp_field:user:email  (props: param=user, field=email, permit_type=scalar)
//     → sp_field:user:roles  (props: param=user, field=roles, permit_type=array)
//     Nested permits: permit(address: [:street, :city]) →
//     → sp_field:user:address.street / address.city (permit_type=nested)
//
// Collision safety: all package-level symbols added here are prefixed railsVal/raVal
// to avoid clashing with the many existing var/func names in this package.
package ruby

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Regexes (prefixed raVal to avoid collisions with validation.go)
// ---------------------------------------------------------------------------

var (
	// validates :field[, key: val, key2: val2, key2: { ... }]
	// Captures: group 1 = field name, group 2 = rest of the line after the field.
	raValValidatesFull = regexp.MustCompile(
		`(?m)^\s*validates?\s+:([a-z_?!]+)((?:\s*,\s*.+)?)$`,
	)

	// validates_<macro>_of :field[, options]
	// Captures: group 1 = macro suffix (presence, length, …), group 2 = field, group 3 = opts.
	raValClassicFull = regexp.MustCompile(
		`(?m)^\s*validates_(presence|length|uniqueness|format|numericality|inclusion|exclusion|confirmation|acceptance|associated)_of\s+:([a-z_]+)((?:\s*,\s*.+)?)$`,
	)

	// with_options <opts> do … end
	// We capture the option hash on the with_options line; fields inside the block
	// are found by walking the indented validates lines.
	raValWithOptions = regexp.MustCompile(
		`(?m)^\s*with_options\s+(.+?)\s+do\b`,
	)

	// params.require(:model) — capture model name.
	raValRequireCapture = regexp.MustCompile(
		`(?m)\bparams\.require\s*\(\s*["':"]?([a-z_]+)["']?\s*\)`,
	)

	// .permit(<args>) — capture argument list (handles nested parens poorly,
	// but covers the common flat+one-level-nested pattern).
	raValPermitArgs = regexp.MustCompile(
		`(?m)\.permit\s*\(([^)]+)\)`,
	)

	// A key: [...] pair inside permit args (nested array permit).
	raValNestedArray = regexp.MustCompile(
		`([a-z_]+)\s*:\s*\[([^\]]*)\]`,
	)

	// A key: { ... } pair inside permit args (nested hash permit).
	raValNestedHash = regexp.MustCompile(
		`([a-z_]+)\s*:\s*\{([^}]*)\}`,
	)

	// Bare :symbol inside permit (scalar field).
	raValBareSymbol = regexp.MustCompile(`:([a-z_]+)`)
)

// ---------------------------------------------------------------------------
// Entry point — called from the existing ruby_validation extractor init.
// ---------------------------------------------------------------------------

// railsValDeepExtract appends deep Rails validation entities to the provided
// accumulator.  It is called by the rubyValidationExtractor after its own
// heuristics so that both sets of entities coexist.
func railsValDeepExtract(src string, file extractor.FileInput, add func(types.EntityRecord)) {
	// 1. Per-validator rule entities from `validates` lines.
	railsValParseValidates(src, file, add)

	// 2. Per-validator rule entities from classic validates_*_of lines.
	railsValParseClassic(src, file, add)

	// 3. with_options blocks — inherit options into validates inside block.
	railsValParseWithOptions(src, file, add)

	// 4. Per-field strong-params entities from params.require/permit chains.
	railsValParseStrongParams(src, file, add)
}

// ---------------------------------------------------------------------------
// 1. validates :field, presence: true, length: { minimum: 2 }
// ---------------------------------------------------------------------------

func railsValParseValidates(src string, file extractor.FileInput, add func(types.EntityRecord)) {
	for _, idx := range raValValidatesFull.FindAllStringSubmatchIndex(src, -1) {
		field := src[idx[2]:idx[3]]
		optsTail := ""
		if idx[4] != -1 {
			optsTail = strings.TrimSpace(src[idx[4]:idx[5]])
		}
		ln := lineOf(src, idx[0])

		validators := railsValExtractValidators(optsTail)
		if len(validators) == 0 {
			// No recognizable validators on this line — emit a bare presence marker
			// so we always have at least one entity per validates line.
			validators = []railsValRule{{name: "validates", opts: ""}}
		}
		for _, v := range validators {
			name := fmt.Sprintf("railsval:%s:%s", field, v.name)
			ent := makeEntity(name, "SCOPE.Pattern", "request_validation", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "rails",
				"provenance", "DEEP_VALIDATES",
				"signal", "validation",
				"field", field,
				"validator", v.name,
			)
			if v.opts != "" {
				ent.Properties["validator_options"] = v.opts
			}
			add(ent)
		}
	}
}

// railsValRule holds a parsed validator name + its options string.
type railsValRule struct {
	name string
	opts string
}

// railsValExtractValidators parses the trailing option tail of a validates line
// into per-validator rules.  Input is the part after `:field,` such as:
//
//	" presence: true, length: { minimum: 2, maximum: 50 }, format: { with: /\A\w+\z/ }"
//
// Simple key: true/false/symbol are treated as stand-alone validators.
// key: { ... } blocks are treated as validators with options.
// allow_nil/allow_blank/on/if/unless/message are filtered out (they are
// modifiers, not validators).
func railsValExtractValidators(tail string) []railsValRule {
	if tail == "" || tail == "," {
		return nil
	}
	// Strip leading comma.
	tail = strings.TrimPrefix(tail, ",")
	tail = strings.TrimSpace(tail)

	modifierKeys := map[string]bool{
		"allow_nil":   true,
		"allow_blank": true,
		"on":          true,
		"if":          true,
		"unless":      true,
		"message":     true,
		"strict":      true,
	}

	var rules []railsValRule

	// Walk token-by-token.  We handle two shapes:
	//   key: true / key: false / key: :symbol / key: "string"
	//   key: { inner }
	// Everything else (including Ruby expressions) is best-effort.

	i := 0
	runes := []rune(tail)
	for i < len(runes) {
		// Skip whitespace and commas.
		for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t' || runes[i] == ',') {
			i++
		}
		if i >= len(runes) {
			break
		}
		// Read the key (identifier chars).
		keyStart := i
		for i < len(runes) && (isAlpha(runes[i]) || runes[i] == '_') {
			i++
		}
		if i == keyStart {
			// Non-identifier; skip one char.
			i++
			continue
		}
		key := string(runes[keyStart:i])

		// Expect `: ` after the key.
		for i < len(runes) && runes[i] == ' ' {
			i++
		}
		if i >= len(runes) || runes[i] != ':' {
			continue
		}
		i++ // consume ':'
		for i < len(runes) && runes[i] == ' ' {
			i++
		}

		// Read the value.
		var valStr string
		if i < len(runes) && runes[i] == '{' {
			// Hash value — scan to matching '}'.
			depth := 0
			start := i
			for i < len(runes) {
				if runes[i] == '{' {
					depth++
				} else if runes[i] == '}' {
					depth--
					if depth == 0 {
						i++
						break
					}
				}
				i++
			}
			valStr = strings.TrimSpace(string(runes[start:i]))
		} else if i < len(runes) && runes[i] == '[' {
			// Array value — scan to ']'.
			start := i
			for i < len(runes) && runes[i] != ']' {
				i++
			}
			if i < len(runes) {
				i++
			}
			valStr = strings.TrimSpace(string(runes[start:i]))
		} else {
			// Scalar value — read until comma or end.
			start := i
			for i < len(runes) && runes[i] != ',' {
				i++
			}
			valStr = strings.TrimSpace(string(runes[start:i]))
		}

		if modifierKeys[key] {
			continue
		}

		// Normalize options: strip outer braces if present.
		opts := valStr
		if strings.HasPrefix(opts, "{") && strings.HasSuffix(opts, "}") {
			opts = strings.TrimSpace(opts[1 : len(opts)-1])
		}

		rules = append(rules, railsValRule{name: key, opts: opts})
	}

	return rules
}

func isAlpha(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// ---------------------------------------------------------------------------
// 2. validates_presence_of :field, options
// ---------------------------------------------------------------------------

func railsValParseClassic(src string, file extractor.FileInput, add func(types.EntityRecord)) {
	for _, idx := range raValClassicFull.FindAllStringSubmatchIndex(src, -1) {
		macro := src[idx[2]:idx[3]]
		field := src[idx[4]:idx[5]]
		opts := ""
		if idx[6] != -1 {
			opts = strings.TrimSpace(strings.TrimPrefix(src[idx[6]:idx[7]], ","))
		}
		ln := lineOf(src, idx[0])

		name := fmt.Sprintf("railsval_classic:%s:%s", macro, field)
		ent := makeEntity(name, "SCOPE.Pattern", "request_validation", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "rails",
			"provenance", "DEEP_VALIDATES_CLASSIC",
			"signal", "validation",
			"field", field,
			"validator", macro,
		)
		if opts != "" {
			ent.Properties["validator_options"] = opts
		}
		add(ent)
	}
}

// ---------------------------------------------------------------------------
// 3. with_options <opts> do … end
// ---------------------------------------------------------------------------

// raValWithOptionsValidates matches validates lines inside a with_options block.
// We use a simple approach: scan for with_options blocks and re-run the
// validates parser on content within the block, injecting inherited options.
var raValIndentedValidates = regexp.MustCompile(
	`(?m)^\s+validates?\s+:([a-z_?!]+)((?:\s*,\s*.+)?)$`,
)

func railsValParseWithOptions(src string, file extractor.FileInput, add func(types.EntityRecord)) {
	for _, woIdx := range raValWithOptions.FindAllStringSubmatchIndex(src, -1) {
		inheritedOpts := strings.TrimSpace(src[woIdx[2]:woIdx[3]])
		blockStart := woIdx[1]

		// Find the matching `end` — look for the first `end` at the same or
		// lower indentation level.  Simple heuristic: find the next `^\s*end\b`
		// that appears after the with_options line.
		endRe := regexp.MustCompile(`(?m)^\s*end\b`)
		endLoc := endRe.FindStringIndex(src[blockStart:])
		blockEnd := len(src)
		if endLoc != nil {
			blockEnd = blockStart + endLoc[1]
		}
		blockSrc := src[blockStart:blockEnd]
		ln := lineOf(src, woIdx[0])

		for _, idx := range raValIndentedValidates.FindAllStringSubmatchIndex(blockSrc, -1) {
			field := blockSrc[idx[2]:idx[3]]
			optsTail := ""
			if idx[4] != -1 {
				optsTail = strings.TrimSpace(blockSrc[idx[4]:idx[5]])
			}
			// Merge inherited options with per-field options.
			merged := inheritedOpts
			if optsTail != "" && optsTail != "," {
				merged = merged + ", " + strings.TrimPrefix(optsTail, ",")
			}
			blockLn := lineOf(src, woIdx[1]+idx[0]) + ln - 1

			validators := railsValExtractValidators(merged)
			if len(validators) == 0 {
				validators = []railsValRule{{name: "validates", opts: ""}}
			}
			for _, v := range validators {
				name := fmt.Sprintf("railsval_wo:%s:%s", field, v.name)
				ent := makeEntity(name, "SCOPE.Pattern", "request_validation", file.Path, file.Language, blockLn)
				setProps(&ent,
					"framework", "rails",
					"provenance", "DEEP_WITH_OPTIONS",
					"signal", "validation",
					"field", field,
					"validator", v.name,
					"inherited_options", inheritedOpts,
				)
				if v.opts != "" {
					ent.Properties["validator_options"] = v.opts
				}
				add(ent)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 4. Strong params per-field entities
// ---------------------------------------------------------------------------

// railsValParseStrongParams finds params.require(:model).permit(...) chains and
// emits one entity per permitted field (scalar, array, and nested).
func railsValParseStrongParams(src string, file extractor.FileInput, add func(types.EntityRecord)) {
	// Walk require matches and find the following permit call on the same logical
	// expression (within ~200 bytes).
	for _, reqIdx := range raValRequireCapture.FindAllStringSubmatchIndex(src, -1) {
		param := src[reqIdx[2]:reqIdx[3]]
		// Look for .permit(...) within the next 300 chars.
		window := src[reqIdx[1]:]
		if len(window) > 300 {
			window = window[:300]
		}
		permIdx := raValPermitArgs.FindStringSubmatchIndex(window)
		if permIdx == nil {
			continue
		}
		argsRaw := strings.TrimSpace(window[permIdx[2]:permIdx[3]])
		ln := lineOf(src, reqIdx[0])

		// Parse the args into field entries.
		fields := railsValParsePermitArgs(param, argsRaw)
		for _, f := range fields {
			name := fmt.Sprintf("sp_field:%s:%s", param, f.field)
			ent := makeEntity(name, "SCOPE.Schema", "dto_field", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "rails",
				"provenance", "DEEP_STRONG_PARAMS",
				"signal", "dto",
				"param", param,
				"field", f.field,
				"permit_type", f.ptype,
			)
			add(ent)
		}
	}
}

type railsValField struct {
	field string
	ptype string // "scalar", "array", "nested"
}

// railsValParsePermitArgs parses the argument string inside .permit(...).
//
// Handles:
//   - :name          → scalar
//   - roles: []      → array
//   - address: [:street, :city]   → array elements (permit_type=array, each separately)
//   - metadata: { key: [] }       → nested hash (emit as nested with dot notation)
func railsValParsePermitArgs(param, raw string) []railsValField {
	var out []railsValField
	consumed := make(map[string]bool)

	// First pass: extract nested array permits  key: [sym, sym, ...]
	for _, idx := range raValNestedArray.FindAllStringSubmatchIndex(raw, -1) {
		key := raw[idx[2]:idx[3]]
		inner := raw[idx[4]:idx[5]]
		if consumed[key] {
			continue
		}
		consumed[key] = true
		// Each symbol inside the array is a permitted nested field.
		for _, sym := range raValBareSymbol.FindAllStringSubmatch(inner, -1) {
			out = append(out, railsValField{
				field: key + "." + sym[1],
				ptype: "nested",
			})
		}
		// If the inner list is empty, the key itself is an array permit.
		if strings.TrimSpace(inner) == "" {
			out = append(out, railsValField{field: key, ptype: "array"})
		}
	}

	// Second pass: extract nested hash permits  key: { ... }
	for _, idx := range raValNestedHash.FindAllStringSubmatchIndex(raw, -1) {
		key := raw[idx[2]:idx[3]]
		inner := raw[idx[4]:idx[5]]
		if consumed[key] {
			continue
		}
		consumed[key] = true
		// Recurse one level.
		subFields := railsValParsePermitArgs(key, inner)
		for _, sf := range subFields {
			out = append(out, railsValField{
				field: key + "." + sf.field,
				ptype: "nested",
			})
		}
	}

	// Third pass: bare :symbol entries that weren't consumed yet.
	// Strip out the already-consumed key: [...] / key: {...} segments first.
	stripped := raValNestedArray.ReplaceAllString(raw, "")
	stripped = raValNestedHash.ReplaceAllString(stripped, "")
	for _, sym := range raValBareSymbol.FindAllStringSubmatch(stripped, -1) {
		field := sym[1]
		if consumed[field] {
			continue
		}
		out = append(out, railsValField{field: field, ptype: "scalar"})
	}

	return out
}
