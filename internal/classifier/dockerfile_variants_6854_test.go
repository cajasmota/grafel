// Package classifier — dockerfile_variants_6854_test.go
//
// Issue #6854. The classifier's basename table mapped the EXACT name
// "Dockerfile" (and "Containerfile") and nothing else. Neither of the two
// conventional variant spellings routed:
//
//	<name>.Dockerfile        — e.g. sample.Dockerfile
//	Dockerfile.<variant>     — e.g. Dockerfile.dev, Dockerfile.multi_stage
//
// MEASURED BEFORE THE FIX (2026-09-06, HEAD 21c7b7dbd). The tree contains
// THREE docker fixtures and no bare `Dockerfile` at all, so every docker file
// this repo ships was dropped before extraction:
//
//	src/fixtures/dockerfile/sample.Dockerfile             skip=true unsupported_extension
//	src/fixtures/sources/dockerfile/sample.Dockerfile     skip=true unsupported_extension
//	src/fixtures/real-world/docker/Dockerfile.multi_stage skip=true unsupported_extension
//
// There is no `Containerfile` and no `Dockerfile.<variant>` other than the
// multi_stage one anywhere under testdata/. The bare-`Dockerfile` row in the
// issue as filed was a synthetic probe, not a corpus file.
//
// THE `src/` PREFIX IS NOT COSMETIC. At their real repo-root paths those three
// files never reach detectLanguage at all: `testdata` is a depDir, so
// universalPathSkip returns `vendor_testdata` two steps earlier, and `build/`
// returns `vendor_build`. The corpus-driven guards that DO see them
// (internal/extractors/file_anchored_carrier_runtime_6847_test.go) rewrite
// `/testdata/` to `/src/` before classifying, which is where the issue's
// `src/fixtures/...` rows come from. Every path below is therefore spelled the
// way the classifier actually receives it; using the on-disk path would make
// this file pass or fail on the vendor rule instead of on the docker rule.
//
// WHAT THIS FILE GRADES, AND IN BOTH DIRECTIONS. Recall alone cannot see a
// widened predicate over-firing: a rule that also swallowed `Dockerfile.md`,
// `Dockerfilex` or `docker-compose.yml` would score identically on every
// accept-shaped assertion. So the forbidden set below is not decoration — it
// is the only half of the grade that constrains the WIDTH of the rule, and it
// is asserted against the same emitted ClassifyResult, never an internal
// counter or the maps themselves.
//
// THE TWO FORMS RIDE DIFFERENT ROUTES, DELIBERATELY, AND THAT IS OBSERVABLE:
//
//   - `<name>.Dockerfile` is an EXTENSION (`.dockerfile`), so it is routed by
//     extensionLanguageMap. That router lowercases, so this form is
//     case-INSENSITIVE — `svc.DOCKERFILE` classifies. It also means
//     classifier.LanguageForExtension(".dockerfile") now answers "dockerfile", which is
//     what stops the unsupported-extension report from listing an extension
//     the classifier does route.
//   - `Dockerfile.<variant>` is a BASENAME, so it sits with the case-SENSITIVE
//     basenameLanguageMap that already held the bare names. Lowercase
//     `dockerfile.dev` therefore does NOT classify — exactly as bare lowercase
//     `dockerfile` does not classify today, and unchanged by this fix.
//
// Both statements are pinned below rather than left to the comment.
package classifier_test

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/cajasmota/grafel/internal/classifier"
)

func newClassifier6854() *classifier.Classifier {
	return classifier.New(noop.NewTracerProvider().Tracer("test"))
}

// dockerVariantCase is one (path → expected emitted language) pair. want == ""
// means the classifier must NOT name a language for it; wantNotDockerfile
// covers the shapes that legitimately classify as something ELSE (Dockerfile.md
// is markdown), where the defect would be "dockerfile" specifically.
type dockerVariantCase struct {
	path string
	want string
	why  string
}

// acceptedDockerShapes6854 — every shape that must reach the dockerfile
// extractor. The three `src/fixtures/...` rows are the repo's real fixtures,
// spelled the way the corpus walk hands them to the classifier (see the
// `src/` note in the file header); they are the reason this issue is not
// hypothetical.
var acceptedDockerShapes6854 = []dockerVariantCase{
	// Controls: worked before the fix, must still work.
	{"Dockerfile", "dockerfile", "exact basename, pre-existing behaviour"},
	{"Containerfile", "dockerfile", "exact basename, pre-existing behaviour"},
	{"deploy/Dockerfile", "dockerfile", "exact basename in a subdirectory"},

	// Suffix form — what BOTH of the repo's own fixtures use.
	{"src/fixtures/dockerfile/sample.Dockerfile", "dockerfile", "in-tree fixture spelling"},
	{"src/fixtures/sources/dockerfile/sample.Dockerfile", "dockerfile", "in-tree fixture spelling"},
	{"app.Dockerfile", "dockerfile", "suffix form"},
	{"svc.Containerfile", "dockerfile", "suffix form, Containerfile stem"},
	{"api.dockerfile", "dockerfile", "suffix form is extension-routed, so case-insensitive"},
	{"api.DOCKERFILE", "dockerfile", "suffix form is extension-routed, so case-insensitive"},

	// Prefix form — the third in-tree fixture plus the conventional targets.
	{"src/fixtures/real-world/docker/Dockerfile.multi_stage", "dockerfile", "in-tree fixture spelling"},
	{"Dockerfile.dev", "dockerfile", "prefix form"},
	{"Dockerfile.prod", "dockerfile", "prefix form"},
	{"Dockerfile.alpine", "dockerfile", "prefix form"},
	{"ci/Dockerfile.ci", "dockerfile", "prefix form in a subdirectory"},
	{"Containerfile.dev", "dockerfile", "prefix form, Containerfile stem"},
}

