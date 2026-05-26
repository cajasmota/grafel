// Package vhdl implements a regex-based extractor for VHDL source files.
//
// VHDL is the second hardware-description language supported by archigraph,
// alongside Verilog/SystemVerilog (internal/extractors/verilog).
//
// Extracted entities:
//   - `entity Name is ... end [entity] [Name];`     → SCOPE.Component (entity)
//   - `architecture Name of Foo is ... end [architecture] [Name];`
//     → SCOPE.Component (architecture) + PORT_OF edge to the entity
//   - `package Name is ... end [package] [Name];`   → SCOPE.Component (package)
//   - `package body Name is ... end [package body] [Name];`
//     → SCOPE.Component (package_body) + PORT_OF edge to the package
//   - `function name (...) return T is ... end [function] [name];`
//     → SCOPE.Operation (function)
//   - `procedure name (...) is ... end [procedure] [name];`
//     → SCOPE.Operation (procedure)
//   - `library ieee;` / `use ieee.std_logic_1164.all;`
//     → IMPORTS edges
//   - Component instantiation `inst: ComponentName port map (...)`
//     → USES edges
//
// Signal declarations (`signal name : type;`) are skipped — they are leaf
// data items that contribute noise rather than structural signal.
//
// File extensions handled: .vhd, .vhdl
//
// No tree-sitter grammar for VHDL is bundled in smacker/go-tree-sitter, so
// this extractor uses regular expressions on comment-stripped source.
//
// Registers itself via init() and is imported by registry_gen.go.
package vhdl

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("vhdl", &Extractor{})
}

// Extractor implements extractor.Extractor for VHDL.
type Extractor struct{}

// Language returns "vhdl".
func (e *Extractor) Language() string { return "vhdl" }

// -----------------------------------------------------------------------
// Compiled regex patterns
// -----------------------------------------------------------------------

var (
	// entityRE matches VHDL entity declarations (case-insensitive):
	//   entity CounterTop is
	//   entity AluCore is
	entityRE = regexp.MustCompile(
		`(?im)^\s*entity\s+([A-Za-z_][A-Za-z0-9_]*)\s+is\b`,
	)

	// architectureRE matches architecture declarations:
	//   architecture rtl of CounterTop is
	//   architecture behavioral of AluCore is
	// Group 1 = architecture name, Group 2 = entity name.
	architectureRE = regexp.MustCompile(
		`(?im)^\s*architecture\s+([A-Za-z_][A-Za-z0-9_]*)\s+of\s+([A-Za-z_][A-Za-z0-9_]*)\s+is\b`,
	)

	// packageRE matches package declarations (but NOT package body).
	// We match "package <name> is" where <name> is not the literal "body".
	// Go regexp has no negative lookahead, so we filter "body" in code.
	packageRE = regexp.MustCompile(
		`(?im)^\s*package\s+([A-Za-z_][A-Za-z0-9_]*)\s+is\b`,
	)

	// packageBodyRE matches package body declarations:
	//   package body ieee_arith is
	// Group 1 = package name.
	packageBodyRE = regexp.MustCompile(
		`(?im)^\s*package\s+body\s+([A-Za-z_][A-Za-z0-9_]*)\s+is\b`,
	)

	// functionRE matches function declarations:
	//   function to_integer (v : std_logic_vector) return integer is
	//   function clamp (val : integer) return integer is
	// Matches both the declaration within an architecture/package and
	// standalone protected-body functions.  The "return ... is" suffix
	// is used to anchor to the definition (not a forward declaration).
	functionRE = regexp.MustCompile(
		`(?im)^\s*function\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:\([^)]*\))?\s+return\s+\S+\s+is\b`,
	)

	// procedureRE matches procedure declarations:
	//   procedure reset_counter (signal cnt : out integer) is
	// The "is" suffix anchors to the definition body.
	procedureRE = regexp.MustCompile(
		`(?im)^\s*procedure\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:\([^)]*\))?\s+is\b`,
	)

	// libraryRE matches library clauses:
	//   library ieee;
	//   library work;
	libraryRE = regexp.MustCompile(
		`(?im)^\s*library\s+([A-Za-z_][A-Za-z0-9_]*)\s*;`,
	)

	// useRE matches use clauses:
	//   use ieee.std_logic_1164.all;
	//   use work.alu_pkg.all;
	// Group 1 = library/package name (first dotted segment).
	useRE = regexp.MustCompile(
		`(?im)^\s*use\s+([A-Za-z_][A-Za-z0-9_]*)\.`,
	)

	// componentInstRE matches component instantiations:
	//   u_counter : CounterTop port map (...)
	//   u_alu     : entity work.AluCore port map (...)
	// Group 1 = instance label, Group 2 = component/entity name.
	// The "entity work." prefix is optional.
	componentInstRE = regexp.MustCompile(
		`(?im)^\s*([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(?:entity\s+\w+\.)?([A-Za-z_][A-Za-z0-9_]*)\s+port\s+map\s*\(`,
	)
)

