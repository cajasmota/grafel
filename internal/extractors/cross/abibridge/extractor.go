// Package abibridge implements the C ↔ assembly ABI cross-language linker.
//
// It is the embedded/firmware analog of the cross-repo HTTP linker
// (internal/extractors/cross/httpclient) and the JCL bridge (#2843): where
// the HTTP linker bridges two services across the network boundary by URL,
// this linker bridges two languages across the *calling-convention* boundary
// by **symbol name**.
//
// A typical embedded / OS / crypto codebase mixes C/C++ with hand-written
// `.S` hot paths. The C side declares the asm routine with a plain prototype
// or `extern "C"` and calls it; the asm side defines the routine under a
// `.globl <sym>` exported label. Both sides already index — the C
// `function_definition` CALLS edge (cpp extractor) carries the bare callee
// name, and the asm exported procedure (assembly extractor) carries the bare
// symbol name — so the resolver's by-name index (internal/resolve) already
// binds a C → asm CALLS edge with no extra work. The gap this linker closes:
//
//	asm → C  : a C `extern <ret> <sym>(...)` declaration is a *declaration*
//	           (tree-sitter `declaration`, not `function_definition`), so the
//	           cpp extractor emits NO entity for it and the asm procedure that
//	           implements it has nothing to link *to*. This linker emits the C
//	           extern-decl entity and an IMPLEMENTS edge
//	           (asm procedure → C extern decl) keyed on the bare symbol name,
//	           which the resolver binds via byName across the c-cpp ↔ assembly
//	           boundary.
//
//	C → asm  : already resolved by the generic byName machinery (the asm
//	           `call <cfunc>` / `bl <cfunc>` edge binds to the C function
//	           definition; the C `crypt()` call binds to the asm procedure).
//	           Proven by fixture here; no new edge is required.
//
//	inline   : C inline asm (`__asm__`/`asm volatile`) that names an external
//	           symbol in its instruction text (e.g. `call rdtsc_helper`) emits
//	           a CALLS edge from the enclosing-file inline-asm marker to that
//	           bare symbol, so an asm label referenced from inline asm is
//	           reachable in the call graph.
//
// Name-mangling reality: C symbols are unmangled, so the bare-name match is
// exact. C++ mangling (`_Z...`) is OUT OF SCOPE here — a C++ function called
// from asm appears under its mangled name in the asm `call`, and the demangle
// step is deferred (see the follow-up issue). `extern "C"` C++ functions ARE
// covered because they are unmangled.
//
// Entity kinds emitted: SCOPE.Operation (the C extern-decl + inline-asm
// marker — both already in AllEntityKinds; no new Kind is registered).
// Relationship kinds emitted: IMPLEMENTS, CALLS (both already in
// AllRelationshipKinds).
//
// Registration key: "_cross_abibridge".
package abibridge

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("_cross_abibridge", &Extractor{})
}

// Extractor detects C ↔ assembly ABI bridges and emits the cross-language
// edges that the generic resolver cannot synthesise on its own.
type Extractor struct{}

// Language returns the registration key.
func (e *Extractor) Language() string { return "_cross_abibridge" }

// abiBridgeProp is the shared marker stamped on every edge this linker emits
// so a graph query can isolate "the C↔asm ABI boundary" with one filter.
const abiBridgeProp = "abi_bridge"

// -----------------------------------------------------------------------
// Compiled patterns (C side)
// -----------------------------------------------------------------------

var (
	// externDeclRE matches a C/C++ external function *declaration* (no body):
	//
	//	extern int crypt_block(const void *in, void *out);
	//	extern "C" void fast_memcpy(void *d, const void *s, unsigned long n);
	//	extern uint32_t crc32_asm(const uint8_t *buf, size_t len) ;
	//
	// Group 1 is the symbol name. The trailing `;` (a declaration, never a
	// definition) is what distinguishes this from a `function_definition`
	// the cpp extractor already handles — the `{` body form is excluded by
	// requiring `)` then optional whitespace then `;`.
	//
	// `extern "C"` and `extern "C++"` linkage prefixes are tolerated. The
	// return type is matched loosely (one-or-more type/qualifier tokens and
	// pointer stars) because we only need the trailing symbol name, not a
	// full C type parse.
	externDeclRE = regexp.MustCompile(
		`(?m)^\s*extern\s+(?:"C(?:\+\+)?"\s+)?(?:[A-Za-z_][\w]*\s+|\*\s*|const\s+|unsigned\s+|signed\s+|struct\s+|static\s+)+\**\s*([A-Za-z_]\w*)\s*\([^;{]*\)\s*;`,
	)

	// inlineAsmRE matches a GCC/Clang inline-asm block and captures its
	// template text:
	//
	//	__asm__ volatile ("call rdtsc_helper" : ...);
	//	asm ("bl  platform_init\n\t" : : : "lr");
	//
	// Group 1 is the full quoted template string (the assembly text). We then
	// scan that text for call/branch mnemonics naming an external label.
	inlineAsmRE = regexp.MustCompile(
		`(?s)\b(?:__asm__|asm)\s+(?:volatile\s+|__volatile__\s+|goto\s+)*\(\s*("(?:[^"\\]|\\.)*"(?:\s*"(?:[^"\\]|\\.)*")*)`,
	)

	// asmCallInTemplateRE picks the target label out of a call/branch mnemonic
	// inside an inline-asm template. Covers the call families the assembly
	// extractor recognises (call/bl/blx/jal/jsr/bsr). Group 1 is the target.
	asmCallInTemplateRE = regexp.MustCompile(
		`(?im)\b(?:call|callq|bl|blx|jal|jalr|jsr|bsr)\s+([A-Za-z_]\w*)`,
	)
)

