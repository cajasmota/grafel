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
// (x86/x86-64, ARM, ARM64/AArch64, RISC-V, m68k) and syntax (AT&T vs
// Intel/NASM) are
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
//   - local-label anchors — `.L*`, numeric, and other non-procedure labels →
//     SCOPE.CodeBlock(subtype=label). These are the intra-file branch
//     destinations; emitting them lets a branch edge resolve to a concrete
//     in-file entity rather than dangling on a bare name (#2836).
//
// Extracted edges (attached to the enclosing procedure):
//   - CALLS — `call`/`callq` (x86), `bl`/`blx`/`blr` (ARM/ARM64),
//     `jal`/`jalr` (RISC-V-style), `jsr`/`bsr` (m68k) targeting a label.
//   - CALLS (edge_kind=branch) — `jmp`/`jXX` (x86), `b`/`bXX`/`br`/`cbz`
//     (ARM/ARM64), `bra`/`Bcc`/`dbra` (m68k) to a label — control flow.
//     Branch ToIDs that name a local label are rewritten to a file-scoped
//     structural-ref stub so the resolver binds them intra-file (#2836).
//   - CALLS edge classification (#2836): a self-targeting call/branch carries
//     Properties["recursion"]="self"; an unconditional branch to another
//     procedure carries Properties["tail_call"]="true".
//   - CALLS to an external symbol declared via `.extern`/`.global` carry
//     Properties["locality"]="external".
//   - IMPORTS — `.include "file"` / `%include "file"` (NASM).
//   - syscall effect — `syscall`/`int 0x80` (x86), `svc`/`swi` (ARM),
//     `ecall` (RISC-V), `trap #0` (m68k) emit a CALLS edge to the synthetic
//     `__syscall` target
//     with Properties["effect"]="syscall" and stamp Properties["has_syscall"]
//     on the enclosing procedure. This is the meaningful OS-boundary effect
//     surface for assembly (#2744 Phase 1A).
//
// Operand parsing is syntax-agnostic across AT&T (gas) and Intel (NASM/MASM):
// register-indirect (`*%rax`, `(a0)`, `[rax]`), memory refs
// (`8(%rbp)`, `offset(%base,%index,scale)`, `[base+index*scale]`),
// immediates (`$imm`, `#imm`), PLT/GOT relocations (`printf@PLT`), and Intel
// size/distance keywords (`near`/`short`/`qword [..]`) are all recognised so
// call/branch target extraction is independent of dialect/syntax (#2835).
//
// File extensions handled (via the classifier): .s, .S, .asm, .nasm.
//
// Registers itself via init() and is imported by registry_gen.go.
package assembly

