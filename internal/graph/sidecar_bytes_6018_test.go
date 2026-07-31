package graph

// sidecar_bytes_6018_test.go — pins the exact bytes WriteSidecar emits.
//
// #6018 converted writeJSONAtomic from json.NewEncoder(f) to
// json.Marshal + atomicfile.WriteFile. json.Encoder.Encode appends a trailing
// newline and json.Marshal does not, so the conversion had to add one back by
// hand. That is precisely the kind of one-byte difference no existing test
// noticed, hence this one.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/testsupport"
)

func sidecarFixture() *GraphStatsSidecar {
	return &GraphStatsSidecar{
		Version:            1,
		ComputedAt:         time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
		TotalEntities:      3,
		TotalRelationships: 2,
		Communities:        1,
		Modularity:         0.5,
	}
}

// TestWriteSidecar_TrailingNewline pins the trailing newline json.Encoder used
// to supply. Deleting the explicit `append(b, '\n')` in writeJSONAtomic fails
// this in both modes.
func TestWriteSidecar_TrailingNewline(t *testing.T) {
	for _, pretty := range []bool{false, true} {
		name := "minified"
		if pretty {
			name = "pretty"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			out := filepath.Join(dir, "graph.fb")
			if err := WriteSidecar(out, sidecarFixture(), pretty); err != nil {
				t.Fatalf("WriteSidecar: %v", err)
			}
			b, err := os.ReadFile(filepath.Join(dir, "graph-stats.json"))
			if err != nil {
				t.Fatalf("read sidecar: %v", err)
			}
			if len(b) == 0 || b[len(b)-1] != '\n' {
				t.Fatalf("sidecar does not end in a newline (last 8 bytes: %q)",
					b[max(0, len(b)-8):])
			}
			if n := len(b); n >= 2 && b[n-2] == '\n' {
				t.Fatalf("sidecar ends in TWO newlines; encoder emitted exactly one")
			}
		})
	}
}

// TestWriteSidecar_ByteIdenticalToEncoder compares the file against what
// json.Encoder would have produced, which is what shipped before #6018.
func TestWriteSidecar_ByteIdenticalToEncoder(t *testing.T) {
	for _, pretty := range []bool{false, true} {
		name := "minified"
		if pretty {
			name = "pretty"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			out := filepath.Join(dir, "graph.fb")
			side := sidecarFixture()
			if err := WriteSidecar(out, side, pretty); err != nil {
				t.Fatalf("WriteSidecar: %v", err)
			}
			got, err := os.ReadFile(filepath.Join(dir, "graph-stats.json"))
			if err != nil {
				t.Fatal(err)
			}

			// The pre-#6018 form, reproduced verbatim against a scratch file.
			ref := filepath.Join(dir, "ref.json")
			f, err := os.Create(ref)
			if err != nil {
				t.Fatal(err)
			}
			enc := json.NewEncoder(f)
			if pretty {
				enc.SetIndent("", "  ")
			}
			if err := enc.Encode(side); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(ref)
			if err != nil {
				t.Fatal(err)
			}

			if string(got) != string(want) {
				t.Fatalf("sidecar bytes changed.\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

// TestWriteSidecar_Perm pins the mode. os.Create used 0666&^umask; the helper
// now applies 0644 verbatim (see the atomicfile package doc on umask).
func TestWriteSidecar_Perm(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "graph.fb")
	if err := WriteSidecar(out, sidecarFixture(), false); err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}
	// Windows has no Unix permission bits, so AssertPerm degrades this to
	// "the sidecar is writable" there rather than failing on 0666 != 0644
	// (#6053). The Unix bits are still asserted exactly on unix.
	testsupport.AssertPerm(t, filepath.Join(dir, "graph-stats.json"), 0o644)
}