// -----------------------------------------------------------------------
// Language gate
// -----------------------------------------------------------------------

// isCFamily reports whether the language token is one this linker scans for
// the C side (declarations + inline asm). The classifier maps .c/.h → "c"
// and .cpp/.cc/.cxx/.hpp → "cpp".
func isCFamily(lang string) bool {
	switch strings.ToLower(lang) {
	case "c", "cpp", "c_cpp", "c-cpp":
		return true
	}
	return false
}

// -----------------------------------------------------------------------
// Extract
// -----------------------------------------------------------------------

// Extract scans a C/C++ source file and emits the asm→C IMPLEMENTS edges
// (one per `extern` decl of a symbol an asm `.globl` will export) and the
// inline-asm CALLS edges. Assembly files are a no-op here: the asm extractor
// already emits the exported procedure and the bare-name `call`/`bl` edges
// that the resolver binds to C function definitions (the C → asm direction),
// so this linker would only duplicate them.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor._cross_abibridge")
	_, span := tracer.Start(ctx, "indexer.abi_bridge_extractor.extract")
	defer span.End()

	span.SetAttributes(
		attribute.String("file_path", file.Path),
		attribute.String("language", file.Language),
	)

	if !isCFamily(file.Language) || len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
	lang := file.Language

	var out []types.EntityRecord
	out = append(out, buildExternDeclEntities(src, file.Path, lang)...)
	out = append(out, buildInlineAsmEntities(src, file.Path, lang)...)

	externCount, inlineCount := 0, 0
	for _, r := range out {
		if r.Subtype == externDeclSubtype {
			externCount++
		} else {
			inlineCount++
		}
	}
	span.SetAttributes(
		attribute.Int("extern_decls_found", externCount),
		attribute.Int("inline_asm_blocks_found", inlineCount),
	)

	return out, nil
}

const (
	// externDeclSubtype tags the synthetic C entity that stands in for an
	// `extern` declaration the cpp extractor does not emit (it has no body).
	externDeclSubtype = "abi_extern_decl"

	// inlineAsmSubtype tags the synthetic per-file marker that owns the CALLS
	// edges discovered inside C inline-asm blocks.
	inlineAsmSubtype = "abi_inline_asm"

	// bridgeMarkerName is the synthetic name of the per-file extern-decl
	// bridge marker. It is deliberately NOT a real symbol name so it never
	// collides with an asm procedure in the resolver's by-name index.
	bridgeMarkerName = "__abi_bridge__"
)

// bridgeMarkerRef builds the structural ref for the per-file extern-decl
// bridge marker.
func bridgeMarkerRef(lang, filePath string) string {
	return "scope:operation:method:" + lang + ":" + filePath + ":" + bridgeMarkerName
}

// itoa is a tiny strconv.Itoa wrapper kept local so the import set stays
// minimal and the call sites read cleanly.
func itoa(i int) string { return strconv.Itoa(i) }

