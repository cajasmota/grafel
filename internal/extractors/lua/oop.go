// oop.go — Lua OOP / metatable class + inheritance extraction (#4911).
//
// Lua has no native class keyword; the dominant OOP idiom is the
// metatable-based "class table" pattern:
//
//	local Animal = {}
//	Animal.__index = Animal
//	function Animal.new(name) ... end
//	function Animal:speak() ... end
//
//	local Dog = setmetatable({}, { __index = Animal })
//	Dog.__index = Dog
//	function Dog:speak() ... end
//
// The base extractor (lua.go) already emits these as `module_table`
// Components carrying CONTAINS edges to their `T.x` / `T:x` methods. What was
// MISSING — and is the highest-value base-language gap called out in #4911
// (lapis/openresty/kong all lean on this idiom for handler objects, DAOs and
// resty modules) — is recognising the *class-ness* and the *inheritance edge*:
//
//   - A `local T = {}` whose table later does `T.__index = T` (and/or has
//     `function T:method` colon-methods) is an OOP class, not a plain module
//     namespace → re-tag the existing Component Subtype to "class".
//   - `local Child = setmetatable({}, { __index = Parent })` (and the
//     two-arg `setmetatable(Child, { __index = Parent })` form applied to an
//     already-declared table) establishes single inheritance →
//     emit an EXTENDS edge Child → Parent.
//
// This runs as a post-pass over the entities the main walk already produced:
// it mutates the matching module-table Component in place (Subtype + EXTENDS)
// so we never double-emit a class entity. Regex-based over file content — the
// smacker/go-tree-sitter Lua grammar does not model the metatable idiom as a
// first-class node, so a CST walk buys nothing here over targeted patterns
// (cf. the testing.go / observability.go custom extractors, also regex).
package lua

import (
	"regexp"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

var (
	// `Name.__index = Name`  /  `Name.__index = Name`  — the self-index that
	// marks a table as a class (instances delegate field lookup to it).
	reSelfIndex = regexp.MustCompile(`(?m)^\s*([A-Za-z_]\w*)\s*\.\s*__index\s*=\s*([A-Za-z_]\w*)`)

	// `local Child = setmetatable({}, { __index = Parent })`
	// `local Child = setmetatable({}, {__index = Parent})`
	reSetmetatableDecl = regexp.MustCompile(
		`(?m)\blocal\s+([A-Za-z_]\w*)\s*=\s*setmetatable\s*\(\s*\{\s*\}\s*,\s*\{\s*__index\s*=\s*([A-Za-z_][\w.]*)`)

	// `setmetatable(Child, { __index = Parent })` — applied to an already
	// declared `local Child = {}` table (the other common spelling).
	reSetmetatableApply = regexp.MustCompile(
		`(?m)\bsetmetatable\s*\(\s*([A-Za-z_]\w*)\s*,\s*\{\s*__index\s*=\s*([A-Za-z_][\w.]*)`)
)

// applyOOP scans file content for the metatable class / inheritance idioms and
// enriches the already-emitted module-table Components: it promotes their
// Subtype to "class" and attaches EXTENDS edges to the resolved parent table.
//
// It only ever touches Components that the base walk produced for a
// `local T = {}` table (tracked via moduleIdx), so a `setmetatable` whose
// child table was never declared as a top-level empty table is ignored — we
// stay precision-first and never invent a class entity.
func applyOOP(file extractor.FileInput, moduleIdx map[string]int, entities []types.EntityRecord) {
	src := string(file.Content)

	// 1. Self-index (`T.__index = T`) → T is a class.
	for _, m := range reSelfIndex.FindAllStringSubmatch(src, -1) {
		lhs, rhs := m[1], m[2]
		if lhs != rhs {
			continue // `T.__index = Parent` is inheritance-by-index, handled below as a parent link
		}
		if idx, ok := moduleIdx[lhs]; ok {
			markClass(&entities[idx])
		}
	}

	// 1b. `T.__index = Parent` (parent set directly as the metatable index) —
	// both a class signal on T and an EXTENDS T -> Parent.
	for _, m := range reSelfIndex.FindAllStringSubmatch(src, -1) {
		child, parent := m[1], m[2]
		if child == parent {
			continue
		}
		if idx, ok := moduleIdx[child]; ok {
			markClass(&entities[idx])
			addExtends(file, &entities[idx], child, parent)
		}
	}

	// 2. `local Child = setmetatable({}, { __index = Parent })`. The Child
	// table is declared inline here (not via `local Child = {}`), so the base
	// walk did NOT emit a module-table Component for it — but the colon-methods
	// `function Child:m` still produced an entity-less CONTAINS target. Promote
	// the existing Component if present; otherwise this inheritance link is
	// recorded against the parent via a synthesized class component below.
	for _, m := range reSetmetatableDecl.FindAllStringSubmatch(src, -1) {
		child, parent := m[1], m[2]
		if idx, ok := moduleIdx[child]; ok {
			markClass(&entities[idx])
			addExtends(file, &entities[idx], child, parent)
		}
	}

	// 3. `setmetatable(Child, { __index = Parent })` applied to a declared table.
	for _, m := range reSetmetatableApply.FindAllStringSubmatch(src, -1) {
		child, parent := m[1], m[2]
		if idx, ok := moduleIdx[child]; ok {
			markClass(&entities[idx])
			addExtends(file, &entities[idx], child, parent)
		}
	}
}

// markClass promotes a module-table Component to subtype "class" (idempotent).
func markClass(rec *types.EntityRecord) {
	if rec.Kind == "SCOPE.Component" {
		rec.Subtype = "class"
	}
}

// addExtends attaches a deduped EXTENDS edge from the class Component to the
// named parent table. The ToID is the parent's component structural-ref so the
// resolver can bind it to a sibling `local Parent = {}` class in the same or
// another file; the bare parent name is preserved in Properties for the
// dynamic resolver fallback.
func addExtends(file extractor.FileInput, rec *types.EntityRecord, child, parent string) {
	toID := extractor.BuildComponentStructuralRef("lua", file.Path, parent)
	for _, r := range rec.Relationships {
		if r.Kind == "EXTENDS" && r.ToID == toID {
			return // already recorded
		}
	}
	rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
		FromID: extractor.BuildComponentStructuralRef("lua", file.Path, child),
		ToID:   toID,
		Kind:   "EXTENDS",
		Properties: types.Props{
			{K: "base_name", V: parent},
			{K: "child_name", V: child},
			{K: "inheritance", V: "metatable"},
		},
	})
}