import (
	"context"
	"path/filepath"
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

	// masmIncludeRE matches the MASM/armasm structured include spellings (no
	// leading dot/percent, quotes optional): MASM `INCLUDE file` / `INCLUDELIB
	// lib`, and armasm `GET file` / `INCLUDE file` (#4950). The path runs to
	// end-of-line (comments are already scrubbed). INCLUDELIB names an import
	// library; both are modelled as IMPORTS so the linkage surfaces.
	masmIncludeRE = regexp.MustCompile(`(?mi)^\s*(?:include|includelib|get)\s+["']?([^"'\s][^"'\n]*?)["']?\s*$`)

	// globlRE matches exported-symbol directives:
	//   .globl name      .global name      .global name1, name2
	globlRE = regexp.MustCompile(`(?m)^\s*\.glob[a]?l\s+(.+)$`)

	// publicRE matches the MASM/armasm exported-symbol spellings (#4950):
	//   PUBLIC name1, name2     (MASM)     EXPORT name     (armasm)
	// Case-insensitive; a PUBLIC line may list several comma-separated names.
	publicRE = regexp.MustCompile(`(?mi)^\s*(?:public|export|global)\s+(.+)$`)

	// externRE matches external-symbol directives:
	//   .extern name      extern name      (NASM)
	externRE = regexp.MustCompile(`(?m)^\s*\.?extern\s+([A-Za-z_][A-Za-z0-9_]*)`)

	// masmExternRE matches the MASM/armasm external-symbol spellings (#4950):
	//   EXTERN name:type   EXTRN name:type   (MASM, optional :PROC/:DWORD type)
	//   IMPORT name        (armasm)
	// Case-insensitive; a list may be comma-separated. The optional `:type`
	// suffix per name is stripped by splitMasmSymbolList.
	masmExternRE = regexp.MustCompile(`(?mi)^\s*(?:extern|extrn|import)\s+(.+)$`)

	// procStartRE matches the MASM/armasm structured-procedure opener (#4950):
	//   name PROC            (MASM / armasm)
	//   name FUNCTION        (armasm alias)
	// The leading token is the procedure name (no trailing colon). PROC may be
	// followed by attributes (NEAR/FAR/visibility) which we ignore.
	procStartRE = regexp.MustCompile(`(?i)^\s*([A-Za-z_$][A-Za-z0-9_$]*)\s+(?:proc|function)\b`)

	// procEndRE matches the MASM/armasm procedure terminator (#4950):
	//   name ENDP   (MASM/armasm)    name ENDFUNC / ENDFUNC   (armasm)
	procEndRE = regexp.MustCompile(`(?i)^\s*(?:([A-Za-z_$][A-Za-z0-9_$]*)\s+)?(?:endp|endfunc)\b`)

	// areaRE matches the armasm section directive (#4950):
	//   AREA <name>, CODE, READONLY      AREA |.text|, DATA
	// The name may be bar-delimited (|.text|); group 1 is the raw name.
	areaRE = regexp.MustCompile(`(?i)^\s*area\s+\|?([^,|\s]+)\|?`)

	// masmStructRE matches the MASM STRUCT/STRUC record-type opener (#5055):
	//   name STRUCT          name STRUC          (both spellings)
	// Group 1 is the record-type name (no trailing colon). An optional
	// alignment/non-unique argument may follow STRUCT and is ignored. Closed by
	// a matching `name ENDS`. Modelled as SCOPE.Component(subtype=struct) to
	// parallel the struct/record types extracted for high-level langs (cpp/
	// csharp emit SCOPE.Component subtype=struct; erlang subtype=record).
	masmStructRE = regexp.MustCompile(`(?i)^\s*([A-Za-z_$@?][A-Za-z0-9_$@?]*)\s+struct?\b`)

	// masmSegmentRE matches the MASM SEGMENT directive opener (#5055):
	//   name SEGMENT                       (full segment definition)
	//   _DATA SEGMENT WORD PUBLIC 'DATA'   (with attributes — ignored)
	// Group 1 is the segment name; closed by a matching `name ENDS`. Modelled
	// as SCOPE.Component(subtype=section) to parallel the .text/.data sections
	// and the armasm AREA directive.
	masmSegmentRE = regexp.MustCompile(`(?i)^\s*([A-Za-z_$@?][A-Za-z0-9_$@?]*)\s+segment\b`)

	// masmEndsRE matches the MASM `name ENDS` terminator (#5055) that closes a
	// STRUCT/STRUC or SEGMENT block. The leading name is the block being closed.
	// `ENDS` (segment/struct end) is distinct from `ENDP` (procedure end) and
	// `END` (module end), so a dedicated anchored pattern avoids false hits.
	masmEndsRE = regexp.MustCompile(`(?i)^\s*([A-Za-z_$@?][A-Za-z0-9_$@?]*)\s+ends\b`)

	// equRE matches constant definitions across dialects:
	//   .equ NAME, value     .set NAME, value
	//   NAME = value
	//   %define NAME value   (NASM)
	//   NAME EQU value       (MASM / NASM)
	equDotRE   = regexp.MustCompile(`(?m)^\s*\.(?:equ|set)\s+([A-Za-z_][A-Za-z0-9_]*)\s*,\s*(.+?)\s*$`)
	equNasmRE  = regexp.MustCompile(`(?m)^\s*%define\s+([A-Za-z_][A-Za-z0-9_]*)\s+(.+?)\s*$`)
	// equMasmRE matches the column-1 EQU equate used by MASM/NASM and armasm:
	//   NAME EQU value          |bar.sym| EQU value      (#5056 bar-delimited)
	// Group 1 is the name (bars stripped), group 2 the value. armasm places the
	// label in column 1 with EQU as the second token — the same shape as MASM.
	equMasmRE  = regexp.MustCompile(`(?m)^\s*\|?([A-Za-z_][A-Za-z0-9_.$]*)\|?\s+[Ee][Qq][Uu]\s+(.+?)\s*$`)
	equEqualRE = regexp.MustCompile(`(?m)^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.+?)\s*$`)

	// dcdRE matches the armasm column-1 data-definition forms (#5056):
	//   LABEL DCD value     LABEL DCW value     LABEL DCB value     LABEL DCQ ...
	// armasm defines initialised data with the label in column 1 and a DCx
	// (Define-Constant) directive as the second token. The label may be
	// bar-delimited (`|tbl.entry| DCD 0`). Group 1 is the label (bars stripped),
	// group 2 the DCx directive (data width), group 3 the initialiser list.
	dcdRE = regexp.MustCompile(`(?im)^\s*\|?([A-Za-z_][A-Za-z0-9_.$]*)\|?\s+(DC[BWDQ]|DCFS|DCFD)\s+(.+?)\s*$`)
)

