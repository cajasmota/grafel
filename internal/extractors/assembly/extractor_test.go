package assembly

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"
)

// loadFixture reads a fixture from testdata.
func loadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// index helpers ------------------------------------------------------------

func byName(recs []types.EntityRecord, name string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Name == name {
			return &recs[i]
		}
	}
	return nil
}

func callTargets(rec *types.EntityRecord) map[string]types.RelationshipRecord {
	out := map[string]types.RelationshipRecord{}
	if rec == nil {
		return out
	}
	for _, r := range rec.Relationships {
		if r.Kind == "CALLS" {
			out[r.ToID] = r
		}
	}
	return out
}

// Registration -------------------------------------------------------------

func TestRegistered(t *testing.T) {
	e, ok := extractor.Get("assembly")
	if !ok {
		t.Fatal("assembly extractor not registered")
	}
	if e.Language() != "assembly" {
		t.Fatalf("Language()=%q want assembly", e.Language())
	}
}

func TestEmptyContent(t *testing.T) {
	e := &Extractor{}
	got, err := e.Extract(context.Background(), extractor.FileInput{Path: "x.s", Content: nil})
	if err != nil || got != nil {
		t.Fatalf("empty content: got %v err %v", got, err)
	}
}

// x86-64 gas ---------------------------------------------------------------

func TestExtractX8664Gas(t *testing.T) {
	src := loadFixture(t, "x86_64_gas.s.fixture")
	recs := extractAssembly(src, "boot.s", "assembly")

	// File entity carries dialect/syntax.
	file := byName(recs, "boot.s")
	if file == nil {
		t.Fatal("missing file entity")
	}
	if file.Properties["dialect"] != "x86-64" {
		t.Errorf("dialect=%q want x86-64", file.Properties["dialect"])
	}
	if file.Properties["syntax"] != "att" {
		t.Errorf("syntax=%q want att", file.Properties["syntax"])
	}

	// Procedures.
	main := byName(recs, "main")
	greet := byName(recs, "greet")
	if main == nil || main.Kind != "SCOPE.Operation" || main.Subtype != "procedure" {
		t.Fatalf("main procedure missing/wrong: %+v", main)
	}
	if greet == nil {
		t.Fatal("greet procedure missing")
	}
	if main.Properties["exported"] != "true" {
		t.Error("main should be exported (.globl)")
	}

	// .L local labels must NOT become procedures, but ARE emitted as
	// SCOPE.CodeBlock anchors (#2836 intra-file branch targets).
	for _, ll := range []string{".Ldone", ".Lok"} {
		a := byName(recs, ll)
		if a == nil {
			t.Errorf("local label %s should be emitted as an anchor", ll)
			continue
		}
		if a.Kind != "SCOPE.CodeBlock" || a.Subtype != "label" {
			t.Errorf("anchor %s kind=%q subtype=%q want SCOPE.CodeBlock/label", ll, a.Kind, a.Subtype)
		}
		if a.Properties["local"] != "true" {
			t.Errorf("anchor %s should be marked local", ll)
		}
	}

	// CALLS edges from main.
	mc := callTargets(main)
	if _, ok := mc["greet"]; !ok {
		t.Error("main should CALL greet")
	}
	if e, ok := mc["printf"]; !ok {
		t.Error("main should CALL printf (PLT suffix stripped)")
	} else if e.Properties["locality"] != "external" {
		t.Errorf("printf call locality=%q want external", e.Properties["locality"])
	}
	// Branch to a local label is rewritten to a file-scoped Format A stub so
	// it resolves intra-file (#2836).
	doneStub := localLabelStub("assembly", "boot.s", ".Ldone")
	if e, ok := mc[doneStub]; !ok {
		t.Errorf("main should branch to .Ldone (stub %q); got %v", doneStub, mc)
	} else if e.Properties["edge_kind"] != "branch" {
		t.Errorf(".Ldone edge_kind=%q want branch", e.Properties["edge_kind"])
	} else if e.Properties["resolution"] != "intra_file" {
		t.Errorf(".Ldone resolution=%q want intra_file", e.Properties["resolution"])
	}

	// Syscall effect on greet.
	if greet.Properties["has_syscall"] != "true" {
		t.Error("greet should have has_syscall=true")
	}
	gc := callTargets(greet)
	if e, ok := gc[syntheticSyscallTarget]; !ok {
		t.Error("greet should CALL __syscall")
	} else if e.Properties["effect"] != "syscall" {
		t.Errorf("__syscall effect=%q want syscall", e.Properties["effect"])
	}

	// Sections.
	for _, s := range []string{".rodata", ".data", ".text"} {
		if r := byName(recs, s); r == nil || r.Subtype != "section" {
			t.Errorf("missing section %s", s)
		}
	}

	// Constants.
	if c := byName(recs, "SYS_write"); c == nil || c.Kind != "SCOPE.Constant" {
		t.Error("missing constant SYS_write")
	}
}

