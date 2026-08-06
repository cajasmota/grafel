package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6202 — #6138 is still live inside TypeScript and JavaScript.
//
// #6138 gated foldFileComponentDuplicates on an extension allow-list, which is
// the right shape: the fold's premise is a CONVENTION ("this module exists to
// export this one declaration"), not a language. But `.ts` and `.js` were
// allow-listed wholesale, and they are the extension for that convention AND
// for every other kind of TypeScript/JavaScript module. So a backend tree still
// lost stem-named declarations: `class UserRepository` in `UserRepository.ts`
// is deleted exactly like `class LoginPage` in `LoginPage.tsx`, and because
// internal/mcp/denoise.go classifies a subtype="file" component as a
// noiseContainer unconditionally, the declaration does not merely change owner
// — it stops being visible to MCP consumers at all.
//
// The distinction is not carryable by the extension. It is carried by the
// module's DEFAULT EXPORT: the React/Vue component convention is
// `export default LoginPage`, and a backend module is `export interface
// IUserRepository` / `export class UserRepository` — a named export. So a file
// whose default export is the stem-named declaration IS that declaration and
// still folds; a file that merely happens to contain a stem-named declaration
// is a container and keeps it.
//
// `.tsx`, `.jsx`, `.vue`, `.svelte` and `.astro` are unchanged: those
// extensions name the convention on their own, so they fold with or without a
// default export, exactly as #1727 and #6138 left them.

// fileEnt6202 builds the SCOPE.Component(subtype="file") record every extractor
// emits per file. defaultExport, when non-empty, is the name the module default
// exports — the signal the JS/TS extractor now stamps (see
// internal/extractors/javascript, TestIssue6202_FileEntityCarriesDefaultExportName).
func fileEnt6202(id, path, defaultExport string) types.EntityRecord {
	r := types.EntityRecord{
		ID:         id,
		Kind:       "SCOPE.Component",
		Subtype:    "file",
		Name:       path,
		SourceFile: path,
	}
	if defaultExport != "" {
		r.Properties = map[string]string{"default_export": defaultExport}
	}
	return r
}

// TestIssue6202_NamedExportModulesKeepTheirDeclarations is the regression gate.
// A stem-named declaration in a `.ts`/`.js` module with NO matching default
// export must survive with its span, subtype and signature intact.
//
// The rows are the three shapes #6202 measured against the real function, plus
// the `.js` and `.mts` variants of the same convention.
func TestIssue6202_NamedExportModulesKeepTheirDeclarations(t *testing.T) {
	for _, tc := range []struct {
		name    string
		path    string
		decl    string
		subtype string
		sig     string
		props   map[string]string
	}{
		{
			name:    "interface",
			path:    "src/domain/IUserRepository.ts",
			decl:    "IUserRepository",
			subtype: "interface",
			sig:     "interface IUserRepository extends IReadRepo<User>",
		},
		{
			name:    "class",
			path:    "src/domain/UserRepository.ts",
			decl:    "UserRepository",
			subtype: "class",
			sig:     "class UserRepository implements IUserRepository",
		},
		{
			name:    "service",
			path:    "src/orders/OrdersService.ts",
			decl:    "OrdersService",
			subtype: "service",
			sig:     "@Injectable class OrdersService",
		},
		{
			name:    "role_class_door",
			path:    "src/orders/OrdersGateway.ts",
			decl:    "OrdersGateway",
			subtype: "gateway",
			sig:     "@WebSocketGateway class OrdersGateway",
			props:   map[string]string{"role": "class"},
		},
		{
			name:    "javascript",
			path:    "src/domain/OrderMapper.js",
			decl:    "OrderMapper",
			subtype: "class",
			sig:     "class OrderMapper",
		},
		{
			name:    "typescript_module",
			path:    "src/domain/Money.mts",
			decl:    "Money",
			subtype: "class",
			sig:     "class Money",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records := []types.EntityRecord{
				fileEnt6202("f000000000006202", tc.path, ""),
				{
					ID:         "d000000000006202",
					Kind:       "SCOPE.Component",
					Subtype:    tc.subtype,
					Name:       tc.decl,
					SourceFile: tc.path,
					StartLine:  7,
					EndLine:    41,
					Signature:  tc.sig,
					Properties: tc.props,
				},
			}

			out, _, stats := dupkindIndexer().foldFileComponentDuplicates(records, nil)

			if stats.Folded != 0 {
				t.Errorf("expected 0 folds — %s is a named export, so the module is a container "+
					"and not a second name for the declaration; got %d", tc.decl, stats.Folded)
			}
			var kept *types.EntityRecord
			for i := range out {
				if out[i].Name == tc.decl && out[i].Subtype == tc.subtype {
					kept = &out[i]
				}
			}
			if kept == nil {
				t.Fatalf("%s in %s lost its entity to the file fold", tc.decl, tc.path)
			}
			if kept.StartLine != 7 || kept.EndLine != 41 {
				t.Errorf("%s span = %d..%d, want 7..41", tc.decl, kept.StartLine, kept.EndLine)
			}
			if kept.Signature != tc.sig {
				t.Errorf("%s signature = %q, want %q", tc.decl, kept.Signature, tc.sig)
			}
		})
	}
}

