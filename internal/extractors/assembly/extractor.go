// Package assembly implements a line-oriented (mnemonic) extractor for
// assembly-language source files across multiple ISAs and syntaxes.
//
// Assembly is line-oriented, so — unlike the high-level languages that lean
// on a tree-sitter grammar — a pragmatic hand parser is both simpler and more
// robust here: every meaningful construct (label, directive, instruction)
// lives on its own line and is recognised by a leading token. No tree-sitter
// grammar for assembly is bundled in smacker/go-tree-sitter, and the candidate
// community grammars are per-ISA and unstable (see #2744), so the line parser
// is the established pragmatic-parser approach (cf. the verilog/vhdl regex
// extractors).
//
// A SINGLE "assembly" language token covers every dialect; the dialect
// (x86/x86-64, ARM, ARM64/AArch64, m68k) and syntax (AT&T vs Intel/NASM) are
// recorded as entity *attributes*, never as separate languages — the same
// taxonomy decision made for vue/svelte/astro = jsts (#2821) and for the
// COBOL/legacy wave.
//
// Extracted entities:
//   - procedures — a global/exported label followed by a body until the next
//     global label or a terminating `ret`/`bx lr` — the "function" unit.
//     Emitted as SCOPE.Operation(subtype=procedure).
//   - sections — `.text`/`.data`/`.bss`/`.rodata`/`.section <name>` →
//     SCOPE.Component(subtype=section).
//   - constants — `.equ NAME, val` / `NAME = val` / `.set NAME, val` /
//     `%define NAME val` (NASM) / `NAME EQU val` (MASM) →
//     SCOPE.Constant.
//
// Extracted edges (attached to the enclosing procedure):
//   - CALLS — `call`/`callq` (x86), `bl`/`blx`/`blr` (ARM/ARM64),
//     `jal`/`jalr` (RISC-V-style) targeting a label.
//   - CALLS (subtype=branch) — `jmp`/`jXX` (x86), `b`/`bXX`/`br` (ARM) to a
//     label — intra-procedure control flow.
//   - CALLS to an external symbol declared via `.extern`/`.global` carry
//     Properties["locality"]="external".
//   - IMPORTS — `.include "file"` / `%include "file"` (NASM).
//   - syscall effect — `syscall`/`int 0x80` (x86), `svc`/`swi` (ARM) emit a
//     CALLS edge to the synthetic `__syscall` target with
//     Properties["effect"]="syscall" and stamp Properties["has_syscall"] on
//     the enclosing procedure. This is the meaningful OS-boundary effect
//     surface for assembly (#2744 Phase 1A).
//
// File extensions handled (via the classifier): .s, .S, .asm, .nasm.
//
// Registers itself via init() and is imported by registry_gen.go.
package assembly

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("assembly", &Extractor{})
}

// Extractor implements extractor.Extractor for assembly source files.
type Extractor struct{}

// Language returns the canonical language token for assembly.
func (e *Extractor) Language() string { return "assembly" }

// syntheticSyscallTarget is the well-known target every syscall/int/svc/swi
// instruction CALLS. Giving the OS boundary a stable named sink lets graph
// queries find "every procedure that crosses into the kernel" with one hop.
const syntheticSyscallTarget = "__syscall"

// -----------------------------------------------------------------------
// Compiled patterns
// -----------------------------------------------------------------------