// ARM64 --------------------------------------------------------------------

func TestExtractARM64(t *testing.T) {
	src := loadFixture(t, "arm64.s.fixture")
	recs := extractAssembly(src, "start.s", "assembly")

	file := byName(recs, "start.s")
	if file == nil || file.Properties["dialect"] != "arm64" {
		t.Fatalf("dialect=%v want arm64", file)
	}

	start := byName(recs, "_start")
	setup := byName(recs, "setup")
	if start == nil || setup == nil {
		t.Fatalf("procedures missing: _start=%v setup=%v", start, setup)
	}

	sc := callTargets(start)
	if _, ok := sc["setup"]; !ok {
		t.Error("_start should CALL setup (bl)")
	}
	if e, ok := sc["memcpy"]; !ok {
		t.Error("_start should CALL memcpy (bl external)")
	} else if e.Properties["locality"] != "external" {
		t.Errorf("memcpy locality=%q want external", e.Properties["locality"])
	}
	loopStub := localLabelStub("assembly", "start.s", ".Lloop")
	if _, ok := sc[loopStub]; !ok {
		t.Errorf("_start should branch to .Lloop (b) via stub %q; got %v", loopStub, sc)
	}
	if start.Properties["has_syscall"] != "true" {
		t.Error("_start should have svc syscall effect")
	}
	if _, ok := sc[syntheticSyscallTarget]; !ok {
		t.Error("_start should CALL __syscall via svc")
	}
}

// NASM ---------------------------------------------------------------------

func TestExtractNASM(t *testing.T) {
	src := loadFixture(t, "x86_64.nasm.fixture")
	recs := extractAssembly(src, "boot.nasm", "assembly")

	start := byName(recs, "_start")
	work := byName(recs, "work")
	if start == nil || work == nil {
		t.Fatalf("procedures missing: _start=%v work=%v", start, work)
	}

	sc := callTargets(start)
	if _, ok := sc["work"]; !ok {
		t.Error("_start should CALL work")
	}
	if e, ok := sc["puts"]; !ok {
		t.Error("_start should CALL puts (extern)")
	} else if e.Properties["locality"] != "external" {
		t.Errorf("puts locality=%q want external", e.Properties["locality"])
	}
	if start.Properties["has_syscall"] != "true" {
		t.Error("_start should have syscall effect")
	}

	// NASM %define constant.
	if c := byName(recs, "SYS_write"); c == nil {
		t.Error("missing NASM define constant SYS_write")
	}
	// Sections.
	if byName(recs, ".data") == nil || byName(recs, ".text") == nil {
		t.Error("missing NASM sections")
	}
}

// int 0x80 syscall gate ----------------------------------------------------

func TestInt0x80Syscall(t *testing.T) {
	src := `	.globl _start
_start:
	mov $1, %eax
	int $0x80
	ret
	.globl other
other:
	int $3
	ret
`
	recs := extractAssembly(src, "i386.s", "assembly")
	start := byName(recs, "_start")
	other := byName(recs, "other")
	if start == nil || start.Properties["has_syscall"] != "true" {
		t.Error("int 0x80 should be a syscall effect")
	}
	if other == nil || other.Properties["has_syscall"] == "true" {
		t.Error("int $3 (breakpoint) must NOT be a syscall effect")
	}
}

// Comment scrubbing --------------------------------------------------------

func TestScrubComments(t *testing.T) {
	src := "mov r0, #4 ; comment\n" + // NASM/ARM: # is immediate, ; is comment
		"call real ; bogus_call fake\n" +
		"add r1, r2 // c++ comment call ghost\n" +
		"/* block call phantom */ ret\n"
	out := scrubComments(src)
	if containsToken(out, "bogus_call") || containsToken(out, "ghost") || containsToken(out, "phantom") {
		t.Errorf("comments not scrubbed: %q", out)
	}
	// The ARM immediate `#4` must survive (it is NOT a comment).
	if !containsToken(out, "#4") {
		t.Errorf("ARM immediate #4 wrongly scrubbed: %q", out)
	}
	if !containsToken(out, "real") {
		t.Errorf("real call lost: %q", out)
	}
}

