// Package classifier — conf_routes_6952_test.go
//
// Issue #6952. Play Framework's router file carries NO EXTENSION, so
// detectLanguage returned "" for it and both classifyInner and
// classifyWithSizeInner answered Skip{unsupported_extension}. Four producers
// were keyed on a file that never arrived — the two rules in
// internal/engine/rules/scala/frameworks/play_framework.yaml, internal/custom/
// scala's rePlayRoute, internal/custom/java/play_routes.go, and internal/custom/
// golang's Revel arm — and every one of them had passing unit tests, because
// those tests construct the file record directly and never go through here.
//
// THERE WAS NO MISSING MECHANISM. basenameLanguageMap has routed extensionless
// well-known files since Dockerfile / Containerfile / Justfile / Caddyfile, and
// extensionLanguageMap routes an unknown-to-the-OS extension like `.routes`
// exactly as it routes `.tf` or `.hcl`. The bug was two absent table entries,
// not an absent hook, which is why Dockerfile / Makefile / Gemfile / Procfile
// need no new machinery either.
//
// WHY THE BARE BASENAME AND NOT A `conf/` PATH ANCHOR. detectLanguage CAN
// express a path rule — the Debezium `.json` block's dirAnchor does — so the
// narrower `conf/routes` rule was available and was rejected on measurement,
// not on taste. Across playframework/playframework, playframework/play-samples,
// lichess-org/lila, revel/examples and the 60-repo corpus at
// ~/Projects/archigraph-corpora, 139 routes files:
//
//	basename `routes`      79 files   (70 at conf/routes, 9 at src/main/resources/routes)
//	suffix   `*.routes`    60 files   (conf/<module>.routes sub-router includes)
//	path     `conf/routes` 70 files   — HALF the population
//
// and the over-fire the path anchor buys protection against measured ZERO:
// every one of the 139 is a genuine routes DSL, and the whole 60-repo corpus
// contains exactly ONE file named `routes` (play-scala-starter's, which is a
// Play routes file). The generic-name risk is real in the abstract and was not
// observed once; the forbidden rows below are what hold it, together with the
// fact that the consumer content-sniffs (detectScalaFramework only answers
// "play" for a column-0 HTTP verb line).
//
// WHY "scala". All three custom producers hard-gate on their own base language
// (`file.Language != "scala"`, `ctx.Language != "java"`, `language != "go"`),
// so no single token reaches more than one of them and a routes-specific token
// reaches NONE. "scala" is the only one of the three whose producer already
// content-sniffs what it is handed. Reaching the Java and Revel producers needs
// those gates widened and is filed separately.
package classifier

import (
	"context"
	"testing"
)

// TestConfRoutesIsClassified6952 grades the REACHABILITY direction: the paths
// that carry a Play/Revel routes DSL must arrive at a language with a producer.
//
// The `.scala` row is a POSITIVE CONTROL, carried from the issue: without it a
// blanket regression in the classifier (an early skip, a broken normalisePath)
// would look identical to the bug this file pins, and the fix would read as
// necessary when it was not.
func TestConfRoutesIsClassified6952(t *testing.T) {
	c := New(nil)
	cases := []struct {
		path string
		why  string
	}{
		{"conf/routes", "the canonical Play/Revel location; 70 of 139 measured files"},
		{"modules/admin/conf/routes", "a Play sub-project's own conf/ — path depth must not matter"},
		{"src/main/resources/routes", "Play's maven-layout / sbt-plugin location; 9 measured files, and every one of them is invisible to a `conf/` path anchor"},
		{"routes", "repo root, e.g. a sub-project indexed with its own directory as root"},
		{"conf/admin.routes", "`-> /admin admin.Routes` sub-router include; 60 measured files"},
		{"a.routes", "the include form at the tree root (playframework's routes-compiler-incremental-compilation fixtures)"},
		{"documentation/manual/working/scalaGuide/main/http/code/scalaguide.http.routing.routes", "a multi-dot include name — filepath.Ext takes only the LAST segment, so this must route on `.routes` and not on `.http`"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := c.Classify(context.Background(), tc.path)
			if got.Skip {
				t.Fatalf("Classify(%q) = Skip{%s} — %s", tc.path, got.SkipReason, tc.why)
			}
			if got.Language != "scala" {
				t.Fatalf("Classify(%q).Language = %q, want %q — internal/custom/scala's "+
					"framework extractor gates on `file.Language != \"scala\"`, so any "+
					"other token leaves the Play producer as unreachable as an outright "+
					"skip does", tc.path, got.Language, "scala")
			}
			// ClassifyWithSize is a SECOND copy of the decision (classifier.go
			// :190) and is the one the extraction pipeline actually calls
			// (cmd/grafel/index.go:3973). A fix applied to only one of them
			// would be green on Classify and dead in production.
			ws := c.ClassifyWithSize(context.Background(), tc.path, 4096)
			if ws.Skip || ws.Language != "scala" {
				t.Fatalf("ClassifyWithSize(%q, 4096) = {lang=%q skip=%v reason=%s}, want "+
					"{scala,false} — the two entry points disagree", tc.path, ws.Language, ws.Skip, ws.SkipReason)
			}
		})
	}

	// Positive control (#6952 as filed): a co-located ordinary Scala file was
	// ALWAYS classified. The skip was a fact about the filename, not about the
	// path or the repo layout.
	if got := c.Classify(context.Background(), "app/controllers/HomeController.scala"); got.Skip || got.Language != "scala" {
		t.Fatalf("premise: Classify(HomeController.scala) = {lang=%q skip=%v} — the control "+
			"is broken, so nothing above grades the routes rules", got.Language, got.Skip)
	}
}

