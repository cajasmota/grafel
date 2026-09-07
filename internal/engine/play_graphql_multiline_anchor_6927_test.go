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

// TestIssue6927_Play_ConfRoutesIsClassified_6952 is the INVERSE of the tripwire
// this used to be. Until #6952 it was TestIssue6927_Play_ConfRoutesIsNot-
// Classified, pinning the defect deliberately the way #6949's
// TestIssue6927_Compose_EnvironmentFirstServiceIsMissed does: `conf/routes`
// carried no extension and no entry in classifier.go's basename tables, so
// classifyInner returned Skip{unsupported_extension} and every assertion above
// described what the rule WOULD do, driven through Detect directly.
//
// #6952 added basenameLanguageMap["routes"] and extensionLanguageMap[".routes"],
// which turned this test red — the point of writing it that way. Its own
// instruction was to read the two consequences recorded on the Route rule in
// play_framework.yaml before deleting it, so they are recorded HERE, measured,
// rather than deleted with it:
//
//  1. A SECOND PRODUCER OF THE SAME ROUTE IS NOW LIVE, and it is no longer a
//     rounding error. On lichess-org/lila's five real routes files, this rule
//     mints 833 `Route` entities against internal/custom/scala's 900
//     `SCOPE.Operation` from the same lines — ~93% overlap, not the ~0.4% that
//     held before #6953's `(?m)` fix landed (4 vs 900). The custom extractor's
//     entity carries http_method, http_path and controller; this rule's carries
//     the path alone.
//  2. THE `ROUTES_TO` EDGE IS WHAT THIS RULE UNIQUELY ADDS, and on the same
//     five files 905 of its 907 ROUTES_TO edges have an UNRESOLVED to-side —
//     dangling `Operation:controllers.X.y` bare names that never bind to the
//     controller method. So the rule's sole advantage over the custom extractor
//     is, today, almost entirely unlanded.
//
// Both figures are why the ownership merge is filed as #6959 rather than
// asserted here: this test grades reachability, not the resolution.
func TestIssue6927_Play_ConfRoutesIsClassified_6952(t *testing.T) {
	c := classifier.New(nil)
	for _, p := range []string{"conf/routes", "modules/admin/conf/routes"} {
		got := c.Classify(context.Background(), p)
		if got.Skip || got.Language != "scala" {
			t.Errorf("%s classified as {lang=%q skip=%v reason=%s}, want scala/not-skipped — "+
				"the Play routes rules are UNREACHABLE again, and every assertion above this "+
				"line describes a rule that production never runs (#6952)",
				p, got.Language, got.Skip, got.SkipReason)
		}
	}
	// Positive control, carried over unchanged: the pipeline classifies Play's
	// Scala sources, so the rows above are a fact about the routes filename and
	// not about the classifier being broken or the paths being malformed.
	if got := c.Classify(context.Background(), "app/controllers/HomeController.scala"); got.Skip ||
		got.Language != "scala" {
		t.Fatalf("a Play controller classified as %+v — the control failed, so the assertions "+
			"above measure nothing", got)
	}
	// Negative control: the routes rules must not have become reachable by the
	// classifier having stopped skipping ANYTHING.
	if got := c.Classify(context.Background(), "conf/routes.bak"); !got.Skip {
		t.Errorf("conf/routes.bak classified as %q — the rows above would pass under a "+
			"classifier that skips nothing, so this is what makes them mean something", got.Language)
	}
}

// ---------------------------------------------------------------------------
// GraphQL — the pattern that was removed rather than widened
// ---------------------------------------------------------------------------

