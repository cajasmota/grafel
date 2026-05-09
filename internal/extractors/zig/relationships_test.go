package zig_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/zig"
	"github.com/cajasmota/archigraph/internal/types"
)

// runZig runs the extractor on raw source and returns entity records.
func runZig(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, _ := extractor.Get("zig")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "zig",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func zigFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func zigHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	for i := range ents {
		if ents[i].Name != name || ents[i].Kind != kind {
			continue
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == edgeKind && r.ToID == toID {
				return true
			}
		}
	}
	return false
}

// TestZig_Imports — @import("...") emits IMPORTS edges from file.Path → module.
func TestZig_Imports(t *testing.T) {
	src := `const std = @import("std");
const foo = @import("./foo.zig");
const bar = @import("../bar.zig");

pub fn main() !void {}
`
	ents := runZig(t, src, "main.zig")

	want := map[string]bool{
		"std":        false,
		"./foo.zig":  false,
		"../bar.zig": false,
	}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				if _, ok := want[r.ToID]; ok {
					want[r.ToID] = true
					if r.FromID != "main.zig" {
						t.Errorf("IMPORTS %q: expected FromID=main.zig, got %q", r.ToID, r.FromID)
					}
				}
			}
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("expected IMPORTS edge for %q", k)
		}
	}
}

// TestZig_CallsBareName — bare-name fn calls and qualified Foo.bar / std.debug.print.
func TestZig_CallsBareName(t *testing.T) {
	src := `const std = @import("std");

fn helper(x: u32) u32 {
    return x * 2;
}

pub fn caller() void {
    helper(1);
    helper(2);
    std.debug.print("hi\n", .{});
    Foo.bar(42);
}
`
	ents := runZig(t, src, "calls.zig")

	if !zigHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Errorf("expected CALLS caller→helper")
	}
	if !zigHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "print") {
		t.Errorf("expected CALLS caller→print (rightmost identifier of std.debug.print)")
	}
	if !zigHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "bar") {
		t.Errorf("expected CALLS caller→bar (rightmost identifier of Foo.bar)")
	}

	caller := zigFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("caller not extracted")
	}
	n := 0
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "helper" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected dedup CALLS caller→helper to 1, got %d", n)
	}
}

// TestZig_CallsExcludeSelfRecursion — caller→caller self-edges are filtered.
func TestZig_CallsExcludeSelfRecursion(t *testing.T) {
	src := `fn factorial(n: u32) u32 {
    if (n <= 1) return 1;
    return n * factorial(n - 1);
}
`
	ents := runZig(t, src, "rec.zig")
	if zigHasRel(ents, "factorial", "SCOPE.Operation", "CALLS", "factorial") {
		t.Errorf("self-recursion CALLS should be filtered")
	}
}

// TestZig_ContainsStructMethods — struct with N fns inside → N CONTAINS.
func TestZig_ContainsStructMethods(t *testing.T) {
	src := `const Foo = struct {
    x: u32,

    pub fn init() Foo {
        return Foo{ .x = 0 };
    }

    pub fn bump(self: *Foo) void {
        self.x += 1;
    }

    fn private(self: *Foo) u32 {
        return self.x;
    }
};
`
	ents := runZig(t, src, "foo.zig")
	foo := zigFind(ents, "Foo", "SCOPE.Component")
	if foo == nil {
		t.Fatal("expected Foo struct component")
	}
	contains := 0
	for _, r := range foo.Relationships {
		if r.Kind == "CONTAINS" {
			contains++
		}
	}
	if contains != 3 {
		t.Errorf("expected 3 CONTAINS edges, got %d (rels=%+v)", contains, foo.Relationships)
	}
	// Format A structural-ref keyed on file path.
	for _, m := range []string{"init", "bump", "private"} {
		want := "scope:operation:method:zig:foo.zig:" + m
		if !zigHasRel(ents, "Foo", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Foo→%s", want)
		}
	}
}

// TestZig_ContainsOnlyForFnsInsideStruct — top-level fns must not appear in
// any struct's CONTAINS edge set.
func TestZig_ContainsTopLevelFnNotInStruct(t *testing.T) {
	src := `const Foo = struct {
    pub fn inner() void {}
};

pub fn topLevel() void {}
`
	ents := runZig(t, src, "scope.zig")
	foo := zigFind(ents, "Foo", "SCOPE.Component")
	if foo == nil {
		t.Fatal("expected Foo struct component")
	}
	for _, r := range foo.Relationships {
		if r.Kind == "CONTAINS" && r.ToID == "scope:operation:method:zig:scope.zig:topLevel" {
			t.Error("Foo should not CONTAINS top-level fn topLevel")
		}
	}
	// inner must be there.
	want := "scope:operation:method:zig:scope.zig:inner"
	if !zigHasRel(ents, "Foo", "SCOPE.Component", "CONTAINS", want) {
		t.Errorf("expected CONTAINS Foo→%s", want)
	}
}

// TestZig_RelationshipsLanguageTagged — every relationship gets language=zig.
func TestZig_RelationshipsLanguageTagged(t *testing.T) {
	src := `const std = @import("std");

const Foo = struct {
    pub fn bar() void {
        std.debug.print("x\n", .{});
    }
};
`
	ents := runZig(t, src, "tag.zig")
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Properties == nil || r.Properties["language"] != "zig" {
				t.Errorf("rel %s→%s missing language=zig (got %v)", r.Kind, r.ToID, r.Properties)
			}
		}
	}
}
