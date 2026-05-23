// Package verilog implements a regex-based extractor for Verilog and
// SystemVerilog source files.
//
// A single package handles both dialects (analogous to how the lisp package
// handles Common Lisp, Scheme, and Racket).
//
// Extracted entities:
//   - `module Foo(...); ... endmodule`          → SCOPE.Component (module)
//   - `interface Foo; ... endinterface`          → SCOPE.Component (interface, SV)
//   - `package Foo; ... endpackage`              → SCOPE.Component (package, SV)
//   - `class Foo; ... endclass`                  → SCOPE.Component (class, SV)
//   - `function [type] name(...); ... endfunction` → SCOPE.Operation (function)
//   - `task name(...); ... endtask`              → SCOPE.Operation (task)
//   - `Foo inst_name(...)` — module instantiations → USES edges
//   - `import Pkg::*;` / `import Pkg::item;`     → IMPORTS (SV)
//   - “ `include "foo.vh" “                    → IMPORTS
//
// File extensions handled: .v, .vh (Verilog), .sv, .svh (SystemVerilog).
//
// No tree-sitter grammar for Verilog/SV is bundled in smacker/go-tree-sitter, so
// this extractor uses regular expressions.
//
// Registers itself via init() and is imported by registry_gen.go.
package verilog

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("verilog", &Extractor{})
	extractor.Register("systemverilog", &Extractor{})
}

// Extractor implements extractor.Extractor for Verilog and SystemVerilog.
type Extractor struct{}

// Language returns "verilog" for this extractor; registration covers both dialects.
func (e *Extractor) Language() string { return "verilog" }

// -----------------------------------------------------------------------
// Compiled regex patterns
// -----------------------------------------------------------------------

var (
	// moduleRE matches module declarations:
	//   module Foo (input a, output b); ... endmodule
	//   module Foo #(...) (...); ... endmodule
	//   module Foo; ... endmodule
	moduleRE = regexp.MustCompile(
		`(?m)^\s*module\s+([A-Za-z_][A-Za-z0-9_$]*)\s*(?:#\s*\([^)]*\)\s*)?\s*(?:\([^;]*\))?\s*;`,
	)

	// interfaceRE matches SV interface declarations:
	//   interface Foo; ... endinterface
	//   interface Foo (input clk); ... endinterface
	interfaceRE = regexp.MustCompile(
		`(?m)^\s*interface\s+([A-Za-z_][A-Za-z0-9_$]*)\s*(?:\([^;]*\))?\s*;`,
	)

	// packageRE matches SV package declarations:
	//   package Foo; ... endpackage
	packageRE = regexp.MustCompile(
		`(?m)^\s*package\s+([A-Za-z_][A-Za-z0-9_$]*)\s*;`,
	)

	// classRE matches SV class declarations:
	//   class Foo; ... endclass
	//   class Foo extends Bar; ... endclass
	classRE = regexp.MustCompile(
		`(?m)^\s*(?:virtual\s+)?class\s+([A-Za-z_][A-Za-z0-9_$]*)\s*(?:extends\s+([A-Za-z_][A-Za-z0-9_$:]*))?[;\s]`,
	)

	// functionRE matches function declarations:
	//   function automatic void myFunc(input logic a);
	//   function automatic [7:0] add_op;
	//   function automatic logic [7:0] clamp(...);
	//   function integer calc;
	//   function myFunc;
	// The return type may be: nothing, a bracket [n:0], an identifier,
	// or an identifier followed by a bracket (e.g. "logic [7:0]").
	functionRE = regexp.MustCompile(
		`(?m)^\s*function\s+(?:automatic\s+)?(?:(?:[A-Za-z_][A-Za-z0-9_$:]*\s+)?(?:\[[^\]]*\]\s+)?)?([A-Za-z_][A-Za-z0-9_$]*)\s*(?:\(|;)`,
	)

	// taskRE matches task declarations:
	//   task automatic myTask(input logic a);
	//   task myTask;
	taskRE = regexp.MustCompile(
		`(?m)^\s*task\s+(?:automatic\s+)?([A-Za-z_][A-Za-z0-9_$]*)\s*(?:\(|;)`,
	)

	// importRE matches SV package imports:
	//   import Pkg::*;
	//   import Pkg::item;
	importRE = regexp.MustCompile(
		`(?m)^\s*import\s+([A-Za-z_][A-Za-z0-9_$]*)\s*::`,
	)

	// includeRE matches `include directives:
	//   `include "foo.vh"
	//   `include "path/to/file.sv"
	includeRE = regexp.MustCompile(
		"(?m)^\\s*`include\\s+[\"']([^\"']+)[\"']",
	)

	// instantiationRE matches module/interface instantiations:
	//   ModuleName inst_name (...)
	//   ModuleName #(...) inst_name (...)
	// Group 1: module type name (leading uppercase or known pattern)
	// Group 2: instance name
	instantiationRE = regexp.MustCompile(
		`(?m)^\s*([A-Za-z_][A-Za-z0-9_$]*)\s+(?:#\s*\([^)]*\)\s+)?([A-Za-z_][A-Za-z0-9_$]*)\s*\(`,
	)
)