// TestIssue6202_DefaultExportModulesStillFold is the door #1727 relies on, and
// the one bullet of #6202's acceptance that had to be a test rather than a line
// of reasoning. A `.ts`/`.js` file that genuinely IS its component — default
// export, name matching the stem — must still fold, or the fix has simply
// traded #6138 for #1727.
//
// THE STEM-CASE ROWS PIN A DELIBERATE ASYMMETRY between the fold's two name
// comparisons, which are NOT interchangeable:
//
//   - stem vs declaration name is case-INSENSITIVE, because a filename is not
//     an identifier. `loginPage.ts`, `loginpage.ts` and `LOGINPAGE.ts` are all
//     the file that holds `class LoginPage`; filesystems disagree about case
//     and developers spell module files in every convention.
//   - default export vs declaration name is case-SENSITIVE, because those ARE
//     two identifiers from one source file, where `logger` and `Logger` are
//     different bindings (see DefaultExportOfADifferentSymbolDoesNotFold).
//
// Collapsing both to one rule breaks something either way: making the stem
// comparison case-sensitive stops every camelCase-filed component folding, and
// making the identifier comparison case-insensitive deletes every singleton's
// class. Without these rows only the first failure mode is untested.
func TestIssue6202_DefaultExportModulesStillFold(t *testing.T) {
	const classSig = "class LoginPage extends React.Component<Props>"

	for _, tc := range []struct{ name, path string }{
		{"ts", "src/components/LoginPage.ts"},
		{"js", "src/components/LoginPage.js"},
		{"mjs", "src/components/LoginPage.mjs"},
		{"cts", "src/components/LoginPage.cts"},
		// Stem case differs from the declaration; the default export does not.
		{"ts_stem_case_differs", "src/components/loginPage.ts"},
		{"ts_stem_all_lower", "src/components/loginpage.ts"},
		{"ts_stem_all_upper", "src/components/LOGINPAGE.ts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records := []types.EntityRecord{
				fileEnt6202("f000000000006210", tc.path, "LoginPage"),
				{
					ID:         "d000000000006210",
					Kind:       "SCOPE.Component",
					Subtype:    "class",
					Name:       "LoginPage",
					SourceFile: tc.path,
					StartLine:  3,
					EndLine:    30,
					Signature:  classSig,
				},
			}

			out, _, stats := dupkindIndexer().foldFileComponentDuplicates(records, nil)

			if stats.Folded != 1 {
				t.Fatalf("expected the default-exported LoginPage to fold into its %s file entity, got %d folds",
					tc.path, stats.Folded)
			}
			var file *types.EntityRecord
			for i := range out {
				if out[i].Subtype == "file" {
					file = &out[i]
				}
				if out[i].Name == "LoginPage" && out[i].Subtype == "class" {
					t.Error("LoginPage was not absorbed by its file entity")
				}
			}
			if file == nil {
				t.Fatal("file entity disappeared")
			}
			if file.StartLine != 3 {
				t.Errorf("file entity start line = %d, want 3", file.StartLine)
			}
			if file.Signature != classSig {
				t.Errorf("file entity signature = %q, want %q", file.Signature, classSig)
			}
		})
	}
}