func containsToken(s, tok string) bool {
	for i := 0; i+len(tok) <= len(s); i++ {
		if s[i:i+len(tok)] == tok {
			return true
		}
	}
	return false
}

// callTarget edge cases ----------------------------------------------------

func TestCallTargetIndirect(t *testing.T) {
	cases := map[string]string{
		"foo":        "foo",
		"*%rax":      "", // x86 indirect
		"x0":         "", // ARM register-indirect (blr x0)
		"printf@PLT": "printf",
		"$0x80":      "",
		"x0, .Lbody": ".Lbody", // cbz x0, .Lbody → label is last
		"#0x100":     "",
	}
	for in, want := range cases {
		if got := callTarget(in); got != want {
			t.Errorf("callTarget(%q)=%q want %q", in, got, want)
		}
	}
}

func TestIsProcedureLabel(t *testing.T) {
	exported := map[string]bool{"main": true}
	if !isProcedureLabel("main", exported) {
		t.Error("exported main is a procedure")
	}
	if !isProcedureLabel("helper", nil) {
		t.Error("plain top-level label is a procedure")
	}
	if isProcedureLabel(".Lloop", nil) {
		t.Error(".L label is not a procedure")
	}
	if isProcedureLabel("1", nil) {
		t.Error("numeric label is not a procedure")
	}
}

// m68k depth (#2835) ---------------------------------------------------------

func TestExtractM68k(t *testing.T) {
	src := loadFixture(t, "m68k.s.fixture")
	recs := extractAssembly(src, "boot68k.s", "assembly")

	file := byName(recs, "boot68k.s")
	if file == nil || file.Properties["dialect"] != "m68k" {
		t.Fatalf("dialect=%v want m68k", file)
	}

	start := byName(recs, "_start")
	setup := byName(recs, "setup")
	helper := byName(recs, "helper")
	if start == nil || setup == nil || helper == nil {
		t.Fatalf("procedures missing: _start=%v setup=%v helper=%v", start, setup, helper)
	}

	sc := callTargets(start)
	// jsr setup → call.
	if e, ok := sc["setup"]; !ok {
		t.Errorf("_start should jsr→CALL setup; got %v", sc)
	} else if e.Properties["edge_kind"] != "call" {
		t.Errorf("setup edge_kind=%q want call", e.Properties["edge_kind"])
	}
	// bsr.w helper → call (size suffix stripped).
	if e, ok := sc["helper"]; !ok {
		t.Error("_start should bsr.w→CALL helper (size suffix stripped)")
	} else if e.Properties["edge_kind"] != "call" {
		t.Errorf("helper edge_kind=%q want call", e.Properties["edge_kind"])
	}
	// jsr memcpy → external call.
	if e, ok := sc["memcpy"]; !ok {
		t.Error("_start should jsr→CALL memcpy")
	} else if e.Properties["locality"] != "external" {
		t.Errorf("memcpy locality=%q want external", e.Properties["locality"])
	}
	// dbra %d0, .Lloop → branch to local label (label is LAST operand).
	loopStub := localLabelStub("assembly", "boot68k.s", ".Lloop")
	if e, ok := sc[loopStub]; !ok {
		t.Errorf("_start should dbra→branch .Lloop via stub %q; got %v", loopStub, sc)
	} else if e.Properties["edge_kind"] != "branch" {
		t.Errorf(".Lloop edge_kind=%q want branch", e.Properties["edge_kind"])
	}
	// bra .Ldone → branch to local label.
	doneStub := localLabelStub("assembly", "boot68k.s", ".Ldone")
	if _, ok := sc[doneStub]; !ok {
		t.Errorf("_start should bra→branch .Ldone via stub %q; got %v", doneStub, sc)
	}
	// trap #0 → syscall effect.
	if start.Properties["has_syscall"] != "true" {
		t.Error("_start trap #0 should be a syscall effect")
	}
	if _, ok := sc[syntheticSyscallTarget]; !ok {
		t.Error("_start should CALL __syscall via trap #0")
	}

	// helper bra helper → self-recursion classification.
	hc := callTargets(helper)
	if e, ok := hc["helper"]; !ok {
		t.Error("helper should branch to itself")
	} else if e.Properties["recursion"] != "self" {
		t.Errorf("helper self-branch recursion=%q want self", e.Properties["recursion"])
	}

	// Local-label anchors emitted.
	if a := byName(recs, ".Lloop"); a == nil || a.Kind != "SCOPE.CodeBlock" {
		t.Error("missing .Lloop anchor")
	}
	// Constant + section.
	if byName(recs, "SYS_exit") == nil {
		t.Error("missing m68k constant SYS_exit")
	}
	if byName(recs, ".text") == nil {
		t.Error("missing .text section")
	}
}