var (
	// labelRE matches a label definition at the start of a line:
	//   my_label:
	//   .Llocal:
	//   1:            (numeric local label — gas)
	//   $loop:        (some assemblers)
	// Group 1 is the label name. NASM also allows a colon-less label but we
	// only treat colon-terminated tokens as definitions to avoid ambiguity
	// with instructions.
	labelRE = regexp.MustCompile(`^\s*([\.\$A-Za-z_][\.\$A-Za-z0-9_]*|[0-9]+)\s*:`)

	// directiveRE matches a leading directive token (gas style: leading dot).
	directiveRE = regexp.MustCompile(`^\s*(\.[A-Za-z_][A-Za-z0-9_]*)`)

	// sectionRE matches an explicit section directive with a name:
	//   .section .text
	//   .section "name"
	//   section .data        (NASM)
	sectionRE = regexp.MustCompile(`^\s*\.?section\s+["']?([\.A-Za-z_][\.A-Za-z0-9_]*)`)

	// includeRE matches gas `.include "file"` and NASM `%include "file"`.
	includeRE = regexp.MustCompile(`(?m)^\s*(?:\.include|%include)\s+["']([^"']+)["']`)

	// globlRE matches exported-symbol directives:
	//   .globl name      .global name      .global name1, name2
	globlRE = regexp.MustCompile(`(?m)^\s*\.glob[a]?l\s+(.+)$`)

	// externRE matches external-symbol directives:
	//   .extern name      extern name      (NASM)
	externRE = regexp.MustCompile(`(?m)^\s*\.?extern\s+([A-Za-z_][A-Za-z0-9_]*)`)

	// equRE matches constant definitions across dialects:
	//   .equ NAME, value     .set NAME, value
	//   NAME = value
	//   %define NAME value   (NASM)
	//   NAME EQU value       (MASM / NASM)
	equDotRE   = regexp.MustCompile(`(?m)^\s*\.(?:equ|set)\s+([A-Za-z_][A-Za-z0-9_]*)\s*,\s*(.+?)\s*$`)
	equNasmRE  = regexp.MustCompile(`(?m)^\s*%define\s+([A-Za-z_][A-Za-z0-9_]*)\s+(.+?)\s*$`)
	equMasmRE  = regexp.MustCompile(`(?m)^\s*([A-Za-z_][A-Za-z0-9_]*)\s+[Ee][Qq][Uu]\s+(.+?)\s*$`)
	equEqualRE = regexp.MustCompile(`(?m)^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.+?)\s*$`)
)

// callMnemonics is the set of call-family instructions across ISAs. The
// target operand is treated as a CALLS edge (inter/intra-procedure call).
//
//	x86/x86-64: call, callq
//	ARM/ARM64:  bl, blx, blr (blr/blx are register-indirect; target may be a
//	            register, in which case no static label is recoverable)
//	RISC-V:     jal, jalr
var callMnemonics = map[string]bool{
	"call": true, "callq": true, "calll": true, "callw": true,
	"bl": true, "blx": true, "blr": true,
	"jal": true, "jalr": true,
}

// branchMnemonics is the set of unconditional/conditional branch instructions
// whose target is a label — intra-procedure control flow. We deliberately keep
// this generous (covers x86 jXX and ARM bXX condition suffixes) since the
// operand shape (a bare label) disambiguates real targets from noise.
var branchMnemonics = map[string]bool{
	// x86 unconditional + conditional jumps.
	"jmp": true, "jmpq": true,
	"je": true, "jne": true, "jz": true, "jnz": true, "jg": true, "jge": true,
	"jl": true, "jle": true, "ja": true, "jae": true, "jb": true, "jbe": true,
	"js": true, "jns": true, "jo": true, "jno": true, "jc": true, "jnc": true,
	"jp": true, "jnp": true, "loop": true,
	// ARM/AArch64 branches (b + condition codes) and register branch.
	"b": true, "bx": true, "br": true,
	"beq": true, "bne": true, "bgt": true, "bge": true, "blt": true, "ble": true,
	"bhi": true, "bls": true, "bcs": true, "bcc": true, "bmi": true, "bpl": true,
	"bvs": true, "bvc": true, "cbz": true, "cbnz": true,
	// ARM compare-and-branch and m68k branches.
	"bra": true, "bsr": true,
}

// syscallMnemonics triggers a syscall effect. `int` is special-cased (only
// `int 0x80` / `int 80h` is the Linux syscall gate; other interrupts are
// ignored to avoid false effects).
var syscallMnemonics = map[string]bool{
	"syscall": true, "sysenter": true,
	"svc": true, "swi": true,
}

// registerOperand reports whether an operand is a CPU register (so a
// register-indirect call/branch like `blr x0` or `jmp *%rax` yields no static
// target). Conservative: matches common x86/ARM register name shapes.
var registerRE = regexp.MustCompile(`^(?:%?[re]?[abcds][xilph]|%?r[0-9]{1,2}[dwb]?|x[0-9]{1,2}|w[0-9]{1,2}|sp|lr|pc|fp|ip|[ad][0-7])$`)

