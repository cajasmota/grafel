package engine

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
)

// #6927 (python arm) — the same defect #6917 fixed for sinatra, in two Python
// rule files. `internal/engine/detector.go` compiles rule patterns with plain
// `regexp.Compile` at :156 and :177, so `^` means START OF TEXT, not start of
// line. Four `^`-anchored patterns are affected:
//
//	rules/python/frameworks/pytest.yaml
//	  ^def\s+(test_\w+)\s*\(          -> Test
//	  ^class\s+(Test\w+)\s*           -> TestClass
//	rules/python/frameworks/sqlalchemy.yaml
//	  ^class\s+(\w+)\s*\([^)]*DeclarativeBase[^)]*\)\s*:  -> Schema
//	  ^class\s+([A-Z]\w*)\s*\(\w+\)\s*:                   -> Model
//
// None of the four carries a `$`, so `(?m)`'s second effect (end-of-line
// semantics for `$`) cannot reach them — checked pattern by pattern, which is
// why the fix is per-pattern here and NOT at the compile site (#6917 rejected
// the compile-site variant because this repo has ungated `$`-bearing patterns
// elsewhere, notably docker_compose and ansible_core).
//
// These tests drive the REAL embedded YAML through LoadAllRules() and
// Detector.Detect(): the defect IS the shipped pattern text, so grading a
// hand-written copy would leave the shipped one unobserved.
//
// Graded in BOTH directions (#6902 — a recall assertion is structurally blind
// to over-firing):
//
//   - recall:    TestIssue6927_RealisticPytestModuleYieldsTests,
//     TestIssue6927_SQLAlchemyModelsYieldSchemaAndModels
//   - forbidden: TestIssue6927_PythonPatternsDoNotOverFire,
//     TestIssue6927_ModelPatternDoesNotClaimEveryPythonClass

// pytest6927Module is deliberately NOT a toy whose first line is a test. It
// opens with a docstring and an import block — the shape every real pytest
// module has — then places tests at three DIFFERENT vertical positions: after
// the fixture, inside a class, and last-in-file. Vertical position IS the
// defect, so it is the one axis this source varies; the constructs themselves
// are held at their idiomatic spellings.
const pytest6927Module = `"""Invoice service tests."""

import pytest

from app.invoices import InvoiceService


@pytest.fixture
def service():
    return InvoiceService()


def test_creates_invoice(service):
    assert service.create()


class TestInvoiceTotals:
    def test_sums_lines(self, service):
        assert service.total() == 0


def test_rejects_negative_amount(service):
    with pytest.raises(ValueError):
        service.create(amount=-1)
`

// sqlalchemy6927Models likewise opens with a docstring + imports, and declares
// its last model near EOF.
const sqlalchemy6927Models = `"""ORM models."""

from sqlalchemy import Column, ForeignKey, Integer, String
from sqlalchemy.orm import DeclarativeBase, relationship


class Base(DeclarativeBase):
    pass


class Customer(Base):
    __tablename__ = "customers"

    id = Column(Integer, primary_key=True)
    name = Column(String)
    invoices = relationship("Invoice")


# class LegacyInvoice(Base):
#     __tablename__ = "legacy_invoices"


class Invoice(Base):
    __tablename__ = "invoices"

    id = Column(Integer, primary_key=True)
    customer_id = Column(Integer, ForeignKey("customers.id"))
`

func detect6927Python(t *testing.T, path, src string) *DetectResult {
	t.Helper()
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	return res
}

func has6927Entity(res *DetectResult, kind, name string) bool {
	for _, e := range res.Entities {
		if e.Kind == kind && e.Name == name {
			return true
		}
	}
	return false
}

func entity6927Names(res *DetectResult, kind string) []string {
	var out []string
	for _, e := range res.Entities {
		if e.Kind == kind {
			out = append(out, e.Name)
		}
	}
	return out
}