// RISC-V -------------------------------------------------------------------

func TestExtractRISCV(t *testing.T) {
	src := loadFixture(t, "riscv.s.fixture")
	recs := extractAssembly(src, "boot_rv.s", "assembly")

	file := byName(recs, "boot_rv.s")
	if file == nil || file.Properties["dialect"] != "riscv" {
		t.Fatalf("dialect=%v want riscv", file)
	}

	start := byName(recs, "_start")
	setup := byName(recs, "setup")
	if start == nil || setup == nil {
		t.Fatalf("procedures missing: _start=%v setup=%v", start, setup)
	}

	sc := callTargets(start)
	// jal ra, setup → intra-file call (label is the LAST operand, ra skipped).
	if e, ok := sc["setup"]; !ok {
		t.Errorf("_start should jal→CALL setup; got %v", sc)
	} else if e.Properties["edge_kind"] != "call" {
		t.Errorf("setup edge_kind=%q want call", e.Properties["edge_kind"])
	}
	// jal ra, memcpy → external call.
	if e, ok := sc["memcpy"]; !ok {
		t.Error("_start should jal→CALL memcpy (external)")
	} else if e.Properties["locality"] != "external" {
		t.Errorf("memcpy locality=%q want external", e.Properties["locality"])
	}
	// beqz a0, .Ldone → branch to local label (label is LAST operand).
	doneStub := localLabelStub("assembly", "boot_rv.s", ".Ldone")
	if e, ok := sc[doneStub]; !ok {
		t.Errorf("_start should beqz→branch .Ldone via stub %q; got %v", doneStub, sc)
	} else if e.Properties["edge_kind"] != "branch" {
		t.Errorf(".Ldone edge_kind=%q want branch", e.Properties["edge_kind"])
	}
	// bnez a0, .Lloop → branch to local label.
	loopStub := localLabelStub("assembly", "boot_rv.s", ".Lloop")
	if _, ok := sc[loopStub]; !ok {
		t.Errorf("_start should bnez→branch .Lloop via stub %q; got %v", loopStub, sc)
	}
	// ecall → syscall effect.
	if start.Properties["has_syscall"] != "true" {
		t.Error("_start ecall should be a syscall effect")
	}
	if _, ok := sc[syntheticSyscallTarget]; !ok {
		t.Error("_start should CALL __syscall via ecall")
	}

	// Local-label anchors emitted.
	if a := byName(recs, ".Lloop"); a == nil || a.Kind != "SCOPE.CodeBlock" {
		t.Error("missing .Lloop anchor")
	}
	// Constant (.equ) + section.
	if byName(recs, "SYS_exit") == nil {
		t.Error("missing RISC-V constant SYS_exit")
	}
	if byName(recs, ".text") == nil {
		t.Error("missing .text section")
	}
}

// trap #N gate selectivity ----------------------------------------------------

func TestTrapSyscallGate(t *testing.T) {
	src := "	.globl a\n" +
		"a:\n\ttrap #0\n\trts\n" +
		"	.globl b\n" +
		"b:\n\ttrap #15\n\trts\n"
	recs := extractAssembly(src, "t.s", "assembly")
	a, b := byName(recs, "a"), byName(recs, "b")
	if a == nil || a.Properties["has_syscall"] != "true" {
		t.Error("trap #0 should be a syscall gate")
	}
	if b == nil || b.Properties["has_syscall"] == "true" {
		t.Error("trap #15 (monitor vector) must NOT be a syscall gate")
	}
}

// Intel-syntax operand coverage (#2835) --------------------------------------

