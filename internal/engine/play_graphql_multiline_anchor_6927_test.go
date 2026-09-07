package engine

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/classifier"
	"github.com/cajasmota/grafel/internal/extractor"
)

// #6927 — the final arm: rules/scala/frameworks/play_framework.yaml (2
// patterns) and rules/graphql/frameworks/graphql_schema.yaml (1). The two
// files got OPPOSITE treatments, and the reason is the measurement, not the
// pattern text:
//
//	play_framework.yaml   ^(?:GET|POST|…)\s+(\S+)\s+controllers\.(\w+\.\w+)
//	                      as a source_pattern AND as a relationship_rule.
//	                      Widened with (?m). Measured over 33 real conf/routes
//	                      files: 4 of 135 route lines before, 135 of 135 after,
//	                      and ZERO matches in 1052 real .scala/.sc files in
//	                      either mode, so the rule is framework-specific
//	                      without a requires_framework gate.
//
//	graphql_schema.yaml   ^\s+(\w+)\s*\([^)]*\)\s*:  -> Operation
//	                      REMOVED. Measured over 1907 real .graphql/.gql
//	                      files: 0 matches today, 1429 under (?m) and all of
//	                      them inside SDL documents — so it does not over-fire
//	                      across file kinds. It is a strict subset of what
//	                      internal/extractors/graphql already emits per field,
//	                      under a bare name, and widening it silently repointed
//	                      two SERVES edges. See the comment where it was
//	                      removed; the graded consequence is in
//	                      internal/quality/golden/graphql-schema-mini.
//
// Graded in BOTH directions (#6902): a recall assertion is structurally blind
// to over-firing, and the play arm's whole risk is over-firing.

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

// playOnlyRules returns the loaded rule map reduced to the SCALA Play rule set
// alone. The rules are the REAL embedded YAML — the defect IS the shipped
// pattern text — but every other rule set is dropped so nothing another file
// mints can be mistaken for evidence about this one.
//
// The scala bucket specifically: rules/java/frameworks/play_framework.yaml
// declares the same `frameworks.name`, so filtering on the name alone returns
// TWO rule sets and an assertion about "the Play routes rule" would be
// ambiguous about which file it graded.
func playOnlyRules(t *testing.T) map[string][]FrameworkRule {
	t.Helper()
	all, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	out := map[string][]FrameworkRule{}
	for _, fr := range all["scala"] {
		if fr.Frameworks.Name == "Play Framework" {
			out["scala"] = append(out["scala"], fr)
		}
	}
	if len(out["scala"]) != 1 {
		t.Fatalf("expected exactly one Play rule set in the scala bucket, got %d",
			len(out["scala"]))
	}
	return out
}

func graphqlOnlyRules(t *testing.T) map[string][]FrameworkRule {
	t.Helper()
	all, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	out := map[string][]FrameworkRule{}
	for _, fr := range all["graphql"] {
		if fr.Frameworks.Name == "GraphQL Schema" {
			out["graphql"] = append(out["graphql"], fr)
		}
	}
	if len(out["graphql"]) != 1 {
		t.Fatalf("expected exactly one GraphQL Schema rule set in the graphql bucket, got %d",
			len(out["graphql"]))
	}
	return out
}

func detectPG6927(t *testing.T, rules map[string][]FrameworkRule, path, lang, src string) *DetectResult {
	t.Helper()
	det := New(rules)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: lang,
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	return res
}

func namesPG6927(res *DetectResult, kind string) []string {
	var out []string
	for _, e := range res.Entities {
		if e.Kind == kind {
			out = append(out, e.Name)
		}
	}
	sort.Strings(out)
	return out
}

func edgesPG6927(res *DetectResult, kind string) []string {
	var out []string
	for _, r := range res.Relationships {
		if r.Kind == kind {
			out = append(out, r.FromID+" -> "+r.ToID)
		}
	}
	sort.Strings(out)
	return out
}

