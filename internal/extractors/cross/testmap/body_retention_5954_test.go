package testmap

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// #5954 — testmap string-retention.
//
// Every identifier the resolver lifts out of a test function comes from
// regexp's *String* API, which slices the input rather than copying it
// (`s[a:b]`). The sliced-out identifier therefore keeps the WHOLE backing
// array — the entire captured test-function body — alive for as long as the
// identifier is reachable. The identifiers land in EntityRecord properties and
// relationship properties, which live from pass 3 until the document is
// written, so each emitted test_coverage record pins its own multi-KB test
// body across the peak-heap phase.
//
// On a heap profile taken at the child's peak (`heap-peak.pprof`,
// materializing) this showed up as 223 MB under
// strings.Join <- extractIndentedBody <- detectPytest, i.e. bodies that are
// semantically dead but structurally reachable.
//
// Both tests below fail on the unfixed extractor:
//   - the pointer test fails deterministically (identifier addresses land
//     inside the body buffer),
//   - the retention test fails by ~16 MB of retained heap.

// pointsInto reports whether s's backing array starts inside buf's backing
// array. A true result means s is a substring view of buf and keeps all of buf
// alive.
func pointsInto(s, buf string) bool {
	if len(s) == 0 || len(buf) == 0 {
		return false
	}
	base := uintptr(unsafe.Pointer(unsafe.StringData(buf)))
	p := uintptr(unsafe.Pointer(unsafe.StringData(s)))
	return p >= base && p < base+uintptr(len(buf))
}

// TestPointsIntoDetectsSubstringViews proves the detector itself works — a
// substring view is reported, an independent copy is not. Without this the
// retention assertions below could pass vacuously.
func TestPointsIntoDetectsSubstringViews(t *testing.T) {
	buf := strings.Repeat("x", 4096) + "MARKER" + strings.Repeat("y", 4096)
	view := buf[4096 : 4096+len("MARKER")]
	if !pointsInto(view, buf) {
		t.Fatal("pointsInto() = false for a substring view of buf; the detector cannot observe retention")
	}
	if pointsInto(strings.Clone(view), buf) {
		t.Fatal("pointsInto() = true for an independent copy; the detector reports false positives")
	}
}

// TestBuildCollapsedEntity_DoesNotRetainBodyBuffer is the deterministic
// assertion: no string reachable from the emitted EntityRecord may be a view
// into the test-body buffer the identifiers were sliced out of.
func TestBuildCollapsedEntity_DoesNotRetainBodyBuffer(t *testing.T) {
	// A realistic shape: one large body buffer, identifiers sliced out of it.
	body := strings.Repeat("    filler_line_that_is_not_a_call\n", 4096) +
		"    result = CreateOrder(payload)\n" +
		"    other = ChargeCard(payload)\n"
	testName := "test_create_order_charges_card"
	holder := testName + "\x00" + body // one buffer holding both, as a real source would

	tf := testFunction{
		qname: holder[:len(testName)],
		body:  holder[len(testName)+1:],
	}
	calls := resolveCalls(tf, "orders.py", "CreateOrder", nil)
	if len(calls) == 0 {
		t.Fatal("resolveCalls() returned no calls; the fixture cannot exhibit retention")
	}
	// The fixture must actually be in the retaining state before the record is
	// built, otherwise the assertion below is vacuous.
	retaining := false
	for _, c := range calls {
		if pointsInto(c.qname, holder) {
			retaining = true
		}
	}
	if !retaining {
		t.Fatal("no resolved call qname is a view into the source buffer; the fixture cannot exhibit the bug")
	}

	rec := buildCollapsedEntity("orders_test.py", "python", "pytest", "unit", tf.qname, calls)

	// A clone that changes the VALUE would satisfy every pointer assertion
	// below while corrupting the graph — the dangerous failure mode for a
	// lifetime change is wrong data, not a crash. Pin the values too, so this
	// file backs its own claim instead of leaning on the rest of the package
	// suite. (Mutation: `strings.Clone(testQName) + "_CORRUPT"` passes every
	// pointer check and fails here.)
	if got, want := rec.Properties["test_function"], testName; got != want {
		t.Errorf("Properties[\"test_function\"] = %q, want %q — the clone changed the value", got, want)
	}
	if got, want := rec.Properties["tested_function"], calls[0].qname; got != want {
		t.Errorf("Properties[\"tested_function\"] = %q, want %q — the clone changed the value", got, want)
	}
	if got, want := rec.Name, testName+" -> "+calls[0].qname; got != want {
		t.Errorf("EntityRecord.Name = %q, want %q — the clone changed the value", got, want)
	}

	for k, v := range rec.Properties {
		if pointsInto(v, holder) {
			t.Errorf("EntityRecord.Properties[%q] = %q is a view into the %d-byte source buffer; it pins the whole test body across the peak-heap phase", k, v, len(holder))
		}
	}
	if pointsInto(rec.Name, holder) {
		t.Errorf("EntityRecord.Name = %q is a view into the source buffer", rec.Name)
	}
	for _, rel := range rec.Relationships {
		for _, kv := range rel.Properties {
			if pointsInto(kv.V, holder) {
				t.Errorf("RelationshipRecord.Properties[%q] = %q is a view into the %d-byte source buffer", kv.K, kv.V, len(holder))
			}
		}
		if pointsInto(rel.FromID, holder) || pointsInto(rel.ToID, holder) {
			t.Errorf("RelationshipRecord endpoint is a view into the source buffer (from=%q to=%q)", rel.FromID, rel.ToID)
		}
	}
}