func TestExtractX8664Intel(t *testing.T) {
	src := loadFixture(t, "x86_64_intel.asm.fixture")
	recs := extractAssembly(src, "intel.asm", "assembly")

	start := byName(recs, "_start")
	work := byName(recs, "work")
	if start == nil || work == nil {
		t.Fatalf("procedures missing: _start=%v work=%v", start, work)
	}

	sc := callTargets(start)
	// Direct call resolves.
	if _, ok := sc["work"]; !ok {
		t.Error("_start should CALL work (direct Intel)")
	}
	if e, ok := sc["printf"]; !ok {
		t.Error("_start should CALL printf (extern)")
	} else if e.Properties["locality"] != "external" {
		t.Errorf("printf locality=%q want external", e.Properties["locality"])
	}
	// Memory-indirect calls must NOT produce a static target.
	for bad := range sc {
		if bad == "rax" || bad == "handler" || bad == "rbx" || bad == "rel" {
			t.Errorf("memory-indirect operand wrongly resolved to %q", bad)
		}
	}
	// `jmp near .next` resolves to the local label .next (size keyword stripped).
	nextStub := localLabelStub("assembly", "intel.asm", ".next")
	if e, ok := sc[nextStub]; !ok {
		t.Errorf("_start should branch to .next (near) via stub %q; got %v", nextStub, sc)
	} else if e.Properties["edge_kind"] != "branch" {
		t.Errorf(".next edge_kind=%q want branch", e.Properties["edge_kind"])
	}
	if start.Properties["has_syscall"] != "true" {
		t.Error("_start should have syscall effect")
	}

	// work: jmp done → tail call to another procedure.
	wc := callTargets(work)
	if e, ok := wc["done"]; !ok {
		t.Error("work should branch (tail call) to done")
	} else if e.Properties["tail_call"] != "true" {
		t.Errorf("work→done tail_call=%q want true", e.Properties["tail_call"])
	}
}

// callTarget Intel/AT&T memory-ref forms -------------------------------------

func TestCallTargetSyntaxAgnostic(t *testing.T) {
	cases := map[string]string{
		// AT&T forms.
		"greet":           "greet",
		"*%rax":           "", // register-indirect
		"*table(,%rax,8)": "", // memory-indirect (commas inside parens)
		"8(%rbp)":         "", // memory ref
		"func(%rip)":      "", // RIP-relative AT&T
		"$0x80":           "", // immediate
		// Intel forms.
		"qword [rbx]":   "",      // bracketed memory-indirect with size kw
		"[rel handler]": "",      // RIP-relative Intel
		"near label":    "label", // size keyword stripped
		"short loop":    "loop",  // distance keyword stripped
		// m68k forms.
		"(a0)":   "", // register-indirect via parens
		"helper": "helper",
		"#0":     "", // m68k immediate
		// multi-operand (label last).
		"d0, .Lloop": ".Lloop", // dbra d0, .Lloop
	}
	for in, want := range cases {
		if got := callTarget(in); got != want {
			t.Errorf("callTarget(%q)=%q want %q", in, got, want)
		}
	}
}

// Cross-file resolution (#2836) ----------------------------------------------

func TestCrossFileResolution(t *testing.T) {
	libSrc := loadFixture(t, "xref_lib.s.fixture")
	mainSrc := loadFixture(t, "xref_main.s.fixture")

	lib := extractAssembly(libSrc, "xref_lib.s", "assembly")
	main := extractAssembly(mainSrc, "xref_main.s", "assembly")

	// The exporter defines lib_func as an exported procedure entity. The
	// resolver's byName index binds the caller's bare-name CALLS ToID to it.
	libFunc := byName(lib, "lib_func")
	if libFunc == nil || libFunc.Properties["exported"] != "true" {
		t.Fatalf("lib_func should be an exported procedure: %v", libFunc)
	}

	caller := byName(main, "caller")
	if caller == nil {
		t.Fatal("caller procedure missing")
	}
	cc := callTargets(caller)
	// Cross-file call carries the bare exported name (resolved by byName).
	if e, ok := cc["lib_func"]; !ok {
		t.Errorf("caller should CALL lib_func (cross-file); got %v", cc)
	} else if e.Properties["locality"] != "external" {
		t.Errorf("lib_func locality=%q want external (declared .extern)", e.Properties["locality"])
	}
	// Intra-file branch to .Lretry is file-scoped, NOT a bare name (so it
	// never mis-binds to a same-named local label in xref_lib.s).
	retryStub := localLabelStub("assembly", "xref_main.s", ".Lretry")
	if _, ok := cc[retryStub]; !ok {
		t.Errorf("caller should branch to .Lretry via file-scoped stub %q; got %v", retryStub, cc)
	}
	if _, ok := cc[".Lretry"]; ok {
		t.Error(".Lretry branch must NOT carry a bare name (would mis-resolve cross-file)")
	}
}

// Resolver integration: branch/cross-file stubs actually bind (#2836) --------