// callMnemonics is the set of call-family instructions across ISAs. The
// target operand is treated as a CALLS edge (inter/intra-procedure call).
//
//	x86/x86-64: call, callq
//	ARM/ARM64:  bl, blx, blr (blr/blx are register-indirect; target may be a
//	            register, in which case no static label is recoverable)
//	RISC-V:     jal, jalr
//	m68k:       jsr (jump-to-subroutine), bsr (branch-to-subroutine) — these
//	            are the call instructions; bsr also has size suffixes
//	            (bsr.w/bsr.s/bsr.l) stripped by normalizeMnemonic.
var callMnemonics = map[string]bool{
	"call": true, "callq": true, "calll": true, "callw": true,
	"bl": true, "blx": true, "blr": true,
	"jal": true, "jalr": true,
	"jsr": true, "bsr": true,
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
	"tbz": true, "tbnz": true,
	// m68k branches. `bra` is the unconditional branch; the Bcc family
	// (beq/bne/bcc/bcs/bhi/bls/bge/blt/bgt/ble/bpl/bmi/bvc/bvs/bhs/blo)
	// overlaps the ARM condition-suffix names above and is covered there.
	// m68k-specific: dbra/dbcc decrement-and-branch (loop primitive), and
	// the bhs/blo aliases. jmp is the unconditional jump (shared with x86).
	"bra": true, "bhs": true, "blo": true,
	"dbra": true, "dbf": true, "dbeq": true, "dbne": true, "dbcc": true,
	"dbcs": true, "dbhi": true, "dbls": true, "dbge": true, "dblt": true,
	"dbgt": true, "dble": true, "dbpl": true, "dbmi": true,
	// RISC-V branches. `j` is the unconditional jump (pseudo for `jal x0`);
	// the b-family compares two registers then a label (beq/bne/blt/bge/bltu/
	// bgeu) and the *z pseudos compare against x0 (beqz/bnez/blez/bgez/bltz/
	// bgtz). beq/bne/blt/bge/ble overlap the ARM/m68k names already present
	// above and are covered there; only the RISC-V-unique tokens are added.
	"j": true, "beqz": true, "bnez": true, "blez": true, "bgez": true,
	"bltz": true, "bgtz": true, "bltu": true, "bgeu": true, "bgtu": true,
	"bleu": true,
}

