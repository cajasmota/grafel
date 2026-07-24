// Package resolve — platform_variants_test.go
//
// Issue #1811: unit tests for build-tag mutual-exclusion detection and the
// platform-variant merge path in BuildIndex / byPackageOperation.
package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// -----------------------------------------------------------------------
// parseBuildTag
// -----------------------------------------------------------------------

func TestParseBuildTag_DarwinLinux(t *testing.T) {
	got := parseBuildTag("darwin || linux")
	if !got["darwin"] || !got["linux"] {
		t.Fatalf("expected {darwin, linux}, got %v", got)
	}
	if len(got) != 2 {
		t.Fatalf("expected exactly 2 entries, got %v", got)
	}
}

func TestParseBuildTag_Windows(t *testing.T) {
	got := parseBuildTag("windows")
	if !got["windows"] || len(got) != 1 {
		t.Fatalf("expected {windows}, got %v", got)
	}
}

func TestParseBuildTag_NegatedIgnored(t *testing.T) {
	// "!windows" carries no positive GOOS entry — the positive set is empty.
	got := parseBuildTag("!windows")
	if len(got) != 0 {
		t.Fatalf("negated tag should produce empty set, got %v", got)
	}
}

func TestParseBuildTag_Empty(t *testing.T) {
	if got := parseBuildTag(""); got != nil {
		t.Fatalf("empty tag should return nil, got %v", got)
	}
}

func TestParseBuildTag_ArchOnly(t *testing.T) {
	// "amd64" is not a GOOS — result should be nil.
	if got := parseBuildTag("amd64"); got != nil {
		t.Fatalf("arch-only tag should return nil, got %v", got)
	}
}

// -----------------------------------------------------------------------
// buildTagsMutuallyExclusive
// -----------------------------------------------------------------------

func TestBuildTagsMutuallyExclusive_DarwinLinuxVsWindows(t *testing.T) {
	// The canonical _unix.go / _windows.go split.
	if !buildTagsMutuallyExclusive("darwin || linux", "windows") {
		t.Fatal("darwin||linux and windows must be mutually exclusive")
	}
}

func TestBuildTagsMutuallyExclusive_DarwinVsLinux(t *testing.T) {
	// Two separate POSIX-platform files — still no overlap.
	if !buildTagsMutuallyExclusive("darwin", "linux") {
		t.Fatal("darwin and linux must be mutually exclusive")
	}
}

func TestBuildTagsMutuallyExclusive_OverlapDarwin(t *testing.T) {
	// "darwin" vs "darwin || linux" — darwin appears in both → overlap.
	if buildTagsMutuallyExclusive("darwin", "darwin || linux") {
		t.Fatal("darwin and darwin||linux overlap on darwin; must NOT be mutually exclusive")
	}
}

func TestBuildTagsMutuallyExclusive_BothWindows(t *testing.T) {
	// Same tag on both sides — trivially not exclusive.
	if buildTagsMutuallyExclusive("windows", "windows") {
		t.Fatal("identical tags must NOT be mutually exclusive")
	}
}

func TestBuildTagsMutuallyExclusive_EmptyTag(t *testing.T) {
	// No-tag file cannot be safely merged — must return false.
	if buildTagsMutuallyExclusive("", "windows") {
		t.Fatal("empty tag vs windows must NOT be considered mutually exclusive")
	}
	if buildTagsMutuallyExclusive("darwin || linux", "") {
		t.Fatal("tag vs empty must NOT be considered mutually exclusive")
	}
}

func TestBuildTagsMutuallyExclusive_NoGOOSTag(t *testing.T) {
	// Pure architecture constraint like "amd64" — no GOOS info.
	if buildTagsMutuallyExclusive("amd64", "windows") {
		t.Fatal("arch-only tag vs windows must NOT be considered mutually exclusive")
	}
}

// -----------------------------------------------------------------------
// ExtractFileBuildTag
// -----------------------------------------------------------------------

func TestExtractFileBuildTag_GoModernDirective(t *testing.T) {
	src := []byte("//go:build darwin || linux\n\npackage mcp\n")
	got := ExtractFileBuildTag(src)
	if got != "darwin || linux" {
		t.Fatalf("want 'darwin || linux', got %q", got)
	}
}

func TestExtractFileBuildTag_Windows(t *testing.T) {
	src := []byte("//go:build windows\n\npackage mcp\n")
	got := ExtractFileBuildTag(src)
	if got != "windows" {
		t.Fatalf("want 'windows', got %q", got)
	}
}