// TestIssue6927_RealisticPytestModuleYieldsTests is the pytest recall arm.
// Before the fix it found ZERO Test and ZERO TestClass entities: the module
// opens with a docstring, so nothing could satisfy start-of-TEXT.
func TestIssue6927_RealisticPytestModuleYieldsTests(t *testing.T) {
	res := detect6927Python(t, "tests/test_invoices.py", pytest6927Module)

	for _, want := range []string{"test_creates_invoice", "test_rejects_negative_amount"} {
		if !has6927Entity(res, "Test", want) {
			t.Errorf("Test %q not extracted; Tests found: %v", want, entity6927Names(res, "Test"))
		}
	}
	if !has6927Entity(res, "TestClass", "TestInvoiceTotals") {
		t.Errorf("TestClass TestInvoiceTotals not extracted; TestClasses found: %v",
			entity6927Names(res, "TestClass"))
	}

	// CONTROL, held constant across the change: the fixture pattern
	// `@pytest\.fixture(?:\s*\([^)]*\))?\s*\ndef\s+(\w+)\s*\(` is NOT
	// `^`-anchored, so it already worked and must keep working. Without it a
	// total-failure mode (rules stop loading at all) would be indistinguishable
	// from the recall win above.
	if !has6927Entity(res, "Fixture", "service") {
		t.Errorf("CONTROL FAILED: the un-anchored @pytest.fixture pattern produced no "+
			"Fixture entity; Fixtures found: %v — this is not the #6927 defect, "+
			"rule loading itself is broken", entity6927Names(res, "Fixture"))
	}

	// KNOWN LIMITATION, pinned in the POSITIVE direction (the #6917 heredoc
	// idiom): `^def` requires the `def` to open the line, so a test METHOD
	// indented inside a class is still not extracted. `(?m)` does not change
	// that and this change deliberately does not widen the anchor to `^\s*def`
	// — that is a second behaviour change with its own over-fire direction
	// (every nested helper `def test_...` in non-test code) and belongs in its
	// own issue with its own forbidden rows. Written as an assertion rather
	// than a comment so whoever does widen it is told by a red test to come
	// here and move this case up into the recall list.
	if has6927Entity(res, "Test", "test_sums_lines") {
		t.Errorf("indented test methods are now extracted (Test %q) — good news; "+
			"move this case into the recall assertions above", "test_sums_lines")
	}
}

// TestIssue6927_SQLAlchemyModelsYieldSchemaAndModels is the sqlalchemy recall
// arm. Before the fix: ZERO Schema and ZERO Model entities from the class
// patterns.
func TestIssue6927_SQLAlchemyModelsYieldSchemaAndModels(t *testing.T) {
	res := detect6927Python(t, "app/models.py", sqlalchemy6927Models)

	if !has6927Entity(res, "Schema", "Base") {
		t.Errorf("Schema Base (class Base(DeclarativeBase)) not extracted; Schemas: %v",
			entity6927Names(res, "Schema"))
	}
	for _, want := range []string{"Customer", "Invoice"} {
		if !has6927Entity(res, "Model", want) {
			t.Errorf("Model %q not extracted; Models found: %v", want, entity6927Names(res, "Model"))
		}
	}

	// CONTROLS, held constant: neither pattern is `^`-anchored, so both were
	// green before the fix and must be green after it.
	if !has6927Entity(res, "Relationship", "Invoice") {
		t.Errorf("CONTROL FAILED: un-anchored `= relationship(\"Invoice\")` produced no "+
			"Relationship entity; found: %v", entity6927Names(res, "Relationship"))
	}
	if !has6927Entity(res, "Constraint", "customers.id") {
		t.Errorf("CONTROL FAILED: un-anchored `ForeignKey(\"customers.id\")` produced no "+
			"Constraint entity; found: %v", entity6927Names(res, "Constraint"))
	}

	// FINDING, pinned rather than papered over. `class Base(DeclarativeBase):`
	// satisfies BOTH class patterns — the Schema one by the `DeclarativeBase`
	// literal, and the Model one because `(\w+)` accepts `DeclarativeBase` as a
	// single-word base — so the declarative base is minted twice, once as
	// Schema and once as Model. That double-mint is a property of the two
	// patterns as written and is unreachable while the anchor defect keeps both
	// dead; `(?m)` makes it live. It is recorded here so a later narrowing is a
	// deliberate change rather than an accident.
	if !has6927Entity(res, "Model", "Base") {
		t.Errorf("the Schema/Model double-mint of `class Base(DeclarativeBase)` is no " +
			"longer happening — good news; delete this assertion and say so")
	}
}

// pytest6927Decoys contains ONLY things that must NOT be extracted. Every
// candidate sits in a position that `(?m)` newly reaches — a line that is not
// the first line of the file — so a too-broad pattern is visible here and
// nowhere else. There is deliberately no real test in this file: a decoy file
// that also holds a genuine construct cannot distinguish "the decoy was
// rejected" from "the real one was accepted".
const pytest6927Decoys = `"""Helpers for the suite. Contains no tests."""

import pytest

# def test_commented_out(client):
# class TestCommentedOut:

CATALOG = """
def test_from_a_docstring(client):
class TestFromADocstring:
"""


def build_client():
    def test_nested_helper(inner):
        return inner

    return test_nested_helper
`

