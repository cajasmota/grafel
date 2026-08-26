package swift_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// issue6367_anchoring_test.go — swift_package DEPENDS_ON must be anchored on the
// swiftpm_target record that OWNS the edge, not on the source file's path.
//
// THE TWO SITES (#6367, allow-list entry swift:extractTargets:DEPENDS_ON{2} in
// internal/extractors/file_anchored_rels_guard_test.go):
//
//   - package.go:189 — the PRODUCT branch, `.product(name:package:)` deps.
//   - package.go:242 — the BARE-STRING branch, "Target" deps.
//
// Both built the edge with FromID: filePath + "::" + d.name, yet both append to
// `rec`, the SCOPE.Component / swiftpm_target entity for the target itself
// (package.go:164-176). That record's Name is the BARE TARGET NAME ("App") and
// it carries NO QualifiedName, so `Package.swift::App` names no node at all.
//
// WHAT WAS MEASURED, not inferred. The fixture below was extracted, id-stamped
// and pushed through the production resolver pipeline (ResolveImports →
// ReferencesEmbedded). Unlike hcl — where a root-relative path HAPPENS to equal
// the file component's basename and the bad id resolves onto the wrong node —
// no swift_package entity carries `path::name` in any identity field, so the
// offence is DANGLING on every path, root and nested alike:
//
//	root   "Package.swift"          → 5 of 5 DEPENDS_ON DANGLING
//	nested "Sources/Package.swift"  → 5 of 5 DEPENDS_ON DANGLING
//
// The TO side, by contrast, already resolves: ReferencesEmbedded rewrites the
// bare ToID ("Vapor", "Models") into the real entity id. That asymmetry is why
// internal/quality/golden/swift-package-mini/expected.json scored 0/7 while
// spelling its rows with to_bare_name: the rows compared a would-be entity id
// against a literal string on the TO side AND the FROM side never resolved.
// Repairing the rows to to_name + to_kind alone moves 0/7 → 0/7; only fixing
// the FROM anchoring here completes it.
//
// IMPORTS is deliberately untouched: #120 keeps the file path on IMPORTS edges.

// anchorManifest is one SwiftPM manifest chosen so that the two FromID sites are
// BOTH exercised, and neither can be satisfied by keying on the other.
//
// AXIS VARIED — dep_kind, the axis that maps one-to-one onto the two sites:
//   - App      has BOTH a .product dep (site :189) and a bare-string dep (:242)
//   - Models   has ONLY a .product dep
//   - AppTests has BOTH, in the opposite source order to App
//
// AXIS ALSO VARIED — swiftpm_kind: .executableTarget (App), .target (Models),
// .testTarget (AppTests), so a fix anchoring only one declaration form is caught.
//
// AXIS HELD CONSTANT — the manifest text itself, which is byte-identical across
// the two subtests; only the FILE PATH varies there. Because the assertion
// compares resolved ENDPOINT IDENTITIES rather than the path string, a mutant
// cannot pass by keying on the constant manifest: it would have to produce the
// right owning record for all five edges under both paths.
const anchorManifest = `// swift-tools-version:5.7
import PackageDescription

let package = Package(
    name: "AnchorMini",
    dependencies: [
        .package(url: "https://github.com/vapor/vapor.git", from: "4.0.0"),
        .package(url: "https://github.com/vapor/fluent.git", from: "4.0.0"),
    ],
    targets: [
        .executableTarget(
            name: "App",
            dependencies: [
                .product(name: "Vapor", package: "vapor"),
                "Models",
            ]
        ),
        .target(
            name: "Models",
            dependencies: [
                .product(name: "Fluent", package: "fluent"),
            ]
        ),
        .testTarget(
            name: "AppTests",
            dependencies: [
                "App",
                .product(name: "Vapor", package: "vapor"),
            ]
        ),
    ]
)
`

// wantAnchorEdges is the exact set of DEPENDS_ON edges the manifest must yield,
// written as resolved ENDPOINTS — "fromKind:fromName -> toKind:toName".
//
// This is the emitted artefact, not a count: a fix that produced five edges
// anchored on the wrong records, or that swapped an owner for a same-named
// entity of a different kind, changes this set while leaving any tally at 5.
var wantAnchorEdges = []string{
	"SCOPE.Component:App -> SCOPE.Component:Models",
	"SCOPE.Component:App -> SCOPE.External:Vapor",
	"SCOPE.Component:AppTests -> SCOPE.Component:App",
	"SCOPE.Component:AppTests -> SCOPE.External:Vapor",
	"SCOPE.Component:Models -> SCOPE.External:Fluent",
}

// extractManifestAt runs the swift_package extractor on src as if it were the
// file at path, then id-stamps and resolves exactly as production does.
func extractManifestAt(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()

	ext, ok := extractor.Get("swift_package")
	if !ok {
		t.Fatal("swift_package extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "swift_package",
	})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	if len(recs) == 0 {
		t.Fatalf("extract %s: no records", path)
	}
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6367", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}

	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))
	return recs
}