func TestExtractFileBuildTag_NoBuildDirective(t *testing.T) {
	src := []byte("package mcp\n\nfunc foo() {}\n")
	got := ExtractFileBuildTag(src)
	if got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestExtractFileBuildTag_LegacyPlusBuild(t *testing.T) {
	src := []byte("// +build darwin linux\n\npackage mcp\n")
	got := ExtractFileBuildTag(src)
	if got != "darwin linux" {
		t.Fatalf("want 'darwin linux', got %q", got)
	}
}

// -----------------------------------------------------------------------
// BuildIndex — byPackageOperation platform-variant merge
//
// These are the four acceptance cases from issue #1811.
// -----------------------------------------------------------------------

// Case 1: darwin||linux vs windows — canonical platform split.
// Both files define the same top-level function; they MUST merge to one entry
// so that a CALLS edge to that function resolves instead of hitting the blank
// sentinel.
func TestBuildIndex_PlatformVariant_DarwinLinuxVsWindows_Merged(t *testing.T) {
	// Simulate readSourceWindow defined in:
	//   internal/mcp/read_source_unix.go    (darwin || linux)
	//   internal/mcp/read_source_windows.go (windows)
	entities := []types.EntityRecord{
		{
			ID:         "aaaaaaaaaaaaaaaa",
			Kind:       "SCOPE.Operation",
			Name:       "readSourceWindow",
			SourceFile: "internal/mcp/read_source_unix.go",
			Language:   "go",
			Properties: map[string]string{"build_tag": "darwin || linux"},
		},
		{
			ID:         "bbbbbbbbbbbbbbbb",
			Kind:       "SCOPE.Operation",
			Name:       "readSourceWindow",
			SourceFile: "internal/mcp/read_source_windows.go",
			Language:   "go",
			Properties: map[string]string{"build_tag": "windows"},
		},
		// Caller in the same package (tools.go — no build tag).
		{
			ID:         "cccccccccccccccc",
			Kind:       "SCOPE.Operation",
			Name:       "handleGetSource",
			SourceFile: "internal/mcp/tools.go",
			Language:   "go",
			Relationships: []types.RelationshipRecord{
				{
					FromID:     "cccccccccccccccc",
					ToID:       "scope:operation:method:go:internal/mcp/tools.go:readSourceWindow",
					Kind:       "CALLS",
					Properties: types.Props{{K: "language", V: "go"}},
				},
			},
		},
	}
	idx := BuildIndex(entities)
	stats := ReferencesEmbedded(entities, idx)

	got := entities[2].Relationships[0].ToID
	// Must resolve to one of the two variant IDs — not the original stub.
	if got != "aaaaaaaaaaaaaaaa" && got != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("#1811 platform-variant merge: readSourceWindow not resolved; ToID=%q, stats=%+v", got, stats)
	}
	if stats.Rewritten < 1 {
		t.Fatalf("#1811 platform-variant merge: expected >=1 rewrite, got %+v", stats)
	}
}

// Case 2: darwin vs linux — two separate POSIX-platform variants, also
// mutually exclusive. Must merge (no darwin+linux overlap).
func TestBuildIndex_PlatformVariant_DarwinVsLinux_Merged(t *testing.T) {
	entities := []types.EntityRecord{
		{
			ID:         "aaaaaaaaaaaaaaaa",
			Kind:       "SCOPE.Operation",
			Name:       "openFd",
			SourceFile: "pkg/fd_darwin.go",
			Language:   "go",
			Properties: map[string]string{"build_tag": "darwin"},
		},
		{
			ID:         "bbbbbbbbbbbbbbbb",
			Kind:       "SCOPE.Operation",
			Name:       "openFd",
			SourceFile: "pkg/fd_linux.go",
			Language:   "go",
			Properties: map[string]string{"build_tag": "linux"},
		},
		{
			ID:         "cccccccccccccccc",
			Kind:       "SCOPE.Operation",
			Name:       "caller",
			SourceFile: "pkg/caller.go",
			Language:   "go",
			Relationships: []types.RelationshipRecord{
				{
					FromID:     "cccccccccccccccc",
					ToID:       "scope:operation:method:go:pkg/caller.go:openFd",
					Kind:       "CALLS",
					Properties: types.Props{{K: "language", V: "go"}},
				},
			},
		},
	}
	idx := BuildIndex(entities)
	_ = ReferencesEmbedded(entities, idx)
	got := entities[2].Relationships[0].ToID
	if got != "aaaaaaaaaaaaaaaa" && got != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("#1811 darwin vs linux: openFd not resolved; ToID=%q", got)
	}
}