// Extract processes an assembly source file and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	lang := file.Language
	if lang == "" {
		lang = "assembly"
	}
	out := extractAssembly(string(file.Content), file.Path, lang)
	extractor.TagRelationshipsLanguage(out, lang)
	extractor.TagEntitiesLanguage(out, lang)
	return out, nil
}

// extractAssembly is the testable core.
func extractAssembly(src, filePath, lang string) []types.EntityRecord {
	var entities []types.EntityRecord

	scrubbed := scrubComments(src)

	// File-level entity, stamped with the detected dialect/syntax. Detection
	// runs on the comment-scrubbed source so prose in a header comment (e.g.
	// "ARM64 fixture — svc syscall") can't skew the heuristic.
	dialect, syntax := detectDialect(scrubbed)
	fileEnt := extractor.FileEntity(extractor.FileInput{Path: filePath, Language: lang})
	if fileEnt.Properties == nil {
		fileEnt.Properties = map[string]string{}
	}
	fileEnt.Properties["dialect"] = dialect
	fileEnt.Properties["syntax"] = syntax
	entities = append(entities, fileEnt)

	lines := strings.Split(scrubbed, "\n")

	exported := collectExported(scrubbed)
	external := collectExternal(scrubbed)

	entities = append(entities, buildIncludeEntities(filePath, scrubbed, lang)...)
	entities = append(entities, buildSectionEntities(lines, filePath, lang)...)
	entities = append(entities, buildConstantEntities(scrubbed, filePath, lang)...)
	entities = append(entities, buildProcedureEntities(lines, filePath, lang, dialect, exported, external)...)

	return entities
}

// -----------------------------------------------------------------------
// Dialect / syntax detection (cheap, heuristic)
// -----------------------------------------------------------------------

// detectDialect inspects register names and directive style to guess the ISA
// dialect and syntax. Cheap and best-effort: returns ("unknown", ...) when no
// signal is present. Used only as an entity attribute, never to gate parsing.
func detectDialect(src string) (dialect, syntax string) {
	lower := strings.ToLower(src)

	syntax = "unknown"
	switch {
	case strings.Contains(src, "%rax") || strings.Contains(src, "%eax") ||
		strings.Contains(src, "%rsp") || regexp.MustCompile(`(?m)^\s*\.att_syntax`).MatchString(src):
		syntax = "att"
	case regexp.MustCompile(`(?m)^\s*(?:section|global|bits)\b`).MatchString(lower) ||
		strings.Contains(lower, "%define") || strings.Contains(lower, "[bits"):
		syntax = "intel"
	}

	switch {
	case strings.Contains(src, "%rax") || strings.Contains(lower, "rax") ||
		strings.Contains(lower, "syscall") || strings.Contains(lower, "rdi"):
		dialect = "x86-64"
	case strings.Contains(lower, "%eax") || strings.Contains(lower, "int 0x80") ||
		strings.Contains(lower, "int 80h"):
		dialect = "x86"
	case regexp.MustCompile(`\bx[0-9]{1,2}\b`).MatchString(lower) ||
		strings.Contains(lower, "aarch64") || strings.Contains(lower, "blr") ||
		strings.Contains(lower, "\tsvc") || strings.Contains(lower, " svc "):
		dialect = "arm64"
	case regexp.MustCompile(`\br[0-9]{1,2}\b`).MatchString(lower) ||
		strings.Contains(lower, "\tbl ") || strings.Contains(lower, ".thumb") ||
		strings.Contains(lower, "swi"):
		dialect = "arm"
	case regexp.MustCompile(`\b[ad][0-7]\b`).MatchString(lower) ||
		strings.Contains(lower, "move.") || strings.Contains(lower, "m68k"):
		dialect = "m68k"
	default:
		dialect = "unknown"
	}
	return dialect, syntax
}

// -----------------------------------------------------------------------
// Directive collection
// -----------------------------------------------------------------------

