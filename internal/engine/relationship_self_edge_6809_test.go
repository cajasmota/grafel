package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6809 — five relationship_rules named the SAME capture group and the
// SAME entity type for both endpoints, so detector.go built `FromID == ToID`
// literally at emission. No resolver was involved: for such a rule
// extractGroup returns the same string for both ends, so EVERY match it can
// ever produce is a self-loop, by construction and not by accident.
//
// The measurement that motivated the change (re-derived from the tree, not
// copied from the issue):
//
//	rule                                                    emitted / self-loop
//	sqlalchemy `from [\w.]+ import ([A-Z]\w*)`   DEPENDS_ON      12 / 12
//	django     `include("...")`                  ROUTES_TO        1 /  1
//	sqlalchemy `ForeignKey("t.c")`               DEPENDS_ON       0 /  0
//	celery     `x.delay(`                        CALLS            0 /  0
//	github_actions `needs: x`                    CALLS            0 /  0
//
// across internal/quality/golden/. 13 self-loops emitted, 12 surviving to the
// end of Detect (one target endpoint is rewritten by rewritePythonModelImports
// into `Model:View -> View:View`, which is a different edge but not a better
// one), and 4 surviving resolution into python-django-mini's persisted graph.
//
// The tests below pin three separate things:
//
//  1. the POPULATION — no embedded rule wears the colliding shape;
//  2. the CLASS — compile() refuses such a rule, and does NOT refuse either
//     legitimate neighbour (same capture / different types, and different
//     captures / same type — the shape that expresses real recursion);
//  3. the EMITTED EDGES — what the two firing rules actually produce now.
//
// Every assertion is on an emitted RelationshipRecord, never on a counter the
// engine keeps about itself.

// collidingRule is the shape the load-time guard rejects.
func collidingRule() FrameworkRule {
	return FrameworkRule{
		RelationshipRules: []RelationshipRule{{
			Pattern:      `IMPORT (\w+)`,
			SourceType:   "Model",
			TargetType:   "Model",
			Relationship: "DEPENDS_ON",
			SourceGroup:  1,
			TargetGroup:  1,
		}},
	}
}