// TestSwiftPackage_DependsOnAnchoredOnOwningTarget is the fix's behavioural
// test: on BOTH a root and a nested path, every DEPENDS_ON edge must land on the
// swiftpm_target record that carries it.
//
// WHAT IS ACTUALLY OBSERVED, stated precisely because an earlier version of this
// comment claimed more than the assertion enforces. The check is that the FROM
// endpoint RESOLVES TO THE OWNING RECORD'S ID. An empty FromID is how the
// extractor guarantees that — graph assembly stamps the owner when FromID is
// empty — and it is the fix this test exists to pin. It is NOT, however, the
// only spelling the assertion accepts.
//
// THE MEASURED BOUNDARY (probed 2026-08-27, both directions run):
//
//	FromID: ""             -> PASSES. Assembly stamps the owner. The fix.
//	FromID: filePath+"::"+name -> FAILS, DANGLING. Names no node. The defect.
//	FromID: "ZZNotAName"   -> FAILS, DANGLING. Any UNRESOLVABLE id is caught.
//	FromID: d.name         -> PASSES. NOT caught, and it is not the fix.
//
// The last line is the guard's edge. extractManifestAt runs the production
// resolver (ResolveImports, ReferencesEmbedded), whose by-name index rewrites a
// bare "App" into the owning record's real id before the assertion ever sees it.
// So resolution REPAIRS a resolvable bare name, and this test cannot distinguish
// it from the fix. That mutant is EQUIVALENT UNDER THIS SUITE — recorded as
// equivalent, with the reason, rather than dressed up as dead; production runs
// the same resolution, so it is likely equivalent there too.
//
// Why write it down instead of forcing a kill: bare-name resolution is the
// FRAGILE path. If two entities ever shared a target name, a bare-name FromID
// would silently misanchor onto whichever the index happened to return, and
// this guard would not see it — the #6122 / #6124 name-collision family.
// Deliberately NOT fixed here; this note marks where the edge is so the next
// person meets a known limit rather than a surprise.
func TestSwiftPackage_DependsOnAnchoredOnOwningTarget(t *testing.T) {
	// BOTH paths are load-bearing and neither may be dropped "to simplify".
	// "Package.swift" is the shape the golden fixture ships and the shape a
	// real SwiftPM package uses; "Sources/Nested/Package.swift" additionally
	// pins that no future fix may reintroduce a path-derived id in a spelling
	// that merely HAPPENS to resolve on root paths — the accident that makes
	// the equivalent hcl site misanchor rather than dangle.
	for _, path := range []string{"Package.swift", "Sources/Nested/Package.swift"} {
		t.Run(path, func(t *testing.T) {
			recs := extractManifestAt(t, anchorManifest, path)

			byID := make(map[string]*types.EntityRecord, len(recs))
			for i := range recs {
				byID[recs[i].ID] = &recs[i]
			}
			// label reports WHICH node an id names. It is used both in the
			// failure text and in the endpoint set; an id naming no record
			// is rendered as <UNRESOLVED:...> so a dangling endpoint can
			// never silently compare equal to a real one.
			label := func(id string) string {
				if e := byID[id]; e != nil {
					return e.Kind + ":" + e.Name
				}
				return "<UNRESOLVED:" + id + ">"
			}

			var got []string
			for i := range recs {
				owner := &recs[i]
				for _, r := range owner.Relationships {
					if r.Kind != "DEPENDS_ON" {
						continue // IMPORTS keeps the file path on purpose (#120).
					}
					// Replay graph assembly: cmd/grafel/index.go and
					// relRecordToGraphRel in internal/extractors/incremental.go
					// substitute the owning record's own id ONLY when
					// FromID is empty.
					from := r.FromID
					if from == "" {
						from = owner.ID
					}
					got = append(got, label(from)+" -> "+label(r.ToID))

					// IDENTITY, not label: two records could share
					// Kind+Name and compare equal by label, so the
					// anchor itself is checked against the owner's id.
					if from != owner.ID {
						what := "MISANCHORED onto " + label(from)
						if byID[from] == nil {
							what = "DANGLING"
						}
						t.Errorf("DEPENDS_ON owned by %s (id %q) -> %s is %s: "+
							"FROM = %q, want the owning record's own id "+
							"(FromID=%q; empty it so assembly stamps the owner)",
							owner.Kind+":"+owner.Name, owner.ID, label(r.ToID),
							what, from, r.FromID)
					}
				}
			}
			sort.Strings(got)

			if strings.Join(got, "\n") != strings.Join(wantAnchorEdges, "\n") {
				t.Errorf("DEPENDS_ON endpoints at %s:\n got:\n  %s\nwant:\n  %s",
					path, strings.Join(got, "\n  "), strings.Join(wantAnchorEdges, "\n  "))
			}
		})
	}
}
