package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6138 — foldFileComponentDuplicates deleted a real declaration whenever
// its name matched the file stem, in EVERY language.
//
// The fold is right for the ecosystem it was written for (#1727): in React/Vue/
// Svelte a module and the component it exports are one entity, so `LoginPage` in
// `LoginPage.tsx` and the file component are a true duplicate pair. Nothing
// carried that premise into the rule, so it also fired on Solidity, Java, Go and
// C#, where a file is a container and the declaration inside it is not a second
// name for the file.
//
// The damage was also uneven in a way that looks arbitrary from outside:
// `interface` is in classLikeComponentSubtypes and `contract`/`library` are not,
// so on @openzeppelin/contracts@5.2.0 the rule dropped 42 of 61 interfaces and
// none of the 71 contracts or 45 libraries. And because the absorb block copied
// the span but not the Signature, the dropped interface took its declaration
// text — `interface IERC4626 is IERC20, IERC20Metadata` — with it, so the
// inheritance clause was not recoverable from the document at all.

// fileEnt6138 builds the SCOPE.Component(subtype="file") record every extractor
// emits per file: Name is the path, and there is no span until a declaration
// donates one.
func fileEnt6138(id, path string) types.EntityRecord {
	return types.EntityRecord{
		ID:         id,
		Kind:       "SCOPE.Component",
		Subtype:    "file",
		Name:       path,
		SourceFile: path,
	}
}

// TestIssue6138_ContainerFileLanguagesKeepStemNamedDeclarations is the
// regression gate. A declaration whose name matches its file stem must survive
// the file fold in every language that treats a file as a container, and it must
// keep the signature that carries its inheritance clause.
//
// The contract and library rows are the control: they were never folded (their
// subtypes are absent from classLikeComponentSubtypes), and the point of the fix
// is that the interface now behaves the same way rather than the contract
// starting to behave like the interface.
func TestIssue6138_ContainerFileLanguagesKeepStemNamedDeclarations(t *testing.T) {
	const ifaceSig = "interface IERC4626 is IERC20, IERC20Metadata"

	records := []types.EntityRecord{
		fileEnt6138("f000000000000001", "interfaces/IERC4626.sol"),
		{
			ID:         "d000000000000001",
			Kind:       "SCOPE.Component",
			Subtype:    "interface",
			Name:       "IERC4626",
			SourceFile: "interfaces/IERC4626.sol",
			Language:   "solidity",
			StartLine:  9,
			EndLine:    64,
			Signature:  ifaceSig,
		},
		fileEnt6138("f000000000000002", "src/Foo.sol"),
		{
			ID:         "d000000000000002",
			Kind:       "SCOPE.Component",
			Subtype:    "contract",
			Name:       "Foo",
			SourceFile: "src/Foo.sol",
			Language:   "solidity",
			StartLine:  5,
			EndLine:    40,
			Signature:  "contract Foo is IERC4626",
		},
		fileEnt6138("f000000000000003", "src/Math.sol"),
		{
			ID:         "d000000000000003",
			Kind:       "SCOPE.Component",
			Subtype:    "library",
			Name:       "Math",
			SourceFile: "src/Math.sol",
			Language:   "solidity",
			StartLine:  4,
			EndLine:    30,
			Signature:  "library Math",
		},
		fileEnt6138("f000000000000004", "api/OrdersController.java"),
		{
			ID:         "d000000000000004",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			Name:       "OrdersController",
			SourceFile: "api/OrdersController.java",
			Language:   "java",
			StartLine:  7,
			EndLine:    18,
			Signature:  "@RestController class OrdersController",
		},
		fileEnt6138("f000000000000005", "svc/handler.go"),
		{
			ID:         "d000000000000005",
			Kind:       "SCOPE.Component",
			Subtype:    "struct",
			Name:       "Handler",
			SourceFile: "svc/handler.go",
			Language:   "go",
			StartLine:  6,
			EndLine:    6,
			Signature:  "type Handler struct",
		},
		fileEnt6138("f000000000000006", "Shop/ShopContext.cs"),
		{
			ID:         "d000000000000006",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			Name:       "ShopContext",
			SourceFile: "Shop/ShopContext.cs",
			Language:   "csharp",
			StartLine:  5,
			EndLine:    8,
			Signature:  "public class ShopContext",
		},
	}

	out, _, stats := dupkindIndexer().foldFileComponentDuplicates(records, nil)

	if stats.Folded != 0 {
		t.Errorf("expected 0 folds in container-file languages, got %d", stats.Folded)
	}

	byName := map[string]*types.EntityRecord{}
	for i := range out {
		if out[i].Subtype != "file" {
			byName[out[i].Name] = &out[i]
		}
	}
	for _, want := range []string{"IERC4626", "Foo", "Math", "OrdersController", "Handler", "ShopContext"} {
		if byName[want] == nil {
			t.Errorf("%s lost its entity to the file fold", want)
		}
	}

	if e := byName["IERC4626"]; e != nil && e.Signature != ifaceSig {
		t.Errorf("IERC4626 signature = %q, want %q — the inheritance clause is unrecoverable without it",
			e.Signature, ifaceSig)
	}

	// The file entities still take their span from the declaration, so the fold
	// was not what was paying for their position — the #6118 donation is.
	//
	// src/Foo.sol and src/Math.sol are absent here on purpose: donation reads
	// the same classLikeComponentSubtypes set as the fold, so a contract and a
	// library have never donated a span either. That is the same asymmetry that
	// made this defect show up on interfaces and nowhere else in Solidity.
	wantSpans := map[string]int{
		"interfaces/IERC4626.sol":   9,
		"api/OrdersController.java": 7,
		"svc/handler.go":            6,
		"Shop/ShopContext.cs":       5,
	}
	for i := range out {
		e := &out[i]
		if e.Subtype != "file" {
			continue
		}
		if want, ok := wantSpans[e.Name]; ok && e.StartLine != want {
			t.Errorf("file component %s start line = %d, want %d", e.Name, e.StartLine, want)
		}
	}
}

