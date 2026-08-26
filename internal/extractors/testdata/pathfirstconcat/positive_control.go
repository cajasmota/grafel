// Package pathfirstconcat is a POSITIVE CONTROL for
// scanPathFirstConcatFromIDs, not production code and not compiled into the
// build (the Go tool ignores testdata/).
//
// It reproduces the exact shape the matcher exists to catch — the shape swift's
// extractTargets had before #6367:
//
//	FromID: filePath + "::" + d.name
//
// path FIRST, then a string literal, then a non-literal ident. isFilePathExpr's
// BinaryExpr case rejects that at the trailing operand, so the MAIN scan cannot
// see it; only the path-first-concat matcher can.
//
// WHY THIS FILE EXISTS. TestKnownInvisibleFileAnchoredOffenders used to prove
// its matcher still worked by requiring at least one such site to exist in the
// PRODUCTION tree. swift was the last one. Fixing it emptied the matcher and
// turned that vacuity check into a failure that reported "the matcher has
// broken" for what was actually the bug being fixed — a guard that can only
// stay green while the defect it guards against is still present. The control
// moves that proof here, so production may reach zero sites (the goal) while
// the matcher is still demonstrably able to match.
//
// DO NOT "FIX" THE ANCHORING BELOW. This file is supposed to contain the
// offending shape; that is the whole point. It is skipped by both scanners'
// production walks, which SkipDir on testdata.
package pathfirstconcat

// relationshipRecord mirrors the shape of types.RelationshipRecord that the
// matcher keys on: it looks for the FromID and Kind fields by NAME, on any
// composite literal, so a local stand-in exercises it exactly as the real type
// would and keeps this file free of imports.
type relationshipRecord struct {
	FromID string
	ToID   string
	Kind   string
}

type decl struct {
	name string
}

// emitPathFirstConcatDependsOn is the control site. Keyed
// "extractors:emitPathFirstConcatDependsOn:DEPENDS_ON", one site, form
// "F-hidden:keyed".
func emitPathFirstConcatDependsOn(filePath string, decls []decl) []relationshipRecord {
	var out []relationshipRecord
	for _, d := range decls {
		out = append(out, relationshipRecord{
			FromID: filePath + "::" + d.name,
			ToID:   d.name,
			Kind:   "DEPENDS_ON",
		})
	}
	return out
}

// emitPathFirstConcatImports is a NEGATIVE control in the same file: identical
// shape, but Kind is IMPORTS, which the matcher must skip (#120 keeps the file
// path on IMPORTS edges). If this ever shows up as a site, the kind filter has
// regressed.
func emitPathFirstConcatImports(filePath string, decls []decl) []relationshipRecord {
	var out []relationshipRecord
	for _, d := range decls {
		out = append(out, relationshipRecord{
			FromID: filePath + "::" + d.name,
			ToID:   d.name,
			Kind:   "IMPORTS",
		})
	}
	return out
}