// Case 3: darwin vs darwin||linux — overlap on darwin → kept ambiguous.
// Must NOT merge; the blank sentinel should remain so find_callers is not
// silently wrong.
func TestBuildIndex_PlatformVariant_DarwinVsDarwinLinux_StaysAmbiguous(t *testing.T) {
	entities := []types.EntityRecord{
		{
			ID:         "aaaaaaaaaaaaaaaa",
			Kind:       "SCOPE.Operation",
			Name:       "openFd",
			SourceFile: "pkg/fd_darwin.go",
			Language:   "go",
			Properties: map[string]string{"build_tag": "darwin"},
		},
		{
			ID:         "bbbbbbbbbbbbbbbb",
			Kind:       "SCOPE.Operation",
			Name:       "openFd",
			SourceFile: "pkg/fd_darwinlinux.go",
			Language:   "go",
			Properties: map[string]string{"build_tag": "darwin || linux"},
		},
		{
			ID:         "cccccccccccccccc",
			Kind:       "SCOPE.Operation",
			Name:       "caller",
			SourceFile: "pkg/caller.go",
			Language:   "go",
			Relationships: []types.RelationshipRecord{
				{
					FromID:     "cccccccccccccccc",
					ToID:       "scope:operation:method:go:pkg/caller.go:openFd",
					Kind:       "CALLS",
					Properties: types.Props{{K: "language", V: "go"}},
				},
			},
		},
	}
	idx := BuildIndex(entities)
	stats := ReferencesEmbedded(entities, idx)
	got := entities[2].Relationships[0].ToID
	// Must NOT resolve to either entity — stub stays unresolved.
	if got == "aaaaaaaaaaaaaaaa" || got == "bbbbbbbbbbbbbbbb" {
		t.Fatalf("#1811 darwin+darwin||linux overlap: incorrectly resolved; ToID=%q", got)
	}
	_ = stats // ambiguous count not verified precisely; just confirming no resolution
}

// Case 4: no-tag file + tagged file → kept ambiguous.
// A file without a build tag could coexist with any platform; we cannot
// safely treat this as a platform split.
func TestBuildIndex_PlatformVariant_NoTagVsTagged_StaysAmbiguous(t *testing.T) {
	entities := []types.EntityRecord{
		{
			ID:         "aaaaaaaaaaaaaaaa",
			Kind:       "SCOPE.Operation",
			Name:       "doWork",
			SourceFile: "pkg/work.go",
			Language:   "go",
			// No build_tag property.
		},
		{
			ID:         "bbbbbbbbbbbbbbbb",
			Kind:       "SCOPE.Operation",
			Name:       "doWork",
			SourceFile: "pkg/work_windows.go",
			Language:   "go",
			Properties: map[string]string{"build_tag": "windows"},
		},
		{
			ID:         "cccccccccccccccc",
			Kind:       "SCOPE.Operation",
			Name:       "caller",
			SourceFile: "pkg/caller.go",
			Language:   "go",
			Relationships: []types.RelationshipRecord{
				{
					FromID:     "cccccccccccccccc",
					ToID:       "scope:operation:method:go:pkg/caller.go:doWork",
					Kind:       "CALLS",
					Properties: types.Props{{K: "language", V: "go"}},
				},
			},
		},
	}
	idx := BuildIndex(entities)
	_ = ReferencesEmbedded(entities, idx)
	got := entities[2].Relationships[0].ToID
	if got == "aaaaaaaaaaaaaaaa" || got == "bbbbbbbbbbbbbbbb" {
		t.Fatalf("#1811 no-tag vs tagged: incorrectly resolved; ToID=%q", got)
	}
}

// -----------------------------------------------------------------------
// Regression: existing same-package ambiguity (no build tags) still blanks.
// -----------------------------------------------------------------------

