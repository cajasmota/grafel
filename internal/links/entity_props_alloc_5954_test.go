package links

// entity_props_alloc_5954_test.go — pins the heap cost of projecting a
// graph.Entity into the link pass's entityNode (#5954, slice 2).
//
// Background: #5976 compacted the extraction records' properties from
// map[string]string to the sorted-slice types.Props. loadAllGraphs then
// RE-INFLATED every entity's property set back into a map[string]string via
// PropsSnapshot, one map per entity, ~427k entities on the reference corpus.
//
// What these tests measure, precisely: BYTES ALLOCATED per projection, via
// runtime.MemStats.TotalAlloc. That is NOT resident heap. Allocation volume
// and steady-state RSS differ — a transient map is allocated and collected,
// and a property set that later passes stamp four more keys onto grows past
// its loaded size. No test in this package pins resident heap; treat any
// RSS figure quoted elsewhere as a separate, hand-run measurement.
//
// The measurement is deliberately BYTES, not testing.AllocsPerRun's
// allocation COUNT. Count alone is a weak pin here: a map with a small hint
// and a slice are both "a couple of allocations", so a count assertion would
// still pass if someone reintroduced the map.

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// allocBytesPerOp returns the number of bytes f allocates per call, averaged
// over n calls and MINIMISED over allocTrials repetitions. Callers must
// pre-size any destination slice so only the work under test is billed.
//
// runtime.MemStats.TotalAlloc is PROCESS-wide, so anything else allocating
// concurrently is billed to f. In practice this measurement is stable — it
// reports exactly 64.0 B/entity both in isolation and with the whole links
// suite running before it — so the min-over-trials is insurance rather than a
// fix for an observed flake. It is sound insurance because outside traffic can
// only ADD to the delta, never subtract: the minimum across trials can be
// dragged down towards f's true cost but never below it, and a genuine
// regression raises every trial including the minimum.
func allocBytesPerOp(n int, f func(i int)) float64 {
	const allocTrials = 7

	// Warm up so lazily-initialised globals inside f are not billed.
	f(0)

	best := math.Inf(1)
	for trial := 0; trial < allocTrials; trial++ {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for i := 0; i < n; i++ {
			f(i)
		}
		runtime.ReadMemStats(&after)
		got := float64(after.TotalAlloc-before.TotalAlloc) / float64(n)
		if got < best {
			best = got
		}
	}
	return best
}

// syntheticEntities builds n graph.Entity values with a UNIFORM 2-property
// set. This is the regression-detector fixture, not the headline-number
// fixture: 2 entries is close to the map's worst case (a full 48 B header plus
// a whole 272 B bucket for two pairs) and the slice's best case (exactly 64 B,
// no slack), which maximises the signal a ceiling assertion can see. It
// deliberately OVERSTATES the corpus-wide saving — see realisticEntities.
func syntheticEntities(n int) []graph.Entity {
	out := make([]graph.Entity, n)
	for i := range out {
		out[i] = graph.Entity{
			ID:         fmt.Sprintf("e%06d", i),
			Name:       fmt.Sprintf("Handler%06d", i),
			Kind:       "function",
			Subtype:    "function",
			SourceFile: fmt.Sprintf("src/pkg%03d/mod%05d.go", i%512, i%4096),
			StartLine:  i%900 + 1,
			EndLine:    i%900 + 12,
		}.WithProperties(map[string]string{
			"language": "go",
			"line":     fmt.Sprintf("%d", i%900+1),
		})
	}
	return out
}

// realisticEntities builds n graph.Entity values with a MIXED property-count
// distribution approximating what the extractors actually emit across a corpus:
//
//	25% with 0 properties   (plain functions/classes carrying no annotations)
//	50% with 2 properties   (the language/line pair)
//	15% with 5 properties   (HTTP endpoint entities: verb/path/framework/…)
//	10% with 12 properties  (richly annotated endpoints, long keys and values)
//
// This is the fixture the headline saving should be quoted from. The two
// representations converge as the property count rises — one map header
// amortises over more pairs while the slice grows linearly — so the uniform
// 2-property fixture flatters the change by roughly a third.
func realisticEntities(n int) []graph.Entity {
	out := make([]graph.Entity, n)
	for i := range out {
		var props map[string]string
		switch bucket := i % 20; {
		case bucket < 5: // 25%: none
			props = nil
		case bucket < 15: // 50%: two
			props = map[string]string{
				"language": "go",
				"line":     fmt.Sprintf("%d", i%900+1),
			}
		case bucket < 18: // 15%: five
			props = map[string]string{
				"verb": "GET", "path": fmt.Sprintf("/api/v1/orders/%d", i%97),
				"framework": "django", "pattern_type": "http_endpoint_synthesis",
				"language": "python",
			}
		default: // 10%: twelve, with long keys and values
			props = map[string]string{
				"verb": "POST", "path": fmt.Sprintf("/api/v1/organisations/{org_id}/orders/%d/reconcile", i%97),
				"framework": "django-rest-framework", "pattern_type": "http_endpoint_synthesis",
				"language": "python", "lookup_url_kwarg": "organisation_identifier",
				"source_caller": "Class:OrderReconciliationViewSet", "url_prefix": "/api/v1",
				"substrate_resolved_via":   "settings.BASE_URL>env(SERVICE_ROOT)>literal",
				"substrate_resolved_value": "https://orders.internal.example.com/api/v1",
				"substrate_confidence":     "0.85", "effects": "db_read,db_write,http_out,env_read",
			}
		}
		out[i] = graph.Entity{
			ID:         fmt.Sprintf("e%06d", i),
			Name:       fmt.Sprintf("Handler%06d", i),
			Kind:       "function",
			Subtype:    "function",
			SourceFile: fmt.Sprintf("src/pkg%03d/mod%05d.go", i%512, i%4096),
			StartLine:  i%900 + 1,
			EndLine:    i%900 + 12,
		}.WithProperties(props)
	}
	return out
}