// TestIssue6202_DefaultExportOfADifferentSymbolDoesNotFold pins that the signal
// is the default export MATCHING the declaration, not the mere presence of a
// default export.
//
// The singleton row is the one that matters most, and it is why the comparison
// is case-SENSITIVE. `export class Logger` + `const logger = new Logger()` +
// `export default logger` is the ubiquitous singleton-instance convention: the
// default export is the INSTANCE, and the module is a container holding both it
// and the class. Under a case-insensitive comparison `logger` reads as a second
// name for `Logger` and the class is deleted — the exact deletion #6202 exists
// to stop, on the shape its own second row names. These are two identifiers
// from one source file, where case is the language's identity rule.
func TestIssue6202_DefaultExportOfADifferentSymbolDoesNotFold(t *testing.T) {
	for _, tc := range []struct {
		name          string
		path          string
		decl          string
		sig           string
		defaultExport string
	}{
		{
			name:          "factory_function",
			path:          "src/domain/UserRepository.ts",
			decl:          "UserRepository",
			sig:           "class UserRepository implements IUserRepository",
			defaultExport: "createUserRepository",
		},
		{
			// `const logger = new Logger(); export default logger;`
			name:          "singleton_instance_differs_only_by_case",
			path:          "src/infra/Logger.ts",
			decl:          "Logger",
			sig:           "class Logger",
			defaultExport: "logger",
		},
		{
			// The issue's own second row, in its singleton form.
			name:          "singleton_instance_camel_case",
			path:          "src/domain/UserRepository.ts",
			decl:          "UserRepository",
			sig:           "class UserRepository implements IUserRepository",
			defaultExport: "userRepository",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records := []types.EntityRecord{
				fileEnt6202("f000000000006220", tc.path, tc.defaultExport),
				{
					ID:         "d000000000006220",
					Kind:       "SCOPE.Component",
					Subtype:    "class",
					Name:       tc.decl,
					SourceFile: tc.path,
					StartLine:  9,
					EndLine:    60,
					Signature:  tc.sig,
				},
			}

			out, _, stats := dupkindIndexer().foldFileComponentDuplicates(records, nil)

			if stats.Folded != 0 {
				t.Fatalf("expected 0 folds — the module default-exports %q, not %q, so it is "+
					"not a second name for the class; got %d", tc.defaultExport, tc.decl, stats.Folded)
			}
			var kept *types.EntityRecord
			for i := range out {
				if out[i].Name == tc.decl && out[i].Subtype == "class" {
					kept = &out[i]
				}
			}
			if kept == nil {
				t.Fatalf("%s was deleted by a default export (%q) that names a different symbol",
					tc.decl, tc.defaultExport)
			}
			if kept.Signature != tc.sig {
				t.Errorf("%s signature = %q, want %q intact", tc.decl, kept.Signature, tc.sig)
			}
		})
	}
}

// TestIssue6202_ComponentExtensionsFoldWithoutADefaultExport is the other half
// of the placement. `.tsx`, `.jsx`, `.vue`, `.svelte` and `.astro` name the
// convention in the extension itself, so the default-export requirement must
// NOT reach them — requiring it everywhere would delete #1727's fold for every
// React component that uses a named export, which is a regression the extension
// gate was specifically shaped to avoid.
func TestIssue6202_ComponentExtensionsFoldWithoutADefaultExport(t *testing.T) {
	for _, tc := range []struct{ name, path string }{
		{"tsx", "src/components/LoginPage.tsx"},
		{"jsx", "src/components/LoginPage.jsx"},
		{"vue", "src/components/LoginPage.vue"},
		{"svelte", "src/components/LoginPage.svelte"},
		{"astro", "src/components/LoginPage.astro"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records := []types.EntityRecord{
				fileEnt6202("f000000000006230", tc.path, ""),
				{
					ID:         "d000000000006230",
					Kind:       "SCOPE.Component",
					Subtype:    "class",
					Name:       "LoginPage",
					SourceFile: tc.path,
					StartLine:  3,
					EndLine:    30,
					Signature:  "class LoginPage",
				},
			}

			_, _, stats := dupkindIndexer().foldFileComponentDuplicates(records, nil)

			if stats.Folded != 1 {
				t.Fatalf("%s names the component convention in the extension itself and must fold "+
					"without a default export, got %d folds", tc.path, stats.Folded)
			}
		})
	}
}