// TestRoutesNameDoesNotOverFire6952 grades the OTHER direction, which is where
// the whole risk of this change lives: `routes` is a plausible basename for
// something that is not a Play router.
//
// Every row is a name that EXISTS in the measured corpus (the file it names is
// noted) except where marked synthetic, and every row would flip if the rule
// were spelled one notch wider — a `strings.Contains(base, "routes")`, a
// `routes.` prefix arm, or a case-insensitive lookup.
func TestRoutesNameDoesNotOverFire6952(t *testing.T) {
	c := New(nil)

	// Rows that must keep the language the EXTENSION router already gave them.
	// These are the ones a `Contains`-style rule would steal.
	keepsLanguage := []struct{ path, lang, corpus string }{
		{"config/routes.rb", "ruby", "rails-realworld/config/routes.rb"},
		{"config/routes.yaml", "yaml", "symfony-demo/config/routes.yaml"},
		{"src/routes.rs", "rust", "actix-examples/databases/mysql/src/routes.rs"},
		{"app/Http/routes.php", "php", "laravel-quickstart/app/Http/routes.php"},
		{"src/app/routes/routes.ts", "typescript", "express-realworld — note the DIRECTORY named routes: a segment match would fire on the path, a basename match cannot"},
		{"Sources/App/routes.swift", "swift", "vapor-api-template/Sources/App/routes.swift"},
		{"_examples/rest/routes.md", "markdown", "chi/_examples/rest/routes.md"},
		{"packages/next/src/export/routes/index.ts", "typescript", "nextjs — a file INSIDE a routes/ directory"},
	}
	for _, tc := range keepsLanguage {
		t.Run("keeps/"+tc.path, func(t *testing.T) {
			got := c.Classify(context.Background(), tc.path)
			if got.Language != tc.lang {
				t.Errorf("Classify(%q).Language = %q, want %q — %s", tc.path, got.Language, tc.lang, tc.corpus)
			}
		})
	}

	// Rows that must stay UNCLASSIFIED. `routes.<unknown>` is the widening
	// internal/custom/java's isPlayRoutesFile already makes (`routes.` prefix);
	// it was measured on exactly one file in the whole corpus — playframework's
	// deliberately-broken `routes.bad` — and would admit every editor and deploy
	// backup in exchange. scala-play-mini's conf/routes.bak forbidden_entities
	// row is the end-to-end half of this.
	staysSkipped := []struct{ path, why string }{
		{"conf/routes.bak", "an editor/deploy backup: valid routes DSL, no longer served"},
		{"conf/routes.orig", "a merge artefact, same shape"},
		{"routes.bad", "playframework/dev-mode/.../dev-mode-compile-and-config-error-source/routes.bad — the only `routes.*` in 139 measured files, and it is deliberately broken"},
		{"conf/routes.txt", "synthetic: the general `routes.<unknown ext>` case"},
		{"conf/Routes", "case-SENSITIVE, matching every other basenameLanguageMap entry (bare `dockerfile` does not route either)"},
		{"conf/ROUTES", "same"},
		{"conf/route", "singular — a `routes` prefix rule would not catch it, a fuzzy one might"},
		{"conf/myroutes", "a suffix-of-basename match would fire here; the lookup is exact"},
		{"conf/routesx", "a prefix-of-basename match would fire here"},
	}
	for _, tc := range staysSkipped {
		t.Run("skips/"+tc.path, func(t *testing.T) {
			got := c.Classify(context.Background(), tc.path)
			if !got.Skip || got.Language != "" {
				t.Errorf("Classify(%q) = {lang=%q skip=%v}, want an unclassified skip — %s",
					tc.path, got.Language, got.Skip, tc.why)
			}
		})
	}

	// The universal path skips still win. A vendored Play sample must not be
	// re-admitted through the new entries: they run AFTER universalPathSkip.
	for _, p := range []string{"vendor/play-sample/conf/routes", "node_modules/x/conf/admin.routes", "testdata/conf/routes"} {
		if got := c.Classify(context.Background(), p); !got.Skip {
			t.Errorf("Classify(%q) = {lang=%q skip=false} — the dependency-directory skip must "+
				"still win over the routes rules", p, got.Language)
		}
	}
}

// TestRoutesExtensionIsRoutedByTheRouter6952 pins the CONSEQUENCE of choosing
// extensionLanguageMap for the `*.routes` half rather than a bespoke suffix
// branch: `.routes` becomes an extension the ROUTER claims, so it stops being
// reported as unsupported and LanguageForExtension answers for it.
//
// This is asserted rather than left implicit because it is the observable
// difference between the two spellings, and because a reader tightening the
// rule into a private suffix check would silently reopen the unsupported-
// extension report row without any other test noticing.
func TestRoutesExtensionIsRoutedByTheRouter6952(t *testing.T) {
	if got := LanguageForExtension(".routes"); got != "scala" {
		t.Errorf("LanguageForExtension(%q) = %q, want %q", ".routes", got, "scala")
	}
	if !SupportedExtension(".ROUTES") {
		t.Error("SupportedExtension(\".ROUTES\") = false — extension routing is case-INsensitive " +
			"(the lookup lowercases), unlike the basename entry; a change that made the two agree " +
			"in the wrong direction would break `conf/module.ROUTES`")
	}
	// The basename half is case-SENSITIVE and stays that way; the two halves of
	// this fix genuinely differ, and pinning both stops a reader "harmonising"
	// them without deciding which way.
	if got := detectLanguage("conf/Routes"); got != "" {
		t.Errorf("detectLanguage(%q) = %q, want \"\" — the basename entry is case-sensitive", "conf/Routes", got)
	}
}