// TestIssue6927_PythonPatternsDoNotOverFire is the forbidden arm required by
// #6902. `(?m)` is precisely the change that lets `^` reach a line other than
// the file's first for the first time, so over-firing only becomes POSSIBLE
// with this fix — which is why the fence is built in the same change.
//
// Each forbidden row below is graded: it is kept out by the LINE ANCHOR alone
// (a `#` or leading whitespace opens the line), so deleting the `^` from the
// pattern makes it fire. That is what makes it a fence rather than decoration
// (#6938).
func TestIssue6927_PythonPatternsDoNotOverFire(t *testing.T) {
	res := detect6927Python(t, "tests/helpers.py", pytest6927Decoys)

	forbidden := []struct{ kind, name string }{
		// A comment opens the line with `#`, so `^def` / `^class` cannot reach
		// past it. Drop the `^` and both fire.
		{"Test", "test_commented_out"},
		{"TestClass", "TestCommentedOut"},
		// Indented inside another def. Only the anchor keeps it out.
		{"Test", "test_nested_helper"},
	}
	for _, f := range forbidden {
		if has6927Entity(res, f.kind, f.name) {
			t.Errorf("over-fired: %s %q extracted from a file that declares no tests", f.kind, f.name)
		}
	}

	// KNOWN AND NEWLY REACHABLE, pinned positively (same idiom as #6917's
	// heredoc rows): a regex has no way to know a line lives inside a triple
	// quoted string, and `(?m)^def` reaches every line. Test-shaped text in a
	// docstring IS extracted. Before this change the same text could only match
	// when it opened the file, so the fix does widen this case; whoever teaches
	// the detector about string literals is told by a red test to move these
	// two rows into the forbidden list above.
	for _, known := range []struct{ kind, name string }{
		{"Test", "test_from_a_docstring"},
		{"TestClass", "TestFromADocstring"},
	} {
		if !has6927Entity(res, known.kind, known.name) {
			t.Errorf("string-literal over-firing is no longer happening for %s %q — "+
				"good news; move this case into the forbidden list above", known.kind, known.name)
		}
	}
}

// plain6927Python is ordinary Python with NO sqlalchemy anywhere in it: no
// import, no Column, no Base. It is the file that grades the sqlalchemy Model
// pattern's blast radius.
//
// `^class\s+([A-Z]\w*)\s*\(\w+\)\s*:` describes "a module-level class with
// exactly one simple base", which is not a SQLAlchemy construct at all — it is
// plain Python syntax. Detect resolves rule sets by file.Language alone, so
// under `(?m)` this pattern would type EVERY single-base class in EVERY
// indexed Python file as a `Model`. That is exactly the shape #6152 already
// had to gate for falcon.yaml and cherrypy.yaml (`class Foo:` -> Controller),
// so this change carries the same remedy: `requires_framework: true`, which
// makes the pattern fire only in files whose content shows one of
// sqlalchemy.yaml's declared import markers.
const plain6927Python = `"""Plain domain code. No ORM within a mile of it."""

import json


class ConfigError(Exception):
    pass


class Serializer(object):
    def dump(self, value):
        return json.dumps(value)
`

// TestIssue6927_ModelPatternDoesNotClaimEveryPythonClass is the over-fire arm
// for the widening's blast radius, and it is the reason the fix is not `(?m)`
// alone. Removing `requires_framework: true` from the Model pattern turns both
// rows below live, so the rows grade the gate rather than decorate it.
func TestIssue6927_ModelPatternDoesNotClaimEveryPythonClass(t *testing.T) {
	res := detect6927Python(t, "app/serializers.py", plain6927Python)

	for _, name := range entity6927Names(res, "Model") {
		t.Errorf("over-fired: Model %q minted from a file with no SQLAlchemy import — "+
			"`^class X(Base):` is plain Python syntax, not an ORM construct", name)
	}
	for _, name := range entity6927Names(res, "Schema") {
		t.Errorf("over-fired: Schema %q minted from a file with no SQLAlchemy import", name)
	}
}

// TestIssue6927_ModelAnchorIsGradedInsideAFrameworkFile separates the two
// fences. The file above is kept out by the FRAMEWORK GATE, so it says nothing
// about the anchor: a mutant that drops `^` from the Model pattern leaves it
// green. This file satisfies the gate (it imports sqlalchemy) and holds a
// commented-out model declaration, so only the line anchor keeps
// `LegacyInvoice` out — the two mutants are distinguishable.
func TestIssue6927_ModelAnchorIsGradedInsideAFrameworkFile(t *testing.T) {
	res := detect6927Python(t, "app/models.py", sqlalchemy6927Models)

	if has6927Entity(res, "Model", "LegacyInvoice") {
		t.Errorf("over-fired: Model \"LegacyInvoice\" extracted from a `# class " +
			"LegacyInvoice(Base):` comment")
	}
	// Blanket sweep: the named row grades the form somebody thought to name;
	// this grades the ones nobody did.
	for _, name := range entity6927Names(res, "Model") {
		switch name {
		case "Customer", "Invoice", "Base":
			continue // the three real declarations, asserted above
		case "models":
			// From the `*/models.py` file_convention, not from a source
			// pattern: the entity is named after the FILE. Unrelated to the
			// anchor and green before and after.
			continue
		}
		t.Errorf("over-fired: unexpected Model %q in a file declaring only "+
			"Base/Customer/Invoice", name)
	}
}
