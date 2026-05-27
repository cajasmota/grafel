package substrate

import (
	"strings"
	"testing"
)

// TestEntryPointsRegistryT1Coverage verifies that every Phase 1B T1
// language (#2766) has a registered entry-point sniffer.
func TestEntryPointsRegistryT1Coverage(t *testing.T) {
	for _, lang := range []string{"go", "python", "java", "jsts"} {
		if EntryPointSnifferFor(lang) == nil {
			t.Errorf("T1 language %q has no registered entry-point sniffer", lang)
		}
	}
}

func TestSniffGoEntryPoints(t *testing.T) {
	src := `package main

func main() {}

func init() { /* runtime hook */ }

func TestFoo(t *testing.T) {}
func BenchmarkBar(b *testing.B) {}
func Exported() {}
func unexported() {}

func (s *Server) Public() {}
func (s *Server) private() {}
`
	eps := sniffGoEntryPoints(src)
	got := map[string]EntryKind{}
	for _, e := range eps {
		got[e.Ident] = e.Kind
	}
	cases := map[string]EntryKind{
		"main":         EntryKindCLIMain,
		"init":         EntryKindFrameworkLifecycle,
		"TestFoo":      EntryKindTestEntry,
		"BenchmarkBar": EntryKindTestEntry,
		"Exported":     EntryKindLibraryExport,
		"Public":       EntryKindLibraryExport,
	}
	for name, want := range cases {
		if got[name] != want {
			t.Errorf("ep %q: want %s, got %s", name, want, got[name])
		}
	}
	if _, ok := got["unexported"]; ok {
		t.Errorf("unexported (lowercase) should not be an entry-point")
	}
	if _, ok := got["private"]; ok {
		t.Errorf("private method should not be an entry-point")
	}
}

func TestSniffPythonEntryPoints(t *testing.T) {
	src := `from foo import bar

__all__ = ["public_api", "Helper"]

def public_api():
    pass

def _private():
    pass

def main():
    pass

def test_login():
    pass

class TestThing:
    pass

class Helper:
    pass

if __name__ == "__main__":
    main()
`
	eps := sniffPythonEntryPoints(src)
	kinds := map[string][]EntryKind{}
	for _, e := range eps {
		kinds[e.Ident] = append(kinds[e.Ident], e.Kind)
	}
	if len(kinds["__main__"]) == 0 || kinds["__main__"][0] != EntryKindCLIMain {
		t.Errorf("missing or wrong kind for __main__: %v", kinds["__main__"])
	}
	if len(kinds["main"]) == 0 || kinds["main"][0] != EntryKindCLIMain {
		t.Errorf("missing main as cli_main: %v", kinds["main"])
	}
	if len(kinds["test_login"]) == 0 || kinds["test_login"][0] != EntryKindTestEntry {
		t.Errorf("missing test_login as test_entry: %v", kinds["test_login"])
	}
	if len(kinds["TestThing"]) == 0 || kinds["TestThing"][0] != EntryKindTestEntry {
		t.Errorf("missing TestThing as test_entry: %v", kinds["TestThing"])
	}
	if len(kinds["public_api"]) == 0 {
		t.Errorf("missing public_api as library_export")
	}
	if _, ok := kinds["_private"]; ok {
		t.Errorf("_private should not be an entry-point")
	}
}

func TestSniffJavaEntryPoints(t *testing.T) {
	src := `package x;

public class App {
    public static void main(String[] args) {}

    @Test
    public void shouldDoThing() {}

    @PostConstruct
    public void init() {}

    public String compute() { return ""; }

    private void helper() {}
}
`
	eps := sniffJavaEntryPoints(src)
	kinds := map[string]EntryKind{}
	for _, e := range eps {
		kinds[e.Ident] = e.Kind
	}
	if kinds["main"] != EntryKindCLIMain {
		t.Errorf("main should be cli_main, got %v", kinds["main"])
	}
	if kinds["shouldDoThing"] != EntryKindTestEntry {
		t.Errorf("shouldDoThing should be test_entry, got %v", kinds["shouldDoThing"])
	}
	if kinds["init"] != EntryKindFrameworkLifecycle {
		t.Errorf("init (@PostConstruct) should be framework_lifecycle, got %v", kinds["init"])
	}
	if kinds["compute"] != EntryKindLibraryExport {
		t.Errorf("compute should be library_export, got %v", kinds["compute"])
	}
	if _, ok := kinds["helper"]; ok {
		t.Errorf("private helper should not be an entry-point")
	}
}

func TestSniffJSTSEntryPoints(t *testing.T) {
	src := `export function foo() {}
export const bar = 1;
export default class Widget {}

function main() {}

describe("suite", () => {
  it("works", () => {});
  test("also works", () => {});
});

export { internalThing as renamedThing };
`
	eps := sniffJSTSEntryPoints(src)
	idents := map[string]EntryKind{}
	for _, e := range eps {
		// Last-wins is fine; we just want to know shapes were seen.
		idents[e.Ident] = e.Kind
	}
	for _, name := range []string{"foo", "bar", "Widget", "internalThing"} {
		if idents[name] != EntryKindLibraryExport {
			t.Errorf("%q should be library_export, got %v", name, idents[name])
		}
	}
	if idents["main"] != EntryKindCLIMain {
		t.Errorf("main should be cli_main, got %v", idents["main"])
	}
	// it/test/describe are runner names → test_entry per call site.
	for _, runner := range []string{"it", "test", "describe"} {
		if idents[runner] != EntryKindTestEntry {
			t.Errorf("%q runner call should be test_entry, got %v", runner, idents[runner])
		}
	}
}

func TestSniffJSTSEntryPointsEmptyInput(t *testing.T) {
	if eps := sniffJSTSEntryPoints(""); eps != nil {
		t.Errorf("empty content should yield nil, got %v", eps)
	}
}

func TestSniffGoEntryPointsIgnoresTestify(t *testing.T) {
	// "Testify" is not a Go test entry — only Test followed by capital
	// letter or underscore counts.
	src := `package x

func Testify() {}
func TestSomething(t *testing.T) {}
`
	eps := sniffGoEntryPoints(src)
	kinds := map[string]EntryKind{}
	for _, e := range eps {
		kinds[e.Ident] = e.Kind
	}
	if kinds["Testify"] != EntryKindLibraryExport {
		t.Errorf("Testify should be library_export (capitalised, not a test), got %v", kinds["Testify"])
	}
	if kinds["TestSomething"] != EntryKindTestEntry {
		t.Errorf("TestSomething should be test_entry, got %v", kinds["TestSomething"])
	}
}

func TestEntryPointLanguagesSorted(t *testing.T) {
	got := EntryPointLanguages()
	if len(got) < 4 {
		t.Fatalf("expected at least 4 T1 languages, got %v", got)
	}
	for i := 1; i < len(got); i++ {
		if strings.Compare(got[i-1], got[i]) >= 0 {
			t.Errorf("not sorted: %v", got)
			break
		}
	}
}