// playRouteSourcePattern and playRouteRelationshipRule select the two rules
// under test STRUCTURALLY — by what they do, not by their text. #6945's review
// found a test selecting its rule by text substring: it went red on a
// behaviour-preserving rewrite and its apparent kill of a real anchor weakening
// was selector loss, not grading.
func playRouteSourcePattern(t *testing.T) SourcePattern {
	t.Helper()
	var found []SourcePattern
	for _, sp := range playOnlyRules(t)["scala"][0].SourcePatterns {
		if sp.EntityType == "Route" {
			found = append(found, sp)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one Route source_pattern in play_framework.yaml, got %d", len(found))
	}
	return found[0]
}

func playRouteRelationshipRule(t *testing.T) RelationshipRule {
	t.Helper()
	var found []RelationshipRule
	for _, rr := range playOnlyRules(t)["scala"][0].RelationshipRules {
		if rr.Relationship == "ROUTES_TO" {
			found = append(found, rr)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one ROUTES_TO relationship_rule in play_framework.yaml, got %d", len(found))
	}
	return found[0]
}

// ---------------------------------------------------------------------------
// The fixture
// ---------------------------------------------------------------------------

// play6927Routes is a realistic Play `conf/routes` file. It opens with the
// three-line `# Routes` header the `play new` template generates and every one
// of the 33 corpus routes files carries in some form — which is exactly why
// start-of-text found a route in only 4 of them.
//
// Everything below the blank line after `/users/:id` is a forbidden shape, one
// per axis, and each is a real construct of the routes format rather than an
// invented one.
const play6927Routes = `# Routes
# This file defines all application routes (Higher priority routes first)
# ~~~~

GET     /                           controllers.HomeController.index
GET     /users                      controllers.UserController.list
POST    /users                      controllers.UserController.create
GET     /users/:id                  controllers.UserController.show
DELETE  /users/:id                  controllers.UserController.delete

# GET   /legacy                     controllers.LegacyController.index
  GET   /indented                   controllers.IndentedController.index
TRACE   /trace                      controllers.TraceController.index
GET     /health                     services.HealthCheck.ping

# Map static resources from the /public folder to the /assets URL path
GET     /assets/*file               controllers.Assets.versioned(path="/public", file: Asset)
`

// ---------------------------------------------------------------------------
// Recall
// ---------------------------------------------------------------------------

// TestIssue6927_Play_RoutesFileYieldsEveryRoute is the recall half.
//
// The EXACT set is asserted, not a membership subset: a subset check passes for
// a rule that also mints `/legacy`, `/indented`, `/trace` and `/health`, which
// is the whole risk of turning `^` into a line anchor.
func TestIssue6927_Play_RoutesFileYieldsEveryRoute(t *testing.T) {
	res := detectPG6927(t, playOnlyRules(t), "conf/routes", "scala", play6927Routes)

	want := []string{"/", "/assets/*file", "/users", "/users/:id"}
	got := namesPG6927(res, "Route")
	if !slices.Equal(got, want) {
		t.Errorf("Route entities = %v, want exactly %v", got, want)
	}

	// Before the fix, start-of-text meant only a route on the file's very first
	// line could match — and this file's first line is a comment, so the count
	// was ZERO. Naming the line numbers makes the failure legible when a later
	// change re-anchors the rule.
	byLine := map[string]int{}
	for _, e := range res.Entities {
		if e.Kind == "Route" {
			byLine[e.Name] = e.StartLine
		}
	}
	for name, line := range map[string]int{"/": 5, "/users": 6, "/users/:id": 8, "/assets/*file": 17} {
		if byLine[name] != line {
			t.Errorf("Route %q at line %d, want %d — the anchor is matching somewhere unexpected",
				name, byLine[name], line)
		}
	}
	// The last route is 17 lines into the file. A rule that still needs
	// start-of-text cannot reach it at all.
	if byLine["/assets/*file"] <= 1 {
		t.Error("no route was found past line 1, which is the #6927 defect itself")
	}
}

// TestIssue6927_Play_RoutesFileYieldsEveryRoutesToEdge grades the SECOND
// pattern. It is a separate regexp.Compile call (detector.go:177 rather than
// :156), so `(?m)` on the source_pattern alone leaves it broken, and no
// entity-level assertion can see that.
//
// This edge is also the reason the rule survives at all: the richer Play route
// entity in internal/custom/scala/frameworks.go carries the controller as a
// PROPERTY and emits no ROUTES_TO edge, so deleting this rule as a duplicate
// producer would lose the edge.
func TestIssue6927_Play_RoutesFileYieldsEveryRoutesToEdge(t *testing.T) {
	res := detectPG6927(t, playOnlyRules(t), "conf/routes", "scala", play6927Routes)

	want := []string{
		"Route:/ -> Operation:HomeController.index",
		"Route:/assets/*file -> Operation:Assets.versioned",
		"Route:/users -> Operation:UserController.create",
		"Route:/users -> Operation:UserController.list",
		"Route:/users/:id -> Operation:UserController.delete",
		"Route:/users/:id -> Operation:UserController.show",
	}
	got := edgesPG6927(res, "ROUTES_TO")
	if !slices.Equal(got, want) {
		t.Errorf("ROUTES_TO edges =\n  %v\nwant exactly\n  %v", got, want)
	}
}

// TestIssue6927_Play_ControllerRuleStillMatches is the control this arm is
// EXPECTED TO PASS, in both worlds. #6945's review found a test that could not
// tell a rename from a break, and the only thing that caught it was running a
// control expected to pass.
//
// It exercises a Play rule that #6927 never touched, on a .scala file, so a
// forbidden-row result elsewhere in this file cannot be an artefact of the
// harness producing nothing at all.
func TestIssue6927_Play_ControllerRuleStillMatches(t *testing.T) {
	const src = `package controllers

import javax.inject._
import play.api.mvc._

@Singleton
class HomeController @Inject() (cc: ControllerComponents) extends AbstractController(cc) {
  def index = Action { implicit request => Ok("hi") }
}
`
	res := detectPG6927(t, playOnlyRules(t), "app/controllers/HomeController.scala", "scala", src)
	if !slices.Contains(namesPG6927(res, "Controller"), "HomeController") {
		t.Fatal("the untouched Controller rule stopped matching a plain Play controller — " +
			"this control exists to prove the harness can produce entities at all, so every " +
			"forbidden-row assertion in this file is now vacuous")
	}
	if got := namesPG6927(res, "Route"); len(got) != 0 {
		t.Errorf("a .scala controller minted Routes %v — the routes rule is reaching Scala source", got)
	}
}

// ---------------------------------------------------------------------------
// Over-firing — the anchor axis
// ---------------------------------------------------------------------------

// TestIssue6927_Play_RouteRuleForbiddenShapes grades the over-fire direction.
//
// Each case is a line the routes format really contains and the rule must
// really decline, and each names the mutant that mints it. All four are
// demonstrably fireable: apply the named mutant to the shipped pattern and the
// corresponding Route appears. They are separated by axis on purpose — #6943
// found that a fixture pinning the anchor axis pins nothing about the name
// axis, and vice versa.
func TestIssue6927_Play_RouteRuleForbiddenShapes(t *testing.T) {
	res := detectPG6927(t, playOnlyRules(t), "conf/routes", "scala", play6927Routes)
	routes := namesPG6927(res, "Route")

	cases := []struct {
		path, why string
	}{
		{
			"/legacy",
			"a COMMENTED-OUT route (`# GET /legacy …`). Play routes files comment with " +
				"`#` and a commented-out route is the most ordinary line in one. Minted by " +
				"deleting `^` altogether, which lets the verb match mid-line.",
		},
		{
			"/indented",
			"an INDENTED verb line. Play's routes parser requires the verb in column 0 and " +
				"rejects this outright. Minted by weakening `^` to `(?m)^[ \\t]*`.",
		},
		{
			"/trace",
			"a verb OUTSIDE the seven-member alternation (`TRACE`). Minted by adding TRACE " +
				"to the alternation — a same-shape addition that a growth ceiling would see " +
				"only as one more entity and a membership assertion would not see at all.",
		},
		{
			"/health",
			"a route whose target is not a controller (`services.HealthCheck.ping`). Minted " +
				"by relaxing the literal `controllers\\.` to `\\w+\\.`, which would also point " +
				"the ROUTES_TO edge at a target this rule set never emits.",
		},
	}
	for _, tc := range cases {
		if slices.Contains(routes, tc.path) {
			t.Errorf("Route %q was minted — %s", tc.path, tc.why)
		}
		for _, e := range edgesPG6927(res, "ROUTES_TO") {
			if strings.Contains(e, "Route:"+tc.path+" ") {
				t.Errorf("ROUTES_TO edge %q was minted — %s", e, tc.why)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Over-firing — the name/content axis
// ---------------------------------------------------------------------------

// TestIssue6927_Play_VerbAlternationIsExactlyTheSevenHTTPVerbs asserts an EXACT
// SET, not membership.
//
// The verb alternation and the literal `controllers.` are where this rule's
// specificity lives — they are the entire reason it takes no
// requires_framework gate. #6945 and #6949 both found that a membership
// assertion and a growth ceiling are each blind to a SAME-COUNT substitution
// (`debug` -> `when` survived two suites at exit 0). An exact set is not.
//
// Both compile sites are checked, because they are separate rules that happen
// to share a pattern and nothing forces them to stay in step.
func TestIssue6927_Play_VerbAlternationIsExactlyTheSevenHTTPVerbs(t *testing.T) {
	want := []string{"DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT"}
	alt := regexp.MustCompile(`\(\?:([A-Z|]+)\)`)

	for _, tc := range []struct {
		what, pattern string
	}{
		{"source_pattern", playRouteSourcePattern(t).Pattern},
		{"relationship_rule", playRouteRelationshipRule(t).Pattern},
	} {
		m := alt.FindStringSubmatch(tc.pattern)
		if m == nil {
			t.Errorf("%s: no verb alternation found in %q — the rule's specificity has moved "+
				"somewhere this assertion cannot see", tc.what, tc.pattern)
			continue
		}
		got := strings.Split(m[1], "|")
		sort.Strings(got)
		if !slices.Equal(got, want) {
			t.Errorf("%s verb alternation = %v, want exactly %v", tc.what, got, want)
		}
		if !strings.Contains(tc.pattern, `\s+controllers\.`) {
			t.Errorf("%s no longer requires a literal `controllers.` target: %q", tc.what, tc.pattern)
		}
	}
}

// TestIssue6927_Play_BothRouteRulesAreMultiline pins the fix itself at both
// compile sites. detector.go compiles source_patterns at :156 and
// relationship_rules at :177 with two separate regexp.Compile calls, so a
// pattern can be fixed in one and left broken in the other.
func TestIssue6927_Play_BothRouteRulesAreMultiline(t *testing.T) {
	for _, tc := range []struct {
		what, pattern string
	}{
		{"source_pattern", playRouteSourcePattern(t).Pattern},
		{"relationship_rule", playRouteRelationshipRule(t).Pattern},
	} {
		if !strings.HasPrefix(tc.pattern, "(?m)^") {
			t.Errorf("%s = %q, want a (?m)-flagged line anchor: without (?m) `^` is start of "+
				"TEXT and the rule finds a route only in a routes file that opens with one "+
				"(4 of 33 measured corpus files)", tc.what, tc.pattern)
		}
		if strings.Contains(tc.pattern, "$") {
			t.Errorf("%s grew a `$`: %q. (?m) changes `$` too, and that is what made the "+
				"docker arm (#6949) a rewrite rather than a flag flip — a new `$` here needs "+
				"its own measurement", tc.what, tc.pattern)
		}
	}
}

// TestIssue6927_Play_RouteRuleIsNotFrameworkGated records, as an assertion, the
// decision NOT to add requires_framework — so a later arm adding one has to
// read why it was left off rather than discover the measurement is gone.
//
// The measurement: the pattern matches 0 times in the 1052 real .scala/.sc
// files of the corpus both before and after the widening. A gate would be
// machinery no input grades, which is the shape #6945 called out.
func TestIssue6927_Play_RouteRuleIsNotFrameworkGated(t *testing.T) {
	if playRouteSourcePattern(t).RequiresFramework {
		t.Error("the Route rule grew requires_framework. That is not wrong in principle, but " +
			"play_framework.yaml declares no frameworks.detection.import_markers, so " +
			"detector.go:161 logs it and frameworkPresent returns false for EVERY file: the " +
			"rule would stop firing entirely rather than fire more narrowly")
	}
	if markers := playOnlyRules(t)["scala"][0].Frameworks.Detection.ImportMarkers; len(markers) != 0 {
		t.Errorf("play_framework.yaml grew import_markers %v with no gated pattern to consume "+
			"them — markers nothing grades are the #6945 shape", markers)
	}
}

// ---------------------------------------------------------------------------
// Reachability — the finding this arm could not fix
// ---------------------------------------------------------------------------

// TestIssue6927_Play_ConfRoutesIsNotClassified pins a DEFECT, deliberately, the
// way #6949's TestIssue6927_Compose_EnvironmentFirstServiceIsMissed does.
//
// `conf/routes` carries no extension and no entry in classifier.go's
// exactBasenameLanguageMap or basenameLanguageMap, so classifyInner returns
// Skip{unsupported_extension} and the file never reaches the extraction
// pipeline at all. Every assertion above therefore describes what the rule
// WOULD do, driven through Detect directly — it is not reached in production
// today. Same shape as #6946's `.github` finding for the GitHub Actions rules.
//
// When this is fixed the test goes red, which is the point: whoever fixes it
// must read the two consequences recorded in the rule comment — that
// internal/custom/scala/frameworks.go's rePlayRoute becomes a second producer
// of the same route, and that the ROUTES_TO edge is what this rule uniquely
// adds.
func TestIssue6927_Play_ConfRoutesIsNotClassified(t *testing.T) {
	c := classifier.New(nil)
	for _, p := range []string{"conf/routes", "modules/admin/conf/routes"} {
		got := c.Classify(context.Background(), p)
		if !got.Skip {
			t.Errorf("%s now classifies as %q — the Play routes rules have become REACHABLE. "+
				"Read the comment on the Route rule in "+
				"internal/engine/rules/scala/frameworks/play_framework.yaml before deleting "+
				"this test: a second producer of the same route entity is waiting behind it",
				p, got.Language)
		}
	}
	// Positive control: the pipeline does classify Play's Scala sources, so the
	// skip above is a fact about this filename and not about the classifier
	// being broken or the paths being malformed.
	if got := c.Classify(context.Background(), "app/controllers/HomeController.scala"); got.Skip ||
		got.Language != "scala" {
		t.Fatalf("a Play controller classified as %+v — the control failed, so the skip "+
			"assertions above measure nothing", got)
	}
}

// ---------------------------------------------------------------------------
// GraphQL — the pattern that was removed rather than widened
// ---------------------------------------------------------------------------

// TestIssue6927_GraphQL_NoOperationSourcePattern fails if the removed rule
// returns to graphql_schema.yaml in any form.
//
// Selected structurally — by the KIND a pattern emits, not by its text — so a
// re-add under a rewritten regex still fails.
func TestIssue6927_GraphQL_NoOperationSourcePattern(t *testing.T) {
	fr := graphqlOnlyRules(t)["graphql"][0]
	for _, sp := range fr.SourcePatterns {
		if sp.EntityType == "Operation" {
			t.Errorf("graphql_schema.yaml emits Operation again via %q. "+
				"internal/extractors/graphql already emits every field as SCOPE.Component "+
				"`Owner.field`; a bare-named duplicate repoints the http_endpoint synthesis's "+
				"SERVES edge off the owner-qualified field (measured on "+
				"internal/quality/golden/graphql-schema-mini). See the comment where the rule "+
				"was removed", sp.Pattern)
		}
	}
}

// TestIssue6927_GraphQL_SchemaMintsNoBareFieldEntity is the engine-level half
// of the same fact, driven over the golden fixture's schema shape.
//
// The `wantKinds` control is what stops this from being a vacuous
// absence-assertion: the surviving rules must still fire on this input, so
// "nothing named `node`" cannot be satisfied by the rule set producing nothing.
func TestIssue6927_GraphQL_SchemaMintsNoBareFieldEntity(t *testing.T) {
	const schema = `# A schema whose Query fields take arguments.

interface Node {
  id: ID!
}

type User implements Node {
  id: ID!
  email: String!
}

type Query {
  node(id: ID!): Node
  user(id: ID!): User
  posts: [Post!]!
}
`
	res := detectPG6927(t, graphqlOnlyRules(t), "schema.graphql", "graphql", schema)

	// Control expected to PASS: the rules this arm did not touch still fire.
	if got := namesPG6927(res, "Model"); !slices.Contains(got, "Query") || !slices.Contains(got, "User") {
		t.Fatalf("Model entities = %v, want at least Query and User — the untouched rules "+
			"stopped firing, so the absence assertions below measure nothing", got)
	}
	if got := namesPG6927(res, "Interface"); !slices.Contains(got, "Node") {
		t.Fatalf("Interface entities = %v, want Node — control failed", got)
	}

	for _, e := range res.Entities {
		if e.Name == "node" || e.Name == "user" {
			t.Errorf("bare field entity %s|%s minted from a schema. The owner-qualified "+
				"`Query.node` comes from internal/extractors/graphql; a bare `node` outranks "+
				"it when the resolver binds the http_endpoint synthesis's `handler_ref`",
				e.Kind, e.Name)
		}
	}
}

// TestIssue6927_LedgerOfAnchoredPatternsWithoutMultiline is the standing sweep
// #6927 was opened from, kept as a test so the remainder is a fact rather than
// a paragraph.
//
// It decodes every compiled rule set — the same population detector.go compiles
// — and lists every pattern carrying a real `^` or `$` anchor without `(?m)`.
// A bare `^` inside a character class (`[^)]`) and an escaped `\$` are NOT
// anchors, so they are stepped over; a scan that counted them reported 134
// offenders where there are 2.
//
// The two survivors are named individually, with what is known about each. This
// is an allow-list, not a ceiling: a THIRD one fails, and so does fixing one of
// these without deleting its entry.
func TestIssue6927_LedgerOfAnchoredPatternsWithoutMultiline(t *testing.T) {
	// Both entries predate this issue's enumerated 18 and are recorded on it as
	// unmeasured:
	//
	//   .NET MAUI    `(?::|$)` — #6927 flagged this one itself as "probably a
	//                recall gain, unmeasured". It is `$`-only: under (?m) it
	//                would start matching a ViewModel class declaration that
	//                ends its line, which needs its own measurement.
	//   Ansible Core `^-\s+name:` — a third anchored pattern in a file the
	//                issue tabulated as carrying two. ansible_core.yaml also
	//                carries an UN-anchored `-\s+name:` -> Task rule, so the
	//                plays this one misses are already partly in the graph
	//                under a different kind, which is why #6949 left it.
	want := map[string]string{
		"csharp/.NET MAUI/source_pattern":     `(?:public|internal)\s+(?:partial\s+)?class\s+(\w+ViewModel)\s*(?::|$)`,
		"ansible/Ansible Core/source_pattern": `^-\s+name:\s+["']?([^"'\n]+)`,
	}

	all, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	got := map[string]string{}
	seen := map[string]bool{}
	sets, pats := 0, 0
	for lang, frs := range all {
		for _, fr := range frs {
			key := lang + "/" + fr.Frameworks.Name
			if seen[key] {
				continue
			}
			seen[key] = true
			sets++
			check := func(where, p string) {
				pats++
				if caret, dollar := hasBareRegexAnchor6927(p); !caret && !dollar {
					return
				}
				if strings.Contains(p, "(?m)") {
					return
				}
				got[key+"/"+where] = p
			}
			for _, sp := range fr.SourcePatterns {
				check("source_pattern", sp.Pattern)
			}
			for _, rr := range fr.RelationshipRules {
				check("relationship_rule", rr.Pattern)
			}
		}
	}

	// Floors, so a loader that silently returns nothing reads as a failure
	// rather than as a clean ledger (#6949's vacuous-walk shape).
	if sets < 250 || pats < 350 {
		t.Fatalf("swept %d rule sets / %d patterns — too few to be the real population; "+
			"an empty ledger below would be vacuous", sets, pats)
	}

	for k, p := range want {
		if got[k] != p {
			t.Errorf("expected residual offender %s to still read %q, got %q — if it was "+
				"fixed, delete its entry here", k, p, got[k])
		}
	}
	for k, p := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("NEW `^`/`$`-anchored rule pattern compiled without (?m): %s = %q. "+
				"detector.go compiles with plain regexp.Compile, so `^` is start of TEXT and "+
				"`$` end of TEXT — see #6927 before adding one", k, p)
		}
	}
}

// hasBareRegexAnchor6927 reports whether p carries a `^` or `$` acting as an
// anchor, stepping over escapes and character classes. `[^)]` and `\$` are not
// anchors and a scan that treats them as such is 67x noise.
func hasBareRegexAnchor6927(p string) (caret, dollar bool) {
	inClass := false
	for i := 0; i < len(p); i++ {
		switch c := p[i]; {
		case c == '\\':
			i++
		case inClass:
			if c == ']' {
				inClass = false
			}
		case c == '[':
			inClass = true
		case c == '^':
			caret = true
		case c == '$':
			dollar = true
		}
	}
	return
}
