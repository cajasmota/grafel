package testmap

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// #3628 — import-aware high-confidence gate for the direct-call TESTS signal.
//
// These tests assert the precision boundary: a direct call to an *imported*
// symbol is the strongest test→SUT signal and stays high; a direct call to a
// same-named identifier that was never imported is held at MEDIUM so it is
// never a high-confidence false link; a symbol that is imported but never
// called produces NO edge to that symbol.
// ---------------------------------------------------------------------------

// extractNamedImports — symbol-list parsing across JS/TS + Python forms.
func TestExtractNamedImports_Forms(t *testing.T) {
	src := `
import { UserService, createOrder } from '../user-service';
import { type Foo, Bar as Baz } from './x';
import DefaultThing from './default-thing';
from app.users import create_user, UserService
from app.orders import (place_order, cancel_order)
from app.aliased import real_name as aliased_name
`
	got := extractNamedImports(src)
	want := []string{
		"UserService", "createOrder", "Foo", "Baz", "DefaultThing",
		"create_user", "place_order", "cancel_order", "aliased_name",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("expected imported symbol %q in set %v", w, keys(got))
		}
	}
	// Alias source name must NOT leak — only the local binding is recorded.
	if got["Bar"] {
		t.Errorf("alias source name Bar should not be recorded (only Baz)")
	}
	if got["real_name"] {
		t.Errorf("alias source name real_name should not be recorded (only aliased_name)")
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// POSITIVE: imported symbol called in body → high-confidence TESTS edge, with
// FROM = test entity (its own scope:testcoverage ref) and TO bound to the
// imported SUT symbol.
func TestImportGate_ImportedSymbolCalled_StaysHigh(t *testing.T) {
	src := `
import { UserService } from '../user-service';
describe('UserService', () => {
  it('creates a user', () => {
    const svc = new UserService();
    UserService.create({ name: 'a' });
  });
});`
	recs := runExtract(t, "src/user-service.test.ts", "typescript", src)
	rec := mustEntityTesting(t, recs, "UserService")
	var edge *struct{ from, to, conf string }
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && rel.Properties["tested"] == "UserService" {
				edge = &struct{ from, to, conf string }{rel.FromID, rel.ToID, rel.Properties["confidence"]}
			}
		}
	}
	if edge == nil {
		t.Fatalf("no TESTS edge to UserService; entities=%d", len(recs))
	}
	if edge.conf != "high" {
		t.Errorf("imported+called UserService: confidence=%q, want high", edge.conf)
	}
	// FROM must be the test entity's own ref (scope:testcoverage:...), not empty.
	if edge.from == "" {
		t.Errorf("TESTS edge FROM id is empty")
	}
	if edge.from != rec.Properties["ref"] {
		t.Errorf("TESTS edge FROM=%q, want entity ref %q", edge.from, rec.Properties["ref"])
	}
	// TO must resolve to the SUT symbol under the production file path.
	if edge.to == "" {
		t.Errorf("TESTS edge TO id is empty")
	}
}

// NEGATIVE (precision): a symbol that is NOT imported but is called with a
// matching name must be held at MEDIUM — never emitted as a high-confidence
// false link. Here `OtherThing` is called but only `UserService` is imported.
func TestImportGate_NonImportedCall_HeldAtMedium(t *testing.T) {
	src := `
import { UserService } from '../user-service';
describe('mixed', () => {
  it('calls both', () => {
    UserService.create();
    OtherThing.doStuff();
  });
});`
	recs := runExtract(t, "src/thing.test.ts", "typescript", src)
	var userConf, otherConf string
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind != "TESTS" {
				continue
			}
			switch rel.Properties["tested"] {
			case "UserService.create":
				userConf = rel.Properties["confidence"]
			case "OtherThing.doStuff":
				otherConf = rel.Properties["confidence"]
			}
		}
	}
	if userConf != "high" {
		t.Errorf("imported UserService.create: confidence=%q, want high", userConf)
	}
	if otherConf == "high" {
		t.Errorf("non-imported OtherThing.doStuff must NOT be high (got %q) — false-link guard", otherConf)
	}
	if otherConf != "medium" {
		t.Errorf("non-imported OtherThing.doStuff: confidence=%q, want medium", otherConf)
	}
}

// NEGATIVE (precision): a symbol that is imported but NEVER called produces no
// TESTS edge naming it. Only the called import (createOrder) is linked.
func TestImportGate_ImportedButNotCalled_NoEdge(t *testing.T) {
	src := `
import { createOrder, UnusedHelper } from '../orders';
describe('orders', () => {
  it('places an order', () => {
    createOrder({ id: 1 });
  });
});`
	recs := runExtract(t, "src/orders.test.ts", "typescript", src)
	var sawUnused, sawCreate string
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind != "TESTS" {
				continue
			}
			switch rel.Properties["tested"] {
			case "UnusedHelper":
				sawUnused = rel.Properties["confidence"]
			case "createOrder":
				sawCreate = rel.Properties["confidence"]
			}
		}
	}
	if sawUnused != "" {
		t.Errorf("imported-but-never-called UnusedHelper must produce NO edge, got confidence=%q", sawUnused)
	}
	if sawCreate != "high" {
		t.Errorf("imported+called createOrder: confidence=%q, want high", sawCreate)
	}
}

// pytest: `from app.users import create_user` + call → high; an un-imported
// same-named call stays at medium.
func TestImportGate_Pytest_ImportedCallHigh(t *testing.T) {
	src := `
from app.users import create_user

def test_create_user():
    create_user(name="a")
    legacy_helper()
`
	recs := runExtract(t, "tests/test_users.py", "python", src)
	var createConf, legacyConf string
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind != "TESTS" {
				continue
			}
			switch rel.Properties["tested"] {
			case "create_user":
				createConf = rel.Properties["confidence"]
			case "legacy_helper":
				legacyConf = rel.Properties["confidence"]
			}
		}
	}
	if createConf != "high" {
		t.Errorf("imported+called create_user: confidence=%q, want high", createConf)
	}
	if legacyConf == "high" {
		t.Errorf("non-imported legacy_helper must NOT be high (got %q)", legacyConf)
	}
}

// Go same-package: no named imports of the SUT → gate is disabled, the
// direct-call signal remains HIGH (regression guard for the disabled path).
func TestImportGate_GoSamePackage_GateDisabled_StaysHigh(t *testing.T) {
	src := `package config
import "testing"
func TestParseConfig(t *testing.T) {
    ParseConfig("a.yaml")
}`
	recs := runExtract(t, "config/config_test.go", "go", src)
	var conf string
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && rel.Properties["tested"] == "ParseConfig" {
				conf = rel.Properties["confidence"]
			}
		}
	}
	if conf != "high" {
		t.Errorf("Go same-package ParseConfig: confidence=%q, want high (gate must be disabled when no named SUT imports)", conf)
	}
}

// mustEntityTesting returns the entity that has at least one TESTS edge to the
// subject, failing the test otherwise.
func mustEntityTesting(t *testing.T, recs []types.EntityRecord, subject string) types.EntityRecord {
	t.Helper()
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && rel.Properties["tested"] == subject {
				return r
			}
		}
	}
	t.Fatalf("no entity with a TESTS edge to %q (got %d entities)", subject, len(recs))
	return types.EntityRecord{}
}
