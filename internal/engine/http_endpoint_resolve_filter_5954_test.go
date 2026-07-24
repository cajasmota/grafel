package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Parity harness for the final drop-compaction step of
// ResolveHTTPEndpointHandlersWithRepo (#5954).
//
// The pass ends by removing every synthetic whose `source_handler` never
// resolved. That compaction used to allocate a SECOND full copy of the
// whole []types.EntityRecord set (183 MB at the corpus index peak — the
// #1 line-level allocation site) purely to skip a handful of entries.
// It now filters in place over the caller-owned backing array.
//
// These tests pin the OBSERVABLE contract of that step so the in-place
// rewrite cannot change it: the surviving records must be exactly the
// non-dropped ones, in exactly the original relative ORDER (issue #481 —
// merged order drives first-writer-wins disambiguation downstream, so any
// reordering silently changes the graph).

// keeper5954 builds a record the resolver never touches or drops.
func keeper5954(name string) types.EntityRecord {
	return types.EntityRecord{
		Kind:       "SCOPE.Operation",
		Name:       name,
		SourceFile: "keep/" + name + ".py",
		Language:   "python",
	}
}

// dropper5954 builds an http_endpoint synthetic whose source_handler
// points at a handler that exists nowhere in the set, so the resolver
// drops it.
func dropper5954(name string) types.EntityRecord {
	return types.EntityRecord{
		Kind:       "http_endpoint",
		Name:       "http:GET:/" + name,
		SourceFile: "routes/" + name + ".py",
		Language:   "python",
		Properties: map[string]string{
			"source_handler": "Controller:absent_" + name,
		},
	}
}

// TestResolveHTTPEndpointHandlers_DropCompactionOrderParity drives the real
// pass over hand-built record sets with a known drop mask and asserts the
// exact surviving names, in the exact original order.
func TestResolveHTTPEndpointHandlers_DropCompactionOrderParity(t *testing.T) {
	cases := []struct {
		name string
		// mask[i] == true  → element i is a dropper (must NOT survive)
		// mask[i] == false → element i is a keeper (must survive, in order)
		mask []bool
	}{
		{"empty", nil},
		{"drop_none", []bool{false, false, false, false}},
		{"drop_all", []bool{true, true, true}},
		{"drop_first", []bool{true, false, false, false}},
		{"drop_last", []bool{false, false, false, true}},
		{"drop_alternating", []bool{true, false, true, false, true, false}},
		{"drop_interior_run", []bool{false, true, true, true, false}},
		{"single_keeper", []bool{false}},
		{"single_dropper", []bool{true}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := make([]types.EntityRecord, 0, len(tc.mask))
			var wantNames []string
			for i, dropped := range tc.mask {
				n := itoaSmall(i)
				if dropped {
					in = append(in, dropper5954(n))
					continue
				}
				in = append(in, keeper5954("keep_"+n))
				wantNames = append(wantNames, "keep_"+n)
			}

			out, stats := ResolveHTTPEndpointHandlers(in)

			if len(out) != len(wantNames) {
				t.Fatalf("len(out)=%d want %d (out=%s)", len(out), len(wantNames), namesOf5954(out))
			}
			for i := range wantNames {
				if out[i].Name != wantNames[i] {
					t.Fatalf("order/membership mismatch at %d: got %q want %q (out=%s want=%v)",
						i, out[i].Name, wantNames[i], namesOf5954(out), wantNames)
				}
			}

			// stats must be unaffected by the compaction strategy.
			wantDropped := 0
			for _, d := range tc.mask {
				if d {
					wantDropped++
				}
			}
			if stats.HandlerDropped != wantDropped {
				t.Errorf("stats.HandlerDropped=%d want %d (%+v)", stats.HandlerDropped, wantDropped, stats)
			}
			if stats.Synthetics != wantDropped {
				t.Errorf("stats.Synthetics=%d want %d (%+v)", stats.Synthetics, wantDropped, stats)
			}
		})
	}
}

// TestResolveHTTPEndpointHandlers_DropCompactionZeroesTail asserts the
// in-place compaction releases the tail of the backing array, so dropped
// records (and the Properties / Relationships maps + slices they carry)
// are not retained by the surviving slice.
func TestResolveHTTPEndpointHandlers_DropCompactionZeroesTail(t *testing.T) {
	in := []types.EntityRecord{
		keeper5954("keep_a"),
		dropper5954("d1"),
		keeper5954("keep_b"),
		dropper5954("d2"),
	}
	n := len(in)

	out, _ := ResolveHTTPEndpointHandlers(in)
	if len(out) != 2 {
		t.Fatalf("len(out)=%d want 2", len(out))
	}

	// The compaction reuses `in`'s backing array; everything past len(out)
	// must be the zero record so the dropped payloads are collectable.
	tail := in[:n]
	for i := len(out); i < n; i++ {
		r := tail[i]
		if r.Kind != "" || r.Name != "" || r.SourceFile != "" ||
			r.Properties != nil || r.Relationships != nil {
			t.Errorf("tail slot %d not released: %+v", i, r)
		}
	}
}

func namesOf5954(rs []types.EntityRecord) []string {
	out := make([]string, 0, len(rs))
	for i := range rs {
		out = append(out, rs[i].Name)
	}
	return out
}
