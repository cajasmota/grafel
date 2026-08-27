// kotlin_names.go — the ONE definition of how a Kotlin SCOPE.Operation entity
// Name splits back into its enclosing type and its leaf (#6499).
//
// Since #6499 a Kotlin member operation is named `<EnclosingType>.<leaf>`
// (matching Java's #65 contract) while a top-level function stays bare. Two
// layers have to undo that qualification and they MUST agree:
//
//   - internal/extractors/kotlin/references.go keys the same-file symbol table
//     by the leaf, because source identifiers are always bare;
//   - internal/resolve keys byKotlinPkgMember[pkg][Type][leaf] by the leaf,
//     because the call edge's `call_leaf` property is always bare.
//
// They previously carried two independent copies of the split. That is the same
// drift hazard #6499 exists to remove, one layer down — so the split lives here,
// in the package both already import, and neither side reimplements it.

package extractor

// KotlinMemberLeaf strips the enclosing-type qualifier from an emitted Kotlin
// operation Name: "OrderService.place" → "place". A bare top-level name is
// returned unchanged.
//
// Backtick-quoted identifiers are respected. Kotlin allows a dot INSIDE a
// backtick-quoted name — “ class Holder { fun `x.y`() } “ emits
// "Holder.`x.y`" — so a naive LastIndex(".") would return "y`" and key the
// resolver's member index on a name no call site spells. Both the enclosing
// type and the leaf may be quoted ("`A.B`.c" → "c"). A single forward scan that
// only counts dots outside backticks handles every combination; an unbalanced
// backtick degrades to "no split found", which returns the name unchanged
// rather than fabricating a leaf.
func KotlinMemberLeaf(name string) string {
	last := -1
	inTick := false
	for i := 0; i < len(name); i++ {
		switch name[i] {
		case '`':
			inTick = !inTick
		case '.':
			if !inTick {
				last = i
			}
		}
	}
	if last < 0 {
		return name
	}
	return name[last+1:]
}