// verilogFunctionKeywords is the set of names that must NOT be treated as
// user-defined function/task names. Notably "new" is excluded here because it
// is a valid SV constructor name (function new(...); endfunction).
var verilogFunctionKeywords = map[string]bool{
	"automatic": true, "virtual": true, "static": true, "local": true,
	"protected": true,
	// Built-in tasks that may syntactically look like declarations.
	"$display": true, "$monitor": true, "$finish": true,
}

// verilogKeywords is the set of Verilog/SV keywords to exclude from USES edges
// to avoid false positives on constructs that look like instantiations.
var verilogKeywords = map[string]bool{
	// Verilog keywords
	"module": true, "endmodule": true, "input": true, "output": true,
	"inout": true, "wire": true, "reg": true, "logic": true, "bit": true,
	"integer": true, "real": true, "time": true, "realtime": true,
	"parameter": true, "localparam": true, "defparam": true,
	"assign": true, "always": true, "initial": true, "begin": true,
	"end": true, "if": true, "else": true, "case": true, "casex": true,
	"casez": true, "endcase": true, "for": true, "while": true,
	"forever": true, "repeat": true, "fork": true, "join": true,
	"function": true, "endfunction": true, "task": true, "endtask": true,
	"posedge": true, "negedge": true, "edge": true,
	"specify": true, "endspecify": true,
	"generate": true, "endgenerate": true, "genvar": true,
	"primitive": true, "endprimitive": true,
	"table": true, "endtable": true,
	"macromodule": true, "event": true, "supply0": true, "supply1": true,
	"tri": true, "tri0": true, "tri1": true, "triand": true, "trior": true,
	"trireg": true, "wand": true, "wor": true,
	"default": true, "disable": true, "force": true, "release": true,
	"deassign": true, "wait": true,
	// SystemVerilog additions
	"interface": true, "endinterface": true, "modport": true,
	"package": true, "endpackage": true, "import": true, "export": true,
	"class": true, "endclass": true, "extends": true, "implements": true,
	"virtual": true, "static": true, "automatic": true, "local": true,
	"protected": true, "rand": true, "randc": true, "constraint": true,
	"covergroup": true, "endgroup": true, "coverpoint": true,
	"property": true, "endproperty": true, "sequence": true,
	"endsequence": true, "program": true, "endprogram": true,
	"clocking": true, "endclocking": true,
	"assert": true, "assume": true, "cover": true,
	"enum": true, "struct": true, "union": true, "typedef": true,
	"type": true, "string": true, "byte": true, "shortint": true,
	"int": true, "longint": true, "shortreal": true, "chandle": true,
	"void": true, "null": true, "this": true, "super": true,
	"new": true, "return": true, "break": true, "continue": true,
	"do": true, "foreach": true, "inside": true, "dist": true,
	"solve": true, "with": true, "unique": true, "priority": true,
	"iff": true, "throughout": true, "within": true, "intersect": true,
	"first_match": true, "and": true, "or": true, "not": true,
	"if_keyword": true,
	// Built-in functions / system tasks
	"$display": true, "$monitor": true, "$strobe": true, "$finish": true,
	"$stop": true, "$time": true, "$realtime": true, "$random": true,
	"$urandom": true, "$cast": true, "$rose": true, "$fell": true,
	"$stable": true, "$past": true, "$changed": true, "$dumpvars": true,
	"$dumpfile": true, "$dumpon": true, "$dumpoff": true,
	// Common primitives / gates
	"and_gate": true, "or_gate": true, "not_gate": true, "nand": true,
	"nor": true, "xor": true, "xnor": true, "buf": true, "notif0": true,
	"notif1": true, "bufif0": true, "bufif1": true,
}

