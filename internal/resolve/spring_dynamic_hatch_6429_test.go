package resolve

import "testing"

// #6429 — the Spring `Controller:` / `Route:` Dynamic escape hatch in
// classifyDispositionLang was wide enough to swallow the route→handler dangle
// that #6429 makes resolvable. It excused those stubs as Spring HandlerMapping
// runtime dispatch, which is why the contributor's resolver-bug rate read 0.0%
// while the edge visibly dangled.
//
// After #6429 the hatch is narrowed on two axes:
//   - `Route:<path>` is no longer swallowed when the edge is TAGGED java (its
//     only Java producer was Spring's self-referential source_handler, which
//     #6429 removed). An UNTAGGED `Route:` stub stays excused: several
//     framework YAML rule files emit `Route:<functionName>` edges carrying no
//     language key, and reclassifying those is a cross-language disposition
//     change that needs its own corpus-wide measurement.
//
// #6444 UPDATE — FastAPI is no longer one of those producers. fastapi.yaml's
// three defective rules (DECORATES / IMPORTS / INJECTED_INTO) now anchor on
// `Route:<path>` and `Operation:<function>`, both of which are real minted
// names, so they resolve and never reach this classifier at all. The
// python-fastapi-mini golden fixture gates that.
//
// The assertions below are UNCHANGED and still correct. This hatch is not a
// statement about FastAPI specifically — it describes what the classifier does
// with an untagged `Route:` stub, and flask.yaml, django.yaml, axum.yaml,
// symfony.yaml and vapor.yaml still produce them (see the per-file audit on
// #6444; actix_web.yaml does NOT — it was a false positive on the original
// list). Narrowing the arm further is the sibling sweep's job, and needs the
// corpus measurement #6429 declined to do blind.
//   - `Controller:<name>` is swallowed ONLY for a BARE method name. That is
//     exactly the shape the spring_mvc.yaml regex rules emit for a controller
//     with no class-level @RequestMapping (the AST composition pass skips
//     those, so no lexical scope is available to qualify the method). The
//     java-spring-mini golden fixture's HealthController is the live case.
func TestSpringDynamicHatch6429_Narrowed(t *testing.T) {
	idx := BuildIndex(nil)

	cases := []struct {
		name string
		stub string
		lang string
		want Disposition
	}{
		{
			name: "bare Controller stub from the YAML regex path stays Dynamic",
			stub: "Controller:health",
			lang: "java",
			want: DispositionDynamic,
		},
		{
			name: "bare Controller stub with no edge language stays Dynamic",
			stub: "Controller:health",
			lang: "",
			want: DispositionDynamic,
		},
		{
			name: "QUALIFIED Controller stub is no longer excused",
			stub: "Controller:HealthController.health",
			lang: "java",
			want: DispositionBugExtractor,
		},
		{
			name: "java-tagged Route stub is no longer excused",
			stub: "Route:/users",
			lang: "java",
			want: DispositionBugExtractor,
		},
		{
			// The example that originally justified this row was FastAPI's
			// `DECORATES Route:list_things -> Service:list_things`, confirmed
			// by execution: props [framework python, pattern_type
			// yaml_driven], no language key, so relLanguage() returns "".
			// #6444 FIXED that rule, so FastAPI no longer emits the shape —
			// but flask/django/axum/symfony/vapor still do, and dropping this
			// arm would raise the reported resolver-bug rate across the whole
			// Python corpus as a side effect of a Java Spring change.
			// Deliberately kept.
			name: "UNTAGGED Route stub stays excused (flask/django/axum et al — not this PR's to move)",
			stub: "Route:list_things",
			lang: "",
			want: DispositionDynamic,
		},
		{
			name: "UNTAGGED path-shaped Route stub stays excused",
			stub: "Route:/users",
			lang: "",
			want: DispositionDynamic,
		},
		{
			name: "non-java-tagged Route stub is untouched by the java gate",
			stub: "Route:list_things",
			lang: "python",
			want: DispositionBugExtractor,
		},
		{
			name: "non-java bare Controller stub is untouched by the java gate",
			stub: "Controller:health",
			lang: "python",
			want: DispositionBugExtractor,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := idx.classifyDispositionLang("", tc.stub, tc.lang, nil)
			if got != tc.want {
				t.Errorf("classifyDispositionLang(%q, lang=%q) = %v, want %v",
					tc.stub, tc.lang, got, tc.want)
			}
		})
	}
}
