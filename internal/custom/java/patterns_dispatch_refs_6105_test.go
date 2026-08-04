package java

import "testing"

// Issue #6105 — canonicalStructuralRef unit coverage.
//
// The end-to-end proof that this repairs real edges lives in
// cmd/grafel/custom_extractor_dangling_6105_test.go, which indexes a fixture and
// asserts the RETURNS / ACCEPTS_INPUT / OWNS endpoints bind to real entities.
// What that test CANNOT reach is the already-canonical branch: no extractor in
// this package emits a six-segment ref today, so the idempotence guard has no
// live caller. It is kept because the natural next step is for extractors to
// mint the full form directly, and a re-canonicalising helper would corrupt
// those refs. Covering it here keeps it from being silently dead.
func TestCanonicalStructuralRef6105(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "five-segment ref gains the language slot",
			in:   "scope:schema:spring_dto:src/main/java/api/Orders.java:OrderRequest",
			want: "scope:schema:spring_dto:java:src/main/java/api/Orders.java:OrderRequest",
		},
		{
			// The tail is everything after the file segment, so a name that
			// itself contains colons is repaired rather than truncated. Before
			// the fix this ref's FILE segment was parsed as the language and its
			// receiver as the file.
			name: "colon-bearing name tail keeps its colons in the tail",
			in:   "scope:pattern:obs_log_statement:src/x/A.java:log:info:42",
			want: "scope:pattern:obs_log_statement:java:src/x/A.java:log:info:42",
		},
		{
			name: "already canonical is returned unchanged",
			in:   "scope:schema:spring_dto:java:src/x/A.java:OrderRequest",
			want: "scope:schema:spring_dto:java:src/x/A.java:OrderRequest",
		},
		{
			// The guard must be SHAPE-aware, not `structuralRefLang`-aware. This
			// package extracts Kotlin too (ExtractSpringAOP accepts it), so the
			// obvious `HasPrefix(tail, "java:")` guard would double-stamp a
			// hand-written kotlin ref into `…:java:kotlin:…` — seven fields, and
			// statusUnmatched. That failure is precisely the scenario the guard
			// exists to protect, so it is covered here rather than left to the
			// java case that cannot distinguish the two implementations.
			name: "already-canonical kotlin slot is not double-stamped",
			in:   "scope:operation:method:kotlin:src/x/A.kt:foo",
			want: "scope:operation:method:kotlin:src/x/A.kt:foo",
		},
		{
			name: "already-canonical go slot is not double-stamped",
			in:   "scope:component:ref:go:a/b.go:Foo",
			want: "scope:component:ref:go:a/b.go:Foo",
		},
		{
			// Language aliases normalizeLang accepts (refs.go:261-276) count as
			// canonical too — `kt` is what a Kotlin emitter may plausibly write.
			name: "language alias in the slot is recognised",
			in:   "scope:operation:method:kt:src/x/A.kt:foo",
			want: "scope:operation:method:kt:src/x/A.kt:foo",
		},
		{
			// A file segment that merely STARTS with a language name is not a
			// language slot — the token must be the whole field. Without this the
			// guard would skip every ref under a `java/` source root.
			name: "file segment beginning with a language name is still repaired",
			in:   "scope:schema:spring_dto:java/api/Orders.java:OrderRequest",
			want: "scope:schema:spring_dto:java:java/api/Orders.java:OrderRequest",
		},
		{
			// `cache:<framework>:<region>` and `Class:<Name>` are name-shaped
			// refs, not structural ones. Repairing them is a different defect
			// (#6105 defect (2)) and must not happen here.
			name: "non-scope ref untouched",
			in:   "cache:spring:orders",
			want: "cache:spring:orders",
		},
		{
			name: "structurally incomplete scope ref untouched",
			in:   "scope:operation",
			want: "scope:operation",
		},
		{
			name: "empty ref untouched",
			in:   "",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canonicalStructuralRef(tc.in); got != tc.want {
				t.Errorf("canonicalStructuralRef(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
