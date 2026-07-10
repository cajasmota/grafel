package fbwriter

// Issue #5726 — fail-soft on oversized-graph marshal panic.
//
// On a very large graph the flatbuffers builder panics with
// "cannot grow buffer beyond 2 gigabytes" (the library's hard cap). That
// panic originates in the vendored flatbuffers library, so the daemon can
// only survive it by recover()-ing at the fbwriter boundary and returning a
// normal error. These are white-box tests (package fbwriter) so they can drive
// the internal marshalPanicHook seam.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func withMarshalPanic(t *testing.T, msg string) {
	t.Helper()
	orig := marshalPanicHook
	marshalPanicHook = func() { panic(msg) }
	t.Cleanup(func() { marshalPanicHook = orig })
}

// TestStreamingMarshalRecoversPanic asserts that a panic raised inside the
// marshal path is converted into a returned error rather than propagating and
// aborting the process.
func TestStreamingMarshalRecoversPanic(t *testing.T) {
	withMarshalPanic(t, "cannot grow buffer beyond 2 gigabytes")

	doc := &graph.Document{Repo: "huge-repo"}

	out, err := Marshal(doc)
	if err == nil {
		t.Fatal("expected an error from the recovered marshal panic, got nil (panic would have aborted the daemon)")
	}
	if out != nil {
		t.Errorf("expected nil output on marshal error, got %d bytes", len(out))
	}
	if !strings.Contains(err.Error(), "too large to serialize") {
		t.Errorf("error should describe the oversized-graph failure, got: %v", err)
	}
}

// TestWriteAtomicPreservesLastGoodOnPanic writes a valid graph.fb, then attempts
// a second WriteAtomic whose marshal panics (recovered → error). The original
// graph.fb must stay byte-identical and no stray .tmp may remain.
func TestWriteAtomicPreservesLastGoodOnPanic(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "graph.fb")

	good := &graph.Document{
		Repo: "good",
		Entities: []graph.Entity{
			{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go", StartLine: 1},
		},
	}
	if err := WriteAtomic(out, good); err != nil {
		t.Fatalf("initial WriteAtomic: %v", err)
	}
	before, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read last-good: %v", err)
	}

	// Now a marshal that panics part-way through serialization.
	withMarshalPanic(t, "cannot grow buffer beyond 2 gigabytes")
	if err := WriteAtomic(out, good); err == nil {
		t.Fatal("expected WriteAtomic to return an error when marshal panics, got nil")
	}

	after, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read graph.fb after failed write: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("last-good graph.fb was mutated by a failed write: before=%d bytes after=%d bytes", len(before), len(after))
	}
	if _, err := os.Stat(out + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("stray .tmp left behind after failed marshal: statErr=%v", err)
	}
}
