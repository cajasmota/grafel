// Package zig — import_top_segment_6852_test.go
//
// Issue #6852, arm 12 (zig), the LAST arm. This file is an INTERNAL test
// (package zig, not zig_test) for one reason: it enumerates the input space of
// importTopSegment, which is unexported and is the only Name site in this
// package that can produce a byte outside [0-9A-Za-z_].
//
// WHY AN ENUMERATION AND NOT AN ARGUMENT. Four arms of #6852 shipped a closure
// of the form "no Name this package emits can equal the file path", and four
// were disproved in review — astro by a prop key spelled as the path, clojure
// by `.clj-kondo/hooks/foo.clj`, cobol twice, the second time by a builder that
// concatenated a mandatory "<OP> " prefix onto a literal-derived operand. The
// two closures that SURVIVED review were the ones that could not be defeated by
// concatenation: crystal's LENGTH invariant and dart's brute force over 299,592
// URIs. This file takes dart's route, and takes it one step further: the
// property enumerated below is not "the output has no '/'" but "the output is a
// SUBSTRING of the input". That is the anti-concatenation property itself. A
// future edit that concatenated anything onto importTopSegment's result —
// exactly the edit that killed cobol's first closure — fails this test on the
// first input that reaches the new code, rather than silently invalidating a
// paragraph of prose somewhere else.
package zig

import (
	"strings"
	"testing"
)

// enumAlphabet6852 is the alphabet the brute force runs over. Every byte in it
// is load-bearing for importTopSegment's four behaviours:
//
//	'/'  the separator LastIndexByte('/') slices on, and the byte a nested
//	     file path must contain;
//	'.'  the separator the extension strip slices on;
//	'a'  a plain [0-9A-Za-z_] byte, so a segment can be non-empty;
//	'_'  a second such byte, so a two-character segment can be distinguished
//	     from a repeated one when a slice boundary is off by one.
//
// The leading "./" and "../" strips are combinations of '.' and '/', so they
// are reached by the alphabet rather than needing members of their own.
var enumAlphabet6852 = []byte{'a', '_', '.', '/'}

// enumMaxLen6852 is the longest input enumerated. Six is not arbitrary: the
// longest single behaviour importTopSegment has is "../" (3 bytes) followed by
// a segment with an extension ("a.a", 3 bytes), so six is the shortest length
// at which every branch can be reached SIMULTANEOUSLY, and the enumeration runs
// every shorter combination as well.
const enumMaxLen6852 = 6

// forEachImportTarget6852 calls fn for every string over enumAlphabet6852 of
// length 0..enumMaxLen6852 inclusive, and returns how many it produced. The
// count is returned rather than asserted inside so the caller can pin it: a
// generator that silently stopped early would leave every property below
// trivially true, which is the vacuity shape #6834 names.
func forEachImportTarget6852(fn func(string)) int {
	n := 0
	buf := make([]byte, 0, enumMaxLen6852)
	var rec func(depth int)
	rec = func(depth int) {
		fn(string(buf))
		n++
		if depth == enumMaxLen6852 {
			return
		}
		for _, c := range enumAlphabet6852 {
			buf = append(buf, c)
			rec(depth + 1)
			buf = buf[:len(buf)-1]
		}
	}
	rec(0)
	return n
}

// wantEnumCount6852 is the exact size of the enumerated space:
// sum(4^k) for k in 0..6 = (4^7-1)/3 = 5461. Pinned so that a generator bug —
// a wrong recursion bound, an alphabet trimmed to one byte — fails here instead
// of quietly shrinking every property in this file to a handful of cases.
const wantEnumCount6852 = 5461

// TestZig_ImportTopSegmentIsAlwaysASubstring_6852 is the anti-concatenation
// property, enumerated rather than argued.
//
// importTopSegment is the ONLY Name site in this package whose output is not a
// verbatim `(\w+)` regex capture, so it is the only site that could ever spell
// a byte a file path needs. What this test establishes is that the function
// only ever SLICES its input: leading "./" / "../" removal, a slice after the
// last '/', a slice before the last '.', and two `return mod` fall-backs. Every
// one of those is a substring, and the enumeration says so for all 5461 inputs.
//
// THE CONSEQUENCE, which is what file_carrier_6852_test.go rests on: an output
// containing '/' must have come from a '/'-containing INPUT, and the second
// property below pins where in that input the '/' can be.
func TestZig_ImportTopSegmentIsAlwaysASubstring_6852(t *testing.T) {
	bad := 0
	n := forEachImportTarget6852(func(mod string) {
		got := importTopSegment(mod)
		if !strings.Contains(mod, got) {
			bad++
			if bad <= 5 {
				t.Errorf("importTopSegment(%q) = %q, which is NOT a substring of the input — "+
					"the function has stopped being slice-only. A builder that concatenates onto "+
					"this result can spell anything, including a file path at any depth, which is "+
					"the exact defeat cobol's first closure took (#6899).", mod, got)
			}
		}
	})
	if n != wantEnumCount6852 {
		t.Fatalf("enumerated %d inputs, want %d — the generator, not the property, is what "+
			"changed; every assertion in this file is only as wide as this number", n, wantEnumCount6852)
	}
	if bad > 0 {
		t.Errorf("%d of %d enumerated inputs produced a non-substring result", bad, n)
	}
}

// TestZig_ImportTopSegmentSlashImpliesTrailingSlash_6852 is the property that
// makes zig's clause-3 depth split a fact about importTopSegment rather than an
// artefact of a closure, which is what cobol's "root depth only" turned out to
// be (#6899).
//
// A nested file path contains a '/' that is not its last byte. This test
// enumerates the whole space and finds that whenever importTopSegment returns a
// '/'-bearing string, that string ENDS with '/' — the two `return mod`
// fall-backs are reached only when the basename slice came out empty, which
// means the input ended with '/'. So no import target can name an import stub
// after a nested path, and the only clause-3 route left at nested depth needs a
// path that ends in '/'. That input is DRIVEN, not asserted away, by
// TestZig_ImportStubNamedLikeThePath_6852/path_ending_in_a_slash.
func TestZig_ImportTopSegmentSlashImpliesTrailingSlash_6852(t *testing.T) {
	bad := 0
	withSlash := 0
	n := forEachImportTarget6852(func(mod string) {
		got := importTopSegment(mod)
		if !strings.Contains(got, "/") {
			return
		}
		withSlash++
		if !strings.HasSuffix(got, "/") {
			bad++
			if bad <= 5 {
				t.Errorf("importTopSegment(%q) = %q — contains '/' without ending in one, so an "+
					"import stub CAN be named after a nested path and zig's clause-3 depth split "+
					"is wrong", mod, got)
			}
		}
	})
	if n != wantEnumCount6852 {
		t.Fatalf("enumerated %d inputs, want %d", n, wantEnumCount6852)
	}
	// POSITIVE CONTROL for this property, not a decoration: if no enumerated
	// output contained a '/' at all, the loop body above would never run and
	// the test would pass while measuring nothing.
	if withSlash == 0 {
		t.Fatal("no enumerated input produced a '/'-bearing result — the property above is " +
			"vacuous, and a slash-free result set means the alphabet or the generator changed")
	}
	if bad > 0 {
		t.Errorf("%d of %d slash-bearing results did not end in '/'", bad, withSlash)
	}
}
