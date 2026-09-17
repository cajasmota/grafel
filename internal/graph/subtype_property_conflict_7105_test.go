package graph

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureStderr7105 runs fn with os.Stderr redirected to a pipe and returns
// what was written. Not parallel-safe, so none of these subtests call
// t.Parallel.
func captureStderr7105(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stderr = orig
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

// #7105 — a Properties["subtype"] that disagrees with the canonical
// Entity.Subtype is PRESERVED, not overwritten, and that resolution is
// REPORTED rather than silent. Measured disagreement on the 38 golden fixtures
// is 0/211, so the report is silent on every known input; it exists so that a
// producer which starts emitting a second, different answer for one entity is
// findable instead of being resolved quietly.
//
// Graded on the emitted artefact — the stderr line — not on the returned
// slice, and in BOTH directions. The no-warning direction is the load-bearing
// one: 796 of 1069 entities take the ordinary derive path, and a report that
// fired on those would put a warning line on three quarters of the corpus at
// every write.
func TestSubtypeConflictIsReportedNotSilent_7105(t *testing.T) {
	t.Run("disagreement_is_reported_and_property_preserved", func(t *testing.T) {
		e := Entity{ID: "c-1", Name: "LegacyShim", Kind: "SCOPE.Component", Subtype: "class"}
		e.PropsReplace(map[string]string{"subtype": "interface"})
		doc := &Document{Version: 2, Entities: []Entity{e}}

		out := captureStderr7105(t, func() { SortDocumentForEmission(doc) })

		if !strings.Contains(out, "subtype-property conflict") {
			t.Fatalf("#7105: a disagreeing subtype twin was resolved SILENTLY; stderr was %q", out)
		}
		for _, want := range []string{"id=c-1", `canonical="class"`, `property="interface"`, "PRESERVED"} {
			if !strings.Contains(out, want) {
				t.Errorf("#7105: conflict report is missing %q, so it cannot be acted on; got %q", want, out)
			}
		}
		if got := doc.Entities[0].PropGet("subtype"); got != "interface" {
			t.Errorf("#7105: existing property = %q, want the pre-existing %q preserved", got, "interface")
		}
	})

	t.Run("forbidden_ordinary_derive_is_silent", func(t *testing.T) {
		gap := Entity{ID: "c-2", Name: "Gateway", Kind: "SCOPE.Component", Subtype: "interface"}
		agree := Entity{ID: "c-3", Name: "Impl", Kind: "SCOPE.Component", Subtype: "class"}
		agree.PropsReplace(map[string]string{"subtype": "class"})
		empty := Entity{ID: "c-4", Name: "charge", Kind: "SCOPE.Function"}
		doc := &Document{Version: 2, Entities: []Entity{gap, agree, empty}}

		out := captureStderr7105(t, func() { SortDocumentForEmission(doc) })

		if strings.Contains(out, "subtype-property conflict") {
			t.Fatalf("#7105 FORBIDDEN: the ordinary derive path reported a conflict. It runs on 796 of 1069 entities, so a report here is a warning line on three quarters of the corpus at every write. stderr was %q", out)
		}
	})

	t.Run("forbidden_second_pass_reports_nothing_new", func(t *testing.T) {
		gap := Entity{ID: "c-5", Name: "Gateway", Kind: "SCOPE.Component", Subtype: "interface"}
		doc := &Document{Version: 2, Entities: []Entity{gap}}
		SortDocumentForEmission(doc)

		out := captureStderr7105(t, func() { SortDocumentForEmission(doc) })

		if out != "" {
			t.Fatalf("#7105 FORBIDDEN: the funnel is invoked more than once per write (index.go:999 and again inside fbwriter), so a key this normaliser stamped itself must not be read back as a conflict on the second pass. stderr was %q", out)
		}
		if got := doc.Entities[0].PropGet("subtype"); got != "interface" {
			t.Errorf("#7105: idempotence broken: property = %q after two passes", got)
		}
	})
}
