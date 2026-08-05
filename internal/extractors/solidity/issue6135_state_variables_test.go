package solidity_test

// Issue #6135: the Solidity extractor emitted nothing for contract-level state
// variables, so a graph of a Solidity repo had no node for `depositCap`,
// `_balances` or any other piece of contract storage, and the contract
// component had no CONTAINS edge naming them.

import (
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// stateVarFixture line 1 is the SPDX comment. The keccak256 argument carries a
// ';' so the fixture fails unless string literals are scrubbed before the
// statement split.
const stateVarFixture = `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";

interface IPriceFeed {
    function latestOracle() external view returns (address);
}

contract Vault is IERC20 {
    using SafeERC20 for IERC20;

    error InsufficientBalance(uint256 available, uint256 required);

    event Deposited(address who, uint256 amount);

    struct Position {
        uint256 shares;
        uint256 debt;
    }

    enum Status { Idle, Active }

    uint256 public depositCap;
    bytes32 public constant MANAGER_ROLE = keccak256("MANAGER_ROLE; not a split");
    address public immutable treasury;
    mapping(address account => uint256) private _balances;
    mapping(address => mapping(address => uint256)) private _allowances;
    IERC4626 public vault;
    uint256 public override totalSupply;
    Status
        public
        status;

    modifier onlyManager() {
        _;
    }

    function deposit(uint256 amount) external onlyManager {
        uint256 fee = amount / 100;
        Position memory p = Position({shares: amount, debt: 0});
        emit Deposited(msg.sender, fee);
    }
}
`

func solFields(ents []types.EntityRecord) []string {
	var out []string
	for i := range ents {
		if ents[i].Kind == "SCOPE.Schema" && ents[i].Subtype == "field" {
			out = append(out, ents[i].Name)
		}
	}
	return out
}

// TestSolidity_StateVariables (#6135) pins the name, line and signature of
// each state variable in stateVarFixture. Visibility, mutability and override
// travel in the Signature, so a `private` variable is emitted exactly like a
// `public` one: the extractor records what the source declares.
func TestSolidity_StateVariables(t *testing.T) {
	ents := runSolidity(t, stateVarFixture, "contracts/Vault.sol")

	for _, tc := range []struct {
		name string
		line int
		sig  string
	}{
		{"Vault.depositCap", 24, "uint256 public depositCap"},
		{"Vault.treasury", 26, "address public immutable treasury"},
		{"Vault._balances", 27, "mapping(address account => uint256) private _balances"},
		{"Vault._allowances", 28, "mapping(address => mapping(address => uint256)) private _allowances"},
		{"Vault.vault", 29, "IERC4626 public vault"},
		{"Vault.totalSupply", 30, "uint256 public override totalSupply"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ent := solFindSubtype(ents, tc.name, "SCOPE.Schema", "field")
			if ent == nil {
				t.Fatalf("no field entity named %q; got %v", tc.name, solFields(ents))
			}
			if ent.StartLine != tc.line {
				t.Errorf("StartLine = %d, want %d", ent.StartLine, tc.line)
			}
			if ent.Signature != tc.sig {
				t.Errorf("Signature = %q, want %q", ent.Signature, tc.sig)
			}
			if ent.Language != "solidity" || ent.SourceFile != "contracts/Vault.sol" {
				t.Errorf("Language/SourceFile = %q/%q", ent.Language, ent.SourceFile)
			}
		})
	}
}

// TestSolidity_StateVariableConstantWithStringLiteral (#6135): the ';' inside
// keccak256("MANAGER_ROLE; not a split") must not terminate the statement.
// stripCommentsAndStrings blanks the literal before findContracts runs, which
// is also why the recorded Signature shows the call with empty parentheses.
func TestSolidity_StateVariableConstantWithStringLiteral(t *testing.T) {
	ents := runSolidity(t, stateVarFixture, "contracts/Vault.sol")

	ent := solFindSubtype(ents, "Vault.MANAGER_ROLE", "SCOPE.Schema", "field")
	if ent == nil {
		t.Fatalf("no field entity for the keccak256 constant; got %v", solFields(ents))
	}
	if ent.StartLine != 25 {
		t.Errorf("StartLine = %d, want 25", ent.StartLine)
	}
	if want := "bytes32 public constant MANAGER_ROLE = keccak256( )"; ent.Signature != want {
		t.Errorf("Signature = %q, want %q", ent.Signature, want)
	}
}

