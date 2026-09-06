// Package classifier — dockerfile_denylist_6854_internal_test.go
//
// #6854 review item 2. The external table in dockerfile_variants_6854_test.go
// grades the emitted ClassifyResult for a hand-written list of shapes. A
// hand-written list over a MAP has one failure mode it cannot cover: a key
// added to nonContainerVariantSuffixes with no row added beside it. A floor on
// the table's size does not fix that — a floor catches shrinkage, never a
// silent addition.
//
// This file is internal (package classifier) for exactly that reason: it can
// read the map and DERIVE the grade from it, so the covered set and the map
// cannot drift. It is the same move carrier_caller_set_6861_test.go makes for
// the carrier roster, and for the same stated reason.
package classifier

import (
	"strings"
	"testing"
)

// TestDockerfileDenylistIsExhaustivelyGraded6854 walks nonContainerVariantSuffixes
// and asserts, per entry, the two properties the entry is supposed to have.
//
// The second is the anti-vacuity half and is the one worth reading twice. The
// docker rule runs LAST in detectLanguage, so a segment the extension router
// already claims never reaches the denylist at all: an entry for `md` would be
// permanently shadowed, would grade nothing, and would read as coverage. That is
// the shape #6861 named — a witness that stands for a property it does not hold.
func TestDockerfileDenylistIsExhaustivelyGraded6854(t *testing.T) {
	if len(nonContainerVariantSuffixes) == 0 {
		t.Fatal("nonContainerVariantSuffixes is empty — the blocklist half of the " +
			"over-firing defence has been deleted, and every row in the external " +
			"forbidden table that depends on it now passes for the wrong reason")
	}
	for variant := range nonContainerVariantSuffixes {
		t.Run(variant, func(t *testing.T) {
			if variant == "" {
				t.Fatal("an empty key can never be reached: the caller rejects an empty variant first")
			}
			if variant != strings.ToLower(variant) {
				t.Errorf("key %q is not lowercase — the lookup lowercases its input, so this "+
					"key can never match", variant)
			}
			if strings.Contains(variant, ".") {
				t.Errorf("key %q contains a dot — the caller rejects a multi-segment variant "+
					"before the lookup, so this key can never match", variant)
			}
			for _, stem := range []string{"Dockerfile", "Containerfile"} {
				name := stem + "." + variant
				if got := detectLanguage(name); got != "" {
					t.Errorf("detectLanguage(%q) = %q, want %q — the entry does not reject",
						name, got, "")
				}
			}
			// Load-bearing, not shadowed: the extension router must claim
			// nothing for this segment, or the docker rule never sees it.
			if got := extensionLanguageMap["."+variant]; got != "" {
				t.Errorf("extensionLanguageMap[%q] = %q — the extension lookup returns before "+
					"the docker rule runs, so this entry is shadowed dead weight and grades "+
					"nothing", "."+variant, got)
			}
		})
	}
}

// TestDockerfileShapeRulesRejectWhatNoListCan6854 grades the two rules that
// exist BECAUSE a blocklist over an unbounded segment space cannot be complete.
// Both are derived from the map rather than hand-listed, so they hold for every
// entry the map ever gains.
//
// The `~` case is the sharp one. Trimming a trailing tilde before the lookup —
// the obvious implementation — rejects `Dockerfile.bak~` and ACCEPTS
// `Dockerfile.dev~`, because "dev" is not a blocked segment. Rejecting on the
// tilde itself is what covers both, and the second loop is what tells the two
// implementations apart.
func TestDockerfileShapeRulesRejectWhatNoListCan6854(t *testing.T) {
	t.Run("tilde on a blocked segment", func(t *testing.T) {
		for variant := range nonContainerVariantSuffixes {
			name := "Dockerfile." + variant + "~"
			if got := detectLanguage(name); got != "" {
				t.Errorf("detectLanguage(%q) = %q, want %q", name, got, "")
			}
		}
	})

	// A trailing tilde on an OTHERWISE-ACCEPTED segment. `Dockerfile.dev` is a
	// build target and classifies; `Dockerfile.dev~` is a backup of it and must
	// not. This is the assertion a trim-then-lookup implementation fails.
	t.Run("tilde on an accepted segment", func(t *testing.T) {
		for _, variant := range []string{"dev", "prod", "alpine", "ci", "multi_stage"} {
			if got := detectLanguage("Dockerfile." + variant); got != "dockerfile" {
				t.Fatalf("premise: detectLanguage(%q) = %q, want dockerfile — this control only "+
					"means something while the un-tilded name IS accepted",
					"Dockerfile."+variant, got)
			}
			name := "Dockerfile." + variant + "~"
			if got := detectLanguage(name); got != "" {
				t.Errorf("detectLanguage(%q) = %q, want %q — a trailing tilde is the backup "+
					"marker whatever precedes it", name, got, "")
			}
		}
	})

	t.Run("purely numeric segments", func(t *testing.T) {
		for _, variant := range []string{"0", "1", "2", "10", "20260101", "000"} {
			name := "Dockerfile." + variant
			if got := detectLanguage(name); got != "" {
				t.Errorf("detectLanguage(%q) = %q, want %q — a numeric segment is a revision "+
					"or a date stamp, never a build target", name, got, "")
			}
		}
		// The contrast: a version-shaped target that merely CONTAINS digits is
		// still a target. Without this the numeric rule could be widened to
		// "starts with a digit" or "contains a digit" and nothing would notice.
		for _, variant := range []string{"v1", "1x", "x1", "go1", "node20"} {
			name := "Dockerfile." + variant
			if got := detectLanguage(name); got != "dockerfile" {
				t.Errorf("detectLanguage(%q) = %q, want dockerfile — only a PURELY numeric "+
					"segment is a revision", name, got)
			}
		}
	})
}
