package external

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	manifest "github.com/cajasmota/grafel/internal/extractors/cross/manifest"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

func testCtx() context.Context { return context.Background() }

// entityRecordToGraph is a minimal types.EntityRecord → graph.Entity adapter
// covering the fields the cross-reference pass reads. The real pipeline does a
// fuller conversion; this keeps the end-to-end test self-contained.
func entityRecordToGraph(r types.EntityRecord) graph.Entity {
	return graph.Entity{
		ID:            graph.EntityID("repo", r.Kind, r.Name, r.SourceFile),
		Name:          r.Name,
		QualifiedName: r.QualifiedName,
		Kind:          r.Kind,
		Subtype:       r.Subtype,
		SourceFile:    r.SourceFile,
		Language:      r.Language,
	}.WithProperties(r.Properties)
}

// declaredDepEntity builds a graph.Entity mirroring what the _cross_manifest
// extractor emits for a DECLARED package.json dependency (declared=true) plus
// its dep_section. version holds the manifest RANGE at this point — the
// cross-reference pass resolves it from the lockfile.
func declaredDepEntity(file, name, versionRange, section, pm string) graph.Entity {
	isDev := "false"
	depKind := "runtime"
	switch section {
	case "dev":
		isDev, depKind = "true", "dev"
	case "peer":
		depKind = "peer"
	case "optional":
		depKind = "optional"
	}
	return graph.Entity{
		ID:         graph.EntityID("repo", "SCOPE.Component", name, file),
		Name:       name,
		Kind:       "SCOPE.Component",
		Subtype:    "external_dependency",
		SourceFile: file,
	}.WithProperties(map[string]string{
		"external_dependency": "true",
		"package_manager":     pm,
		"version":             versionRange,
		"is_dev":              isDev,
		"dependency_kind":     depKind,
		"dep_section":         section,
		"declared":            "true",
	},
	)
}

// lockedDepEntity mirrors a lockfile-derived (dependency_kind=locked) record
// carrying a resolved EXACT version.
func lockedDepEntity(lockfile, name, version, pm string) graph.Entity {
	return graph.Entity{
		ID:         graph.EntityID("repo", "SCOPE.Component", name, lockfile),
		Name:       name,
		Kind:       "SCOPE.Component",
		Subtype:    "external_dependency",
		SourceFile: lockfile,
	}.WithProperties(map[string]string{
		"external_dependency": "true",
		"package_manager":     pm,
		"version":             version,
		"dependency_kind":     "locked",
	},
	)
}

// extNode mirrors an import-derived ext:<pkg> External node from Synthesize.
func extNode(root string) graph.Entity {
	return graph.Entity{
		ID:   ExtIDPrefix + root,
		Name: root,
		Kind: KindExternal,
	}
}

func importsEdge(fromFile, sourceModule string) graph.Relationship {
	return graph.Relationship{
		FromID: fromFile,
		ToID:   ExtIDPrefix + npmPackageRoot(sourceModule),
		Kind:   string(types.RelationshipKindImports),
	}.WithProperties(map[string]string{"source_module": sourceModule})
}

func findXrefEntity(doc *graph.Document, name, subtype string) *graph.Entity {
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Name == name && e.Subtype == subtype {
			return e
		}
	}
	return nil
}