// TestSolidity_StateVariableMultiLineDeclaration (#6135): a declaration split
// across lines reports the line of its first token, matching how functions,
// events and modifiers are attributed (#6040).
func TestSolidity_StateVariableMultiLineDeclaration(t *testing.T) {
	ents := runSolidity(t, stateVarFixture, "contracts/Vault.sol")

	ent := solFindSubtype(ents, "Vault.status", "SCOPE.Schema", "field")
	if ent == nil {
		t.Fatalf("no field entity named Vault.status; got %v", solFields(ents))
	}
	if ent.StartLine != 31 {
		t.Errorf("StartLine = %d, want 31 (the `Status` token, not `status`)", ent.StartLine)
	}
	if ent.EndLine != 33 {
		t.Errorf("EndLine = %d, want 33", ent.EndLine)
	}
	if want := "Status public status"; ent.Signature != want {
		t.Errorf("Signature = %q, want %q", ent.Signature, want)
	}
}

// TestSolidity_StateVariableContainsEdge (#6135): the owning contract carries a
// CONTAINS edge to each state variable, so no field is an orphan.
func TestSolidity_StateVariableContainsEdge(t *testing.T) {
	ents := runSolidity(t, stateVarFixture, "contracts/Vault.sol")

	for _, field := range []string{"Vault.depositCap", "Vault._balances", "Vault.status"} {
		want := extractor.BuildSchemaFieldStructuralRef("solidity", "contracts/Vault.sol", field)
		if !solHasRel(ents, "Vault", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("contract Vault has no CONTAINS edge to %q", want)
		}
	}
}

// TestSolidity_StateVariablesExcludeNonDeclarations (#6135): everything else
// that ends with ';' at depth zero, plus everything that ends with ';' deeper
// in, must stay out. The depth rule covers locals and struct members; the
// leading-token filter covers the bodiless and directive forms.
func TestSolidity_StateVariablesExcludeNonDeclarations(t *testing.T) {
	ents := runSolidity(t, stateVarFixture, "contracts/Vault.sol")

	for _, tc := range []struct {
		name string
		why  string
	}{
		{"Vault.fee", "local variable inside deposit()"},
		{"Vault.p", "local struct variable inside deposit()"},
		{"Vault.shares", "member of struct Position"},
		{"Vault.debt", "member of struct Position"},
		{"IPriceFeed.address", "return type of a bodiless interface function"},
		{"IPriceFeed.latestOracle", "bodiless interface function"},
		{"Vault.required", "parameter of the error declaration"},
		{"Vault.InsufficientBalance", "error declaration"},
		{"Vault.IERC20", "using ... for directive"},
		{"Vault.amount", "parameter of the event declaration"},
		{"Vault.Active", "member of enum Status"},
	} {
		if ent := solFindSubtype(ents, tc.name, "SCOPE.Schema", "field"); ent != nil {
			t.Errorf("emitted field %q (%s); want none. all fields: %v", tc.name, tc.why, solFields(ents))
		}
	}

	want := []string{
		"Vault.depositCap", "Vault.MANAGER_ROLE", "Vault.treasury", "Vault._balances",
		"Vault._allowances", "Vault.vault", "Vault.totalSupply", "Vault.status",
	}
	if got := solFields(ents); !slices.Equal(got, want) {
		t.Errorf("fields = %v, want exactly %v", got, want)
	}
}

// TestSolidity_StateVariablesLeaveOperationsUntouched (#6135): adding the field
// pass must not change what the function, event and modifier passes emit.
func TestSolidity_StateVariablesLeaveOperationsUntouched(t *testing.T) {
	ents := runSolidity(t, stateVarFixture, "contracts/Vault.sol")

	counts := map[string]int{}
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" {
			counts[ents[i].Subtype]++
		}
	}
	for subtype, want := range map[string]int{"function": 2, "event": 1, "modifier": 1} {
		if counts[subtype] != want {
			t.Errorf("%s count = %d, want %d", subtype, counts[subtype], want)
		}
	}
	for _, name := range []string{"IPriceFeed.latestOracle", "Vault.deposit", "Vault.Deposited", "Vault.onlyManager"} {
		if solFind(ents, name, "SCOPE.Operation") == nil {
			t.Errorf("operation %q disappeared", name)
		}
	}
}