// buildExternDeclEntities emits a single per-file bridge marker that owns one
// IMPLEMENTS edge per `extern` function declaration whose symbol an asm
// `.globl` exports. Each edge points the C declaration at the asm procedure
// that implements it: FromID is the marker's structural ref and ToID is the
// bare symbol name, which the resolver binds by name to the asm SCOPE.Operation
// (the cross-language hop c-cpp → assembly).
//
// CRITICAL — the marker's Name is the synthetic `__abi_bridge__`, NOT the
// symbol. Emitting an entity *named* `crypt_block` would collide in the
// resolver's kind-agnostic by-name index with the asm procedure of the same
// name, blanking the slot (ambiguous) and breaking BOTH this IMPLEMENTS edge
// AND the pre-existing C call-site → asm-procedure CALLS resolution. The asm
// `.globl` procedure is the canonical owner of the symbol name; the bridge
// records each declared symbol as edge Properties["symbol"], never as a
// competing entity.
func buildExternDeclEntities(src, filePath, lang string) []types.EntityRecord {
	seen := map[string]bool{}
	var rels []types.RelationshipRecord
	markerRef := bridgeMarkerRef(lang, filePath)

	for _, m := range externDeclRE.FindAllStringSubmatchIndex(src, -1) {
		sym := src[m[2]:m[3]]
		if sym == "" || seen[sym] || isCKeyword(sym) {
			continue
		}
		seen[sym] = true
		line := strings.Count(src[:m[0]], "\n") + 1
		rels = append(rels, types.RelationshipRecord{
			FromID: markerRef,
			ToID:   sym,
			Kind:   string(types.RelationshipKindImplements),
			Properties: map[string]string{
				abiBridgeProp: "asm_implements_c_decl",
				"symbol":      sym,
				"linkage":     "extern",
				"resolution":  "by_name",
				"boundary":    "c_asm_abi",
				"line":        itoa(line),
			},
		})
	}
	if len(rels) == 0 {
		return nil
	}
	rec := types.EntityRecord{
		Name:       bridgeMarkerName,
		Kind:       string(types.EntityKindOperation),
		Subtype:    externDeclSubtype,
		SourceFile: filePath,
		Language:   lang,
		Signature:  "C↔asm ABI bridge (" + filePath + ")",
		Properties: map[string]string{
			"ref":         markerRef,
			abiBridgeProp: "c_extern_decl",
			"provenance":  "INFERRED_FROM_C_EXTERN_DECL",
			"language":    lang,
		},
		QualityScore:  0.8,
		Relationships: rels,
	}
	extractor.TagRelationshipsLanguage([]types.EntityRecord{rec}, lang)
	return []types.EntityRecord{rec}
}

// buildInlineAsmEntities emits at most one per-file SCOPE.Operation marker
// that owns a CALLS edge for every external label named in a call/branch
// mnemonic inside a C inline-asm block. The CALLS ToID is the bare label
// name; the resolver binds it to the asm `.globl` procedure (or C function)
// of the same name.
func buildInlineAsmEntities(src, filePath, lang string) []types.EntityRecord {
	seenTarget := map[string]bool{}
	var rels []types.RelationshipRecord
	markerRef := "scope:operation:method:" + lang + ":" + filePath + ":__inline_asm__"

	for _, m := range inlineAsmRE.FindAllStringSubmatch(src, -1) {
		template := m[1]
		for _, c := range asmCallInTemplateRE.FindAllStringSubmatch(template, -1) {
			target := c[1]
			if target == "" || seenTarget[target] || isCKeyword(target) {
				continue
			}
			seenTarget[target] = true
			rels = append(rels, types.RelationshipRecord{
				FromID: markerRef,
				ToID:   target,
				Kind:   string(types.RelationshipKindCalls),
				Properties: map[string]string{
					abiBridgeProp: "c_inline_asm_call",
					"symbol":      target,
					"resolution":  "by_name",
					"boundary":    "c_asm_abi",
				},
			})
		}
	}
	if len(rels) == 0 {
		return nil
	}
	rec := types.EntityRecord{
		Name:       "__inline_asm__",
		Kind:       string(types.EntityKindOperation),
		Subtype:    inlineAsmSubtype,
		SourceFile: filePath,
		Language:   lang,
		Signature:  "inline asm (" + filePath + ")",
		Properties: map[string]string{
			"ref":         markerRef,
			abiBridgeProp: "c_inline_asm",
			"provenance":  "INFERRED_FROM_C_INLINE_ASM",
			"language":    lang,
		},
		QualityScore:  0.7,
		Relationships: rels,
	}
	extractor.TagRelationshipsLanguage([]types.EntityRecord{rec}, lang)
	return []types.EntityRecord{rec}
}

// cKeywords are C/C++ tokens the loose regexes might surface as a "symbol"
// (e.g. a return-type-only declaration, or a control-flow keyword preceding a
// parenthesised expression inside inline asm). Dropping them avoids phantom
// extern-decl entities and phantom inline-asm calls.
var cKeywords = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "return": true,
	"sizeof": true, "typedef": true, "struct": true, "union": true, "enum": true,
	"void": true, "int": true, "char": true, "long": true, "short": true,
	"float": true, "double": true, "unsigned": true, "signed": true, "const": true,
	"static": true, "inline": true, "register": true, "volatile": true,
}

func isCKeyword(s string) bool { return cKeywords[s] }