// maxEntityNodeProjectionBytes is the per-entity allocation ceiling for
// newEntityNode on the uniform 2-property fixture. A 2-entry types.Props is
// 64 B. The ceiling must sit below the CHEAPEST re-inflation shape, not merely
// below a bare map — TestEntityNodeProjection_MapReintroductionWouldFail
// asserts exactly that, so raising this constant past the cheapest bounce
// fails there rather than silently defanging the gate.
const maxEntityNodeProjectionBytes = 150

// TestEntityNodeProjection_DoesNotAllocateAMapPerEntity is the primary gate:
// projecting one entity must cost roughly one small slice, not a map.
func TestEntityNodeProjection_DoesNotAllocateAMapPerEntity(t *testing.T) {
	const n = 50_000
	src := syntheticEntities(n)
	dst := make([]entityNode, n)

	got := allocBytesPerOp(n, func(i int) {
		dst[i] = newEntityNode(src[i])
	})

	// Keep dst alive past the measurement so the projected property storage
	// is genuinely resident, exactly as it is for the whole link pass.
	if dst[n-1].ID == "" {
		t.Fatal("projection produced an empty entity — measurement is not exercising the path")
	}

	t.Logf("newEntityNode: %.1f bytes/entity over %d entities", got, n)
	if got > maxEntityNodeProjectionBytes {
		t.Fatalf("newEntityNode allocates %.1f bytes/entity, want <= %d — "+
			"a map[string]string property set has been (re)introduced on the load path",
			got, maxEntityNodeProjectionBytes)
	}
}

// TestEntityNodeProjection_MapReintroductionWouldFail is the anti-vacuity
// check on maxEntityNodeProjectionBytes.
//
// It bounds the ceiling against the CHEAPEST re-inflation shape, not against a
// bare map. That distinction is the whole point. A bare PropsSnapshot costs
// ~336 B/entity, but the realistic regression — someone writing
// `types.PropsFromMap(e.PropsSnapshot())` — is far cheaper, because the map
// never escapes and much of it is billed away by inlining. Measured, that
// bounce is ~168 B/entity. A ceiling asserted only against 336 would happily
// admit a 200-byte limit that lets the 168-byte bounce through; asserting
// against the bounce itself closes that hole.
func TestEntityNodeProjection_MapReintroductionWouldFail(t *testing.T) {
	const n = 50_000
	src := syntheticEntities(n)

	bare := make([]map[string]string, n)
	bareBytes := allocBytesPerOp(n, func(i int) {
		bare[i] = src[i].PropsSnapshot()
	})

	// The shape a regression would actually take: inflate to a map, then
	// convert straight back to the field's Props type.
	bounced := make([]types.Props, n)
	bounceBytes := allocBytesPerOp(n, func(i int) {
		bounced[i] = types.PropsFromMap(src[i].PropsSnapshot())
	})

	t.Logf("bare map projection: %.1f bytes/entity; cheapest map bounce: %.1f bytes/entity (over %d entities)",
		bareBytes, bounceBytes, n)

	if bounceBytes <= maxEntityNodeProjectionBytes {
		t.Fatalf("the cheapest map re-inflation costs %.1f bytes/entity, which is UNDER the "+
			"%d-byte ceiling — the ceiling no longer excludes a map bounce and the primary "+
			"assertion is vacuous. Lower maxEntityNodeProjectionBytes.",
			bounceBytes, maxEntityNodeProjectionBytes)
	}
	if bareBytes <= bounceBytes {
		t.Errorf("bare map (%.1f B) is not more expensive than the bounce (%.1f B) — "+
			"the cost model this test reasons about no longer holds", bareBytes, bounceBytes)
	}
}