// vhdlKeywords is the set of VHDL reserved words that must not be treated as
// component instance types in USES edges.
var vhdlKeywords = map[string]bool{
	"entity": true, "architecture": true, "package": true, "body": true,
	"is": true, "end": true, "begin": true, "port": true, "generic": true,
	"map": true, "of": true, "in": true, "out": true, "inout": true,
	"buffer": true, "linkage": true, "signal": true, "variable": true,
	"constant": true, "file": true, "type": true, "subtype": true,
	"use": true, "library": true, "work": true, "all": true,
	"if": true, "then": true, "else": true, "elsif": true, "when": true,
	"case": true, "for": true, "loop": true, "while": true,
	"process": true, "wait": true, "until": true,
	"function": true, "procedure": true, "return": true,
	"component": true, "configuration": true, "generate": true,
	"assert": true, "report": true, "severity": true,
	"null": true, "others": true, "open": true,
	"std_logic": true, "std_logic_vector": true, "bit": true, "bit_vector": true,
	"integer": true, "natural": true, "positive": true, "boolean": true,
	"string": true, "real": true, "time": true, "severity_level": true,
	"array": true, "record": true, "access": true, "protected": true,
	"impure": true, "pure": true, "shared": true, "new": true,
	"with": true, "select": true, "after": true, "transport": true,
	"reject": true, "inertial": true, "unaffected": true, "guarded": true,
	"block": true, "disconnect": true, "postponed": true,
	"attribute": true, "group": true, "label": true, "literal": true,
	"range": true, "reverse_range": true, "downto": true, "to": true,
	"and": true, "or": true, "not": true, "nand": true, "nor": true,
	"xor": true, "xnor": true, "mod": true, "rem": true, "abs": true,
	"sll": true, "srl": true, "sla": true, "sra": true, "rol": true,
	"ror": true,
}

// Extract processes a VHDL source file and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	lang := file.Language
	if lang == "" {
		lang = "vhdl"
	}
	out := extractVHDL(string(file.Content), file.Path, lang)
	extractor.TagRelationshipsLanguage(out, lang)
	extractor.TagEntitiesLanguage(out, lang)
	return out, nil
}

// extractVHDL is the testable core.
func extractVHDL(src, filePath, lang string) []types.EntityRecord {
	var entities []types.EntityRecord

	// Emit file-level entity.
	entities = append(entities, extractor.FileEntity(extractor.FileInput{
		Path:     filePath,
		Language: lang,
	}))

	scrubbed := stripVHDLComments(src)

	// ── 1. Library / use clauses (IMPORTS edges) ──────────────────────────
	entities = append(entities, buildVHDLImportEntities(filePath, scrubbed, lang)...)

	// ── 2. Design units ───────────────────────────────────────────────────
	entities = append(entities, findVHDLEntities(scrubbed, filePath, lang)...)
	entities = append(entities, findVHDLArchitectures(scrubbed, filePath, lang)...)
	entities = append(entities, findVHDLPackages(scrubbed, filePath, lang)...)
	entities = append(entities, findVHDLPackageBodies(scrubbed, filePath, lang)...)

	return entities
}

// -----------------------------------------------------------------------
// Import extraction
// -----------------------------------------------------------------------