// TestIssue6927_GraphQL_NoOperationSourcePattern fails if a source_pattern
// emitting the `Operation` KIND returns to graphql_schema.yaml.
//
// The ban is on the KIND, and it rests on the repointing — not on the removed
// rule being subsumed by the extractor. PR #6953 review corrected that premise
// and it must not creep back into this message: internal/extractors/graphql
// does NOT emit field entities for an `extend type Foo { … }` block (its
// extend branch at graphql.go:350 emits only an IMPORTS/FEDERATES stub), which
// is how every Apollo Federation subgraph declares its fields, so the removed
// rule was not a subset there. The removal still loses nothing at HEAD — the
// pattern matched 0 of 1907 real .graphql/.gql files — but "loses nothing
// today" and "is subsumed" are different claims and only the first is true.
//
// What IS true, and is the whole reason for the ban: resolve/refs.go's
// isOperationKind is `k == "SCOPE.Operation" || k == "Operation"`, so an
// entity of kind `Operation` is a first-class candidate when the resolver
// binds an operation reference by BARE name. The http_endpoint synthesis emits
// `SCOPE.Operation "QUERY /node"` carrying a bare `handler_ref: node`, and a
// bare `Operation|node` therefore outranks the owner-qualified
// `SCOPE.Component|Query.node` — measured on
// internal/quality/golden/graphql-schema-mini, where widening the rule moved
// two SERVES edges with recall and forbidden hits both unchanged.
//
// So this is not a ban on ever extracting graphql fields here. Widening the
// rule and OWNER-QUALIFYING the capture — `Owner.field`, or any kind
// isOperationKind does not accept — is open, and would reach 1429 sites across
// 160 SDL files. What is closed is re-adding a BARE-named `Operation`.
//
// Selected structurally, by the kind a pattern emits rather than by its text,
// so a re-add under a rewritten regex still fails. That selector is NOT the
// fence, though, and PR #6953 review MU1 demonstrated it: re-adding the same
// regex with `entity_type: Component` passes this test and dies on
// TestIssue6927_GraphQL_SchemaMintsNoBareFieldEntity instead. The entity-level
// test is what actually holds the line; this one names the kind and the reason.
func TestIssue6927_GraphQL_NoOperationSourcePattern(t *testing.T) {
	fr := graphqlOnlyRules(t)["graphql"][0]
	for _, sp := range fr.SourcePatterns {
		if sp.EntityType == "Operation" {
			t.Errorf("graphql_schema.yaml emits the `Operation` kind again via %q. "+
				"resolve/refs.go's isOperationKind accepts `Operation`, so a BARE-named one "+
				"outranks the owner-qualified `SCOPE.Component|Query.node` when the resolver "+
				"binds the http_endpoint synthesis's bare `handler_ref` — measured on "+
				"internal/quality/golden/graphql-schema-mini, where it repointed two SERVES "+
				"edges with recall unchanged. Emitting `Owner.field`, or any kind "+
				"isOperationKind does not accept, is NOT banned by this test", sp.Pattern)
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
				// Anchor DETECTION and flag PLACEMENT are separate questions and
				// each was a hole in the first version of this test (PR #6953
				// review MU3 and MU5). Both live in
				// anchorCompiledAsStartOfText6927, which
				// TestIssue6927_LedgerHelperDetectsAnchorsAndFlagPlacement grades
				// directly — the sweep cannot grade them, because both survivors
				// happen to carry `^` at index 0 and neither carries a `(?m)`.
				if !anchorCompiledAsStartOfText6927(p) {
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

// firstBareRegexAnchor6927 returns the byte index of the first `^` or `$` in p
// that is acting as an ANCHOR, or -1 if there is none. Escapes and character
// classes are stepped over: `[^)]` and `\$` are not anchors, and a scan that
// counts them reports 134 offenders where there are 2.
//
// Returning the INDEX rather than a bool is not cosmetic — the placement check
// below needs it, and PR #6953 review MU5 is what made that necessary.
func firstBareRegexAnchor6927(p string) int {
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
		case c == '^', c == '$':
			return i
		}
	}
	return -1
}

// multilineFlagIndex6927 returns the byte index of a `(?m)` flag group in p
// that is outside a character class, or -1.
//
// PR #6953 review MU5: the first version of the ledger asked
// `strings.Contains(p, "(?m)")`, which is PLACEMENT-BLIND. Go applies a flag
// group from its own position onward, so `^foo(?m)` is still start-of-TEXT and
// carries the exact defect #6927 is about — and it satisfied the ledger. The
// caller therefore compares this index against the FIRST anchor's, which is
// the conservative reading: in `^a(?m)b$` the `^` is unaffected by the flag and
// the pattern is an offender even though a later anchor is fine.
func multilineFlagIndex6927(p string) int {
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
		case c == '(':
			if strings.HasPrefix(p[i:], "(?m)") {
				return i
			}
		}
	}
	return -1
}