// TestIssue6138_FileIsTheDeclarationEcosystemsStillFold pins #1727 unchanged:
// where the module and its exported declaration ARE one entity the fold still
// fires, a differently-named class in the same file is still left alone, and the
// absorbed declaration now hands its signature to the survivor instead of taking
// it out of the document.
func TestIssue6138_FileIsTheDeclarationEcosystemsStillFold(t *testing.T) {
	const classSig = "class LoginPage extends React.Component<Props>"

	for _, tc := range []struct{ name, path, decl string }{
		{"tsx", "src/components/LoginPage.tsx", "LoginPage"},
		{"js", "src/components/LoginPage.js", "LoginPage"},
		{"vue", "src/components/LoginPage.vue", "LoginPage"},
		{"svelte", "src/components/LoginPage.svelte", "LoginPage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records := []types.EntityRecord{
				fileEnt6138("f000000000000010", tc.path),
				{
					ID:         "c000000000000010",
					Kind:       "SCOPE.Component",
					Subtype:    "class",
					Name:       tc.decl,
					SourceFile: tc.path,
					StartLine:  3,
					EndLine:    30,
					Signature:  classSig,
				},
				{
					ID:         "c000000000000011",
					Kind:       "SCOPE.Component",
					Subtype:    "class",
					Name:       "FormValidator",
					SourceFile: tc.path,
					StartLine:  40,
					EndLine:    50,
					Signature:  "class FormValidator",
				},
			}

			out, _, stats := dupkindIndexer().foldFileComponentDuplicates(records, nil)

			if stats.Folded != 1 {
				t.Fatalf("expected exactly 1 fold (%s only), got %d", tc.decl, stats.Folded)
			}

			var file *types.EntityRecord
			var validator int
			for i := range out {
				switch {
				case out[i].Subtype == "file":
					file = &out[i]
				case out[i].Name == "FormValidator":
					validator++
				case out[i].Name == tc.decl:
					t.Errorf("%s was not absorbed by its file entity", tc.decl)
				}
			}
			if validator != 1 {
				t.Errorf("FormValidator does not match the file stem and must survive, got %d entities", validator)
			}
			if file == nil {
				t.Fatal("file entity disappeared")
			}
			if file.StartLine != 3 {
				t.Errorf("file entity start line = %d, want 3", file.StartLine)
			}
			if file.Signature != classSig {
				t.Errorf("file entity signature = %q, want %q — an absorbed declaration must not "+
					"take its declaration text out of the document", file.Signature, classSig)
			}
		})
	}
}