// TestExtract_DoesNotRetainTestBodies is the end-to-end retention check: after
// extracting N large pytest files and keeping ONLY the returned records, the
// retained heap must be a small fraction of the source that produced them.
func TestExtract_DoesNotRetainTestBodies(t *testing.T) {
	const (
		files    = 8
		bodyKB   = 2048 // 2 MB of body per file => 16 MB of bodies total
		budgetMB = 6    // unfixed retains ~16 MB; fixed retains well under 1 MB
	)

	// The `before` snapshot MUST be taken while the fixture does not yet exist.
	// Taking it with `sources` already live silently subtracts the fixture size
	// from the measurement: the sources are released before `after`, so ~17 MB
	// of legitimate shrinkage cancels ~16 MB of illegitimate retention and the
	// test reads ≈0. That confounding is what let the call-qname retention path
	// slip past this guard in the first place.
	runtime.GC()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	sources := make([]string, files)
	totalBytes := 0
	for i := range sources {
		var sb strings.Builder
		sb.WriteString("import pytest\n\n")
		for f := 0; f < 4; f++ {
			fmt.Fprintf(&sb, "def test_case_%d_%d():\n", i, f)
			// Padding lines are indented so they are part of the function body.
			sb.WriteString(strings.Repeat("    x = 1  # padding padding padding padding padding\n", bodyKB*1024/(4*50)))
			fmt.Fprintf(&sb, "    result = CreateOrder_%d_%d(payload)\n", i, f)
		}
		sources[i] = sb.String()
		totalBytes += len(sources[i])
	}

	// The budget assertion below can only observe the bug if the bodies the
	// records would pin are substantially larger than the budget. Without this
	// gate, shrinking the fixture silently turns the test into a tautology —
	// the exact failure mode that has bitten this repo repeatedly. Verified by
	// mutation: dropping bodyKB to 8 fails HERE rather than passing green.
	totalMB := float64(totalBytes) / (1 << 20)
	if totalMB < 2*budgetMB {
		t.Fatalf("fixture generates only %.1f MB of test bodies against a %d MB budget; it cannot exhibit retention and the assertion below would pass vacuously", totalMB, budgetMB)
	}

	e := &Extractor{}
	var kept [][]types.EntityRecord

	for i, src := range sources {
		recs, err := e.Extract(context.Background(), extractor.FileInput{
			Path:     fmt.Sprintf("tests/test_orders_%d.py", i),
			Language: "python",
			Content:  []byte(src),
		})
		if err != nil {
			t.Fatalf("Extract() error = %v", err)
		}
		if len(recs) == 0 {
			t.Fatalf("Extract() returned no records for file %d; the fixture cannot exhibit retention", i)
		}
		kept = append(kept, recs)
	}
	// Drop every reference to the inputs; only `kept` may hold memory now.
	for i := range sources {
		sources[i] = ""
	}

	runtime.GC()
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	retainedMB := float64(retained) / (1 << 20)
	nrec := 0
	for _, r := range kept {
		nrec += len(r)
	}
	t.Logf("retained heap after extracting %.1f MB of test source across %d files: %.1f MB (%d records)",
		totalMB, files, retainedMB, nrec)

	if retainedMB > budgetMB {
		t.Errorf("Extract() retained %.1f MB, want <= %d MB — the emitted records are pinning the test-function bodies they were sliced out of", retainedMB, budgetMB)
	}
	runtime.KeepAlive(kept)
}