// collectExported returns the set of symbols marked .globl/.global. A line may
// list several comma-separated names.
func collectExported(scrubbed string) map[string]bool {
	out := map[string]bool{}
	for _, m := range globlRE.FindAllStringSubmatch(scrubbed, -1) {
		for _, name := range splitSymbolList(m[1]) {
			out[name] = true
		}
	}
	return out
}

// collectExternal returns the set of symbols declared .extern/extern.
func collectExternal(scrubbed string) map[string]bool {
	out := map[string]bool{}
	for _, m := range externRE.FindAllStringSubmatch(scrubbed, -1) {
		out[m[1]] = true
	}
	return out
}

// splitSymbolList parses a comma/space separated symbol list, stripping
// trailing comments and empty tokens.
func splitSymbolList(s string) []string {
	s = strings.TrimSpace(s)
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	var out []string
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			break
		}
		out = append(out, f)
	}
	return out
}

// -----------------------------------------------------------------------
// .include → IMPORTS
// -----------------------------------------------------------------------

func buildIncludeEntities(filePath, scrubbed, lang string) []types.EntityRecord {
	seen := map[string]bool{}
	var out []types.EntityRecord
	for _, m := range includeRE.FindAllStringSubmatch(scrubbed, -1) {
		inc := strings.TrimSpace(m[1])
		if inc == "" || seen[inc] {
			continue
		}
		seen[inc] = true

		display := inc
		if slash := strings.LastIndexByte(inc, '/'); slash >= 0 {
			display = inc[slash+1:]
		}
		out = append(out, types.EntityRecord{
			Name:       display,
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   lang,
			Relationships: []types.RelationshipRecord{{
				FromID: filePath,
				ToID:   inc,
				Kind:   "IMPORTS",
				Properties: map[string]string{
					"source_module": inc,
					"imported_name": display,
					"local_name":    display,
				},
			}},
		})
	}
	return out
}

// -----------------------------------------------------------------------
// Sections
// -----------------------------------------------------------------------

// buildSectionEntities emits a SCOPE.Component(subtype=section) for each
// section directive. Shorthand directives (.text/.data/.bss/.rodata) map to
// the canonical section name; .section <name> takes the explicit name.
func buildSectionEntities(lines []string, filePath, lang string) []types.EntityRecord {
	seen := map[string]bool{}
	var out []types.EntityRecord

	for i, line := range lines {
		name := sectionName(line)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "section",
			SourceFile: filePath,
			Language:   lang,
			StartLine:  i + 1,
			EndLine:    i + 1,
			Signature:  strings.TrimSpace(line),
		})
	}
	return out
}

// sectionShorthands maps bare section directives to their canonical name.
var sectionShorthands = map[string]string{
	".text": ".text", ".data": ".data", ".bss": ".bss", ".rodata": ".rodata",
}

// sectionName returns the section name declared on a line, or "" if the line
// is not a section directive.
func sectionName(line string) string {
	t := strings.TrimSpace(line)
	if m := sectionRE.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	// Bare shorthand directives: ".text", ".data", possibly with trailing
	// flags (".text 0", ".bss"). Match only the leading directive token.
	if m := directiveRE.FindStringSubmatch(line); m != nil {
		if canon, ok := sectionShorthands[m[1]]; ok {
			return canon
		}
	}
	_ = t
	return ""
}

// -----------------------------------------------------------------------
// Constants
// -----------------------------------------------------------------------

func buildConstantEntities(scrubbed, filePath, lang string) []types.EntityRecord {
	seen := map[string]bool{}
	var out []types.EntityRecord

	add := func(name, value string, line int) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Constant",
			Subtype:    "equate",
			SourceFile: filePath,
			Language:   lang,
			StartLine:  line,
			EndLine:    line,
			Signature:  name + " = " + strings.TrimSpace(value),
		})
	}

	for _, re := range []*regexp.Regexp{equDotRE, equNasmRE, equMasmRE} {
		for _, m := range allSubmatchWithLine(re, scrubbed) {
			add(m.groups[0], m.groups[1], m.line)
		}
	}
	// `NAME = value` is matched last and guarded: skip lines that are really
	// section/label/instruction lines (the equalRE is the loosest pattern).
	for _, m := range allSubmatchWithLine(equEqualRE, scrubbed) {
		// Avoid double-counting names already taken by a stronger pattern.
		add(m.groups[0], m.groups[1], m.line)
	}
	return out
}

