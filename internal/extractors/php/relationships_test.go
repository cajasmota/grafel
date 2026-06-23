package php_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/php"
	"github.com/cajasmota/grafel/internal/types"
)

// runPHP parses the PHP source and runs the registered extractor, returning
// every emitted entity record.
func runPHP(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("php")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "php",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func phpFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func phpCallsRels(e *types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range e.Relationships {
		if r.Kind == "CALLS" {
			out = append(out, r)
		}
	}
	return out
}

func phpHasCallTo(e *types.EntityRecord, toID string) bool {
	for _, r := range e.Relationships {
		if r.Kind == "CALLS" && r.ToID == toID {
			return true
		}
	}
	return false
}

func phpReceiverFor(e *types.EntityRecord, toID string) string {
	for _, r := range e.Relationships {
		if r.Kind == "CALLS" && r.ToID == toID {
			if r.Properties == nil {
				return ""
			}
			return r.Properties["receiver_type"]
		}
	}
	return ""
}

// TestPHP_Calls_BareFunction (#376): calls to a top-level PHP function emit a
// bare-name CALLS edge with no receiver_type stamp.
func TestPHP_Calls_BareFunction(t *testing.T) {
	src := `<?php
function helper(int $x): int { return $x + 1; }

function caller(): int {
    return helper(5);
}
`
	ents := runPHP(t, src, "f.php")
	caller := phpFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller function entity")
	}
	if !phpHasCallTo(caller, "helper") {
		t.Errorf("expected CALLS caller→helper, got %+v", caller.Relationships)
	}
	if rt := phpReceiverFor(caller, "helper"); rt != "" {
		t.Errorf("expected no receiver_type for bare call, got %q", rt)
	}
}

// TestPHP_Calls_ThisMethod (#376): `$this->m()` inside class Foo emits a
// CALLS edge to "Foo.m" with receiver_type=Foo so the resolver binds it
// to the same-class method instead of an arbitrary same-named method
// elsewhere in the codebase.
func TestPHP_Calls_ThisMethod(t *testing.T) {
	src := `<?php
class Greeter {
    public function greet(): string {
        return $this->build();
    }
    public function build(): string { return "hi"; }
}
`
	ents := runPHP(t, src, "greeter.php")
	greet := phpFind(ents, "Greeter.greet", "SCOPE.Operation")
	if greet == nil {
		t.Fatal("expected Greeter.greet method entity")
	}
	if !phpHasCallTo(greet, "Greeter.build") {
		t.Errorf("expected CALLS Greeter.greet→Greeter.build, got %+v", greet.Relationships)
	}
	if rt := phpReceiverFor(greet, "Greeter.build"); rt != "Greeter" {
		t.Errorf("expected receiver_type=Greeter, got %q", rt)
	}
}

// TestPHP_Calls_StaticScoped (#376): `Foo::m()` and `self::m()` both produce
// dotted "<Class>.m" targets. The Foo:: scope binds to the literal name;
// self:: binds to the enclosing class.
func TestPHP_Calls_StaticScoped(t *testing.T) {
	src := `<?php
class Util {
    public static function fmt(string $s): string { return $s; }
    public function run(): string {
        $a = Util::fmt("a");
        $b = self::fmt("b");
        return $a . $b;
    }
}
`
	ents := runPHP(t, src, "util.php")
	run := phpFind(ents, "Util.run", "SCOPE.Operation")
	if run == nil {
		t.Fatal("expected Util.run method entity")
	}
	// Util::fmt and self::fmt both resolve to "Util.fmt" — dedup expected.
	calls := phpCallsRels(run)
	if len(calls) != 1 {
		t.Errorf("expected 1 CALLS edge after dedup, got %d (%+v)", len(calls), calls)
	}
	if !phpHasCallTo(run, "Util.fmt") {
		t.Errorf("expected CALLS Util.run→Util.fmt, got %+v", run.Relationships)
	}
	if rt := phpReceiverFor(run, "Util.fmt"); rt != "Util" {
		t.Errorf("expected receiver_type=Util, got %q", rt)
	}
}

// TestPHP_Calls_LocalNewBinding (#376): local-var type inference from
// `$x = new Foo()` lets a follow-up `$x->m()` resolve to "Foo.m" with
// receiver_type=Foo.
func TestPHP_Calls_LocalNewBinding(t *testing.T) {
	src := `<?php
class Mailer { public function send(): void {} }
function notify(): void {
    $m = new Mailer();
    $m->send();
}
`
	ents := runPHP(t, src, "n.php")
	notify := phpFind(ents, "notify", "SCOPE.Operation")
	if notify == nil {
		t.Fatal("expected notify function entity")
	}
	if !phpHasCallTo(notify, "Mailer.send") {
		t.Errorf("expected CALLS notify→Mailer.send, got %+v", notify.Relationships)
	}
	if rt := phpReceiverFor(notify, "Mailer.send"); rt != "Mailer" {
		t.Errorf("expected receiver_type=Mailer, got %q", rt)
	}
	// Constructor edge `new Mailer()` is also emitted.
	if !phpHasCallTo(notify, "Mailer") {
		t.Errorf("expected CALLS notify→Mailer (constructor), got %+v", notify.Relationships)
	}
}