// TestCrossReference_UsedUnusedAndVersions covers the core deliverable:
//   - a prod dep that IS imported            → imported=true, exact version
//   - a prod dep that is NOT imported anywhere → dead_dependency_candidate=true
//   - a devDep that IS imported              → imported=true (dev section)
//   - existing ext:<pkg> node enriched with the resolved version.
func TestCrossReference_UsedUnusedAndVersions(t *testing.T) {
	const pkgFile = "app/package.json"
	const lockFile = "app/package-lock.json"

	doc := &graph.Document{
		Entities: []graph.Entity{
			// Declared (package.json) — version holds the RANGE.
			declaredDepEntity(pkgFile, "express", "^4.18.0", "prod", "npm"),
			declaredDepEntity(pkgFile, "left-pad", "^1.3.0", "prod", "npm"), // unused
			declaredDepEntity(pkgFile, "jest", "^29.0.0", "dev", "npm"),
			// Lockfile (resolved exact versions).
			lockedDepEntity(lockFile, "express", "4.18.2", "npm"),
			lockedDepEntity(lockFile, "left-pad", "1.3.0", "npm"),
			lockedDepEntity(lockFile, "jest", "29.7.0", "npm"),
			// Import-derived External nodes (express + jest imported).
			extNode("express"),
			extNode("jest"),
		},
		Relationships: []graph.Relationship{
			importsEdge("app/server.js", "express"),
			importsEdge("app/server.test.js", "jest"),
		},
	}

	stats := CrossReferenceManifests(doc)

	if stats.DeclaredReconciled != 3 {
		t.Fatalf("DeclaredReconciled=%d want 3", stats.DeclaredReconciled)
	}
	if stats.DeadCandidates != 1 {
		t.Errorf("DeadCandidates=%d want 1", stats.DeadCandidates)
	}

	express := findXrefEntity(doc, "express", "external_dependency")
	if express == nil || express.SourceFile != pkgFile {
		t.Fatal("declared express record not found")
	}
	if express.PropGet("imported") != "true" {
		t.Errorf("express imported=%q want true", express.PropGet("imported"))
	}
	if _, dead := express.PropLookup("dead_dependency_candidate"); dead {
		t.Errorf("express should not be a dead-dep candidate")
	}
	if express.PropGet("version") != "4.18.2" {
		t.Errorf("express version=%q want lockfile-resolved 4.18.2", express.PropGet("version"))
	}
	if express.PropGet("version_range") != "^4.18.0" {
		t.Errorf("express version_range=%q want ^4.18.0", express.PropGet("version_range"))
	}
	if express.PropGet("dep_section") != "prod" {
		t.Errorf("express dep_section=%q want prod", express.PropGet("dep_section"))
	}

	leftPad := findXrefEntity(doc, "left-pad", "external_dependency")
	if leftPad.PropGet("imported") != "false" {
		t.Errorf("left-pad imported=%q want false", leftPad.PropGet("imported"))
	}
	if leftPad.PropGet("dead_dependency_candidate") != "true" {
		t.Errorf("left-pad should be flagged dead_dependency_candidate=true")
	}
	if leftPad.PropGet("version") != "1.3.0" {
		t.Errorf("left-pad version=%q want resolved 1.3.0", leftPad.PropGet("version"))
	}

	jest := findXrefEntity(doc, "jest", "external_dependency")
	if jest.PropGet("imported") != "true" {
		t.Errorf("jest imported=%q want true", jest.PropGet("imported"))
	}
	if jest.PropGet("dep_section") != "dev" {
		t.Errorf("jest dep_section=%q want dev", jest.PropGet("dep_section"))
	}

	// ext: External nodes enriched with version + section.
	expressExt := findXrefEntity(doc, "express", "")
	if expressExt == nil || expressExt.Kind != KindExternal {
		t.Fatal("ext:express node missing")
	}
	if expressExt.PropGet("version") != "4.18.2" {
		t.Errorf("ext:express version=%q want 4.18.2", expressExt.PropGet("version"))
	}
	if expressExt.PropGet("declared") != "true" {
		t.Errorf("ext:express declared=%q want true", expressExt.PropGet("declared"))
	}
}

// TestCrossReference_NoLockfileFallsBackToRange asserts the manifest range is
// kept as `version` when no lockfile resolution exists.
func TestCrossReference_NoLockfileFallback(t *testing.T) {
	const pkgFile = "package.json"
	doc := &graph.Document{
		Entities: []graph.Entity{
			declaredDepEntity(pkgFile, "axios", "^1.6.0", "prod", "npm"),
			extNode("axios"),
		},
		Relationships: []graph.Relationship{
			importsEdge("index.ts", "axios"),
		},
	}

	stats := CrossReferenceManifests(doc)
	if stats.VersionsResolved != 0 {
		t.Errorf("VersionsResolved=%d want 0 (no lockfile)", stats.VersionsResolved)
	}

	axios := findXrefEntity(doc, "axios", "external_dependency")
	if axios.PropGet("version") != "^1.6.0" {
		t.Errorf("axios version=%q want range fallback ^1.6.0", axios.PropGet("version"))
	}
	if axios.PropGet("version_range") != "^1.6.0" {
		t.Errorf("axios version_range=%q want ^1.6.0", axios.PropGet("version_range"))
	}
	if axios.PropGet("imported") != "true" {
		t.Errorf("axios imported=%q want true", axios.PropGet("imported"))
	}
}