// TestIssue6202_BackendTypeScriptSurvivesFullIndex is the end-to-end form. The
// unit tests above hand foldFileComponentDuplicates records built by hand, so
// they cannot show that the JS/TS extractor really emits the default-export
// property the gate now reads — without that, the gate would deny every `.ts`
// fold and #1727 would be broken in production while the unit tests stayed
// green.
//
// UserRepository is the shape that actually reproduces #6202 through the real
// extractor. (`export interface IUserRepository` does NOT: the TS extractor
// emits an interface as SCOPE.Schema, which the fold never considers. The
// interface row is still pinned at the record level above, because the fold is
// language-agnostic and any extractor emitting SCOPE.Component/interface for a
// `.ts` file reaches it — as the Solidity one does.)
func TestIssue6202_BackendTypeScriptSurvivesFullIndex(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"src/domain/UserRepository.ts": `import { User } from "./User";

export class UserRepository {
  async findById(id: string): Promise<User | null> { return null; }
  async save(u: User): Promise<void> {}
}
`,
		"src/domain/User.ts": `export class User {
  constructor(public id: string) {}
}
`,
		// The singleton-instance convention: the default export is the
		// INSTANCE, whose name differs from the class only by case. The module
		// is a container holding both, so the class must keep its entity.
		"src/infra/Logger.ts": `export class Logger {
  log(m: string) {}
}

const logger = new Logger();

export default logger;
`,
		"src/components/LoginPage.ts": `import React from "react";

class LoginPage extends React.Component {
  render() { return null; }
}

export default LoginPage;
`,
		"src/components/SignupPage.ts": `import React from "react";

export default class SignupPage extends React.Component {
  render() { return null; }
}
`,
	}
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	doc := runIndexerOn(t, root, "issue6202", nil)

	find := func(name, subtype string) *graph.Entity {
		for i := range doc.Entities {
			e := &doc.Entities[i]
			if e.Kind == "SCOPE.Component" && e.Name == name && e.Subtype == subtype {
				return e
			}
		}
		return nil
	}

	// The file entity is the fold survivor, so its presence is what makes the
	// deletion reachable: without it the test would pass for the wrong reason.
	if find("src/domain/UserRepository.ts", "file") == nil {
		t.Fatal("no file component for src/domain/UserRepository.ts — the fold trigger is gone " +
			"and this test would pass for the wrong reason")
	}

	repo := find("UserRepository", "class")
	if repo == nil {
		t.Fatal("class UserRepository in src/domain/UserRepository.ts lost its entity to the file " +
			"fold — it is a named export, so the module is a container, not a second name for it")
	}
	if repo.StartLine <= 0 {
		t.Errorf("UserRepository start line = %d, want a real line", repo.StartLine)
	}
	if !strings.Contains(repo.Signature, "UserRepository") {
		t.Errorf("UserRepository signature = %q, want the declaration text", repo.Signature)
	}
	if find("User", "class") == nil {
		t.Error("class User in src/domain/User.ts lost its entity")
	}

	// The singleton module, end to end. `default_export` IS stamped here
	// (`logger`), the stem guard DOES pass, and the only thing standing between
	// `class Logger` and deletion is that the name comparison is
	// case-sensitive. A case-insensitive one folds this and leaves nothing but
	// the tier-5 file container MCP treats as noise.
	if lf := find("src/infra/Logger.ts", "file"); lf == nil {
		t.Fatal("no file component for src/infra/Logger.ts — the fold trigger is gone")
	} else if lf.PropGet("default_export") != "logger" {
		t.Fatalf("src/infra/Logger.ts default_export = %q, want %q — without the stamped "+
			"instance name this row cannot exercise the case-sensitivity it exists to pin",
			lf.PropGet("default_export"), "logger")
	}
	if find("Logger", "class") == nil {
		t.Error("class Logger in src/infra/Logger.ts lost its entity: the module default-exports " +
			"the INSTANCE `logger`, not the class, so it is a container for both")
	}

	// The #1727 door, end to end: both default-export forms still fold, so the
	// declaration is absorbed and the file entity carries its span+signature.
	for _, tc := range []struct{ decl, path string }{
		{"LoginPage", "src/components/LoginPage.ts"},
		{"SignupPage", "src/components/SignupPage.ts"},
	} {
		if find(tc.decl, "class") != nil {
			t.Errorf("%s default-exports itself from %s and must still fold into its file entity",
				tc.decl, tc.path)
		}
		fe := find(tc.path, "file")
		if fe == nil {
			t.Fatalf("no file component for %s", tc.path)
		}
		if fe.PropGet("default_export") != tc.decl {
			t.Errorf("%s file entity default_export = %q, want %q — the fold gate reads this "+
				"property and silently denies every fold when it is absent",
				tc.path, fe.PropGet("default_export"), tc.decl)
		}
		if !strings.Contains(fe.Signature, tc.decl) {
			t.Errorf("%s file entity signature = %q, want the absorbed declaration text",
				tc.path, fe.Signature)
		}
	}
}