// Extract processes a Verilog/SystemVerilog source file and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	lang := file.Language
	if lang == "" {
		lang = "verilog"
	}
	out := extractVerilog(string(file.Content), file.Path, lang)
	extractor.TagRelationshipsLanguage(out, lang)
	return out, nil
}

// extractVerilog is the testable core.
func extractVerilog(src, filePath, lang string) []types.EntityRecord {
	var entities []types.EntityRecord

	// Emit file-level entity.
	entities = append(entities, extractor.FileEntity(extractor.FileInput{
		Path:     filePath,
		Language: lang,
	}))

	scrubbed := stripCommentsAndStrings(src)

	// ── 1. Include directives (`include) ─────────────────────────────────
	entities = append(entities, buildIncludeEntities(filePath, src, lang)...)

	// ── 2. SV package imports ─────────────────────────────────────────────
	entities = append(entities, buildImportEntities(filePath, scrubbed, lang)...)

	// ── 3. Module declarations ────────────────────────────────────────────
	entities = append(entities, findComponents(scrubbed, src, filePath, lang)...)

	return entities
}

// -----------------------------------------------------------------------
// Component extraction (module, interface, package, class)
// -----------------------------------------------------------------------

// componentSpec describes a top-level block construct in Verilog/SV.
type componentSpec struct {
	re      *regexp.Regexp
	subtype string
	endKW   string
}

var componentSpecs = []componentSpec{
	{moduleRE, "module", "endmodule"},
	{interfaceRE, "interface", "endinterface"},
	{packageRE, "package", "endpackage"},
	{classRE, "class", "endclass"},
}

// findComponents extracts all top-level component declarations plus their
// nested functions and tasks.
func findComponents(scrubbed, src, filePath, lang string) []types.EntityRecord {
	var out []types.EntityRecord

	for _, spec := range componentSpecs {
		matches := spec.re.FindAllStringSubmatchIndex(scrubbed, -1)
		for _, m := range matches {
			if len(m) < 4 {
				continue
			}
			name := scrubbed[m[2]:m[3]]
			startLine := strings.Count(scrubbed[:m[0]], "\n") + 1

			// For class, check for extends.
			var extends []string
			if spec.subtype == "class" && len(m) >= 6 && m[4] >= 0 && m[5] > m[4] {
				parent := strings.TrimSpace(scrubbed[m[4]:m[5]])
				// Strip any package-qualified prefix (Pkg::ClassName → ClassName).
				if dcolon := strings.LastIndex(parent, "::"); dcolon >= 0 {
					parent = parent[dcolon+2:]
				}
				if parent != "" {
					extends = append(extends, parent)
				}
			}

			// Find the body between declaration and endXxx.
			body, endLine := extractKeywordBody(scrubbed, m[1], spec.endKW)
			if endLine == 0 {
				endLine = startLine
			}

			rawSig := strings.Join(strings.Fields(scrubbed[m[0]:m[1]]), " ")

			rec := types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Component",
				Subtype:    spec.subtype,
				SourceFile: filePath,
				Language:   lang,
				StartLine:  startLine,
				EndLine:    endLine,
				Signature:  rawSig,
			}

			// EXTENDS edges for class.
			for _, parent := range extends {
				rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
					FromID: filePath,
					ToID:   parent,
					Kind:   "EXTENDS",
				})
			}

			compIdx := len(out)
			out = append(out, rec)

			if body == "" {
				continue
			}

			bodyLineOffset := startLine

			// ── Functions inside this component ──────────────────────────
			for _, fm := range functionRE.FindAllStringSubmatchIndex(body, -1) {
				if len(fm) < 4 {
					continue
				}
				fnName := body[fm[2]:fm[3]]
				if verilogFunctionKeywords[fnName] {
					continue
				}
				qualName := name + "." + fnName
				fnStartLine := bodyLineOffset + strings.Count(body[:fm[0]], "\n")
				fnBody, fnEndLine := extractKeywordBody(body, fm[1], "endfunction")
				if fnEndLine == 0 {
					fnEndLine = fnStartLine
				}
				rawFnSig := strings.Join(strings.Fields(body[fm[0]:fm[1]]), " ")

				fnRec := types.EntityRecord{
					Name:       qualName,
					Kind:       "SCOPE.Operation",
					Subtype:    "function",
					SourceFile: filePath,
					Language:   lang,
					StartLine:  fnStartLine,
					EndLine:    fnStartLine + strings.Count(fnBody, "\n"),
					Signature:  rawFnSig,
				}
				out = append(out, fnRec)
				_ = fnEndLine

				// CONTAINS edge.
				toID := extractor.BuildOperationStructuralRef(lang, filePath, qualName)
				out[compIdx].Relationships = append(out[compIdx].Relationships, types.RelationshipRecord{
					ToID: toID,
					Kind: "CONTAINS",
				})
			}

			// ── Tasks inside this component ───────────────────────────────
			for _, tm := range taskRE.FindAllStringSubmatchIndex(body, -1) {
				if len(tm) < 4 {
					continue
				}
				taskName := body[tm[2]:tm[3]]
				if verilogFunctionKeywords[taskName] {
					continue
				}
				qualName := name + "." + taskName
				tStartLine := bodyLineOffset + strings.Count(body[:tm[0]], "\n")
				tBody, _ := extractKeywordBody(body, tm[1], "endtask")
				rawTSig := strings.Join(strings.Fields(body[tm[0]:tm[1]]), " ")

				tRec := types.EntityRecord{
					Name:       qualName,
					Kind:       "SCOPE.Operation",
					Subtype:    "task",
					SourceFile: filePath,
					Language:   lang,
					StartLine:  tStartLine,
					EndLine:    tStartLine + strings.Count(tBody, "\n"),
					Signature:  rawTSig,
				}
				out = append(out, tRec)

				toID := extractor.BuildOperationStructuralRef(lang, filePath, qualName)
				out[compIdx].Relationships = append(out[compIdx].Relationships, types.RelationshipRecord{
					ToID: toID,
					Kind: "CONTAINS",
				})
			}

			// ── Module instantiations (USES edges) ────────────────────────
			usesRels := collectInstantiations(body, name)
			for _, u := range usesRels {
				out[compIdx].Relationships = append(out[compIdx].Relationships, u)
			}
		}
	}

	return out
}