// TestIssue6138_SolidityInterfaceSurvivesFullIndex is the end-to-end form: the
// unit tests above hand foldFileComponentDuplicates hand-built records, so they
// cannot show that the Solidity extractor really emits the subtype and the
// co-located file entity that made the fold fire on a real repository.
func TestIssue6138_SolidityInterfaceSurvivesFullIndex(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"interfaces/IVault.sol": `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {IERC20} from "./IERC20.sol";
import {IERC20Metadata} from "./IERC20Metadata.sol";

interface IVault is IERC20, IERC20Metadata {
    function deposit(uint256 assets, address receiver) external returns (uint256 shares);
    function withdraw(uint256 assets, address receiver) external returns (uint256 shares);
}
`,
		"interfaces/IERC20.sol": `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface IERC20 {
    function totalSupply() external view returns (uint256);
}
`,
		"interfaces/IERC20Metadata.sol": `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface IERC20Metadata {
    function decimals() external view returns (uint8);
}
`,
		"src/Vault.sol": `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {IVault} from "../interfaces/IVault.sol";

contract Vault is IVault {
    uint256 public totalAssets;

    function deposit(uint256 assets, address receiver) external returns (uint256 shares) {
        totalAssets += assets;
        return assets;
    }
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

	doc := runIndexerOn(t, root, "issue6138", nil)

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
	// deletion reachable: without it there is nothing to absorb into.
	if find("interfaces/IVault.sol", "file") == nil {
		t.Fatal("no file component for interfaces/IVault.sol — the fold trigger is no longer present " +
			"and this test would pass for the wrong reason")
	}

	iface := find("IVault", "interface")
	if iface == nil {
		t.Fatal("interface IVault in interfaces/IVault.sol lost its entity to the file fold")
	}
	if iface.StartLine <= 0 {
		t.Errorf("IVault start line = %d, want a real line", iface.StartLine)
	}
	if !strings.Contains(iface.Signature, "is IERC20, IERC20Metadata") {
		t.Errorf("IVault signature = %q, want the inheritance clause", iface.Signature)
	}

	if find("Vault", "contract") == nil {
		t.Error("contract Vault in src/Vault.sol lost its entity")
	}
}

// TestIssue6138_RoleClassRecordsAreGatedByExtension pins the gate to the fold
// decision rather than to one of the two ways a record qualifies for it.
//
// The predicate admits a candidate through EITHER door:
//
//	if !classLikeComponentSubtypes[r.Subtype] && r.Properties["role"] != "class"
//
// Every producer of role="class" today also sets Subtype="class", so the second
// door is currently redundant and the subtype door alone would look sufficient.
// It is not something to rely on: a gate written into the subtype branch would
// leave role="class" as an unguarded entrance to the same deletion, and #6138
// would survive in a shape no existing test names. These records qualify ONLY
// through the role door — their subtypes are absent from
// classLikeComponentSubtypes — so they fail if the gate ever stops governing it.
func TestIssue6138_RoleClassRecordsAreGatedByExtension(t *testing.T) {
	const contractSig = "contract Vault is IERC4626"

	records := []types.EntityRecord{
		fileEnt6138("f000000000000020", "src/Vault.sol"),
		{
			ID:         "r000000000000020",
			Kind:       "SCOPE.Component",
			Subtype:    "contract",
			Name:       "Vault",
			SourceFile: "src/Vault.sol",
			Language:   "solidity",
			StartLine:  11,
			EndLine:    90,
			Signature:  contractSig,
			Properties: map[string]string{"role": "class"},
		},
	}

	out, _, stats := dupkindIndexer().foldFileComponentDuplicates(records, nil)

	if stats.Folded != 0 {
		t.Errorf("expected 0 folds in a container-file language, got %d", stats.Folded)
	}
	var kept *types.EntityRecord
	for i := range out {
		if out[i].Name == "Vault" && out[i].Subtype == "contract" {
			kept = &out[i]
		}
	}
	if kept == nil {
		t.Fatal("contract Vault reached the fold through role=class and was deleted")
	}
	if kept.Signature != contractSig {
		t.Errorf("Vault signature = %q, want %q intact", kept.Signature, contractSig)
	}
}

// TestIssue6138_RoleClassStillFoldsWhereTheFileIsTheDeclaration is the other
// half: gating the fold decision must not cost #1727 the role door it relies on.
// A React class component whose extractor emits subtype="component" qualifies
// only through role="class", and .tsx is an ecosystem where the module and the
// component it exports are one entity, so this pair is still a true duplicate.
func TestIssue6138_RoleClassStillFoldsWhereTheFileIsTheDeclaration(t *testing.T) {
	const classSig = "class LoginPage extends React.Component<Props>"

	records := []types.EntityRecord{
		fileEnt6138("f000000000000021", "src/components/LoginPage.tsx"),
		{
			ID:         "r000000000000021",
			Kind:       "SCOPE.Component",
			Subtype:    "component",
			Name:       "LoginPage",
			SourceFile: "src/components/LoginPage.tsx",
			StartLine:  4,
			EndLine:    52,
			Signature:  classSig,
			Properties: map[string]string{"role": "class"},
		},
	}

	out, _, stats := dupkindIndexer().foldFileComponentDuplicates(records, nil)

	if stats.Folded != 1 {
		t.Fatalf("expected the role=class component to fold into its .tsx file entity, got %d folds", stats.Folded)
	}
	var file *types.EntityRecord
	for i := range out {
		if out[i].Subtype == "file" {
			file = &out[i]
		}
	}
	if file == nil {
		t.Fatal("file entity disappeared")
	}
	if file.StartLine != 4 {
		t.Errorf("file entity start line = %d, want 4", file.StartLine)
	}
	if file.Signature != classSig {
		t.Errorf("file entity signature = %q, want %q", file.Signature, classSig)
	}
}