func buildVHDLImportEntities(filePath, scrubbed, lang string) []types.EntityRecord {
	seen := make(map[string]bool)
	var out []types.EntityRecord

	// library clauses
	for _, m := range libraryRE.FindAllStringSubmatch(scrubbed, -1) {
		if len(m) < 2 {
			continue
		}
		libName := strings.ToLower(strings.TrimSpace(m[1]))
		if libName == "work" || libName == "" || seen["lib:"+libName] {
			continue
		}
		seen["lib:"+libName] = true

		props := map[string]string{
			"source_module": libName,
			"imported_name": libName,
			"local_name":    libName,
		}
		out = append(out, types.EntityRecord{
			Name:       libName,
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   lang,
			Relationships: []types.RelationshipRecord{
				{
					FromID:     filePath,
					ToID:       libName,
					Kind:       "IMPORTS",
					Properties: props,
				},
			},
		})
	}

	// use clauses — emit IMPORTS for the library/package (first segment)
	for _, m := range useRE.FindAllStringSubmatch(scrubbed, -1) {
		if len(m) < 2 {
			continue
		}
		libName := strings.ToLower(strings.TrimSpace(m[1]))
		if libName == "work" || libName == "" || seen["use:"+libName] {
			continue
		}
		seen["use:"+libName] = true

		props := map[string]string{
			"source_module": libName,
			"imported_name": libName,
			"local_name":    libName,
		}
		out = append(out, types.EntityRecord{
			Name:       libName,
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   lang,
			Relationships: []types.RelationshipRecord{
				{
					FromID:     filePath,
					ToID:       libName,
					Kind:       "IMPORTS",
					Properties: props,
				},
			},
		})
	}

	return out
}

// -----------------------------------------------------------------------
// Entity declarations
// -----------------------------------------------------------------------

func findVHDLEntities(scrubbed, filePath, lang string) []types.EntityRecord {
	var out []types.EntityRecord

	for _, m := range entityRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) < 4 {
			continue
		}
		name := scrubbed[m[2]:m[3]]
		startLine := strings.Count(scrubbed[:m[0]], "\n") + 1

		// Find the matching "end [entity] [Name];" to determine endLine.
		endLine := findVHDLEnd(scrubbed, m[1], "entity", name, startLine)

		rec := types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "entity",
			SourceFile: filePath,
			Language:   lang,
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  strings.Join(strings.Fields(scrubbed[m[0]:m[1]]), " "),
		}
		out = append(out, rec)
	}

	return out
}

// -----------------------------------------------------------------------
// Architecture declarations
// -----------------------------------------------------------------------

func findVHDLArchitectures(scrubbed, filePath, lang string) []types.EntityRecord {
	var out []types.EntityRecord

	for _, m := range architectureRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) < 6 {
			continue
		}
		archName := scrubbed[m[2]:m[3]]
		entityName := scrubbed[m[4]:m[5]]
		qualName := archName + "_of_" + entityName
		startLine := strings.Count(scrubbed[:m[0]], "\n") + 1

		// Find the begin keyword to locate the concurrent body.
		body, endLine := extractVHDLArchBody(scrubbed, m[1])
		if endLine == 0 {
			endLine = startLine
		}

		rec := types.EntityRecord{
			Name:       qualName,
			Kind:       "SCOPE.Component",
			Subtype:    "architecture",
			SourceFile: filePath,
			Language:   lang,
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  strings.Join(strings.Fields(scrubbed[m[0]:m[1]]), " "),
			// PORT_OF edge: this architecture belongs to the named entity.
			Relationships: []types.RelationshipRecord{
				{
					ToID: entityName,
					Kind: "PORT_OF",
				},
			},
		}

		archIdx := len(out)
		out = append(out, rec)

		if body == "" {
			continue
		}

		bodyOffset := startLine

		// Functions inside architecture.
		for _, fm := range functionRE.FindAllStringSubmatchIndex(body, -1) {
			if len(fm) < 4 {
				continue
			}
			fnName := body[fm[2]:fm[3]]
			fnQual := qualName + "." + fnName
			fnStart := bodyOffset + strings.Count(body[:fm[0]], "\n")

			fnRec := types.EntityRecord{
				Name:       fnQual,
				Kind:       "SCOPE.Operation",
				Subtype:    "function",
				SourceFile: filePath,
				Language:   lang,
				StartLine:  fnStart,
				EndLine:    fnStart,
				Signature:  strings.Join(strings.Fields(body[fm[0]:fm[1]]), " "),
			}
			out = append(out, fnRec)

			toID := extractor.BuildOperationStructuralRef(lang, filePath, fnQual)
			out[archIdx].Relationships = append(out[archIdx].Relationships, types.RelationshipRecord{
				ToID: toID,
				Kind: "CONTAINS",
			})
		}

		// Procedures inside architecture.
		for _, pm := range procedureRE.FindAllStringSubmatchIndex(body, -1) {
			if len(pm) < 4 {
				continue
			}
			procName := body[pm[2]:pm[3]]
			procQual := qualName + "." + procName
			pStart := bodyOffset + strings.Count(body[:pm[0]], "\n")

			pRec := types.EntityRecord{
				Name:       procQual,
				Kind:       "SCOPE.Operation",
				Subtype:    "procedure",
				SourceFile: filePath,
				Language:   lang,
				StartLine:  pStart,
				EndLine:    pStart,
				Signature:  strings.Join(strings.Fields(body[pm[0]:pm[1]]), " "),
			}
			out = append(out, pRec)

			toID := extractor.BuildOperationStructuralRef(lang, filePath, procQual)
			out[archIdx].Relationships = append(out[archIdx].Relationships, types.RelationshipRecord{
				ToID: toID,
				Kind: "CONTAINS",
			})
		}

		// Component instantiations (USES edges).
		for _, usesRel := range collectVHDLInstantiations(body, qualName) {
			out[archIdx].Relationships = append(out[archIdx].Relationships, usesRel)
		}
	}

	return out
}