// forbiddenDockerShapes6854 — the near misses. Each names the way a plausible
// widening would swallow it:
//
//	Dockerfile.md / Dockerfile.py  a variant segment that is itself a known
//	                               extension; these must keep their OWN
//	                               language, which is only true while the
//	                               extension lookup runs BEFORE the docker
//	                               prefix rule. Moving the rule earlier is a
//	                               mutant these two rows kill.
//	Dockerfilex / Dockerfiles      a prefix match with no separator.
//	MyDockerfile / prefix-Dockerfile
//	                               a substring/suffix match with no separator.
//	docker-compose.yml             the "anything docker-ish" widening.
//	Dockerfile.                    an empty variant segment.
//	Dockerfile.a.b                 a multi-segment variant: the rule takes one
//	                               segment, not "everything after the dot".
//	dockerfile.dev                 the case-sensitive half of the rule.
//	Dockerfile/README              a DIRECTORY named Dockerfile — the rule
//	                               reads the basename, never the path.
var forbiddenDockerShapes6854 = []dockerVariantCase{
	{"Dockerfile.md", "markdown", "known extension wins; must not be dockerfile"},
	{"Dockerfile.py", "python", "known extension wins; must not be dockerfile"},
	{"Dockerfile.dev.md", "markdown", "known extension wins over a variant-looking stem"},
	{"docker-compose.yml", "yaml", "docker-adjacent but not a build file"},
	{"Dockerfilex", "", "no separator"},
	{"Dockerfiles", "", "no separator"},
	{"MyDockerfile", "", "no separator before the stem"},
	{"prefix-Dockerfile", "", "hyphen is not the suffix separator"},
	{"Dockerfile.", "", "empty variant segment"},
	{"Dockerfile.a.b", "", "multi-segment variant"},
	{"dockerfile.dev", "", "prefix form is case-sensitive, as the bare-name table is"},
	{"DOCKERFILE.dev", "", "prefix form is case-sensitive, as the bare-name table is"},
	{"Dockerfile/README", "", "a directory named Dockerfile must not classify its children"},
	{"src/Dockerfile/notes", "", "a directory named Dockerfile must not classify its children"},
	{"Containerfilex", "", "no separator, Containerfile stem"},

	// State/format suffixes. These are NOT covered by the extension-lookup
	// ordering — the router claims none of them — so each is rejected by
	// nonContainerVariantSuffixes and each row here is the only thing grading
	// that entry. Dockerfile.bak in particular was already forbidden by
	// TestMX1100_DockerfileWithExtension_NotDockerfile, which caught the first
	// cut of this fix.
	{"Dockerfile.bak", "", "backup, not a build target"},
	{"Dockerfile.backup", "", "backup, not a build target"},
	{"Dockerfile.old", "", "backup, not a build target"},
	{"Dockerfile.orig", "", "merge artefact, not a build target"},
	{"Dockerfile.rej", "", "merge artefact, not a build target"},
	{"Dockerfile.save", "", "editor artefact, not a build target"},
	{"Dockerfile.swp", "", "editor artefact, not a build target"},
	{"Dockerfile.swo", "", "editor artefact, not a build target"},
	{"Dockerfile.tmp", "", "scratch, not a build target"},
	{"Dockerfile.temp", "", "scratch, not a build target"},
	{"Dockerfile.log", "", "output, not a build target"},
	{"Dockerfile.txt", "", "document, not a build target"},
	{"Dockerfile.json", "", "data, not a build target"},
	{"Dockerfile.patch", "", "diff artefact, not a build target"},
	{"Dockerfile.diff", "", "diff artefact, not a build target"},
	{"Dockerfile.lock", "", "lockfile, not a build target"},
	{"Containerfile.bak", "", "the suffix set applies to the Containerfile stem too"},
	{"Dockerfile.BAK", "", "the suffix set is matched case-insensitively"},
}