// TestSolidity_StateVariableFunctionTypeNotEmitted (#6135) documents a known
// miss rather than hiding it. A function-type state variable starts with the
// `function` keyword, which the leading-token filter rejects because that same
// keyword opens a bodiless interface function. Distinguishing the two needs a
// real declaration parse, so the rare function-type variable is left out.
func TestSolidity_StateVariableFunctionTypeNotEmitted(t *testing.T) {
	src := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract Callbacks {
    function(uint256) external returns (bool) public callback;
    uint256 public counter;
}
`
	ents := runSolidity(t, src, "Callbacks.sol")

	if ent := solFindSubtype(ents, "Callbacks.callback", "SCOPE.Schema", "field"); ent != nil {
		t.Errorf("function-type state variables are now extracted; update this test and the docs")
	}
	if ent := solFindSubtype(ents, "Callbacks.counter", "SCOPE.Schema", "field"); ent == nil {
		t.Errorf("the declaration after it must still be found; got %v", solFields(ents))
	}
}

// ── CALLS binding ────────────────────────────────────────────────────────────

func solFactoryFieldSrc(contract string) string {
	return fmt.Sprintf(`// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract %s {
    address public factory;
}
`, contract)
}

const solConsumerSrc = `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract Consumer {
    function poke() external {
        pool.factory();
    }
}
`

// solResolve runs the production resolver pipeline over the supplied files in
// the order cmd/grafel/index.go uses — import table, import-aware CALLS
// rewrite, entity index, embedded-reference rewrite — and returns the rewritten
// records.
func solResolve(t *testing.T, files map[string]string) []types.EntityRecord {
	t.Helper()
	var recs []types.EntityRecord
	for _, path := range slices.Sorted(maps.Keys(files)) {
		recs = append(recs, runSolidity(t, files[path], path)...)
	}
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6135", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))
	return recs
}

// solCallTargets describes what every CALLS edge of the named caller bound to:
// "<Name> [<Kind>/<Subtype>] @ <SourceFile>" for a bound edge, and
// "<dangling: stub>" for one the resolver left alone. Sorted for comparison.
func solCallTargets(recs []types.EntityRecord, caller string) []string {
	byID := make(map[string]*types.EntityRecord, len(recs))
	for i := range recs {
		byID[recs[i].ID] = &recs[i]
	}
	var out []string
	for i := range recs {
		if recs[i].Name != caller {
			continue
		}
		for _, rel := range recs[i].Relationships {
			if rel.Kind != "CALLS" {
				continue
			}
			if ent := byID[rel.ToID]; ent != nil {
				out = append(out, fmt.Sprintf("%s [%s/%s] @ %s", ent.Name, ent.Kind, ent.Subtype, ent.SourceFile))
				continue
			}
			out = append(out, "<dangling: "+rel.ToID+">")
		}
	}
	slices.Sort(out)
	return out
}

// TestSolidity_StateVariableCallTargets (#6135) pins what the CALLS edges out
// of a caller BIND TO once state variables have entities, not how many of them
// dangle. `pool.factory()` yields two stubs: the dotted "pool.factory", which
// only ever matches an entity of that exact name, and the bare leaf "factory",
// which reaches the same-directory leaf-name tier (refs.go
// lookupPackageMemberByLeafName). That tier is what the new field entities
// become eligible for, so a bind that moved to another contract's field, or to
// a field in another directory, fails here even though the dangling count would
// improve.
func TestSolidity_StateVariableCallTargets(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{
			// One contract in the directory declares `factory`, so the leaf
			// tier has a single candidate and binds the bare stub to it.
			name: "sole declarer in the directory",
			files: map[string]string{
				"contracts/Pool.sol":     solFactoryFieldSrc("Pool"),
				"contracts/Consumer.sol": solConsumerSrc,
			},
			want: []string{
				"<dangling: pool.factory>",
				"Pool.factory [SCOPE.Schema/field] @ contracts/Pool.sol",
			},
		},
		{
			// Two contracts declare `factory`. The resolver must decline
			// rather than pick one — a bind to either is a wrong edge.
			name: "two declarers leave the call unbound",
			files: map[string]string{
				"contracts/Pool.sol":     solFactoryFieldSrc("Pool"),
				"contracts/Registry.sol": solFactoryFieldSrc("Registry"),
				"contracts/Consumer.sol": solConsumerSrc,
			},
			want: []string{
				"<dangling: factory>",
				"<dangling: pool.factory>",
			},
		},
		{
			// The leaf tier is package-scoped, so a field one directory over
			// must not capture the call.
			name: "declarer in another directory does not capture",
			files: map[string]string{
				"pool/Pool.sol":    solFactoryFieldSrc("Pool"),
				"app/Consumer.sol": solConsumerSrc,
			},
			want: []string{
				"<dangling: factory>",
				"<dangling: pool.factory>",
			},
		},
		{
			// Nothing declares the name: both stubs stay dangling.
			name: "undeclared target still dangles",
			files: map[string]string{
				"contracts/Pool.sol": solFactoryFieldSrc("Pool"),
				"contracts/Consumer.sol": `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract Consumer {
    function poke() external {
        y.somethingNoOneDeclares();
    }
}
`,
			},
			want: []string{
				"<dangling: somethingNoOneDeclares>",
				"<dangling: y.somethingNoOneDeclares>",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := solCallTargets(solResolve(t, tc.files), "Consumer.poke")
			if !slices.Equal(got, tc.want) {
				t.Errorf("Consumer.poke CALLS targets =\n  %v\nwant\n  %v", got, tc.want)
			}
		})
	}
}