func TestBuildIndex_PlatformVariant_NoTags_StillAmbiguous(t *testing.T) {
	// Two genuinely ambiguous definitions with no build tags — must behave
	// exactly like before the platform-variant change (blank sentinel).
	entities := []types.EntityRecord{
		{
			ID:         "aaaaaaaaaaaaaaaa",
			Kind:       "SCOPE.Operation",
			Name:       "doWork",
			SourceFile: "pkg/a.go",
			Language:   "go",
		},
		{
			ID:         "bbbbbbbbbbbbbbbb",
			Kind:       "SCOPE.Operation",
			Name:       "doWork",
			SourceFile: "pkg/b.go",
			Language:   "go",
		},
		{
			ID:         "cccccccccccccccc",
			Kind:       "SCOPE.Operation",
			Name:       "caller",
			SourceFile: "pkg/caller.go",
			Language:   "go",
			Relationships: []types.RelationshipRecord{
				{
					FromID:     "cccccccccccccccc",
					ToID:       "scope:operation:method:go:pkg/caller.go:doWork",
					Kind:       "CALLS",
					Properties: types.Props{{K: "language", V: "go"}},
				},
			},
		},
	}
	idx := BuildIndex(entities)
	_ = ReferencesEmbedded(entities, idx)
	got := entities[2].Relationships[0].ToID
	if got == "aaaaaaaaaaaaaaaa" || got == "bbbbbbbbbbbbbbbb" {
		t.Fatalf("regression: no-tag ambiguity should not resolve; ToID=%q", got)
	}
}

// -----------------------------------------------------------------------
// Issue #1818 — CALLS fan-out: both platform variants receive caller edges.
// -----------------------------------------------------------------------

// TestBuildIndex_PlatformVariant_BothVariantsReceiveCallerEdge is the primary
// regression test for #1818. After #1815 the resolver correctly resolves a
// CALLS stub to the canonical (Unix) variant, but the Windows variant ends up
// with zero inbound edges so find_callers returns "no_incoming_edges" for it.
//
// After the fix, ReferencesEmbeddedWithAllowlist must clone the resolved CALLS
// relationship so both the canonical AND every non-canonical variant ID appear
// as ToID in at least one CALLS edge in the output.
func TestBuildIndex_PlatformVariant_BothVariantsReceiveCallerEdge(t *testing.T) {
	// Mirrors the live dogfood scenario from #1818:
	//   read_source_unix.go    //go:build darwin || linux   ID=aaaa…
	//   read_source_windows.go //go:build windows           ID=bbbb…
	//   tools.go               (no build tag)  CALLS readSourceWindow
	entities := []types.EntityRecord{
		{
			ID:         "aaaaaaaaaaaaaaaa",
			Kind:       "SCOPE.Operation",
			Name:       "readSourceWindow",
			SourceFile: "internal/mcp/read_source_unix.go",
			Language:   "go",
			Properties: map[string]string{"build_tag": "darwin || linux"},
		},
		{
			ID:         "bbbbbbbbbbbbbbbb",
			Kind:       "SCOPE.Operation",
			Name:       "readSourceWindow",
			SourceFile: "internal/mcp/read_source_windows.go",
			Language:   "go",
			Properties: map[string]string{"build_tag": "windows"},
		},
		{
			ID:         "cccccccccccccccc",
			Kind:       "SCOPE.Operation",
			Name:       "handleGetSource",
			SourceFile: "internal/mcp/tools.go",
			Language:   "go",
			Relationships: []types.RelationshipRecord{
				{
					FromID:     "cccccccccccccccc",
					ToID:       "scope:operation:method:go:internal/mcp/tools.go:readSourceWindow",
					Kind:       "CALLS",
					Properties: types.Props{{K: "language", V: "go"}},
				},
			},
		},
	}
	idx := BuildIndex(entities)
	_ = ReferencesEmbedded(entities, idx)

	// Collect all ToIDs emitted by the caller (entity index 2).
	callerRels := entities[2].Relationships
	toIDs := make(map[string]bool, len(callerRels))
	for _, r := range callerRels {
		toIDs[r.ToID] = true
	}

	// Both the canonical (unix) and the non-canonical (windows) variant must
	// appear as a CALLS target. Before the fix, only one of them did.
	if !toIDs["aaaaaaaaaaaaaaaa"] {
		t.Errorf("#1818: unix variant aaaa… not present in caller's CALLS edges; toIDs=%v", toIDs)
	}
	if !toIDs["bbbbbbbbbbbbbbbb"] {
		t.Errorf("#1818: windows variant bbbb… not present in caller's CALLS edges; toIDs=%v", toIDs)
	}
	if len(callerRels) < 2 {
		t.Errorf("#1818: expected at least 2 CALLS relationships (one per variant), got %d", len(callerRels))
	}
}

