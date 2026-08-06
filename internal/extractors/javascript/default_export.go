package javascript

// Default-export surfacing — Issue #6202.
//
// WHAT THIS IS FOR. foldFileComponentDuplicates (cmd/grafel/index.go) collapses
// a stem-named class-like declaration into its SCOPE.Component(subtype="file")
// sibling. That is correct where a module and the declaration it exports are
// the same entity (#1727: `LoginPage` in `LoginPage.tsx`) and destructive
// everywhere else (#6138: `interface IERC4626` in `IERC4626.sol`).
//
// #6138 drew that line with an extension allow-list. `.tsx`, `.jsx`, `.vue`,
// `.svelte` and `.astro` name the component convention on their own, so the
// extension is a sufficient signal for them. `.ts` and `.js` are not: they are
// the extension for that convention AND for every other kind of TypeScript and
// JavaScript module, so gating on them left `class UserRepository` in
// `UserRepository.ts` being deleted exactly like a React component (#6202).
//
// The mechanic behind the convention is the DEFAULT EXPORT. A component module
// is `export default LoginPage` — importers name the file and get the
// declaration, so the two really are one entity. A domain module is
// `export class UserRepository` / `export interface IUserRepository`: a named
// export, addressed by its own name, in a file that is a container.
//
// Nothing surfaced that at the point the fold runs. walk() unwraps
// `export_statement` and recurses into the declaration inside it, so the export
// form was discarded before any entity was emitted. This pass reads it back off
// the AST and stamps it on the file entity, which is the record the fold
// already holds as the fold survivor.
//
// DELIBERATELY CONSERVATIVE. Only a default export that names a declaration in
// this module is stamped: `export default class Foo`, `export default function
// foo`, and `export default Foo;`. An anonymous default, a call-expression
// default (`export default connect(...)(Foo)`) and a re-exported default
// (`export { default } from './Foo'`) are left unstamped, because the fold
// treats the absence of the signal as "keep the declaration" — the answer that
// loses nothing when the guess would have been wrong.

import "github.com/cajasmota/grafel/internal/treesitter/ts"

// DefaultExportProp is the file-entity property carrying the name of the
// symbol this module default-exports, or absent when the module has no default
// export that names one of its own declarations.
const DefaultExportProp = "default_export"

// stampDefaultExport records the module's default-export name on the file
// entity. No-op when the module has no resolvable default export.
func (x *extractor) stampDefaultExport(root ts.Node) {
	name := x.defaultExportName(root)
	if name == "" {
		return
	}
	for i := range x.entities {
		e := &x.entities[i]
		if e.Kind != "SCOPE.Component" || e.Subtype != "file" {
			continue
		}
		if e.Properties == nil {
			e.Properties = map[string]string{}
		}
		e.Properties[DefaultExportProp] = name
		return
	}
}

// defaultExportName returns the name of the declaration this module default
// exports, or "" when there is none or it does not name a declaration.
//
// A default export is only legal at module scope, so this scans the program's
// direct children rather than the whole tree.
func (x *extractor) defaultExportName(root ts.Node) string {
	if root == nil {
		return ""
	}
	count := int(root.ChildCount())
	for i := 0; i < count; i++ {
		n := root.Child(i)
		if n == nil || n.Type() != "export_statement" {
			continue
		}
		// The `default` keyword must be a DIRECT child. That is also what
		// excludes `export { default } from './Foo'`, which forwards ANOTHER
		// module's default: there the token sits inside an export_clause, so
		// this module is correctly not read as the declaration.
		if !exportStatementIsDefault(n) {
			continue
		}
		// `export default class Foo {}` / `export default function foo() {}`.
		if d := n.ChildByFieldName("declaration"); d != nil {
			if nm := d.ChildByFieldName("name"); nm != nil {
				return x.nodeText(nm)
			}
			// Anonymous declaration — nothing to match a file stem against.
			//
			// KEPT DELIBERATELY THOUGH IT IS EQUIVALENT TO FALLING THROUGH: a
			// declaration node carries no `value` field today, so the check
			// below would return "" anyway and no test can distinguish the
			// two. That equivalence is a property of the grammar, not of this
			// pass — reaching the `value` branch with a declaration in hand
			// would be reading a second answer out of a node that already gave
			// one. The unreachable re-export guard removed alongside this was
			// a different case: its intent was already carried by
			// exportStatementIsDefault, so it said nothing this file did not.
			return ""
		}
		// `export default Foo;` — a bare identifier referring to a declaration
		// made earlier in the same module. Anything else (a call expression, an
		// object literal, an arrow function) names no declaration.
		if v := n.ChildByFieldName("value"); v != nil && v.Type() == "identifier" {
			return x.nodeText(v)
		}
		return ""
	}
	return ""
}

// exportStatementIsDefault reports whether an export_statement carries the
// `default` keyword as a direct child. The keyword is an anonymous token, so it
// is found by type rather than by field.
func exportStatementIsDefault(n ts.Node) bool {
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		c := n.Child(i)
		if c != nil && c.Type() == "default" {
			return true
		}
	}
	return false
}