// -----------------------------------------------------------------------
// Import / include extraction
// -----------------------------------------------------------------------

func buildIncludeEntities(filePath, src, lang string) []types.EntityRecord {
	seen := make(map[string]bool)
	var out []types.EntityRecord

	for _, m := range includeRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		includePath := strings.TrimSpace(m[1])
		if includePath == "" || seen[includePath] {
			continue
		}
		seen[includePath] = true

		displayName := includePath
		if slash := strings.LastIndexByte(includePath, '/'); slash >= 0 {
			displayName = includePath[slash+1:]
		}
		// Strip common header extensions.
		displayName = strings.TrimSuffix(displayName, ".vh")
		displayName = strings.TrimSuffix(displayName, ".svh")
		displayName = strings.TrimSuffix(displayName, ".v")
		displayName = strings.TrimSuffix(displayName, ".sv")

		props := map[string]string{
			"source_module": includePath,
			"imported_name": displayName,
			"local_name":    displayName,
		}

		out = append(out, types.EntityRecord{
			Name:       displayName,
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   lang,
			Relationships: []types.RelationshipRecord{
				{
					FromID:     filePath,
					ToID:       includePath,
					Kind:       "IMPORTS",
					Properties: props,
				},
			},
		})
	}
	return out
}

func buildImportEntities(filePath, scrubbed, lang string) []types.EntityRecord {
	seen := make(map[string]bool)
	var out []types.EntityRecord

	for _, m := range importRE.FindAllStringSubmatch(scrubbed, -1) {
		if len(m) < 2 {
			continue
		}
		pkgName := strings.TrimSpace(m[1])
		if pkgName == "" || seen[pkgName] {
			continue
		}
		seen[pkgName] = true

		props := map[string]string{
			"source_module": pkgName,
			"imported_name": pkgName,
			"local_name":    pkgName,
		}

		out = append(out, types.EntityRecord{
			Name:       pkgName,
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   lang,
			Relationships: []types.RelationshipRecord{
				{
					FromID:     filePath,
					ToID:       pkgName,
					Kind:       "IMPORTS",
					Properties: props,
				},
			},
		})
	}
	return out
}