// syscallMnemonics triggers a syscall effect. `int` and `trap` are
// special-cased (see isSyscall): `int 0x80`/`int 80h` is the Linux i386
// syscall gate, and m68k `trap #0` is the Linux/m68k syscall gate — other
// interrupt/trap vectors are ignored to avoid false effects.
//
//	x86:    syscall, sysenter, int 0x80
//	ARM:    svc, swi (legacy 32-bit gate)
//	m68k:   trap #0 (Linux), and the unconditional trap variants
//	RISC-V: ecall (the environment-call instruction — the RISC-V OS/EE
//	        boundary gate; ebreak is the debugger trap, not an OS syscall,
//	        so it is intentionally excluded)
var syscallMnemonics = map[string]bool{
	"syscall": true, "sysenter": true,
	"svc": true, "swi": true,
	"ecall": true,
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
	entities = append(entities, buildMasmBlockEntities(lines, filePath, lang, dialect)...)
	entities = append(entities, buildConstantEntities(scrubbed, filePath, lang)...)
	entities = append(entities, buildDataEntities(scrubbed, filePath, lang, dialect, exported)...)
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
	case strings.Contains(lower, "ecall") || strings.Contains(lower, "riscv") ||
		regexp.MustCompile(`(?m)^\s*\.option\b`).MatchString(lower) ||
		regexp.MustCompile(`\b(?:jal|jalr)\b[^\n]*\bra\b`).MatchString(lower) ||
		regexp.MustCompile(`\b(?:lw|sw|ld|sd|addi|li|mv)\b[^\n]*\b(?:ra|sp|gp|tp|[at][0-7]|s[0-9]|s1[01])\b`).MatchString(lower):
		// RISC-V shares x-registers with AArch64 but is uniquely identified by
		// the ecall gate, the .option directive, the jal/jalr+ra return-address
		// idiom, or its ABI register file (ra/sp/gp/tp/a0-a7/t0-t6/s0-s11)
		// appearing as an instruction operand. Must precede the arm64 case below
		// (which would otherwise claim the shared `x[0-9]` register shape).
		dialect = "riscv"
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
	// MASM PUBLIC / armasm EXPORT (and the `GLOBAL name` spelling) — #4950.
	for _, m := range publicRE.FindAllStringSubmatch(scrubbed, -1) {
		for _, name := range splitMasmSymbolList(m[1]) {
			out[name] = true
		}
	}
	return out
}

// collectExternal returns the set of symbols declared .extern/extern, plus the
// MASM EXTERN/EXTRN and armasm IMPORT structured spellings (#4950).
func collectExternal(scrubbed string) map[string]bool {
	out := map[string]bool{}
	for _, m := range externRE.FindAllStringSubmatch(scrubbed, -1) {
		out[m[1]] = true
	}
	for _, m := range masmExternRE.FindAllStringSubmatch(scrubbed, -1) {
		for _, name := range splitMasmSymbolList(m[1]) {
			out[name] = true
		}
	}
	return out
}