// TestPHP_Calls_DocblockVar (#376): a /** @var Foo $x */ docblock binds
// `$x` so a subsequent `$x->m()` resolves with receiver_type=Foo even
// when the variable was assigned from a non-constructor expression.
func TestPHP_Calls_DocblockVar(t *testing.T) {
	src := `<?php
function persist($repo): void {
    /** @var UserRepo $repo */
    $repo->save();
}
`
	ents := runPHP(t, src, "p.php")
	persist := phpFind(ents, "persist", "SCOPE.Operation")
	if persist == nil {
		t.Fatal("expected persist function entity")
	}
	if !phpHasCallTo(persist, "UserRepo.save") {
		t.Errorf("expected CALLS persist→UserRepo.save, got %+v", persist.Relationships)
	}
	if rt := phpReceiverFor(persist, "UserRepo.save"); rt != "UserRepo" {
		t.Errorf("expected receiver_type=UserRepo, got %q", rt)
	}
}

// TestPHP_Calls_UntypedVar (#376): `$x->m()` with no resolvable type for
// `$x` falls back to a bare-leaf CALLS edge with no receiver_type stamp,
// so the resolver routes it to bug-resolver as ambiguous-method.
func TestPHP_Calls_UntypedVar(t *testing.T) {
	src := `<?php
function f($obj): void {
    $obj->doWork();
}
`
	ents := runPHP(t, src, "u.php")
	f := phpFind(ents, "f", "SCOPE.Operation")
	if f == nil {
		t.Fatal("expected f function entity")
	}
	if !phpHasCallTo(f, "doWork") {
		t.Errorf("expected bare CALLS f→doWork, got %+v", f.Relationships)
	}
	if rt := phpReceiverFor(f, "doWork"); rt != "" {
		t.Errorf("expected no receiver_type for untyped receiver, got %q", rt)
	}
}

// TestPHP_Calls_ConstructorEdge (#376): `new Foo()` emits a CALLS edge
// whose ToID is the type leaf (qualified names are stripped to leaf so
// the edge merges with framework class entities).
func TestPHP_Calls_ConstructorEdge(t *testing.T) {
	src := `<?php
namespace App;
function build(): void {
    $r = new \Symfony\Component\HttpFoundation\Request();
}
`
	ents := runPHP(t, src, "b.php")
	build := phpFind(ents, "build", "SCOPE.Operation")
	if build == nil {
		t.Fatal("expected build function entity")
	}
	if !phpHasCallTo(build, "Request") {
		t.Errorf("expected CALLS build→Request (constructor leaf), got %+v", build.Relationships)
	}
}

// TestPHP_Calls_NoSelfRecursion (#376): a method calling itself with the
// same dotted name must not emit a self-loop.
func TestPHP_Calls_NoSelfRecursion(t *testing.T) {
	src := `<?php
class Counter {
    public function tick(int $n): int {
        if ($n <= 0) { return 0; }
        return $this->tick($n - 1);
    }
}
`
	ents := runPHP(t, src, "c.php")
	tick := phpFind(ents, "Counter.tick", "SCOPE.Operation")
	if tick == nil {
		t.Fatal("expected Counter.tick method entity")
	}
	for _, r := range tick.Relationships {
		if r.Kind == "CALLS" && (r.ToID == "tick" || r.ToID == "Counter.tick") {
			t.Errorf("self-recursion CALLS edge should be dropped, got %+v", r)
		}
	}
}

// TestPHP_Calls_DedupSameTarget (#376): repeated invocations of the same
// callee within a single body collapse to one CALLS edge.
func TestPHP_Calls_DedupSameTarget(t *testing.T) {
	src := `<?php
function logger(string $m): void {}
function caller(): void {
    logger("a");
    logger("b");
    logger("c");
}
`
	ents := runPHP(t, src, "d.php")
	caller := phpFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller function entity")
	}
	count := 0
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "logger" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 deduped CALLS logger edge, got %d", count)
	}
}

// TestPHP_Calls_LanguageTag (#376/#90): every CALLS edge carries
// Properties["language"]="php" so the resolver routes it through the PHP
// dynamic-pattern catalog.
func TestPHP_Calls_LanguageTag(t *testing.T) {
	src := `<?php
function caller(): void { other(); }
`
	ents := runPHP(t, src, "lt.php")
	caller := phpFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller function entity")
	}
	for _, r := range caller.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		if r.Properties["language"] != "php" {
			t.Errorf("expected Properties[language]=php on CALLS edge, got %v", r.Properties)
		}
	}
}
