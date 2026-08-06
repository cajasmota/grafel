package javascript_test

// Issue #6202 — the file entity must carry the name of the module's default
// export.
//
// foldFileComponentDuplicates (cmd/grafel/index.go) collapses a stem-named
// declaration into its subtype="file" sibling. #6138 gated that on the file
// extension, but `.ts` and `.js` are the extension for the React/Vue component
// convention AND for every other kind of module, so a backend module still lost
// its declarations. The signal that separates the two is the DEFAULT EXPORT:
// a component module is `export default LoginPage`, a domain module is
// `export class UserRepository`.
//
// Nothing surfaced that at the point the fold runs. The walk unwraps
// `export_statement` and recurses into the declaration inside it
// (extractor.go), so the export form was discarded before any entity was
// emitted, and the file entity's properties were kind/subtype/module/language
// only. These tests pin the property that closes that gap; the fold's use of it
// is pinned in cmd/grafel (TestIssue6202_*).

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// fileEntityOf returns the per-file SCOPE.Component(subtype="file") record.
func fileEntityOf(t *testing.T, entities []types.EntityRecord) *types.EntityRecord {
	t.Helper()
	for i := range entities {
		if entities[i].Kind == "SCOPE.Component" && entities[i].Subtype == "file" {
			return &entities[i]
		}
	}
	t.Fatal("no file entity emitted")
	return nil
}

// TestIssue6202_FileEntityCarriesDefaultExportName pins the property on every
// default-export form the convention actually uses, and pins its ABSENCE for
// the module shapes that must not be read as "this module is this declaration".
func TestIssue6202_FileEntityCarriesDefaultExportName(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "default_class_declaration",
			src:  "import React from 'react';\nexport default class LoginPage extends React.Component {\n  render() { return null; }\n}\n",
			want: "LoginPage",
		},
		{
			name: "default_function_declaration",
			src:  "export default function LoginPage() {\n  return null;\n}\n",
			want: "LoginPage",
		},
		{
			name: "default_async_function_declaration",
			src:  "export default async function loadPage() {\n  return null;\n}\n",
			want: "loadPage",
		},
		{
			name: "default_identifier_after_declaration",
			src:  "class LoginPage {\n  render() { return null; }\n}\n\nexport default LoginPage;\n",
			want: "LoginPage",
		},
		{
			name: "default_identifier_after_const_arrow",
			src:  "const LoginPage = () => null;\n\nexport default LoginPage;\n",
			want: "LoginPage",
		},
		{
			// The backend shape #6202 is about. A named export says nothing
			// about the module being a second name for the declaration.
			name: "named_export_only",
			src:  "export class UserRepository {\n  findById(id: string) { return null; }\n}\n",
			want: "",
		},
		{
			name: "named_interface_export_only",
			src:  "export interface IUserRepository {\n  findById(id: string): void;\n}\n",
			want: "",
		},
		{
			// Anonymous default: there is no name to match a stem against, so
			// stamping anything here would be an invention.
			name: "anonymous_default_class",
			src:  "export default class {\n  render() { return null; }\n}\n",
			want: "",
		},
		{
			// `export default connect(mapState)(LoginPage)` — the default
			// export is a call result, not a declaration in this module.
			// Deliberately not resolved: the conservative answer keeps the
			// declaration's entity rather than deleting it on a guess.
			name: "default_call_expression",
			src:  "const LoginPage = () => null;\nexport default connect(mapState)(LoginPage);\n",
			want: "",
		},
		{
			// A re-export forwards another module's default; this module is
			// not the declaration.
			name: "reexport_default_from_other_module",
			src:  "export { default } from './LoginPage';\n",
			want: "",
		},
		{
			name: "no_exports_at_all",
			src:  "class LoginPage {\n  render() { return null; }\n}\n",
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(tc.src)
			ents := extractAtPath(t, src, "typescript", "src/components/LoginPage.ts")
			got := fileEntityOf(t, ents).Properties["default_export"]
			if got != tc.want {
				t.Errorf("file entity default_export = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestIssue6202_DefaultExportStampedForJavaScriptToo pins the same property on
// the JavaScript grammar. `.js` is allow-listed by the fold for the same reason
// `.ts` is, so it needs the same signal or the gate denies every `.js` fold.
func TestIssue6202_DefaultExportStampedForJavaScriptToo(t *testing.T) {
	src := []byte("import React from 'react';\n\nclass LoginPage extends React.Component {\n  render() { return null; }\n}\n\nexport default LoginPage;\n")
	ents := extractAtPath(t, src, "javascript", "src/components/LoginPage.js")
	if got := fileEntityOf(t, ents).Properties["default_export"]; got != "LoginPage" {
		t.Errorf("file entity default_export = %q, want %q", got, "LoginPage")
	}
}
