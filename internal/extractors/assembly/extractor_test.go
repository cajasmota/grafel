package assembly

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
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

	// .L local labels must NOT become procedures.
	if byName(recs, ".Ldone") != nil || byName(recs, ".Lok") != nil {
		t.Error("local .L labels must not be procedures")
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
	if e, ok := mc[".Ldone"]; !ok {
		t.Error("main should branch to .Ldone")
	} else if e.Properties["edge_kind"] != "branch" {
		t.Errorf(".Ldone edge_kind=%q want branch", e.Properties["edge_kind"])
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
	if _, ok := sc[".Lloop"]; !ok {
		t.Error("_start should branch to .Lloop (b)")
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