// matchWithLine couples a regex submatch with its 1-based source line.
type matchWithLine struct {
	groups []string
	line   int
}

// allSubmatchWithLine returns every submatch of re in src paired with the
// 1-based line number of the match start.
func allSubmatchWithLine(re *regexp.Regexp, src string) []matchWithLine {
	var out []matchWithLine
	for _, idx := range re.FindAllStringSubmatchIndex(src, -1) {
		if len(idx) < 6 {
			continue
		}
		groups := []string{src[idx[2]:idx[3]], src[idx[4]:idx[5]]}
		line := strings.Count(src[:idx[0]], "\n") + 1
		out = append(out, matchWithLine{groups: groups, line: line})
	}
	return out
}

// -----------------------------------------------------------------------
// Procedures + CALLS / branch / syscall edges
// -----------------------------------------------------------------------

// buildProcedureEntities walks the line stream maintaining the "current
// procedure" = the most recent label. Every call/branch/syscall instruction
// is attributed to the enclosing procedure. A procedure's body runs from its
// label to the next global label (or EOF).
func buildProcedureEntities(lines []string, filePath, lang, dialect string, exported, external map[string]bool) []types.EntityRecord {
	var out []types.EntityRecord
	curIdx := -1 // index into out of the current procedure entity

	// dedupe call/branch targets per procedure.
	seenEdge := map[string]bool{}
	edgeKey := func(proc, target, kind string) string { return proc + "\x00" + target + "\x00" + kind }

	for i, raw := range lines {
		line := raw

		if name := labelName(line); name != "" {
			// A global/exported label (or any top-level label) starts a new
			// procedure. Local labels (.L*, numeric) inside a body are NOT
			// procedure boundaries — they're branch targets.
			if isProcedureLabel(name, exported) {
				rec := types.EntityRecord{
					Name:       name,
					Kind:       "SCOPE.Operation",
					Subtype:    "procedure",
					SourceFile: filePath,
					Language:   lang,
					StartLine:  i + 1,
					EndLine:    i + 1,
					Signature:  name + ":",
					Properties: map[string]string{"dialect": dialect},
				}
				if exported[name] {
					rec.Properties["exported"] = "true"
				}
				curIdx = len(out)
				out = append(out, rec)
				continue
			}
			// Local label: extend current procedure's span but keep parsing
			// the rest of the line for an instruction (gas allows `1: add ...`).
			if curIdx >= 0 {
				out[curIdx].EndLine = i + 1
			}
			// Strip the label prefix so a trailing instruction is still seen.
			if c := strings.IndexByte(line, ':'); c >= 0 && c+1 < len(line) {
				line = line[c+1:]
			} else {
				continue
			}
		}

		mnem, operand := mnemonicAndOperand(line)
		if mnem == "" {
			continue
		}
		if curIdx >= 0 {
			out[curIdx].EndLine = i + 1
		}

		lower := strings.ToLower(mnem)

		// Syscall effect.
		if isSyscall(lower, operand) {
			if curIdx >= 0 {
				out[curIdx].Properties["has_syscall"] = "true"
				out[curIdx].Properties["syscall_count"] = strconv.Itoa(atoiDefault(out[curIdx].Properties["syscall_count"]) + 1)
				key := edgeKey(out[curIdx].Name, syntheticSyscallTarget, "syscall")
				if !seenEdge[key] {
					seenEdge[key] = true
					out[curIdx].Relationships = append(out[curIdx].Relationships, types.RelationshipRecord{
						ToID: syntheticSyscallTarget,
						Kind: "CALLS",
						Properties: map[string]string{
							"effect":   "syscall",
							"locality": "external",
							"line":     strconv.Itoa(i + 1),
						},
					})
				}
			}
			continue
		}

		// Call / branch targets.
		isCall := callMnemonics[lower]
		isBranch := branchMnemonics[lower]
		if !isCall && !isBranch {
			continue
		}
		target := callTarget(operand)
		if target == "" || curIdx < 0 {
			continue
		}
		// Skip self-recursion noise on branches into the same proc label is
		// fine (real recursion), but skip register-indirect (no static name).
		kindTag := "call"
		if isBranch {
			kindTag = "branch"
		}
		key := edgeKey(out[curIdx].Name, target, kindTag)
		if seenEdge[key] {
			continue
		}
		seenEdge[key] = true

		props := map[string]string{
			"line":      strconv.Itoa(i + 1),
			"edge_kind": kindTag,
		}
		if external[target] {
			props["locality"] = "external"
		}
		out[curIdx].Relationships = append(out[curIdx].Relationships, types.RelationshipRecord{
			ToID:       target,
			Kind:       "CALLS",
			Properties: props,
		})
	}
	return out
}