// assignIDs stamps deterministic graph IDs (as the pipeline does before
// resolution) so BuildIndex — which skips ID-less entities — can index them.
func assignIDs(recs []types.EntityRecord) {
	for i := range recs {
		recs[i].ID = graph.EntityID("", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
}

func idForName(recs []types.EntityRecord, name, file string) string {
	for _, r := range recs {
		if r.Name == name && (file == "" || r.SourceFile == file) {
			return r.ID
		}
	}
	return ""
}

func TestResolverBindsBranchAndCrossFile(t *testing.T) {
	lib := extractAssembly(loadFixture(t, "xref_lib.s.fixture"), "xref_lib.s", "assembly")
	main := extractAssembly(loadFixture(t, "xref_main.s.fixture"), "xref_main.s", "assembly")
	assignIDs(lib)
	assignIDs(main)

	// Merge all entities (as the pipeline does) and build the resolver index.
	all := append(append([]types.EntityRecord{}, lib...), main...)
	idx := resolve.BuildIndex(all)

	// 1) Intra-file branch stub → the local-label anchor in xref_main.s.
	//    The structural-ref (Format A) path is exercised via LookupStatus,
	//    which routes "scope:" stubs through lookupStructural.
	retryStub := localLabelStub("assembly", "xref_main.s", ".Lretry")
	gotID, st := idx.LookupStatus(retryStub)
	if gotID == "" {
		t.Fatalf("intra-file branch stub %q did not resolve (status=%d)", retryStub, st)
	}
	wantAnchor := idForName(main, ".Lretry", "xref_main.s")
	if wantAnchor == "" {
		t.Fatal(".Lretry anchor has no ID")
	}
	if gotID != wantAnchor {
		t.Errorf("stub resolved to %q want anchor %q", gotID, wantAnchor)
	}

	// 2) Cross-file call: caller's bare ToID "lib_func" binds to the exported
	//    procedure entity defined in xref_lib.s (resolver byName).
	libFuncID, ok := idx.Lookup("lib_func")
	if !ok || libFuncID == "" {
		t.Fatal("cross-file bare name lib_func did not resolve")
	}
	if want := idForName(lib, "lib_func", "xref_lib.s"); want != libFuncID {
		t.Errorf("lib_func resolved to %q want %q (exporter entity)", libFuncID, want)
	}
}

// importedModules returns the set of source_module values across all IMPORTS
// edges in the record set (#4950 INCLUDE/GET linkage assertions).
func importedModules(recs []types.EntityRecord) map[string]bool {
	out := map[string]bool{}
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "IMPORTS" {
				out[rel.Properties["source_module"]] = true
			}
		}
	}
	return out
}

// MASM structured directives (#4950) --------------------------------------