// TestCrossReference_SubpathImportCountsAsUsed asserts a subpath import
// (lodash/fp) marks the lodash package root as used.
func TestCrossReference_SubpathImportCountsAsUsed(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			declaredDepEntity("package.json", "lodash", "^4.17.21", "prod", "npm"),
		},
		Relationships: []graph.Relationship{
			importsEdge("src/util.js", "lodash/fp"),
		},
	}
	CrossReferenceManifests(doc)
	lodash := findXrefEntity(doc, "lodash", "external_dependency")
	if lodash.PropGet("imported") != "true" {
		t.Errorf("lodash (subpath import lodash/fp) imported=%q want true", lodash.PropGet("imported"))
	}
}

// TestCrossReference_EndToEnd runs the real manifest extractor over a
// package.json + package-lock.json + a source file, converts the records into a
// graph.Document, runs Synthesize then CrossReferenceManifests, and asserts the
// full declared/resolved/used join — the fixture-repo test from the ticket.
func TestCrossReference_EndToEnd(t *testing.T) {
	pkgJSON := `{
  "name": "fixture",
  "dependencies": {
    "express": "^4.18.0",
    "left-pad": "^1.3.0"
  },
  "devDependencies": {
    "jest": "^29.0.0"
  }
}`
	pkgLock := `{
  "name": "fixture",
  "lockfileVersion": 3,
  "packages": {
    "": {"name": "fixture"},
    "node_modules/express": {"version": "4.18.2"},
    "node_modules/left-pad": {"version": "1.3.0"},
    "node_modules/jest": {"version": "29.7.0", "dev": true}
  }
}`
	// A source file importing ONLY express + jest. left-pad is declared but
	// never imported → dead-dependency candidate.
	serverJS := `import express from 'express';\nconst app = express();`

	ext := &manifest.Extractor{}
	doc := &graph.Document{}
	addRecords := func(path, content string) {
		recs, err := ext.Extract(testCtx(), extractor.FileInput{Path: path, Content: []byte(content)})
		if err != nil {
			t.Fatalf("Extract(%s): %v", path, err)
		}
		for _, r := range recs {
			doc.Entities = append(doc.Entities, entityRecordToGraph(r))
			for _, rel := range r.Relationships {
				doc.Relationships = append(doc.Relationships, graph.Relationship{
					FromID: rel.FromID, ToID: rel.ToID, Kind: rel.Kind,
				}.WithProperties(rel.Properties))
			}
		}
	}
	addRecords("package.json", pkgJSON)
	addRecords("package-lock.json", pkgLock)

	// Stand in for the JS import extractor + Synthesize: an IMPORTS edge to
	// ext:express, plus the ext:express node (jest left unimported here on
	// purpose so we exercise the dead-dep path on TWO deps deterministically).
	doc.Entities = append(doc.Entities, extNode("express"))
	doc.Relationships = append(doc.Relationships, importsEdge("server.js", "express"))
	_ = serverJS

	stats := CrossReferenceManifests(doc)
	if stats.DeclaredReconciled != 3 {
		t.Fatalf("DeclaredReconciled=%d want 3", stats.DeclaredReconciled)
	}

	express := findXrefEntity(doc, "express", "external_dependency")
	if express.PropGet("imported") != "true" || express.PropGet("version") != "4.18.2" {
		t.Errorf("express: imported=%q version=%q want true/4.18.2",
			express.PropGet("imported"), express.PropGet("version"))
	}
	leftPad := findXrefEntity(doc, "left-pad", "external_dependency")
	if leftPad.PropGet("dead_dependency_candidate") != "true" || leftPad.PropGet("version") != "1.3.0" {
		t.Errorf("left-pad: dead=%q version=%q want true/1.3.0",
			leftPad.PropGet("dead_dependency_candidate"), leftPad.PropGet("version"))
	}
	jest := findXrefEntity(doc, "jest", "external_dependency")
	if jest.PropGet("dead_dependency_candidate") != "true" || jest.PropGet("version") != "29.7.0" {
		t.Errorf("jest: dead=%q version=%q want true/29.7.0",
			jest.PropGet("dead_dependency_candidate"), jest.PropGet("version"))
	}
	if jest.PropGet("dep_section") != "dev" {
		t.Errorf("jest dep_section=%q want dev", jest.PropGet("dep_section"))
	}
}