// -----------------------------------------------------------------------
// Package declarations
// -----------------------------------------------------------------------

func findVHDLPackages(scrubbed, filePath, lang string) []types.EntityRecord {
	var out []types.EntityRecord

	for _, m := range packageRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) < 4 {
			continue
		}
		name := scrubbed[m[2]:m[3]]
		// Skip "package body <name> is" matches — packageBodyRE handles those.
		if strings.EqualFold(name, "body") {
			continue
		}
		startLine := strings.Count(scrubbed[:m[0]], "\n") + 1
		endLine := findVHDLEnd(scrubbed, m[1], "package", name, startLine)

		body, _ := extractVHDLBody(scrubbed, m[1])

		rec := types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "package",
			SourceFile: filePath,
			Language:   lang,
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  strings.Join(strings.Fields(scrubbed[m[0]:m[1]]), " "),
		}

		pkgIdx := len(out)
		out = append(out, rec)

		// Functions declared in the package header.
		for _, fm := range functionRE.FindAllStringSubmatchIndex(body, -1) {
			if len(fm) < 4 {
				continue
			}
			fnName := body[fm[2]:fm[3]]
			fnQual := name + "." + fnName
			fnStart := startLine + strings.Count(body[:fm[0]], "\n")

			fnRec := types.EntityRecord{
				Name:       fnQual,
				Kind:       "SCOPE.Operation",
				Subtype:    "function",
				SourceFile: filePath,
				Language:   lang,
				StartLine:  fnStart,
				EndLine:    fnStart,
				Signature:  strings.Join(strings.Fields(body[fm[0]:fm[1]]), " "),
			}
			out = append(out, fnRec)

			toID := extractor.BuildOperationStructuralRef(lang, filePath, fnQual)
			out[pkgIdx].Relationships = append(out[pkgIdx].Relationships, types.RelationshipRecord{
				ToID: toID,
				Kind: "CONTAINS",
			})
		}
	}

	return out
}

// -----------------------------------------------------------------------
// Package body declarations
// -----------------------------------------------------------------------