// TestDockerfileVariantsClassify6854 asserts the ACCEPT direction against the
// emitted ClassifyResult — Language and Skip together, because a language name
// with Skip=true is what `languagesAwaitingExtractor` produces and is NOT the
// same as reaching the extractor.
func TestDockerfileVariantsClassify6854(t *testing.T) {
	c := newClassifier6854()
	ctx := context.Background()

	for _, tc := range acceptedDockerShapes6854 {
		t.Run(tc.path, func(t *testing.T) {
			got := c.Classify(ctx, tc.path)
			if got.Language != tc.want {
				t.Errorf("Classify(%q).Language = %q, want %q (%s); skip=%v reason=%q",
					tc.path, got.Language, tc.want, tc.why, got.Skip, got.SkipReason)
			}
			if got.Skip {
				t.Errorf("Classify(%q).Skip = true (reason %q), want false (%s) — "+
					"a classified docker file must reach the extractor, not be named and dropped",
					tc.path, got.SkipReason, tc.why)
			}
		})
	}
}

// TestDockerfileNearMissesStayUnclassified6854 is the FORBIDDEN direction. It
// is the only assertion here that can fail when the rule is too wide, and a
// widening mutant that swallows everything must die on this test alone.
func TestDockerfileNearMissesStayUnclassified6854(t *testing.T) {
	c := newClassifier6854()
	ctx := context.Background()

	for _, tc := range forbiddenDockerShapes6854 {
		t.Run(tc.path, func(t *testing.T) {
			got := c.Classify(ctx, tc.path)
			if got.Language == "dockerfile" {
				t.Fatalf("Classify(%q).Language = \"dockerfile\" — over-fired (%s)", tc.path, tc.why)
			}
			if got.Language != tc.want {
				t.Errorf("Classify(%q).Language = %q, want %q (%s)",
					tc.path, got.Language, tc.want, tc.why)
			}
			if tc.want == "" && !got.Skip {
				t.Errorf("Classify(%q).Skip = false, want true (%s)", tc.path, tc.why)
			}
		})
	}
}

// TestDockerfileSuffixDenylistIsLoadBearing6854 is the anti-vacuity check for
// the forbidden rows above. `Dockerfile.md` is rejected by ORDERING — the
// extension router claims `.md` and returns before the docker rule — so that
// row would still pass if nonContainerVariantSuffixes were deleted entirely. A
// row is only grading the denylist when the router claims NOTHING for its
// trailing segment. This asserts that for every single-segment
// `Dockerfile.<x>` / `Containerfile.<x>` row whose expected language is "",
// leaving no row in that set silently shadowed.
//
// It also fails if the set is emptied to fewer than the twelve distinct
// segments measured at #6854, so deleting rows to make a widening pass shows up
// here rather than as a quietly smaller grade.
func TestDockerfileSuffixDenylistIsLoadBearing6854(t *testing.T) {
	seen := map[string]bool{}
	for _, tc := range forbiddenDockerShapes6854 {
		if tc.want != "" {
			continue
		}
		var variant string
		for _, stem := range []string{"Dockerfile.", "Containerfile."} {
			if strings.HasPrefix(tc.path, stem) {
				variant = strings.TrimPrefix(tc.path, stem)
			}
		}
		if variant == "" || strings.Contains(variant, ".") || strings.Contains(variant, "/") {
			continue
		}
		seen[strings.ToLower(variant)] = true
		if got := classifier.LanguageForExtension("." + variant); got != "" {
			t.Errorf("%q is rejected by the extension router (LanguageForExtension(%q) = %q), "+
				"not by the docker suffix denylist — this row grades nothing",
				tc.path, "."+variant, got)
		}
	}
	if len(seen) < 16 {
		t.Errorf("only %d distinct denylisted variant segments are graded (%v); "+
			"#6854 measured sixteen and the forbidden set must not shrink silently",
			len(seen), seen)
	}
}

// TestDockerfileExtensionIsRouterVisible6854 pins the consequence the accept
// test cannot see: the suffix form works because `.dockerfile` is a ROUTED
// extension, so the unsupported-extension report must stop calling it
// unsupported. Asserting only Classify would leave a fix that special-cased the
// basename indistinguishable from this one, while leaving the report wrong.
func TestDockerfileExtensionIsRouterVisible6854(t *testing.T) {
	for _, ext := range []string{".dockerfile", ".Dockerfile", ".DOCKERFILE", ".containerfile"} {
		if got := classifier.LanguageForExtension(ext); got != "dockerfile" {
			t.Errorf("classifier.LanguageForExtension(%q) = %q, want %q", ext, got, "dockerfile")
		}
		if !classifier.SupportedExtension(ext) {
			t.Errorf("classifier.SupportedExtension(%q) = false, want true", ext)
		}
	}
	// Forbidden direction for the router view too: a near-miss extension must
	// not become supported as a side effect.
	for _, ext := range []string{".dockerfilex", ".docker", ".dockerignore"} {
		if got := classifier.LanguageForExtension(ext); got == "dockerfile" {
			t.Errorf("classifier.LanguageForExtension(%q) = %q — over-fired", ext, got)
		}
	}
}
