// Package testmap — value-asserting tests for the `zig test` detector (#5377).
package testmap

import "testing"

// TestZigTest_DirectCallHighConfidence proves a direct production call inside a
// `test "..." { … }` body yields a TESTS edge, attributed to zig-test.
func TestZigTest_DirectCallHighConfidence(t *testing.T) {
	src := `const std = @import("std");
const expect = std.testing.expect;

fn add(a: i32, b: i32) i32 {
    return a + b;
}

test "add sums two numbers" {
    try expect(add(2, 2) == 4);
}
`
	recs := runExtract(t, "src/math.zig", "zig", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 testmap entity for zig-test")
	}
	rec := findByTested(t, recs, "add_sums_two_numbers", "add")
	if rec.Properties["test_framework"] != "zig-test" {
		t.Errorf("framework=%q, want zig-test", rec.Properties["test_framework"])
	}
	if !hasEdge(recs, "add_sums_two_numbers", "add") {
		t.Errorf("missing TESTS edge add_sums_two_numbers -> add")
	}
}

// TestZigTest_IdentifierForm proves the `test add { … }` identifier form is
// detected and carries `add` as the naming-convention subject-under-test.
func TestZigTest_IdentifierForm(t *testing.T) {
	src := `const std = @import("std");

fn parse(s: []const u8) u32 {
    return s.len;
}

test parse {
    try std.testing.expectEqual(@as(u32, 3), parse("abc"));
}
`
	recs := runExtract(t, "src/parser.zig", "zig", src)
	if !hasEdgeAny(recs, "parse", "parse") {
		t.Errorf("expected identifier-form test parse -> parse edge")
	}
}

// TestZigTest_BodyScoped proves the brace-body extractor scans only the block's
// own body — a call in a SIBLING test must not leak into another test's body.
func TestZigTest_BodyScoped(t *testing.T) {
	src := `const std = @import("std");

fn runAlpha(x: i32) bool { return x > 0; }
fn runBeta(x: i32) bool { return x < 0; }

test "alpha" {
    try std.testing.expect(runAlpha(1));
}

test "beta" {
    try std.testing.expect(runBeta(2));
}
`
	recs := runExtract(t, "src/svc.zig", "zig", src)
	if !hasEdgeAny(recs, "alpha", "runAlpha") {
		t.Errorf("expected alpha -> runAlpha edge")
	}
	if !hasEdgeAny(recs, "beta", "runBeta") {
		t.Errorf("expected beta -> runBeta edge")
	}
	if hasEdgeAny(recs, "alpha", "runBeta") {
		t.Errorf("alpha test body leaked into sibling beta (runBeta)")
	}
}

// TestZigTest_AssertionDSLNotSubject proves the std.testing.* assertion DSL is
// stop-worded — it never surfaces as the production subject under test.
func TestZigTest_AssertionDSLNotSubject(t *testing.T) {
	src := `const std = @import("std");

test "only assertions" {
    try std.testing.expectEqual(@as(i32, 1), @as(i32, 1));
    try std.testing.expect(true);
}
`
	recs := runExtract(t, "src/dsl.zig", "zig", src)
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind != "TESTS" {
				continue
			}
			if containsAny(rel.Properties.Get("tested"),
				"expect", "expectEqual", "testing", "std", "expect_equal") {
				t.Errorf("DSL identifier surfaced as production subject: %s", rel.Properties.Get("tested"))
			}
		}
	}
}

// TestZigTest_NonTestZigDropped proves a plain .zig source file with NO test
// block yields zero testmap entities (the detector self-confirms; the `.zig`
// filename gate alone must not produce a spurious test record).
func TestZigTest_NonTestZigDropped(t *testing.T) {
	src := `const std = @import("std");

pub fn main() void {
    std.debug.print("hello\n", .{});
}
`
	recs := runExtract(t, "src/main.zig", "zig", src)
	if len(recs) != 0 {
		t.Errorf("non-test .zig file produced %d testmap entities, want 0", len(recs))
	}
}