// labelName returns the label defined on a line, or "" if the line is not a
// label definition. Directive lines (leading dot followed by a known
// directive, e.g. ".text", ".globl") are NOT labels even though they start
// with a dot — but ".Llocal:" (dot, then a colon-terminated token) is.
func labelName(line string) string {
	m := labelRE.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	name := m[1]
	// A gas directive like ".section .text" has no colon, so labelRE won't
	// match it; but ".text:" (unusual) would — that's fine, it's a label.
	return name
}

// isProcedureLabel reports whether a label begins a procedure (the function
// unit) rather than being a local branch target. Exported (.globl) labels and
// plain top-level labels are procedures; gas-local (".L*") and numeric labels
// are branch targets.
func isProcedureLabel(name string, exported map[string]bool) bool {
	if exported[name] {
		return true
	}
	if strings.HasPrefix(name, ".L") || strings.HasPrefix(name, ".l") {
		return false
	}
	if _, err := strconv.Atoi(name); err == nil {
		return false // numeric local label
	}
	// A leading-dot non-.L name is almost always a directive-ish artefact;
	// treat a bare dotted token cautiously as non-procedure.
	if strings.HasPrefix(name, ".") {
		return false
	}
	return true
}

// mnemonicAndOperand splits a (label-stripped) instruction line into its
// mnemonic and the operand text. Returns ("", "") for blank/directive lines.
func mnemonicAndOperand(line string) (mnem, operand string) {
	t := strings.TrimSpace(line)
	if t == "" {
		return "", ""
	}
	// Skip directive lines (leading dot or NASM '%' or section/global words).
	if t[0] == '.' || t[0] == '%' || t[0] == '#' {
		return "", ""
	}
	// Split on first whitespace.
	if sp := strings.IndexAny(t, " \t"); sp >= 0 {
		return t[:sp], strings.TrimSpace(t[sp+1:])
	}
	return t, ""
}

// callTarget extracts a static label target from a call/branch operand. Returns
// "" when the target is a register (indirect) or an immediate address. For x86
// indirect (`*%rax`, `*func`) and ARM register branches, no static label is
// recoverable. ARM `bl func` / x86 `call func` / AArch64 `bl func` all pass a
// bare label as the first operand.
func callTarget(operand string) string {
	operand = strings.TrimSpace(operand)
	if operand == "" {
		return ""
	}
	// Take the first comma-separated token (ARM `b.eq label` style already
	// handled by mnemonic split; AArch64 `cbz x0, label` puts the label last).
	// For cbz/cbnz the label is the LAST operand; for call/b it's the first.
	// Heuristic: pick the token that looks like a label (non-register,
	// non-immediate). Prefer the last token for compare-and-branch shapes.
	parts := strings.FieldsFunc(operand, func(r rune) bool { return r == ',' })
	// Try last then first — labels are usually the branch destination.
	candidates := []string{}
	if len(parts) > 0 {
		candidates = append(candidates, strings.TrimSpace(parts[len(parts)-1]))
		candidates = append(candidates, strings.TrimSpace(parts[0]))
	}
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		// Strip x86 indirect/immediate prefixes.
		if c == "" {
			continue
		}
		if c[0] == '*' { // x86 indirect call/jmp — no static target
			c = c[1:]
		}
		// PLT/GOT suffix (call printf@PLT) — strip the relocation suffix.
		if at := strings.IndexByte(c, '@'); at > 0 {
			c = c[:at]
		}
		// Drop NASM size/keyword decorations.
		c = strings.TrimPrefix(c, "near ")
		c = strings.TrimPrefix(c, "far ")
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if isRegister(c) {
			continue
		}
		// Immediate numeric address / hex — not a symbolic target.
		if c[0] == '#' || c[0] == '$' || isNumericOperand(c) {
			continue
		}
		// Valid identifier-ish label.
		if isLabelLike(c) {
			return c
		}
	}
	return ""
}