// TestEntityNodeProjection_RealisticDistributionSaving is the fixture the
// headline corpus number should be quoted from. It asserts only the DIRECTION
// (Props strictly cheaper) and logs the magnitude, because the magnitude
// depends on the property-count distribution, which is a property of the
// corpus and not of this code. Pinning it to a constant would be pinning an
// assumption about someone else's repositories.
func TestEntityNodeProjection_RealisticDistributionSaving(t *testing.T) {
	const n = 50_000
	src := realisticEntities(n)

	nodes := make([]entityNode, n)
	propsBytes := allocBytesPerOp(n, func(i int) {
		nodes[i] = newEntityNode(src[i])
	})

	maps := make([]map[string]string, n)
	mapBytes := allocBytesPerOp(n, func(i int) {
		maps[i] = src[i].PropsSnapshot()
	})

	saving := mapBytes - propsBytes
	t.Logf("realistic distribution (25%%×0 / 50%%×2 / 15%%×5 / 10%%×12 props): "+
		"Props %.1f B/entity vs map %.1f B/entity — saving %.1f B/entity "+
		"(~%.0f MB at the 427k-entity reference corpus)",
		propsBytes, mapBytes, saving, saving*427_000/1e6)

	if saving <= 0 {
		t.Fatalf("Props (%.1f B/entity) is not cheaper than the map (%.1f B/entity) on a "+
			"realistic property distribution — the premise of this slice does not hold",
			propsBytes, mapBytes)
	}
	runtime.KeepAlive(nodes)
	runtime.KeepAlive(maps)
}

// TestEntityNodeProjection_PropertiesReadBackIdentically guards the obvious
// way to pass the allocation gate for the wrong reason: dropping the
// properties entirely. Every key/value on the source entity must still be
// readable off the projected node.
func TestEntityNodeProjection_PropertiesReadBackIdentically(t *testing.T) {
	want := map[string]string{
		"language":     "python",
		"path":         "/api/users/{id}",
		"verb":         "GET",
		"pattern_type": "http_endpoint_synthesis",
	}
	e := graph.Entity{ID: "x1", Name: "getUser", Kind: "function"}.WithProperties(want)
	n := newEntityNode(e)

	if got := n.Properties.Len(); got != len(want) {
		t.Fatalf("projected property count = %d, want %d", got, len(want))
	}
	for k, v := range want {
		if got := n.Properties.Get(k); got != v {
			t.Errorf("Properties[%q] = %q, want %q", k, got, v)
		}
	}
	if got, ok := n.Properties.Lookup("absent"); ok || got != "" {
		t.Errorf("Lookup(absent) = (%q, %v), want (\"\", false)", got, ok)
	}
}

// TestLinkPass_NoPropertyMapBounceInProductionCode is LOAD-BEARING, not
// belt-and-braces, for two independent reasons:
//
//  1. The allocation tests pin only the LOAD path (newEntityNode). They say
//     nothing about the 14 passes that read entityNode.Properties afterwards.
//     A single `e.Properties.Snapshot()` inside one of those passes would
//     rebuild the per-entity map this slice removed, spread across the pass
//     pipeline where no bytes/entity assertion is watching, and every
//     functional test would still pass.
//  2. It is the only test that kills the combined mutation "loosen the ceiling
//     AND reintroduce the map". Adjusting maxEntityNodeProjectionBytes upward
//     is caught by MapReintroductionWouldFail, but a re-inflation added
//     somewhere other than newEntityNode is caught ONLY here.
//
// Scope of the guarantee, stated narrowly: this is a TEXTUAL check for
// `.Snapshot()` / `.PropsSnapshot()` calls in non-test files of this package.
// It does NOT detect every possible re-inflation. Building a shadow
// `map[string]string` by hand via `Properties.Range` would slip past it, as
// would any conversion in another package (notably cmd/, which is out of this
// glob entirely). It catches the idiomatic mistake, which is the one that
// actually happens; it is not a proof of absence.
//
// PropsFromMap is allowed — it converts INTO the compact form, which is the
// direction this slice wants. Snapshot/PropsSnapshot are the ones that inflate.
func TestLinkPass_NoPropertyMapBounceInProductionCode(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		checked++
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			code := line
			if j := strings.Index(code, "//"); j >= 0 {
				code = code[:j] // prose about Snapshot is fine; calls are not
			}
			if strings.Contains(code, ".Snapshot()") || strings.Contains(code, ".PropsSnapshot()") {
				t.Errorf("%s:%d re-inflates a property set into a map:\n\t%s\n"+
					"use the types.Props accessors (Get/Lookup/Range/Set) instead", f, i+1, strings.TrimSpace(line))
			}
		}
	}
	if checked < 20 {
		t.Fatalf("only scanned %d non-test files in this package — the glob is not finding the sources", checked)
	}
}

// BenchmarkEntityNodeProjection reports the same figure as
// TestEntityNodeProjection_DoesNotAllocateAMapPerEntity through the standard
// -benchmem surface, for before/after comparison across commits.
func BenchmarkEntityNodeProjection(b *testing.B) {
	src := syntheticEntities(4096)
	dst := make([]entityNode, len(src))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst[i%len(src)] = newEntityNode(src[i%len(src)])
	}
}