func TestExtractMASMStructured(t *testing.T) {
	src := loadFixture(t, "x86_64.masm.fixture")
	recs := extractAssembly(src, "win.asm", "assembly")

	// name PROC / ENDP framing yields procedures even without a trailing colon.
	for _, p := range []string{"main", "helper"} {
		r := byName(recs, p)
		if r == nil || r.Kind != "SCOPE.Operation" || r.Subtype != "procedure" {
			t.Fatalf("PROC %q not a procedure: %v", p, r)
		}
		if r.Properties["framing"] != "proc" {
			t.Errorf("PROC %q framing=%q want proc", p, r.Properties["framing"])
		}
	}
	if m := byName(recs, "main"); m == nil || m.Properties["exported"] != "true" {
		t.Errorf("main should be exported via PUBLIC: %v", m)
	}

	// PROC body is bounded by ENDP — helper's call to printf must attribute to
	// helper-or-main, never leak to a stale procedure. Verify main's CALLS.
	cc := callTargets(byName(recs, "main"))
	if _, ok := cc["helper"]; !ok {
		t.Errorf("main should CALL helper; got %v", cc)
	}
	// EXTERN printf:PROC → external locality on the printf call.
	if e, ok := cc["printf"]; !ok {
		t.Errorf("main should CALL printf; got %v", cc)
	} else if e.Properties["locality"] != "external" {
		t.Errorf("printf locality=%q want external (EXTERN)", e.Properties["locality"])
	}
	if e, ok := cc["ExitProcess"]; !ok {
		t.Errorf("main should CALL ExitProcess; got %v", cc)
	} else if e.Properties["locality"] != "external" {
		t.Errorf("ExitProcess locality=%q want external (EXTERN)", e.Properties["locality"])
	}

	// INCLUDE / INCLUDELIB → IMPORTS edges.
	imps := importedModules(recs)
	for _, want := range []string{"windows.inc", "kernel32.lib"} {
		if !imps[want] {
			t.Errorf("missing IMPORTS for %q; got %v", want, imps)
		}
	}
	// EQU constant still parses alongside the structured directives.
	if c := byName(recs, "KMAX"); c == nil || c.Kind != "SCOPE.Constant" {
		t.Errorf("KMAX EQU should be a constant; got %v", c)
	}

	// #5055 — `name STRUCT`/`ENDS` record type → SCOPE.Component(subtype=struct).
	if s := byName(recs, "POINT"); s == nil {
		t.Fatalf("POINT STRUCT not extracted")
	} else {
		if s.Kind != "SCOPE.Component" || s.Subtype != "struct" {
			t.Errorf("POINT STRUCT kind/subtype = %q/%q want SCOPE.Component/struct", s.Kind, s.Subtype)
		}
		if s.Properties["framing"] != "masm" {
			t.Errorf("POINT framing = %q want masm", s.Properties["framing"])
		}
		// ENDS must bound the struct span beyond its opener line.
		if s.EndLine <= s.StartLine {
			t.Errorf("POINT STRUCT span not closed by ENDS: start=%d end=%d", s.StartLine, s.EndLine)
		}
	}

	// #5055 — `name SEGMENT`/`ENDS` directive → SCOPE.Component(subtype=section).
	if seg := byName(recs, "_DATA"); seg == nil {
		t.Fatalf("_DATA SEGMENT not extracted")
	} else {
		if seg.Kind != "SCOPE.Component" || seg.Subtype != "section" {
			t.Errorf("_DATA SEGMENT kind/subtype = %q/%q want SCOPE.Component/section", seg.Kind, seg.Subtype)
		}
		if seg.EndLine <= seg.StartLine {
			t.Errorf("_DATA SEGMENT span not closed by ENDS: start=%d end=%d", seg.StartLine, seg.EndLine)
		}
	}
}

// #5055 — STRUCT/SEGMENT no-op guards -------------------------------------

// masmStructComponents returns SCOPE.Component records whose framing=masm,
// i.e. the STRUCT/SEGMENT blocks introduced by #5055.
func masmBlockComponents(recs []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Properties["framing"] == "masm" {
			out = append(out, r)
		}
	}
	return out
}

// A non-MASM fixture (gas x86-64) contains no MASM STRUCT/SEGMENT blocks, so
// the #5055 path must be a no-op for it — wrong-dialect input yields nothing.
func TestMasmBlocksWrongDialectNoOp(t *testing.T) {
	src := loadFixture(t, "x86_64_gas.s.fixture")
	recs := extractAssembly(src, "boot.s", "assembly")
	if blocks := masmBlockComponents(recs); len(blocks) != 0 {
		t.Errorf("gas fixture should yield no MASM STRUCT/SEGMENT blocks; got %v", blocks)
	}
}

// MASM source with PROC framing but no STRUCT/SEGMENT directives yields no
// #5055 block components — no spurious matches on PROC/EQU/INCLUDE lines.
func TestMasmBlocksNoMatchNoOp(t *testing.T) {
	src := `INCLUDE windows.inc
PUBLIC main
KMAX EQU 100
.code
main PROC
    call printf
    ret
main ENDP
END
`
	recs := extractAssembly(src, "nomatch.asm", "assembly")
	if blocks := masmBlockComponents(recs); len(blocks) != 0 {
		t.Errorf("MASM without STRUCT/SEGMENT should yield no block components; got %v", blocks)
	}
}

// ARM armasm structured directives (#4950) --------------------------------