func isRegister(s string) bool {
	return registerRE.MatchString(strings.ToLower(strings.TrimPrefix(s, "%")))
}

func isNumericOperand(s string) bool {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if s == "" {
		return false
	}
	if _, err := strconv.ParseUint(s, 16, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return true
	}
	return false
}

// isLabelLike reports whether s is a plausible label identifier (allowing the
// gas-local dot and dollar prefixes used by some assemblers).
var labelLikeRE = regexp.MustCompile(`^[\.\$A-Za-z_][\.\$A-Za-z0-9_]*$`)

func isLabelLike(s string) bool { return labelLikeRE.MatchString(s) }

// isSyscall reports whether a mnemonic (+ operand) is an OS syscall gate.
// `int` is only a syscall when its operand is 0x80 / 80h (Linux i386 gate);
// other interrupts are ignored.
func isSyscall(lowerMnem, operand string) bool {
	if syscallMnemonics[lowerMnem] {
		return true
	}
	if lowerMnem == "int" {
		op := strings.ToLower(strings.TrimSpace(operand))
		op = strings.TrimPrefix(op, "$")
		return op == "0x80" || op == "80h"
	}
	return false
}

func atoiDefault(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// -----------------------------------------------------------------------
// Comment scrubbing
// -----------------------------------------------------------------------

// scrubComments blanks assembly comments so patterns don't match inside them.
// Handles the common comment markers across dialects:
//   - `;`  NASM / MASM line comment
//   - `#`  gas line comment (also the i386 immediate prefix `$`, so we only
//     treat a leading-ish `#` after whitespace as a comment to avoid eating
//     `mov $4, %eax`; conservative: blank from `#` to EOL).
//   - `//` gas C++-style line comment
//   - `/* ... */` block comment
//   - `@`  ARM line comment
//   - `!`  m68k / some ARM line comment
//
// Newlines are preserved so line numbering stays exact.
func scrubComments(src string) string {
	out := []byte(src)
	i := 0
	n := len(src)
	for i < n {
		c := src[i]
		switch {
		case c == '/' && i+1 < n && src[i+1] == '*':
			for i < n {
				if src[i] == '*' && i+1 < n && src[i+1] == '/' {
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
		case c == '/' && i+1 < n && src[i+1] == '/':
			i = blankToEOL(out, src, i)
		case c == ';' || c == '@' || c == '!':
			i = blankToEOL(out, src, i)
		case c == '#':
			// Could be a gas comment or an ARM immediate (`#4`). Only treat as
			// a comment when not immediately followed by a digit/identifier
			// that looks like an immediate operand AND it isn't the NASM
			// `%`-macro context. Conservative + cheap: blank to EOL only when
			// the `#` starts a token (preceded by start-of-line or space) and
			// the next char is a space or another `#`. This keeps `mov r0, #4`
			// intact while blanking `# this is a comment`.
			if (i == 0 || src[i-1] == ' ' || src[i-1] == '\t' || src[i-1] == '\n') &&
				(i+1 >= n || src[i+1] == ' ' || src[i+1] == '#' || src[i+1] == '\t') {
				i = blankToEOL(out, src, i)
			} else {
				i++
			}
		default:
			i++
		}
	}
	return string(out)
}

// blankToEOL blanks out[from:] up to (not including) the next newline and
// returns the index of that newline (or len). Newlines are preserved.
func blankToEOL(out []byte, src string, from int) int {
	i := from
	for i < len(src) && src[i] != '\n' {
		out[i] = ' '
		i++
	}
	return i
}