// -----------------------------------------------------------------------
// Module instantiation (USES edges)
// -----------------------------------------------------------------------

// collectInstantiations scans a module body for module/interface instantiations
// and returns USES relationship records.
func collectInstantiations(body, ownerName string) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	seen := make(map[string]bool)
	var out []types.RelationshipRecord

	for _, m := range instantiationRE.FindAllStringSubmatch(body, -1) {
		if len(m) < 3 {
			continue
		}
		typeName := m[1]
		instName := m[2]

		// Skip keywords, built-ins, and the owner's own name.
		if verilogKeywords[typeName] || verilogKeywords[strings.ToLower(typeName)] {
			continue
		}
		if typeName == ownerName {
			continue
		}
		// Skip names that look like statement keywords in lowercase.
		lower := strings.ToLower(typeName)
		if lower == "always" || lower == "initial" || lower == "assign" ||
			lower == "generate" || lower == "if" || lower == "else" ||
			lower == "for" || lower == "while" || lower == "case" {
			continue
		}
		// The instance name should not be a keyword.
		if verilogKeywords[instName] {
			continue
		}
		// Skip single-character names (likely parameters like N, M).
		if len(typeName) <= 1 {
			continue
		}
		// Skip names starting with lowercase unless they contain underscore (module names
		// typically start uppercase or use underscore_style).
		// Allow any identifier that could reasonably be a module name.

		key := typeName + "." + instName
		if seen[key] {
			continue
		}
		seen[key] = true

		out = append(out, types.RelationshipRecord{
			ToID: typeName,
			Kind: "USES",
		})
	}
	return out
}

// -----------------------------------------------------------------------
// Body extraction helpers
// -----------------------------------------------------------------------

// extractKeywordBody extracts text between a declaration and its matching
// end keyword (endmodule, endfunction, endtask, etc.).
// afterPos is the position in src just after the opening declaration semicolon.
// Returns (body, endLine) where endLine is 1-based from the start of src.
func extractKeywordBody(src string, afterPos int, endKeyword string) (string, int) {
	if afterPos >= len(src) {
		return "", 0
	}
	rest := src[afterPos:]
	// Build open keywords that increment depth (begin/fork).
	openRE := regexp.MustCompile(`\b(begin|fork)\b`)
	// The specific end keyword for the outer block.
	endRE := regexp.MustCompile(`\b` + regexp.QuoteMeta(endKeyword) + `\b`)
	// Generic nested "end" to balance begin/fork.
	genericEndRE := regexp.MustCompile(`\bend\b`)

	depth := 0 // tracks begin/fork nesting
	i := 0
	for i < len(rest) {
		// Check for the end keyword first (it must be at depth==0).
		if loc := endRE.FindStringIndex(rest[i:]); loc != nil && loc[0] == 0 {
			body := rest[:i]
			endLine := strings.Count(src[:afterPos+i+loc[1]], "\n") + 1
			return body, endLine
		}
		// Check for begin/fork (open a depth).
		if loc := openRE.FindStringIndex(rest[i:]); loc != nil && loc[0] == 0 {
			depth++
			i += loc[1]
			continue
		}
		// Check for generic "end" (closes depth if depth>0).
		if depth > 0 {
			if loc := genericEndRE.FindStringIndex(rest[i:]); loc != nil && loc[0] == 0 {
				depth--
				i += loc[1]
				continue
			}
		}
		i++
	}
	// Fallback: return all of rest.
	return rest, 0
}

// -----------------------------------------------------------------------
// Comment and string stripping
// -----------------------------------------------------------------------

// stripCommentsAndStrings replaces Verilog/SV // and /* */ comments and string
// literals with spaces so regexes don't match inside them.
func stripCommentsAndStrings(src string) string {
	out := make([]byte, len(src))
	copy(out, src)
	i := 0
	for i < len(src) {
		ch := src[i]

		// Single-line comment: // ...
		if ch == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				out[i] = ' '
				i++
			}
			continue
		}

		// Block comment: /* ... */
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
				if src[i] == '\\' && i+1 < len(src) {
					out[i] = ' '
					out[i+1] = ' '
					i += 2
					continue
				}
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
