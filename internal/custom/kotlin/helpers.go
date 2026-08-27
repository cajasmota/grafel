// Package kotlin provides regex-based custom extractors for Kotlin source files.
// Each extractor targets a specific framework and registers via init().
package kotlin

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

func lineOf(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}

func makeEntity(name, kind, subtype, filePath, language string, lineNum int) types.EntityRecord {
	e := types.EntityRecord{
		Name:             name,
		Kind:             kind,
		Subtype:          subtype,
		SourceFile:       filePath,
		StartLine:        lineNum,
		EndLine:          lineNum,
		Language:         language,
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
		Properties: map[string]string{
			"kind":    kind,
			"subtype": subtype,
		},
	}
	e.ID = e.ComputeID()
	return e
}

func setProps(e *types.EntityRecord, kv ...string) {
	if len(kv)%2 != 0 {
		return
	}
	for i := 0; i < len(kv); i += 2 {
		e.Properties[kv[i]] = kv[i+1]
	}
}

// reKtOwnerDecl matches a Kotlin `class`/`object`/`interface` declaration head.
//
// Deliberately separate from spring_transactions.go's reKtClassDecl, which
// matches `class` only. Widening that one in place would silently change the
// two ktFindEnclosingClass callers there; this is additive.
//
// `companion object {` has no name and does not match, so a companion member
// resolves to the enclosing class — the same outer-class attribution the base
// Kotlin extractor makes (#6499).
var reKtOwnerDecl = regexp.MustCompile(`\b(?:class|object|interface)\s+(\w+)`)

// ktFindEnclosingOwner returns the name of the nearest class/object/interface
// declared before offset, or "" when the offset is at file top level.
//
// Used to class-qualify a custom-extractor operation name so it matches the
// Name the BASE Kotlin extractor emits for the same declaration (#6499).
// That agreement is load-bearing rather than cosmetic: entity IDs hash
// (SourceFile, Kind, Name) with subtype EXCLUDED, so an identical name is what
// makes the custom record collapse onto the base entity in buildDocument's
// dedupe (cmd/grafel/index.go) and merge its framework properties onto it. A
// bare name here annotates nothing — it mints a second, name-colliding
// operation instead.
//
// Regex-level model, matching ktFindEnclosingClass: last-wins, no brace
// matching. A genuinely top-level function declared AFTER a class body closes
// is therefore attributed to that class. Acceptable for annotation-driven call
// sites (`@Tool` methods are class members by construction), not for general
// use.
func ktFindEnclosingOwner(src string, offset int) string {
	last := ""
	for _, m := range reKtOwnerDecl.FindAllStringSubmatchIndex(src, -1) {
		if m[0] >= offset {
			break
		}
		last = src[m[2]:m[3]]
	}
	return last
}