// TestBuildIndex_PlatformVariant_MultipleCallers_BothVariantsReceiveAll tests
// that when there are MULTIPLE callers of a platform-split function, every
// caller fans out its CALLS edge to both variants.
func TestBuildIndex_PlatformVariant_MultipleCallers_BothVariantsReceiveAll(t *testing.T) {
	entities := []types.EntityRecord{
		{
			ID:         "aaaaaaaaaaaaaaaa",
			Kind:       "SCOPE.Operation",
			Name:       "openPlatformFd",
			SourceFile: "pkg/fd_unix.go",
			Language:   "go",
			Properties: map[string]string{"build_tag": "darwin || linux"},
		},
		{
			ID:         "bbbbbbbbbbbbbbbb",
			Kind:       "SCOPE.Operation",
			Name:       "openPlatformFd",
			SourceFile: "pkg/fd_windows.go",
			Language:   "go",
			Properties: map[string]string{"build_tag": "windows"},
		},
		// First caller.
		{
			ID:         "cccccccccccccccc",
			Kind:       "SCOPE.Operation",
			Name:       "callerA",
			SourceFile: "pkg/caller_a.go",
			Language:   "go",
			Relationships: []types.RelationshipRecord{
				{
					FromID:     "cccccccccccccccc",
					ToID:       "scope:operation:method:go:pkg/caller_a.go:openPlatformFd",
					Kind:       "CALLS",
					Properties: types.Props{{K: "language", V: "go"}},
				},
			},
		},
		// Second caller.
		{
			ID:         "dddddddddddddddd",
			Kind:       "SCOPE.Operation",
			Name:       "callerB",
			SourceFile: "pkg/caller_b.go",
			Language:   "go",
			Relationships: []types.RelationshipRecord{
				{
					FromID:     "dddddddddddddddd",
					ToID:       "scope:operation:method:go:pkg/caller_b.go:openPlatformFd",
					Kind:       "CALLS",
					Properties: types.Props{{K: "language", V: "go"}},
				},
			},
		},
	}
	idx := BuildIndex(entities)
	_ = ReferencesEmbedded(entities, idx)

	// Both callers must have expanded their single stub into two CALLS edges.
	for i, callerName := range []string{"callerA", "callerB"} {
		rels := entities[2+i].Relationships
		toIDs := make(map[string]bool, len(rels))
		for _, r := range rels {
			toIDs[r.ToID] = true
		}
		if !toIDs["aaaaaaaaaaaaaaaa"] || !toIDs["bbbbbbbbbbbbbbbb"] {
			t.Errorf("#1818 multiple callers: %s missing variant edge; toIDs=%v", callerName, toIDs)
		}
	}
}

// TestBuildIndex_PlatformVariant_NoFanOutForAmbiguous verifies that the
// platform-variant fan-out does NOT fire when the two definitions are
// ambiguous (overlapping or missing build tags) — i.e. we never introduce
// false caller edges for genuine same-name collisions.
func TestBuildIndex_PlatformVariant_NoFanOutForAmbiguous(t *testing.T) {
	// Two functions with no build tags — genuine ambiguity.
	entities := []types.EntityRecord{
		{
			ID:         "aaaaaaaaaaaaaaaa",
			Kind:       "SCOPE.Operation",
			Name:       "doWork",
			SourceFile: "pkg/a.go",
			Language:   "go",
		},
		{
			ID:         "bbbbbbbbbbbbbbbb",
			Kind:       "SCOPE.Operation",
			Name:       "doWork",
			SourceFile: "pkg/b.go",
			Language:   "go",
		},
		{
			ID:         "cccccccccccccccc",
			Kind:       "SCOPE.Operation",
			Name:       "caller",
			SourceFile: "pkg/caller.go",
			Language:   "go",
			Relationships: []types.RelationshipRecord{
				{
					FromID:     "cccccccccccccccc",
					ToID:       "scope:operation:method:go:pkg/caller.go:doWork",
					Kind:       "CALLS",
					Properties: types.Props{{K: "language", V: "go"}},
				},
			},
		},
	}
	idx := BuildIndex(entities)
	_ = ReferencesEmbedded(entities, idx)

	// The stub must remain unresolved (ambiguous sentinel → no rewrite).
	// Crucially, PlatformVariants must be empty so no fan-out occurs.
	if len(idx.PlatformVariants) != 0 {
		t.Errorf("#1818 fan-out guard: PlatformVariants must be empty for ambiguous pair, got %v", idx.PlatformVariants)
	}
	rels := entities[2].Relationships
	if len(rels) != 1 {
		t.Errorf("#1818 fan-out guard: caller should still have exactly 1 relationship, got %d", len(rels))
	}
	if rels[0].ToID == "aaaaaaaaaaaaaaaa" || rels[0].ToID == "bbbbbbbbbbbbbbbb" {
		t.Errorf("#1818 fan-out guard: ambiguous doWork must not resolve; ToID=%q", rels[0].ToID)
	}
}