func detectRels(t *testing.T, rule FrameworkRule, content string) []types.RelationshipRecord {
	t.Helper()
	d := New(map[string][]FrameworkRule{"python": {rule}})
	res, err := d.Detect(context.Background(), extractor.FileInput{
		Path: "a.py", Content: []byte(content), Language: "python",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	return res.Relationships
}

func hasEdge(rels []types.RelationshipRecord, from, kind, to string) bool {
	for _, r := range rels {
		if r.FromID == from && r.Kind == kind && r.ToID == to {
			return true
		}
	}
	return false
}

// Test6809_LoadTimeGuardRejectsCollidingRule is the rejecting direction: a rule
// that names one capture and one type for both endpoints never reaches the
// emission site, so the self-loop it would have built is never built.
func Test6809_LoadTimeGuardRejectsCollidingRule(t *testing.T) {
	rels := detectRels(t, collidingRule(), "IMPORT User\n")
	for _, r := range rels {
		if r.FromID == r.ToID {
			t.Fatalf("colliding rule was compiled and emitted a literal self-loop %s -[%s]-> %s",
				r.FromID, r.Kind, r.ToID)
		}
	}
	if len(rels) != 0 {
		t.Fatalf("colliding rule emitted %d relationship(s), want 0: %+v", len(rels), rels)
	}
}

// Test6809_LoadTimeGuardKeepsSameCaptureDifferentTypes is the FIRST direction a
// rejection test cannot see: the "one capture, two endpoint types" idiom is
// legitimate (django.yaml's `from <app>.models import X` is View -> Model on a
// single capture) and must still compile and still fire.
func Test6809_LoadTimeGuardKeepsSameCaptureDifferentTypes(t *testing.T) {
	rule := collidingRule()
	rule.RelationshipRules[0].SourceType = "View"

	rels := detectRels(t, rule, "IMPORT User\n")
	if !hasEdge(rels, "View:User", "DEPENDS_ON", "Model:User") {
		t.Fatalf("same-capture/different-types rule did not fire; got %+v", rels)
	}
}

// Test6809_LoadTimeGuardKeepsDifferentCapturesSameType is the SECOND direction,
// and the one that would cost the most recall if the guard were widened to
// `source_type == target_type` alone: a same-type edge between two DIFFERENT
// captures is how genuine recursion and task-to-task composition are written
// (celery's `chain(a.s() | b.s())` rule is Task -> Task on groups 1 and 2).
func Test6809_LoadTimeGuardKeepsDifferentCapturesSameType(t *testing.T) {
	rule := collidingRule()
	rule.RelationshipRules[0].Pattern = `CHAIN (\w+) (\w+)`
	rule.RelationshipRules[0].TargetGroup = 2

	rels := detectRels(t, rule, "CHAIN alpha beta\n")
	if !hasEdge(rels, "Model:alpha", "DEPENDS_ON", "Model:beta") {
		t.Fatalf("different-captures/same-type rule did not fire; got %+v", rels)
	}
}

// Test6809_LoadTimeGuardKeepsSameCaptureSameTypeApart is the compound guard's
// third leg, scored separately so the two conditions are not graded only in
// combination: a rule that shares NEITHER a capture NOR a type must obviously
// still fire, and a rule that shares BOTH must not.
func Test6809_LoadTimeGuardKeepsDifferentCapturesDifferentTypes(t *testing.T) {
	rule := collidingRule()
	rule.RelationshipRules[0].Pattern = `CHAIN (\w+) (\w+)`
	rule.RelationshipRules[0].TargetGroup = 2
	rule.RelationshipRules[0].SourceType = "View"

	rels := detectRels(t, rule, "CHAIN alpha beta\n")
	if !hasEdge(rels, "View:alpha", "DEPENDS_ON", "Model:beta") {
		t.Fatalf("different-captures/different-types rule did not fire; got %+v", rels)
	}
}

// Test6809_NoEmbeddedRuleCollapsesBothEndpoints is the population assertion.
// It reads the SHIPPED rule tree — not a synthetic one — so a new rule authored
// with the colliding shape fails here whether or not any fixture exercises it.
func Test6809_NoEmbeddedRuleCollapsesBothEndpoints(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	var bad []string
	langs := make([]string, 0, len(rules))
	for lang := range rules {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	for _, lang := range langs {
		for _, fr := range rules[lang] {
			for _, rr := range fr.RelationshipRules {
				if rr.SourceGroup == rr.TargetGroup && rr.SourceType == rr.TargetType {
					bad = append(bad, lang+"/"+fr.Frameworks.Name+": "+rr.Relationship+
						" "+rr.SourceType+" group "+rr.Pattern)
				}
			}
		}
	}
	if len(bad) != 0 {
		t.Fatalf("%d embedded relationship_rule(s) name one capture and one type for both "+
			"endpoints, so every edge they emit is FromID==ToID by construction (#6809):\n  %s",
			len(bad), strings.Join(bad, "\n  "))
	}
}

// Test6809_DjangoIncludeRoutesPrefixToUrlconf pins the repaired django rule by
// its EMITTED EDGE. The construct it reads is one call expression, so both
// endpoints were reachable from the pattern all along:
//
//	path("users/", include("users.urls"))
//
// Before the repair this emitted `Route:users.urls -> Route:users.urls`.
func Test6809_DjangoIncludeRoutesPrefixToUrlconf(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	d := New(rules)

	src, err := os.ReadFile(filepath.Join("..", "quality", "golden",
		"python-django-mini", "src", "myproject", "urls.py"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	res, err := d.Detect(context.Background(), extractor.FileInput{
		Path: "myproject/urls.py", Content: src, Language: "python",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasEdge(res.Relationships, "Route:users/", "ROUTES_TO", "Route:users.urls") {
		t.Fatalf("django include() rule did not emit the prefix -> urlconf edge; got %+v",
			res.Relationships)
	}
	for _, r := range res.Relationships {
		if r.FromID == r.ToID {
			t.Fatalf("myproject/urls.py still emits a literal self-loop %s -[%s]-> %s",
				r.FromID, r.Kind, r.ToID)
		}
	}
}

// Test6809_GoldenCorpusEmitsNoLiteralSelfLoop is the corpus-level pin. It walks
// the same golden trees the extraction-quality gate grades and asserts that
// nothing the YAML rule layer emits has FromID == ToID.
//
// Before the change this reported 13 edges across python-django-mini (6),
// python-fastapi-mini (2) and python-rq-mini (4) — the rq ones from
// sqlalchemy's over-broad `from <anything> import <Capitalised>` rule firing on
// files that have nothing to do with SQLAlchemy.
func Test6809_GoldenCorpusEmitsNoLiteralSelfLoop(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	d := New(rules)

	root := filepath.Join("..", "quality", "golden")
	var found []string
	filesWalked, relsSeen := 0, 0
	walkErr := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		lang := ""
		switch filepath.Ext(p) {
		case ".py":
			lang = "python"
		case ".yml", ".yaml":
			if strings.Contains(filepath.ToSlash(p), ".github/workflows/") {
				lang = "cicd"
			}
		}
		if lang == "" {
			return nil
		}
		// Read and Detect errors are FAILURES, not skips. A `return nil` here
		// is one of the five ways a scan-and-assert-absence guard reports clean
		// without having looked: the walk runs, the file is selected, and the
		// assertion then grades an empty set.
		content, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(root, p)
		res, detErr := d.Detect(context.Background(), extractor.FileInput{
			Path: rel, Content: content, Language: lang,
		})
		if detErr != nil {
			return detErr
		}
		if res == nil {
			return errors.New("Detect returned a nil result for " + rel)
		}
		filesWalked++
		relsSeen += len(res.Relationships)
		for _, r := range res.Relationships {
			if r.FromID == r.ToID {
				found = append(found, rel+": "+r.FromID+" -["+r.Kind+"]-> "+r.ToID)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk golden: %v", walkErr)
	}

	// The floor. This test asserts an ABSENCE, so it passes for free the moment
	// it stops looking — and the ways it can stop looking do not all need a code
	// edit: the fixtures could move out of internal/quality/golden, or the
	// extension mapping above could stop matching, and the assertion below would
	// report a clean corpus having graded nothing. Measured on the corpus this
	// change landed against: 21 files selected, 30 relationships emitted before
	// the fix and 18 after. The floors are set well under those so ordinary
	// fixture churn does not trip them, and far enough over zero that a collapse
	// to "nothing was walked" or "nothing was extracted" is a failure rather
	// than a pass.
	const (
		minFiles = 12
		minRels  = 8
	)
	t.Logf("walked %d golden file(s), %d relationship(s) emitted", filesWalked, relsSeen)
	if filesWalked < minFiles {
		t.Fatalf("only %d golden file(s) were walked (want >= %d) — this test asserts an "+
			"absence, so a corpus it cannot see reports clean without grading anything",
			filesWalked, minFiles)
	}
	if relsSeen < minRels {
		t.Fatalf("the %d walked file(s) emitted only %d relationship(s) (want >= %d) — the "+
			"walk found files but the rule layer extracted nothing, so the self-loop "+
			"assertion below has no population to grade",
			filesWalked, relsSeen, minRels)
	}

	if len(found) != 0 {
		sort.Strings(found)
		t.Fatalf("%d literal self-loop(s) emitted over the golden corpus, out of %d "+
			"relationship(s) from %d file(s) (#6809):\n  %s",
			len(found), relsSeen, filesWalked, strings.Join(found, "\n  "))
	}
}

// Test6809_DjangoIncludeEmptyPrefixIsAKnownGap pins what the repaired rule does
// NOT do, so the next reader meets a failure that names the decision instead of
// discovering the hole.
//
// The root-URLconf mount is the commonest form of this construct:
//
//	path("", include("core.urls"))
//
// Its prefix capture is the EMPTY string, and detector.go's emission site drops
// any match whose source or target name is empty (`if sourceName == "" ||
// targetName == "" { continue }`). So the repaired rule emits NOTHING there.
//
// That is not a regression — before #6809 this construct emitted
// `Route:core.urls -> Route:core.urls`, a self-loop — but it does mean the
// claim "the mounting prefix was in the construct all along" holds for a
// PREFIXED mount and not for a root mount. Expressing the root mount needs an
// endpoint that is not the prefix (the including FILE, say), which is a
// different rule rather than a wider regex.
//
// The third leg records a smaller wart on the same rule: `re_path` carries a
// regex, and the capture keeps its anchor, so the source entity is named
// `Route:^api/` rather than `Route:api/`.
func Test6809_DjangoIncludeEmptyPrefixIsAKnownGap(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	d := New(rules)

	detect := func(src string) []types.RelationshipRecord {
		t.Helper()
		res, detErr := d.Detect(context.Background(), extractor.FileInput{
			Path: "myproject/urls.py", Content: []byte(src), Language: "python",
		})
		if detErr != nil {
			t.Fatalf("Detect: %v", detErr)
		}
		return res.Relationships
	}

	// Control: a PREFIXED mount does emit, so the leg below is measuring the
	// empty prefix and not a rule that has stopped working altogether.
	prefixed := detect("urlpatterns = [\n    path(\"users/\", include(\"users.urls\")),\n]\n")
	if !hasEdge(prefixed, "Route:users/", "ROUTES_TO", "Route:users.urls") {
		t.Fatalf("control: a prefixed include() must still emit; got %+v", prefixed)
	}

	// The gap. Asserted as a SHAPE (no ROUTES_TO at all), not merely "no
	// self-loop": if the rule ever learns to express a root mount this fails,
	// and it is meant to.
	root := detect("urlpatterns = [\n    path(\"\", include(\"core.urls\")),\n]\n")
	for _, r := range root {
		if r.FromID == r.ToID {
			t.Fatalf("root mount emitted a literal self-loop %s -[%s]-> %s",
				r.FromID, r.Kind, r.ToID)
		}
		if r.Kind == "ROUTES_TO" {
			t.Fatalf("KNOWN GAP CLOSED: `path(\"\", include(\"core.urls\"))` now emits "+
				"%s -[ROUTES_TO]-> %s. The empty prefix used to be dropped by the "+
				"emission site's empty-name guard. If this is the intended fix, update "+
				"this test and the #6809 note on the rule in django.yaml", r.FromID, r.ToID)
		}
	}

	// The anchor wart: re_path keeps `^` in the entity name.
	re := detect("urlpatterns = [\n    re_path(r\"^api/\", include(\"api.urls\")),\n]\n")
	if !hasEdge(re, "Route:^api/", "ROUTES_TO", "Route:api.urls") {
		t.Fatalf("re_path leg: expected the source Route to keep its regex anchor "+
			"(`Route:^api/`); got %+v", re)
	}
}
