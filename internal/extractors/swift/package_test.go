package swift_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/swift"
	"github.com/cajasmota/archigraph/internal/types"
)

// runPackage invokes the swift_package extractor on src and returns the result.
func runPackage(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("swift_package")
	if !ok {
		t.Fatal("swift_package extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Package.swift",
		Content:  []byte(src),
		Language: "swift_package",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

// findPkg finds an entity by name + kind in a slice, returning nil when absent.
func findPkg(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

// findRel returns the first relationship of kind relKind attached to entity e
// whose ToID is toID, or nil.
func findRel(e *types.EntityRecord, relKind, toID string) *types.RelationshipRecord {
	for i := range e.Relationships {
		r := &e.Relationships[i]
		if r.Kind == relKind && r.ToID == toID {
			return r
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestPackageExtractor_Registration verifies the "swift_package" key is wired.
func TestPackageExtractor_Registration(t *testing.T) {
	ext, ok := extractor.Get("swift_package")
	if !ok {
		t.Fatal("swift_package extractor not registered")
	}
	if ext.Language() != "swift_package" {
		t.Fatalf("Language()=%q want swift_package", ext.Language())
	}
}

// TestPackageExtractor_EmptyContent returns nil, nil on empty input.
func TestPackageExtractor_EmptyContent(t *testing.T) {
	ext, _ := extractor.Get("swift_package")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Package.swift",
		Language: "swift_package",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ents) != 0 {
		t.Fatalf("expected empty slice, got %d entities", len(ents))
	}
}

// TestPackageExtractor_TargetDeclaration verifies that a plain .target() call
// produces a SCOPE.Component with subtype="swiftpm_target".
func TestPackageExtractor_TargetDeclaration(t *testing.T) {
	src := `// swift-tools-version:5.7
import PackageDescription

let package = Package(
    name: "MyLib",
    targets: [
        .target(
            name: "MyLib",
            dependencies: []
        ),
    ]
)
`
	ents := runPackage(t, src)
	e := findPkg(ents, "MyLib", "SCOPE.Component")
	if e == nil {
		t.Fatal("expected SCOPE.Component named MyLib, not found")
	}
	if e.Subtype != "swiftpm_target" {
		t.Errorf("Subtype=%q want swiftpm_target", e.Subtype)
	}
	if e.Properties["swiftpm_kind"] != "target" {
		t.Errorf("Properties[swiftpm_kind]=%q want target", e.Properties["swiftpm_kind"])
	}
	if e.Language != "swift_package" {
		t.Errorf("Language=%q want swift_package", e.Language)
	}
}

// TestPackageExtractor_ExecutableTarget verifies that .executableTarget() is
// extracted and tagged with swiftpm_kind="executableTarget".
func TestPackageExtractor_ExecutableTarget(t *testing.T) {
	src := `// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "App",
    targets: [
        .executableTarget(
            name: "App",
            dependencies: []
        ),
    ]
)
`
	ents := runPackage(t, src)
	e := findPkg(ents, "App", "SCOPE.Component")
	if e == nil {
		t.Fatal("expected SCOPE.Component named App")
	}
	if e.Properties["swiftpm_kind"] != "executableTarget" {
		t.Errorf("swiftpm_kind=%q want executableTarget", e.Properties["swiftpm_kind"])
	}
}

// TestPackageExtractor_TestTarget verifies that .testTarget() is extracted.
func TestPackageExtractor_TestTarget(t *testing.T) {
	src := `// swift-tools-version:5.7
import PackageDescription

let package = Package(
    name: "MyLib",
    targets: [
        .target(name: "MyLib", dependencies: []),
        .testTarget(
            name: "MyLibTests",
            dependencies: ["MyLib"]
        ),
    ]
)
`
	ents := runPackage(t, src)
	e := findPkg(ents, "MyLibTests", "SCOPE.Component")
	if e == nil {
		t.Fatal("expected SCOPE.Component named MyLibTests")
	}
	if e.Properties["swiftpm_kind"] != "testTarget" {
		t.Errorf("swiftpm_kind=%q want testTarget", e.Properties["swiftpm_kind"])
	}
}

// TestPackageExtractor_TargetToTargetDependency verifies that a string
// dependency from one target to another emits a DEPENDS_ON edge with
// dep_kind="target".
func TestPackageExtractor_TargetToTargetDependency(t *testing.T) {
	src := `// swift-tools-version:5.7
import PackageDescription

let package = Package(
    name: "MultiTarget",
    targets: [
        .target(
            name: "Core",
            dependencies: []
        ),
        .target(
            name: "App",
            dependencies: ["Core"]
        ),
    ]
)
`
	ents := runPackage(t, src)

	app := findPkg(ents, "App", "SCOPE.Component")
	if app == nil {
		t.Fatal("expected entity App")
	}
	rel := findRel(app, "DEPENDS_ON", "Core")
	if rel == nil {
		t.Fatalf("expected DEPENDS_ON edge App→Core; relationships=%+v", app.Relationships)
	}
	if rel.Properties["dep_kind"] != "target" {
		t.Errorf("dep_kind=%q want target", rel.Properties["dep_kind"])
	}
}

// TestPackageExtractor_ProductDependency verifies that a .product(name:package:)
// dependency emits a DEPENDS_ON edge and a SCOPE.External entity.
func TestPackageExtractor_ProductDependency(t *testing.T) {
	src := `// swift-tools-version:5.7
import PackageDescription

let package = Package(
    name: "VaporApp",
    dependencies: [
        .package(url: "https://github.com/vapor/vapor.git", from: "4.0.0"),
    ],
    targets: [
        .target(
            name: "App",
            dependencies: [
                .product(name: "Vapor", package: "vapor"),
            ]
        ),
    ]
)
`
	ents := runPackage(t, src)

	// The App target must have a DEPENDS_ON edge to "Vapor".
	app := findPkg(ents, "App", "SCOPE.Component")
	if app == nil {
		t.Fatal("expected entity App")
	}
	rel := findRel(app, "DEPENDS_ON", "Vapor")
	if rel == nil {
		t.Fatalf("expected DEPENDS_ON edge App→Vapor; relationships=%+v", app.Relationships)
	}
	if rel.Properties["dep_kind"] != "product" {
		t.Errorf("dep_kind=%q want product", rel.Properties["dep_kind"])
	}
	if rel.Properties["package"] != "vapor" {
		t.Errorf("package=%q want vapor", rel.Properties["package"])
	}

	// A SCOPE.External entity for the product must also be emitted.
	ext := findPkg(ents, "Vapor", "SCOPE.External")
	if ext == nil {
		t.Fatal("expected SCOPE.External named Vapor")
	}
	if ext.Subtype != "swiftpm_product" {
		t.Errorf("SCOPE.External Subtype=%q want swiftpm_product", ext.Subtype)
	}
	if ext.Properties["package"] != "vapor" {
		t.Errorf("SCOPE.External package=%q want vapor", ext.Properties["package"])
	}
}

// TestPackageExtractor_LanguageTagOnRelationships verifies that all emitted
// relationship records carry Properties["language"]="swift_package".
func TestPackageExtractor_LanguageTagOnRelationships(t *testing.T) {
	src := `// swift-tools-version:5.7
import PackageDescription

let package = Package(
    name: "Pkg",
    targets: [
        .target(name: "A", dependencies: []),
        .target(name: "B", dependencies: ["A"]),
    ]
)
`
	ents := runPackage(t, src)
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Properties["language"] != "swift_package" {
				t.Errorf("entity %q rel kind=%q toID=%q: Properties[language]=%q want swift_package",
					e.Name, r.Kind, r.ToID, r.Properties["language"])
			}
		}
	}
}

// TestPackageExtractor_URLsNotExtractedAsDeps verifies that package URL
// strings adjacent to dependency declarations are not emitted as DEPENDS_ON
// targets (they are not target names).
func TestPackageExtractor_URLsNotExtractedAsDeps(t *testing.T) {
	src := `// swift-tools-version:5.7
import PackageDescription

let package = Package(
    name: "Pkg",
    dependencies: [
        .package(url: "https://github.com/vapor/vapor.git", from: "4.0.0"),
    ],
    targets: [
        .target(
            name: "Pkg",
            dependencies: [
                .product(name: "Vapor", package: "vapor"),
            ]
        ),
    ]
)
`
	ents := runPackage(t, src)
	pkg := findPkg(ents, "Pkg", "SCOPE.Component")
	if pkg == nil {
		t.Fatal("expected Pkg entity")
	}
	for _, r := range pkg.Relationships {
		if r.Kind == "DEPENDS_ON" && (r.ToID == "4.0.0" || r.ToID == "https://github.com/vapor/vapor.git") {
			t.Errorf("should not emit DEPENDS_ON edge to URL/version string, got ToID=%q", r.ToID)
		}
	}
}