func TestExtractARMArmasmStructured(t *testing.T) {
	src := loadFixture(t, "arm.armasm.fixture")
	recs := extractAssembly(src, "arm.s", "assembly")

	// name PROC and name FUNCTION both open procedures.
	for _, p := range []string{"main", "compute"} {
		r := byName(recs, p)
		if r == nil || r.Subtype != "procedure" {
			t.Fatalf("armasm proc %q missing: %v", p, r)
		}
	}
	if m := byName(recs, "main"); m == nil || m.Properties["exported"] != "true" {
		t.Errorf("main should be exported via EXPORT: %v", m)
	}

	// AREA |.text|, CODE → section component.
	if s := byName(recs, ".text"); s == nil || s.Subtype != "section" {
		t.Errorf("AREA .text should be a section; got %v", s)
	}

	// IMPORT printf → external locality on the printf call.
	cc := callTargets(byName(recs, "main"))
	if e, ok := cc["printf"]; !ok {
		t.Errorf("main should CALL printf; got %v", cc)
	} else if e.Properties["locality"] != "external" {
		t.Errorf("printf locality=%q want external (IMPORT)", e.Properties["locality"])
	}
	if _, ok := cc["compute"]; !ok {
		t.Errorf("main should CALL compute; got %v", cc)
	}

	// GET / INCLUDE → IMPORTS edges.
	imps := importedModules(recs)
	for _, want := range []string{"macros.inc", "defs.inc"} {
		if !imps[want] {
			t.Errorf("missing IMPORTS for %q; got %v", want, imps)
		}
	}

	// #5056 — column-1 EQU equate (plain + bar-delimited |Cfg.Flags|).
	if c := byName(recs, "MAXLEN"); c == nil || c.Kind != "SCOPE.Constant" || c.Subtype != "equate" {
		t.Errorf("MAXLEN should be a SCOPE.Constant equate; got %v", c)
	}
	if c := byName(recs, "Cfg.Flags"); c == nil || c.Kind != "SCOPE.Constant" {
		t.Errorf("bar-delimited |Cfg.Flags| EQU should be a constant (bars stripped); got %v", c)
	}

	// #5056 — column-1 DCx data definitions (plain + bar-delimited).
	if d := byName(recs, "buffer"); d == nil || d.Kind != "SCOPE.Variable" || d.Subtype != "data" {
		t.Fatalf("buffer DCD should be a SCOPE.Variable data; got %v", d)
	} else if d.Properties["data_width"] != "DCD" {
		t.Errorf("buffer data_width=%q want DCD", d.Properties["data_width"])
	}
	if d := byName(recs, "tbl.entry"); d == nil || d.Kind != "SCOPE.Variable" {
		t.Errorf("bar-delimited |tbl.entry| DCW should be a data variable (bars stripped); got %v", d)
	} else if d.Properties["data_width"] != "DCW" {
		t.Errorf("tbl.entry data_width=%q want DCW", d.Properties["data_width"])
	}
	if d := byName(recs, "banner"); d == nil || d.Properties["data_width"] != "DCB" {
		t.Errorf("banner DCB should be a data variable width DCB; got %v", d)
	}

	// #5056 — bar-delimited AREA name survives scrubbing.
	if s := byName(recs, ".data"); s == nil || s.Subtype != "section" {
		t.Errorf("AREA |.data| should be a section; got %v", s)
	}

	// #5056 — a trailing `| comment` after the bl is still blanked: the call
	// edge survives but the comment text must not leak into any entity.
	if byName(recs, "tail-call") != nil || byName(recs, "into") != nil {
		t.Errorf("trailing m68k-style | comment leaked into an entity")
	}
}

// #5056 — no-op guards: wrong dialect and no-match inputs emit no EQU/DCD
// constants or data beyond the file entity.
func TestArmasmColumn1NoOp(t *testing.T) {
	// Wrong language: a NASM source has no armasm column-1 DCx; the DCx path
	// must not fabricate data entities from `dd`/`db` style NASM directives.
	nasm := loadFixture(t, "x86_64.nasm.fixture")
	for _, r := range extractAssembly(nasm, "boot.nasm", "assembly") {
		if r.Subtype == "data" {
			t.Errorf("NASM fixture produced an armasm DCx data entity: %v", r)
		}
	}

	// No-match: a body with only instructions and comments emits no EQU/DCD.
	noMatch := "        AREA    code, CODE\n        mov     r0, #1   | just a comment\n        bx      lr\n"
	for _, r := range extractAssembly(noMatch, "n.s", "assembly") {
		if r.Subtype == "data" || r.Subtype == "equate" {
			t.Errorf("no-match input produced %s entity %q", r.Subtype, r.Name)
		}
	}
}

// Full pipeline through Extract (language tagging) -------------------------

func TestExtractTagsLanguage(t *testing.T) {
	e := &Extractor{}
	src := loadFixture(t, "x86_64_gas.s.fixture")
	recs, err := e.Extract(context.Background(), extractor.FileInput{
		Path: "boot.s", Content: []byte(src), Language: "assembly",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.Language != "assembly" {
			t.Errorf("entity %q language=%q want assembly", r.Name, r.Language)
		}
	}
}