func findVHDLPackageBodies(scrubbed, filePath, lang string) []types.EntityRecord {
	var out []types.EntityRecord

	for _, m := range packageBodyRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) < 4 {
			continue
		}
		name := scrubbed[m[2]:m[3]]
		qualName := name + "_body"
		startLine := strings.Count(scrubbed[:m[0]], "\n") + 1
		endLine := findVHDLEnd(scrubbed, m[1], "package body", name, startLine)

		body, _ := extractVHDLBody(scrubbed, m[1])

		rec := types.EntityRecord{
			Name:       qualName,
			Kind:       "SCOPE.Component",
			Subtype:    "package_body",
			SourceFile: filePath,
			Language:   lang,
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  strings.Join(strings.Fields(scrubbed[m[0]:m[1]]), " "),
			// PORT_OF edge: this body belongs to the named package.
			Relationships: []types.RelationshipRecord{
				{
					ToID: name,
					Kind: "PORT_OF",
				},
			},
		}

		bodyIdx := len(out)
		out = append(out, rec)

		// Functions in the package body.
		for _, fm := range functionRE.FindAllStringSubmatchIndex(body, -1) {
			if len(fm) < 4 {
				continue
			}
			fnName := body[fm[2]:fm[3]]
			fnQual := qualName + "." + fnName
			fnStart := startLine + strings.Count(body[:fm[0]], "\n")

			fnRec := types.EntityRecord{
				Name:       fnQual,
				Kind:       "SCOPE.Operation",
				Subtype:    "function",
				SourceFile: filePath,
				Language:   lang,
				StartLine:  fnStart,
				EndLine:    fnStart,
				Signature:  strings.Join(strings.Fields(body[fm[0]:fm[1]]), " "),
			}
			out = append(out, fnRec)

			toID := extractor.BuildOperationStructuralRef(lang, filePath, fnQual)
			out[bodyIdx].Relationships = append(out[bodyIdx].Relationships, types.RelationshipRecord{
				ToID: toID,
				Kind: "CONTAINS",
			})
		}

		// Procedures in the package body.
		for _, pm := range procedureRE.FindAllStringSubmatchIndex(body, -1) {
			if len(pm) < 4 {
				continue
			}
			procName := body[pm[2]:pm[3]]
			procQual := qualName + "." + procName
			pStart := startLine + strings.Count(body[:pm[0]], "\n")

			pRec := types.EntityRecord{
				Name:       procQual,
				Kind:       "SCOPE.Operation",
				Subtype:    "procedure",
				SourceFile: filePath,
				Language:   lang,
				StartLine:  pStart,
				EndLine:    pStart,
				Signature:  strings.Join(strings.Fields(body[pm[0]:pm[1]]), " "),
			}
			out = append(out, pRec)

			toID := extractor.BuildOperationStructuralRef(lang, filePath, procQual)
			out[bodyIdx].Relationships = append(out[bodyIdx].Relationships, types.RelationshipRecord{
				ToID: toID,
				Kind: "CONTAINS",
			})
		}
	}

	return out
}

// -----------------------------------------------------------------------
// Component instantiation (USES edges)
// -----------------------------------------------------------------------

func collectVHDLInstantiations(body, ownerName string) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	seen := make(map[string]bool)
	var out []types.RelationshipRecord

	for _, m := range componentInstRE.FindAllStringSubmatch(body, -1) {
		if len(m) < 3 {
			continue
		}
		instLabel := strings.ToLower(m[1])
		compType := m[2]

		// Skip VHDL keywords.
		if vhdlKeywords[strings.ToLower(compType)] {
			continue
		}
		if vhdlKeywords[instLabel] {
			continue
		}
		// Skip the owner itself (avoid self-loops).
		if strings.EqualFold(compType, ownerName) {
			continue
		}
		// Skip very short names (likely variables).
		if len(compType) <= 1 {
			continue
		}

		key := instLabel + ":" + strings.ToLower(compType)
		if seen[key] {
			continue
		}
		seen[key] = true

		out = append(out, types.RelationshipRecord{
			ToID: compType,
			Kind: "USES",
		})
	}
	return out
}

// -----------------------------------------------------------------------
// Body extraction helpers
// -----------------------------------------------------------------------

// extractVHDLBody extracts the text between the declarative/statement region
// opener (afterPos) and the matching "end" keyword. Returns the body text
// and the 1-based end line.
func extractVHDLBody(src string, afterPos int) (string, int) {
	if afterPos >= len(src) {
		return "", 0
	}
	rest := src[afterPos:]

	// VHDL nests with "begin ... end" for architecture/process/generate.
	// We track depth using begin/end pairs, where "is" also opens a region.
	// For simplicity: scan for the top-level "end" that is not inside a nested
	// "begin...end" — depth starts at 0, increment on is/begin, decrement on end.

	beginRE := regexp.MustCompile(`(?i)\b(begin|is)\b`)
	endRE := regexp.MustCompile(`(?i)\bend\b`)

	depth := 0
	i := 0
	for i < len(rest) {
		// Check for end first at depth==0.
		if loc := endRE.FindStringIndex(rest[i:]); loc != nil && loc[0] == 0 {
			if depth == 0 {
				body := rest[:i]
				endLine := strings.Count(src[:afterPos+i+loc[1]], "\n") + 1
				return body, endLine
			}
			depth--
			i += loc[1]
			continue
		}
		// Check for begin/is that opens a nested block.
		if loc := beginRE.FindStringIndex(rest[i:]); loc != nil && loc[0] == 0 {
			depth++
			i += loc[1]
			continue
		}
		i++
	}
	return rest, 0
}

