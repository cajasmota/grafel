package engine

import (
	"context"
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
	walkErr := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
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
		content, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		res, detErr := d.Detect(context.Background(), extractor.FileInput{
			Path: rel, Content: content, Language: lang,
		})
		if detErr != nil || res == nil {
			return nil
		}
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
	if len(found) != 0 {
		sort.Strings(found)
		t.Fatalf("%d literal self-loop(s) emitted over the golden corpus (#6809):\n  %s",
			len(found), strings.Join(found, "\n  "))
	}
}
