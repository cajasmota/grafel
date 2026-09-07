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
	//
	// WHAT ACTUALLY PROTECTS THESE, corrected after review. It is tempting to
	// say "extensionLanguageMap is consulted before basenameLanguageMap, so
	// routes.rb is ruby before the routes entry is ever reached". That ordering
	// is INERT: swapping the two lookups is equivalent under the current tables
	// (probed on nginx.conf, luarocks.lock, .justfile, conf/routes and
	// config/routes.rb — identical output both ways, because `.conf` and
	// `.lock` are not in extensionLanguageMap and `.justfile` is in both with
	// the same value). The thing that actually holds is that
	// basenameLanguageMap is keyed on the FULL basename, exactly: `routes.rb`
	// is simply not the string `routes`. A reader tightening the rule should
	// preserve exact-full-basename keying, not the lookup order.
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

	// THE `.routes` EXTENSION HALF, graded on its own. Every row above keys on
	// the BASENAME entry; before review this direction had zero forbidden rows,
	// and adding `".route": "scala"` (singular) to extensionLanguageMap was
	// ALIVE against the whole suite and the golden fixture. Two populations were
	// measured (79 basename / 60 suffix) and only one was graded.
	extensionHalfStaysSkipped := []struct{ path, why string }{
		{"conf/admin.route", "SINGULAR. Zero instances in 139 measured files; Play's include suffix is plural, and a `.route` entry is invisible to every basename row"},
		{"conf/admin.routez", "a one-character-off suffix — the lookup is exact, not a prefix or fuzzy match"},
		{"conf/admin.routes.bak", "filepath.Ext takes the LAST segment, so this is `.bak` and must stay skipped even though `.routes` appears in the name"},
		{"conf/admin.routes~", "the editor backup marker; `.routes~` is not `.routes`"},
	}
	for _, tc := range extensionHalfStaysSkipped {
		t.Run("ext/"+tc.path, func(t *testing.T) {
			if got := c.Classify(context.Background(), tc.path); !got.Skip || got.Language != "" {
				t.Errorf("Classify(%q) = {lang=%q skip=%v}, want an unclassified skip — %s",
					tc.path, got.Language, got.Skip, tc.why)
			}
		})
	}

	// A `routes/` DIRECTORY must not pull its contents in. The two rows in
	// keepsLanguage that advertise this (express-realworld, nextjs) CANNOT
	// grade it — review's N2 proved it: they carry extensions, so the extension
	// lookup answers first and a `strings.Contains(norm, "/routes/")` widening
	// at the natural edit site never reaches them. Killing that shape needs an
	// EXTENSIONLESS file under a routes/ directory, which is what these are.
	// The corpus has six `routes` directories (symfony-demo, nextjs x2,
	// rails-actionpack, express-realworld, awesome-compose) and zero
	// extensionless files in any of them, so these rows are synthetic by
	// necessity — the widening is not production-reachable on the measured
	// corpus, but it is the cheapest wrong turn a future editor can take.
	routesDirStaysSkipped := []string{
		"routes/health",
		"app/routes/index",
		"src/routes/admin/handler",
		// NOT a row: a bare `routes/` with a trailing slash. path.Base strips it
		// and the file classifies — but the classifier is only ever handed FILE
		// paths (the walker decides directories, and does so before this), so a
		// row for it would assert a contract that does not exist. Recorded
		// rather than silently omitted: it was tried and rejected, not missed.
	}
	for _, p := range routesDirStaysSkipped {
		t.Run("dir/"+p, func(t *testing.T) {
			if got := c.Classify(context.Background(), p); !got.Skip || got.Language != "" {
				t.Errorf("Classify(%q) = {lang=%q skip=%v}, want an unclassified skip — a "+
					"segment match on `routes/` would fire here; the rule is a basename "+
					"lookup and must not read the parent directory", p, got.Language, got.Skip)
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