// extractVHDLArchBody extracts an architecture body starting from afterPos.
// The architecture declarative region ends at "begin" and the concurrent
// statement region ends at "end architecture ...;". We return the full
// text so subunit extractors can find functions, procedures, and instantiations.
func extractVHDLArchBody(src string, afterPos int) (string, int) {
	if afterPos >= len(src) {
		return "", 0
	}
	rest := src[afterPos:]

	// Find the top-level "end" that closes this architecture.
	// We increment depth on every "is" or "begin" and decrement on "end".
	openRE := regexp.MustCompile(`(?i)\b(begin|is)\b`)
	endRE := regexp.MustCompile(`(?i)\bend\b`)

	depth := 0
	i := 0
	for i < len(rest) {
		if loc := endRE.FindStringIndex(rest[i:]); loc != nil && loc[0] == 0 {
			if depth == 0 {
				body := rest[:i]
				endLine := strings.Count(src[:afterPos+i+loc[1]], "\n") + 1
				return body, endLine
			}
			depth--
			i += loc[1]
			continue
		}
		if loc := openRE.FindStringIndex(rest[i:]); loc != nil && loc[0] == 0 {
			depth++
			i += loc[1]
			continue
		}
		i++
	}
	return rest, 0
}

// findVHDLEnd returns the 1-based end line for a design unit.
// keyword is the unit keyword (e.g. "entity", "package", "package body"),
// name is the unit name. afterPos is where to start scanning.
func findVHDLEnd(src string, afterPos int, keyword, name string, startLine int) int {
	if afterPos >= len(src) {
		return startLine
	}
	rest := src[afterPos:]
	// Build a pattern that matches "end [keyword] [name] ;" with optional parts.
	endRE := regexp.MustCompile(`(?i)\bend\b`)

	openRE := regexp.MustCompile(`(?i)\b(begin|is)\b`)

	depth := 0
	i := 0
	for i < len(rest) {
		if loc := endRE.FindStringIndex(rest[i:]); loc != nil && loc[0] == 0 {
			if depth == 0 {
				return strings.Count(src[:afterPos+i+loc[1]], "\n") + 1
			}
			depth--
			i += loc[1]
			continue
		}
		if loc := openRE.FindStringIndex(rest[i:]); loc != nil && loc[0] == 0 {
			depth++
			i += loc[1]
			continue
		}
		i++
	}
	_ = keyword
	_ = name
	return startLine
}

// -----------------------------------------------------------------------
// Comment stripping
// -----------------------------------------------------------------------

// stripVHDLComments replaces VHDL single-line comments (-- ...) and string
// literals with spaces so regexes don't match inside them.
// VHDL does not have block comments in VHDL-87/93/2000/2002; VHDL-2008
// introduced /* */ but they are uncommon; we handle both.
func stripVHDLComments(src string) string {
	out := make([]byte, len(src))
	copy(out, src)
	i := 0
	for i < len(src) {
		ch := src[i]

		// Single-line comment: -- ...
		if ch == '-' && i+1 < len(src) && src[i+1] == '-' {
			for i < len(src) && src[i] != '\n' {
				out[i] = ' '
				i++
			}
			continue
		}

		// Block comment (VHDL-2008): /* ... */
		if ch == '/' && i+1 < len(src) && src[i+1] == '*' {
			out[i] = ' '
			out[i+1] = ' '
			i += 2
			for i+1 < len(src) {
				if src[i] == '*' && src[i+1] == '/' {
					out[i] = ' '
					out[i+1] = ' '
					i += 2
					break
				}
				if src[i] != '\n' {
					out[i] = ' '
				}
				i++
			}
			continue
		}

		// String literal: "..."
		if ch == '"' {
			out[i] = ' '
			i++
			for i < len(src) && src[i] != '"' {
				if src[i] != '\n' {
					out[i] = ' '
				}
				i++
			}
			if i < len(src) {
				out[i] = ' '
				i++
			}
			continue
		}

		i++
	}
	return string(out)
}