// anchorCompiledAsStartOfText6927 reports whether p carries an anchor that
// plain regexp.Compile will read as start/end of TEXT — i.e. an anchor with no
// `(?m)` in effect at its position.
func anchorCompiledAsStartOfText6927(p string) bool {
	anchor := firstBareRegexAnchor6927(p)
	if anchor < 0 {
		return false
	}
	flag := multilineFlagIndex6927(p)
	return flag < 0 || flag > anchor
}

// TestIssue6927_LedgerHelperDetectsAnchorsAndFlagPlacement grades the ledger's
// own detection, which PR #6953 review found ungraded in two places.
//
// The ledger is what #6927 is being closed against, so a helper that quietly
// stops detecting anchors turns it into a test that always passes. Both
// survivors in the allow-list happen to carry `^` at index 0, so the sweep
// alone cannot tell a real scanner from `p[0] == '^'` (review MU3), and it
// could not tell `(?m)` present from `(?m)` EFFECTIVE (review MU5).
//
// The table is enumerated over the shapes rather than hand-picked from the two
// live offenders, because hand-picking is what left both holes.
func TestIssue6927_LedgerHelperDetectsAnchorsAndFlagPlacement(t *testing.T) {
	for _, tc := range []struct {
		pattern    string
		wantAnchor int
		wantOffend bool
		why        string
	}{
		// --- anchor DETECTION, including anchors that are not at index 0.
		// Kills the `p[0] == '^'` mutant (MU3): both allow-listed survivors
		// carry `^` at 0, so the live sweep cannot distinguish them.
		{`^foo`, 0, true, "the ordinary case"},
		{`foo$`, 3, true, "a `$` anchor with no `^` at all — index 0 is not it"},
		{`(?:a|b)^x`, 7, true, "a `^` in the middle of the pattern"},
		{`\s+(\w+)$`, 8, true, "the ansible `$` shape, anchor at the end"},
		{`x(?::|$)`, 6, true, "the .NET MAUI shape — `$` inside a group, still an anchor"},
		// --- NON-anchors. Kills a scanner that just counts the bytes.
		{`[^)]*`, -1, false, "`^` inside a character class is a negation"},
		{`\$\{`, -1, false, "an ESCAPED `$` is a literal dollar"},
		{`\^x`, -1, false, "an escaped `^` is a literal caret"},
		{`["$]`, -1, false, "`$` inside a character class"},
		{`[\]^]`, -1, false, "a class whose `]` is escaped does not end early"},
		{`(\w+)`, -1, false, "no anchor at all"},
		// --- FLAG PLACEMENT. Kills the strings.Contains mutant (MU5).
		{`(?m)^foo`, 4, false, "the fix: the flag precedes the anchor"},
		{`^foo(?m)`, 0, true, "MU5 — `(?m)` AFTER the anchor leaves `^` start-of-TEXT"},
		{`^a(?m)b$`, 0, true, "a later anchor being multiline does not rescue the first"},
		{`(?m)a[^)]$`, 9, false, "a class between flag and anchor must not confuse either scan"},
		{`[(?m)]^x`, 6, true, "a `(?m)` INSIDE a character class is a literal set, not a flag"},
	} {
		if got := firstBareRegexAnchor6927(tc.pattern); got != tc.wantAnchor {
			t.Errorf("firstBareRegexAnchor6927(%q) = %d, want %d — %s",
				tc.pattern, got, tc.wantAnchor, tc.why)
		}
		if got := anchorCompiledAsStartOfText6927(tc.pattern); got != tc.wantOffend {
			t.Errorf("anchorCompiledAsStartOfText6927(%q) = %v, want %v — %s",
				tc.pattern, got, tc.wantOffend, tc.why)
		}
	}

	// Positive control on the two shapes the live ledger actually holds, so a
	// helper that passes the table above but disagrees with the sweep is caught
	// here rather than by the sweep silently going empty.
	for _, p := range []string{
		`(?:public|internal)\s+(?:partial\s+)?class\s+(\w+ViewModel)\s*(?::|$)`,
		`^-\s+name:\s+["']?([^"'\n]+)`,
	} {
		if !anchorCompiledAsStartOfText6927(p) {
			t.Errorf("the live ledger's own survivor %q is no longer detected as an offender — "+
				"TestIssue6927_LedgerOfAnchoredPatternsWithoutMultiline would report a clean "+
				"ledger for the wrong reason", p)
		}
	}
}