// splitMasmSymbolList parses a MASM/armasm symbol list, stripping the optional
// per-name `:type` attribute (e.g. `EXTERN printf:PROC, exit:PROC` →
// {printf, exit}) and trailing scrubbed-comment whitespace.
func splitMasmSymbolList(s string) []string {
	var out []string
	for _, f := range splitSymbolList(s) {
		if colon := strings.IndexByte(f, ':'); colon > 0 {
			f = f[:colon]
		}
		f = strings.TrimSpace(f)
		if f != "" && isLabelLike(f) {
			out = append(out, f)
		}
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

	// gas `.include` / NASM `%include`, then the MASM/armasm structured
	// INCLUDE/INCLUDELIB/GET spellings (#4950). Both feed the same dedupe set.
	matches := includeRE.FindAllStringSubmatch(scrubbed, -1)
	matches = append(matches, masmIncludeRE.FindAllStringSubmatch(scrubbed, -1)...)
	for _, m := range matches {
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
	// armasm `AREA <name>, CODE|DATA` section framing (#4950).
	if m := areaRE.FindStringSubmatch(line); m != nil {
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
// MASM STRUCT / SEGMENT blocks (#5055)
// -----------------------------------------------------------------------

// buildMasmBlockEntities emits an entity for each MASM `name STRUCT`/`name
// STRUC` record-type definition and each `name SEGMENT` segment directive,
// both terminated by a matching `name ENDS`. A single linear scan tracks the
// one open block (MASM does not nest STRUCT/SEGMENT in practice) so the
// trailing `name ENDS` line can stamp the block's EndLine before the entity is
// emitted.
//
//   - `name STRUCT` / `name STRUC` → SCOPE.Component(subtype=struct), paralleling
//     the struct/record types extracted for high-level langs (cpp/csharp emit
//     SCOPE.Component subtype=struct; erlang subtype=record). #5055.
//   - `name SEGMENT`              → SCOPE.Component(subtype=section), paralleling
//     the .text/.data sections and the armasm AREA directive.
//
// These spellings have no leading dot or trailing colon, so neither the
// section nor the label/procedure paths claim them; the ENDS terminator is
// `name ENDS`, distinct from procedure `ENDP` and module `END`.
func buildMasmBlockEntities(lines []string, filePath, lang, dialect string) []types.EntityRecord {
	var out []types.EntityRecord

	// openBlock tracks the single currently-open STRUCT/SEGMENT, if any.
	type openBlock struct {
		name      string
		kind      string // "struct" | "section"
		startLine int
		signature string
	}
	var open *openBlock

	flush := func(endLine int) {
		if open == nil {
			return
		}
		out = append(out, types.EntityRecord{
			Name:       open.name,
			Kind:       "SCOPE.Component",
			Subtype:    open.kind,
			SourceFile: filePath,
			Language:   lang,
			StartLine:  open.startLine,
			EndLine:    endLine,
			Signature:  open.signature,
			Properties: map[string]string{"dialect": dialect, "framing": "masm"},
		})
		open = nil
	}

	for i, raw := range lines {
		line := raw

		// `name ENDS` closes the open STRUCT or SEGMENT. Tested first because a
		// SEGMENT name reused as the ENDS leading token must not be re-read as a
		// new opener. ENDS is anchored (not ENDP/END), so this is unambiguous.
		if m := masmEndsRE.FindStringSubmatch(line); m != nil {
			flush(i + 1)
			continue
		}
		if m := masmStructRE.FindStringSubmatch(line); m != nil {
			flush(i + 1) // close any unterminated prior block defensively
			open = &openBlock{name: m[1], kind: "struct", startLine: i + 1, signature: strings.TrimSpace(line)}
			continue
		}
		if m := masmSegmentRE.FindStringSubmatch(line); m != nil {
			flush(i + 1)
			open = &openBlock{name: m[1], kind: "section", startLine: i + 1, signature: strings.TrimSpace(line)}
			continue
		}
	}
	// An unterminated block (no closing ENDS) still emits, spanning to its
	// opener line, so a malformed/truncated file does not silently drop it.
	if open != nil {
		flush(open.startLine)
	}
	return out
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
			Kind:       string(types.EntityKindConstant),
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

// -----------------------------------------------------------------------
// Data definitions (armasm DCx)
// -----------------------------------------------------------------------

// buildDataEntities emits a SCOPE.Variable for each armasm column-1
// data-definition (`LABEL DCD value`, `LABEL DCB/DCW/DCQ/DCFS/DCFD ...`)
// (#5056). armasm places the label in column 1 with a DCx (Define-Constant)
// directive as the second token, defining initialised data — the parallel of
// the gas `.word`/`.byte` data the high-level data-entity model expects. The
// label may be bar-delimited (`|tbl.entry| DCD 0`); the bars are stripped by
// the capture. Each entity records the data width and initialiser via
// Properties, and is marked exported when EXPORT/GLOBAL named it.
func buildDataEntities(scrubbed, filePath, lang, dialect string, exported map[string]bool) []types.EntityRecord {
	seen := map[string]bool{}
	var out []types.EntityRecord

	for _, idx := range dcdRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(idx) < 8 {
			continue
		}
		name := strings.TrimSpace(scrubbed[idx[2]:idx[3]])
		width := strings.ToUpper(strings.TrimSpace(scrubbed[idx[4]:idx[5]]))
		initVal := strings.TrimSpace(scrubbed[idx[6]:idx[7]])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		line := strings.Count(scrubbed[:idx[0]], "\n") + 1
		props := map[string]string{
			"dialect":     dialect,
			"data_form":   "dcx",
			"data_width":  width,
			"initialiser": initVal,
		}
		if exported[name] {
			props["exported"] = "true"
		}
		out = append(out, types.EntityRecord{
			Name:       name,
			Kind:       string(types.EntityKindVariable),
			Subtype:    "data",
			SourceFile: filePath,
			Language:   lang,
			StartLine:  line,
			EndLine:    line,
			Signature:  name + " " + width + " " + initVal,
			Properties: props,
		})
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

	// Pre-scan local labels (#2836 intra-file branch resolution). Local labels
	// (`.L*`, numeric) are not procedures but are real branch destinations; we
	// emit each as a SCOPE.CodeBlock anchor so a branch CALLS edge can resolve
	// to a concrete in-file entity via the resolver's byLocation index, instead
	// of dangling on a bare name that might collide across files.
	localLabels := collectLocalLabels(lines, exported)

	// dedupe call/branch targets per procedure.
	seenEdge := map[string]bool{}
	edgeKey := func(proc, target, kind string) string { return proc + "\x00" + target + "\x00" + kind }

	for i, raw := range lines {
		line := raw

		// MASM/armasm structured procedure framing (#4950): `name PROC` /
		// `name FUNCTION` opens a procedure with no trailing colon, which
		// labelRE would miss. `name ENDP` / `ENDFUNC` closes it (we record the
		// EndLine and clear the current procedure so a following body line is
		// not mis-attributed). These spellings coexist with colon-labels.
		if m := procStartRE.FindStringSubmatch(line); m != nil {
			name := m[1]
			rec := types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Operation",
				Subtype:    "procedure",
				SourceFile: filePath,
				Language:   lang,
				StartLine:  i + 1,
				EndLine:    i + 1,
				Signature:  strings.TrimSpace(line),
				Properties: map[string]string{"dialect": dialect, "framing": "proc"},
			}
			if exported[name] {
				rec.Properties["exported"] = "true"
			}
			curIdx = len(out)
			out = append(out, rec)
			continue
		}
		if procEndRE.MatchString(line) {
			if curIdx >= 0 {
				out[curIdx].EndLine = i + 1
			}
			curIdx = -1
			continue
		}

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
			// Local label: emit a SCOPE.CodeBlock anchor (the branch target,
			// #2836) and extend the current procedure's span. The anchor is
			// scoped to this file via SourceFile so the resolver binds branch
			// edges intra-file even when the same `.L*` name recurs elsewhere.
			out = append(out, types.EntityRecord{
				Name:       name,
				Kind:       string(types.EntityKindCodeBlock),
				Subtype:    "label",
				SourceFile: filePath,
				Language:   lang,
				StartLine:  i + 1,
				EndLine:    i + 1,
				Signature:  name + ":",
				Properties: map[string]string{"dialect": dialect, "local": "true"},
			})
			// curIdx still indexes the enclosing procedure — anchors are
			// appended after it and never become the "current procedure", so
			// out[curIdx] continues to reference the right entity.
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

		lower := normalizeMnemonic(mnem)

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

		// #2836 — edge classification.
		// Self-recursion: a call/branch whose target is the enclosing
		// procedure's own label.
		if target == out[curIdx].Name {
			props["recursion"] = "self"
		}
		// Tail call: an unconditional branch (jmp/bra/b) to ANOTHER
		// procedure (not a local label, not self) is a tail call — control
		// transfers without a return frame.
		if isBranch && !localLabels[target] && target != out[curIdx].Name &&
			(lower == "jmp" || lower == "jmpq" || lower == "bra" || lower == "b" || lower == "j") {
			props["tail_call"] = "true"
		}

		// #2836 — intra-file branch-target resolution. When the destination
		// is a local label, rewrite the ToID to a file-scoped structural-ref
		// stub (Format A) so the resolver binds it to THIS file's anchor via
		// byLocation, never to a same-named local label in another file.
		toID := target
		if localLabels[target] {
			toID = localLabelStub(lang, filePath, target)
			props["resolution"] = "intra_file"
		}
		out[curIdx].Relationships = append(out[curIdx].Relationships, types.RelationshipRecord{
			ToID:       toID,
			Kind:       "CALLS",
			Properties: props,
		})
	}
	return out
}

// collectLocalLabels returns the set of local-label names (`.L*`, numeric, and
// other non-procedure labels) defined anywhere in the file. These are the
// intra-file branch destinations emitted as SCOPE.CodeBlock anchors (#2836).
func collectLocalLabels(lines []string, exported map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, line := range lines {
		name := labelName(line)
		if name == "" {
			continue
		}
		if !isProcedureLabel(name, exported) {
			out[name] = true
		}
	}
	return out
}

// localLabelStub builds the Format A structural-ref stub the resolver uses to
// bind a branch edge to the in-file SCOPE.CodeBlock anchor of the given local
// label. Shape: scope:codeblock:label:<lang>:<file>:<name> — matched by the
// resolver's byLocation[file][name] index.
func localLabelStub(lang, filePath, name string) string {
	return "scope:codeblock:label:" + lang + ":" + filepath.ToSlash(filePath) + ":" + name
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

// normalizeMnemonic lowercases a mnemonic and strips ISA size/width suffixes
// that decorate the operation but never change which control-flow family it
// belongs to:
//
//   - m68k: `.b`/`.w`/`.l`/`.s` byte/word/long/short suffixes
//     (`bsr.w`, `bra.s`, `jsr.l`, `movem.l`).
//   - ARM Thumb-2: `.n`/`.w` narrow/wide encoding hints (`b.w`, `bl.w`).
//   - gas branch-predication dot form is NOT stripped here (`b.eq` on
//     AArch64 is handled at the mnemonic-split layer because the `.eq` is a
//     real condition, but `b.eq`'s family — branch — is preserved since the
//     base token `b` is a branch).
//
// Only a single trailing `.suffix` of a known width token is removed; an
// unknown suffix is left intact so we never silently merge distinct ops.
func normalizeMnemonic(mnem string) string {
	m := strings.ToLower(strings.TrimSpace(mnem))
	dot := strings.IndexByte(m, '.')
	if dot <= 0 {
		return m
	}
	base, suffix := m[:dot], m[dot+1:]
	switch suffix {
	case "b", "w", "l", "s", "n":
		// m68k size suffix or ARM narrow/wide. Only strip when the base is a
		// control-flow mnemonic we recognise, so data ops like `move.l` (not
		// control flow) are unaffected by accident and `b.eq` (AArch64
		// conditional) keeps its meaning as a branch via the base token.
		if callMnemonics[base] || branchMnemonics[base] || base == "jmp" || base == "movem" {
			return base
		}
	}
	// AArch64 conditional branch `b.eq`, `b.ne`, ... — base `b` is a branch.
	if base == "b" {
		return "b"
	}
	return m
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

// callTarget extracts a static label target from a call/branch operand,
// syntax-agnostically across AT&T (gas) and Intel (NASM/MASM) operand forms.
// Returns "" when the target is a register (indirect), an immediate address,
// or a memory reference (no statically-recoverable symbol).
//
// Handled operand shapes:
//
//	AT&T:   call greet            → greet
//	        call printf@PLT       → printf  (relocation suffix stripped)
//	        call *%rax            → ""      (register-indirect)
//	        jmp  *table(,%rax,8)  → ""      (memory-indirect, parens)
//	        bl   func             → func
//	        cbz  x0, .Lbody       → .Lbody  (label is the last operand)
//	Intel:  call work             → work
//	        call qword [rax]      → ""      (memory-indirect, brackets)
//	        call [rel func]       → ""      (RIP-relative indirect)
//	        jmp  near .exit       → .exit   (size keyword stripped)
//	        jmp  short loop       → loop
//	m68k:   jsr  helper           → helper
//	        bra  .Lloop           → .Lloop
//	        jmp  (a0)             → ""      (register-indirect via parens)
//	        dbra d0, .Lloop       → .Lloop  (counter reg first, label last)
//
// AArch64 multi-operand branches put the destination label last
// (cbz/cbnz/tbz/tbnz), while x86/ARM/m68k call & unconditional branch put it
// first; we try the last operand then the first and return the first
// label-shaped candidate.
func callTarget(operand string) string {
	operand = strings.TrimSpace(operand)
	if operand == "" {
		return ""
	}
	// Split on commas that are NOT inside an AT&T `(...)` memory ref or an
	// Intel `[...]` memory ref, so `*table(,%rax,8)` stays one token.
	parts := splitOperandTokens(operand)
	candidates := []string{}
	if len(parts) > 0 {
		candidates = append(candidates, strings.TrimSpace(parts[len(parts)-1]))
		candidates = append(candidates, strings.TrimSpace(parts[0]))
	}
	for _, c := range candidates {
		c = cleanTargetToken(c)
		if c == "" {
			continue
		}
		if isLabelLike(c) {
			return c
		}
	}
	return ""
}

// cleanTargetToken strips operand decorations and returns "" if the token is a
// register, immediate, or memory reference (i.e. not a static symbolic label).
func cleanTargetToken(c string) string {
	c = strings.TrimSpace(c)
	if c == "" {
		return ""
	}
	// Intel memory-indirect: bracketed operand, optionally with a size/segment
	// prefix (`qword [rax]`, `[rel func]`, `dword ptr [rbx]`). No static label.
	if strings.ContainsAny(c, "[]") {
		return ""
	}
	// AT&T indirect prefix `*` (`*%rax`, `*table(,%rax,8)`).
	if c[0] == '*' {
		c = c[1:]
	}
	// AT&T / m68k memory-indirect via parentheses (`(a0)`, `8(%rbp)`,
	// `func(%rip)`, `(,%rax,8)`). A parenthesised operand is a memory ref, not
	// a static call target.
	if strings.ContainsAny(c, "()") {
		return ""
	}
	// PLT/GOT relocation suffix (`printf@PLT`, `sym@GOTPCREL`).
	if at := strings.IndexByte(c, '@'); at > 0 {
		c = c[:at]
	}
	// Intel size / distance keywords that may prefix a label target.
	for _, kw := range []string{"near ", "far ", "short ", "dword ", "qword ", "word ", "ptr ", "rel "} {
		c = strings.TrimPrefix(c, kw)
	}
	c = strings.TrimSpace(c)
	if c == "" || isRegister(c) {
		return ""
	}
	// Immediate numeric / hex address — AT&T `$`, ARM/m68k `#`, bare number.
	if c[0] == '#' || c[0] == '$' || isNumericOperand(c) {
		return ""
	}
	return c
}

// splitOperandTokens splits an operand list on top-level commas, treating
// AT&T `(...)` and Intel `[...]` groupings as opaque so an AT&T memory ref
// like `(,%rax,8)` is not torn apart on its internal commas.
func splitOperandTokens(operand string) []string {
	var out []string
	depthParen, depthBrack := 0, 0
	start := 0
	for i := 0; i < len(operand); i++ {
		switch operand[i] {
		case '(':
			depthParen++
		case ')':
			if depthParen > 0 {
				depthParen--
			}
		case '[':
			depthBrack++
		case ']':
			if depthBrack > 0 {
				depthBrack--
			}
		case ',':
			if depthParen == 0 && depthBrack == 0 {
				out = append(out, operand[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, operand[start:])
	return out
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
	// m68k `trap #0` is the Linux/m68k syscall gate. `trap #15` is the
	// classic 68k monitor/exception vector and other vectors are debugger
	// traps, so we only treat `trap #0` as the OS syscall boundary.
	if lowerMnem == "trap" {
		op := strings.ToLower(strings.TrimSpace(operand))
		op = strings.TrimPrefix(op, "#")
		op = strings.TrimPrefix(op, "$")
		return op == "0" || op == "0x0"
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
//   - `|`  m68k line comment (only when it starts a token, so an in-operand
//     bitwise-OR `#(A|B)` survives)
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
		case c == '|':
			// `|` is the m68k line-comment marker, but armasm also uses it to
			// bar-delimit a symbol that contains characters not otherwise legal
			// in an identifier (`|My.Sym|`, `AREA |.text|`), and it is the
			// bitwise-OR operator inside expression operands (`#(A|B)`) (#5056).
			//
			// Disambiguation (cheap, single-line lookahead):
			//   - Not a token start (preceded by a non-space) → bitwise OR; keep.
			//   - Token start AND there is a matching closing `|` later on the
			//     same line with at least one non-space identifier char between
			//     them → a bar-delimited symbol (`|name|`); keep so the label/
			//     section/data paths can read it.
			//   - Otherwise (token start, no well-formed `|...|` pair) → m68k
			//     line comment; blank to EOL. This still blanks a trailing
			//     `| comment` (space after `|`, no closing bar).
			tokenStart := i == 0 || src[i-1] == ' ' || src[i-1] == '\t' || src[i-1] == '\n'
			if tokenStart && barDelimitedSymbol(src, i) {
				i++ // leave the `|name|` intact; advance past the opening bar.
			} else if tokenStart {
				i = blankToEOL(out, src, i)
			} else {
				i++
			}
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

// barDelimitedSymbol reports whether the `|` at src[open] opens a well-formed
// armasm bar-delimited symbol `|name|` (#5056): a matching closing `|` exists
// later on the same line, and at least one non-space character sits between the
// bars. A trailing m68k comment (`| like this`) has a space immediately after
// the bar and no closing bar on the line, so it returns false and is scrubbed.
func barDelimitedSymbol(src string, open int) bool {
	n := len(src)
	seenContent := false
	for j := open + 1; j < n; j++ {
		c := src[j]
		if c == '\n' {
			return false // no closing bar on this line.
		}
		if c == '|' {
			return seenContent // closing bar; require non-empty content.
		}
		if c != ' ' && c != '\t' {
			seenContent = true
		}
	}
	return false // closing bar runs past EOF.
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
