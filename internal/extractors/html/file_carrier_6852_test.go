package html_test

// file_carrier_6852_test.go — #6852, html arm. buildAssetImportRel
// (extractor.go) stamps `FromID: file.Path` on EVERY html asset IMPORTS edge,
// while nothing the html extractor emits is named after the file:
// internal/resolve/refs.go has no path→entity index, so a path-valued FromID
// resolves if and only if some emitted record carries that exact string as its
// Name. Nothing did, at any path depth, so the raw path reached the graph as
// the edge's FROM end. Same defect #6815 fixed in erlang, nim and groovy and
// #6852 fixed in bicep (#6864) and terraform (#6871); same fix, the CONDITIONAL
// carrier in internal/extractor/file_carrier.go.
//
// ONE ANCHOR, SIX SOURCE SITES, FIVE OF THEM REACHABLE — established from
// source and then MEASURED, not assumed. Every path-anchored edge in this
// package comes from ONE constructor, buildAssetImportRel, whose FromID
// argument is `file.Path` at all six of its call sites: <link> and <img> in
// visitElement, <script> in visitScriptElement, and <img>/<script>/<link> in
// visitSelfClosingTag. They are not six anchors — they are six producers of ONE
// anchor string, so the resolution requirement is one string and one carrier
// serves them all. The ledger row (#6847) says "script/link refs"; <img>
// anchors identically and is covered here too.
//
// The SELF-CLOSING <script> site is STRUCTURALLY DEAD, not merely untested, and
// the argument is from the grammar rather than from a fixture that happened to
// come back empty. tree-sitter-html v0.23.2's grammar.js defines
// `self_closing_tag` over `$._start_tag_name` (grammar.js:95-97), and
// src/scanner.c's scan_start_tag_name emits START_TAG_NAME only in its
// `default:` branch — a tag whose tag_for_name is SCRIPT gets
// SCRIPT_START_TAG_NAME instead (and STYLE gets STYLE_START_TAG_NAME). So no
// self_closing_tag node can ever carry tag_name == "script", case-insensitively,
// and visitSelfClosingTag's "script" case is unreachable for every input.
//
// TestHTML_SelfClosingScriptSiteIsDead_6852 pins that rather than arguing it: a
// document whose <script> is written self-closing yields no script_include
// record at all, while the <img> beside it and the carrier are both emitted. If
// a future tree-sitter bump ever routes that spelling to self_closing_tag, the
// pin goes red — which is exactly when a reader needs to know that a sixth
// producer has become live and ungraded.
//
// MULTIPLICITY IS THE AXIS THIS ARM ADDS. bicep had one anchored edge shape and
// terraform two; a real html page routinely references many scripts and
// stylesheets, so this is the arm where "one carrier per FILE, not per EDGE"
// is exercised under genuine multiplicity —
// TestHTML_OneCarrierPerFileNotPerImport_6852 drives seven anchored edges from
// all five reachable sites and requires exactly one carrier.
//
// PATH DEPTH IS NOT THE AXIS IT WAS FOR TERRAFORM, and that difference is
// measured rather than assumed. hcl already emitted a file-level SCOPE.Component
// named basename(path), so its root case resolved by accident and only its
// nested case could fail. html emits NO file-scoped record of any kind — every
// record it emits is named after the REFERENCE (the src/href value), never
// after the file — so both depths dangled before this change and both are
// pinned below.
//
// GRADED IN BOTH DIRECTIONS. A recall-shaped assertion ("the carrier exists")
// licenses an UNCONDITIONAL carrier, which would mint one bare orphan node per
// .html file across a whole repo — invisible to every such assertion. Most
// pages reference nothing local at all (an inline-styled page, a page whose
// only <script> is a CDN URL), so the forbidden-row controls below are the half
// of the grade that forbids it.

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// extractHTML6852 runs the REGISTERED html extractor, so the test drives the
// same entry point production does rather than an internal helper.
func extractHTML6852(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("html")
	if !ok {
		t.Fatal("html extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "html",
	})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	return recs
}

// htmlNamedExactly6852 returns every record whose Name or QualifiedName is
// path. This is the resolution question refs.go actually asks — it has no
// path→entity index — so it is the forbidden-row form: a carrier smuggled in
// under a different Kind or Subtype is caught too.
func htmlNamedExactly6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Name == path || r.QualifiedName == path {
			out = append(out, r)
		}
	}
	return out
}

// htmlPathAnchored6852 returns every relationship whose FromID is exactly path
// — the shape whose FROM end has nothing to resolve onto.
func htmlPathAnchored6852(recs []types.EntityRecord, path string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.FromID == path {
				out = append(out, r)
			}
		}
	}
	return out
}

// resolveHTML6852 extracts src at path, stamps ids the way graph assembly does,
// runs the production resolver pipeline, and returns the records plus the
// id→record index. The assertion is on the EMITTED ARTEFACT after resolution —
// the edge's FROM end — not on a helper's return value or a counter the code
// keeps about itself.
func resolveHTML6852(t *testing.T, src, path string) ([]types.EntityRecord, map[string]*types.EntityRecord) {
	t.Helper()
	recs := extractHTML6852(t, src, path)
	if len(recs) == 0 {
		t.Fatalf("extract %s: no records", path)
	}
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6852", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))
	byID := make(map[string]*types.EntityRecord, len(recs))
	for i := range recs {
		byID[recs[i].ID] = &recs[i]
	}
	return recs, byID
}

// assertHTMLImportsResolve6852 fails for every IMPORTS edge whose FROM end
// names no record, and fails vacuously-empty fixtures.
func assertHTMLImportsResolve6852(t *testing.T, recs []types.EntityRecord, byID map[string]*types.EntityRecord) {
	t.Helper()
	seen := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			seen++
			if _, ok := byID[r.FromID]; !ok {
				t.Errorf("IMPORTS owned by %q: FROM end %q resolves to no record "+
					"(refs.go has no path→entity index; a path-valued FromID resolves "+
					"iff some record carries that exact string as its Name — emit a file "+
					"carrier, internal/extractor/file_carrier.go)", recs[i].Name, r.FromID)
			}
		}
	}
	if seen == 0 {
		t.Fatal("fixture produced no IMPORTS edges — this measurement is vacuous")
	}
}

// The five reachable anchoring sites, one per fixture, so a fix wired to one tag or one
// tag SYNTAX cannot pass by riding on another. Each fixture contains exactly
// ONE local asset reference and no other, which the premise assertions below
// enforce rather than assume.
const (
	scriptElementSrc6852 = `<html><head><script src="/static/app.js"></script></head><body></body></html>`
	linkElementSrc6852   = `<html><head><link rel="stylesheet" href="/static/site.css"></head><body></body></html>`
	imgElementSrc6852    = `<html><body><img src="/static/logo.png"></body></html>`
	linkClosingSrc6852   = `<html><head><link rel="stylesheet" href="/static/site.css"/></head><body></body></html>`
	imgClosingSrc6852    = `<html><body><img src="/static/logo.png"/></body></html>`
)

// anchoringFixtures6852 is the fixture set the resolution and shape tests
// share. Only fixtures that actually anchor belong here; a fixture that stopped
// anchoring would fail the premise assertion in every test that uses it rather
// than quietly weakening one.
func anchoringFixtures6852() map[string]string {
	return map[string]string{
		"script_element":    scriptElementSrc6852,
		"link_element":      linkElementSrc6852,
		"img_element":       imgElementSrc6852,
		"link_self_closing": linkClosingSrc6852,
		"img_self_closing":  imgClosingSrc6852,
	}
}

// TestHTML_ImportsFromEndResolves_6852 is the fix's behavioural test, driven
// over the CROSS PRODUCT of the anchoring site and the path depth. Axes VARIED:
// the referencing tag (script/link/img), its syntax (start-tag pair vs
// self-closing, for the two tags the grammar routes both ways) and the path
// depth (nested / root). HELD CONSTANT: exactly one
// local reference per fixture, the html token, the same resolver pipeline.
//
// BOTH DEPTHS FAIL BEFORE THE FIX, unlike terraform's arm: html emits no
// file-scoped record at all, so the root case never resolved by accident.
func TestHTML_ImportsFromEndResolves_6852(t *testing.T) {
	for site, src := range anchoringFixtures6852() {
		for _, path := range []string{"src/pages/index.html", "index.html"} {
			t.Run(site+"/"+path, func(t *testing.T) {
				// The premise is read BEFORE resolution: ReferencesEmbedded
				// rewrites a resolved FromID onto the carrier's id, so counting
				// path-anchored edges afterwards would report 0 for a working
				// fix and 1 for a broken one — backwards.
				if n := len(htmlPathAnchored6852(extractHTML6852(t, src, path), path)); n != 1 {
					t.Fatalf("premise: want exactly 1 path-anchored IMPORTS edge as EMITTED, got %d", n)
				}
				recs, byID := resolveHTML6852(t, src, path)
				assertHTMLImportsResolve6852(t, recs, byID)
			})
		}
	}
}

// TestHTML_NoCarrierWithoutAPathAnchoredImport_6852 is the OVER-FIRING control,
// and it is the half of the grade a "the edge now resolves" test cannot supply.
// Axis VARIED: the presence of any local asset reference (absent). HELD
// CONSTANT: a full set of other declarations — a form with fields, a custom
// element, a mustache expression and a Jinja2 directive — so the file still
// extracts a full record set. Only the path-anchored edge is gone.
func TestHTML_NoCarrierWithoutAPathAnchoredImport_6852(t *testing.T) {
	const src = `<html>
<body>
{% block content %}
<form action="/submit"><input name="q"><button>go</button></form>
<my-widget data-x="1"></my-widget>
<p>{{ user.display_name }}</p>
{% endblock %}
</body>
</html>`
	for _, path := range []string{"src/pages/index.html", "index.html"} {
		t.Run(path, func(t *testing.T) {
			recs := extractHTML6852(t, src, path)
			if len(recs) == 0 {
				t.Fatal("premise: fixture produced no records at all")
			}
			if n := len(htmlPathAnchored6852(recs, path)); n != 0 {
				t.Fatalf("premise: want 0 path-anchored relationships, got %d", n)
			}
			got := htmlNamedExactly6852(recs, path)
			if len(got) != 0 {
				t.Errorf("an .html file with nothing to carry must not gain a file carrier: "+
					"%d records answer to %q, want 0 — an unconditional carrier mints one bare "+
					"orphan node per .html file across a whole repo, which no recall-shaped "+
					"assertion can see", len(got), path)
				for _, r := range got {
					t.Errorf("  named %q: kind=%q subtype=%q name=%q qname=%q",
						path, r.Kind, r.Subtype, r.Name, r.QualifiedName)
				}
			}
		})
	}
}

// TestHTML_ExternalRefsGetNoCarrier_6852 is the second over-firing control, on
// the shape most likely to be mistaken for an anchoring one: a page whose only
// <script> and <link> are CDN URLs. #506 drops external refs before any record
// is built, so no IMPORTS edge exists and nothing is anchored — but the tags
// ARE present, so a carrier keyed on "this page has a script tag" rather than
// on the anchored edge would fire here.
func TestHTML_ExternalRefsGetNoCarrier_6852(t *testing.T) {
	const src = `<html><head>
<script src="https://cdn.example.com/lib.js"></script>
<link rel="stylesheet" href="https://cdn.example.com/lib.css">
</head><body><img src="https://cdn.example.com/logo.png"></body></html>`
	for _, path := range []string{"src/pages/index.html", "index.html"} {
		t.Run(path, func(t *testing.T) {
			recs := extractHTML6852(t, src, path)
			if n := len(htmlPathAnchored6852(recs, path)); n != 0 {
				t.Fatalf("premise: want 0 path-anchored relationships, got %d", n)
			}
			if n := len(htmlNamedExactly6852(recs, path)); n != 0 {
				t.Errorf("a page whose only asset refs are external URLs must gain no record "+
					"named %q, got %d", path, n)
			}
		})
	}
}

// TestHTML_EmptyFileGetsNoCarrier_6852 is the third over-firing control, on the
// OTHER shape Extract can return: an empty file returns nil before any walk
// happens. Nothing to anchor, nothing to carry — and an unconditional carrier
// placed at the top of Extract would still mint a node here.
func TestHTML_EmptyFileGetsNoCarrier_6852(t *testing.T) {
	for _, path := range []string{"src/pages/index.html", "index.html"} {
		t.Run(path, func(t *testing.T) {
			recs := extractHTML6852(t, "", path)
			if n := len(htmlNamedExactly6852(recs, path)); n != 0 {
				t.Errorf("an empty .html file must gain no record named %q, got %d", path, n)
			}
		})
	}
}

// TestHTML_EmailTemplateGetsNoCarrier_6852 is the fourth over-firing control
// and covers Extract's third return path: #506 skips transactional email
// templates wholesale, before any entity is built. The fixture DOES contain a
// local stylesheet ref, so it would anchor if it were extracted at all — which
// is what makes this a control on the skip rather than a restatement of
// TestHTML_NoCarrierWithoutAPathAnchoredImport_6852.
func TestHTML_EmailTemplateGetsNoCarrier_6852(t *testing.T) {
	const path = "templates/welcome_email.html"
	recs := extractHTML6852(t, linkElementSrc6852, path)
	if len(recs) != 0 {
		t.Fatalf("premise: #506 must skip %s wholesale, got %d records", path, len(recs))
	}
	if n := len(htmlNamedExactly6852(recs, path)); n != 0 {
		t.Errorf("a skipped email template must gain no record named %q, got %d", path, n)
	}
}

// TestHTML_OneCarrierPerFileNotPerImport_6852 is the fifth over-firing control
// and the property this arm exists to exercise: html pages reference MANY
// assets, so "one carrier per FILE, not per EDGE and not per SITE" is under
// real multiplicity here rather than under a two-edge fixture. Axis VARIED: the
// NUMBER of path-anchored edges (seven) and the fact that they come from ALL
// FIVE reachable sites. HELD CONSTANT: one file, one path.
// A per-edge or per-site carrier would put several nodes under one id.
func TestHTML_OneCarrierPerFileNotPerImport_6852(t *testing.T) {
	const src = `<html><head>
<script src="/static/a.js"></script>
<link rel="stylesheet" href="/static/a.css">
<link rel="stylesheet" href="/static/b.css"/>
</head><body>
<img src="/static/a.png">
<img src="/static/b.png"/>
<script src="/static/c.js"></script>
<link rel="stylesheet" href="/static/c.css">
</body></html>`
	for _, path := range []string{"src/pages/index.html", "index.html"} {
		t.Run(path, func(t *testing.T) {
			recs := extractHTML6852(t, src, path)
			if n := len(htmlPathAnchored6852(recs, path)); n != 7 {
				t.Fatalf("premise: want 7 path-anchored IMPORTS edges, got %d", n)
			}
			if n := len(htmlNamedExactly6852(recs, path)); n != 1 {
				t.Errorf("7 path-anchored imports across all five reachable emission sites must still "+
					"yield exactly 1 record named %q, got %d", path, n)
			}
		})
	}
}

// TestHTML_SelfReferencingRefGetsNoSecondCarrier_6852 is the sixth over-firing
// control and the ONLY thing in this package that reaches FileCarrierFor clause
// 3 ("no record is ALREADY named path"). html names every record after the
// REFERENCE, so the only way a record can be named after the file is for the
// page to reference its own path — contrived, but reachable, and clause 3 is
// otherwise ungraded for this caller. Without the clause a second
// SCOPE.Component would be minted under a name a record already holds, making
// the rewrite target ambiguous.
//
// This is html's analogue of terraform's root-depth accident, and the contrast
// is the point: for terraform the clause fires on ordinary root-level files,
// for html it fires only here.
func TestHTML_SelfReferencingRefGetsNoSecondCarrier_6852(t *testing.T) {
	const path = "src/pages/index.html"
	const src = `<html><head><link rel="stylesheet" href="src/pages/index.html"></head></html>`
	recs := extractHTML6852(t, src, path)
	if n := len(htmlPathAnchored6852(recs, path)); n != 1 {
		t.Fatalf("premise: want 1 path-anchored IMPORTS edge, got %d", n)
	}
	got := htmlNamedExactly6852(recs, path)
	if len(got) != 1 {
		t.Errorf("exactly 1 record may answer to %q when the page references its own path "+
			"(the style_include record already carries that Name), got %d", path, len(got))
		for _, r := range got {
			t.Errorf("  kind=%q subtype=%q name=%q qname=%q quality=%v",
				r.Kind, r.Subtype, r.Name, r.QualifiedName, r.QualityScore)
		}
	}
}

// TestHTML_SelfClosingScriptSiteIsDead_6852 pins the one buildAssetImportRel
// call site this file's fixture set does not drive — visitSelfClosingTag's
// "script" case — as unreachable, so the claim above is a pin and not an
// argument.
//
// The grammar reason is in the header. What is asserted here is the OBSERVABLE
// consequence: the self-closing <script> contributes no script_include record,
// while the <img> beside it and the carrier are both emitted. Asserting the
// neighbours matters — a fixture that only checked "no script_include" would
// pass just as well if the whole document failed to parse, which is the shape
// of vacuity that would hide the site becoming live under some OTHER tag.
func TestHTML_SelfClosingScriptSiteIsDead_6852(t *testing.T) {
	const path = "src/pages/index.html"
	const src = `<html><head><script src="/static/a.js"/></head><body><img src="/static/b.png"></body></html>`
	recs := extractHTML6852(t, src, path)

	var script, img, carrier int
	for _, r := range recs {
		switch {
		case r.Subtype == "script_include":
			script++
		case r.Subtype == "image_include":
			img++
		case r.Name == path:
			carrier++
		}
	}
	if script != 0 {
		t.Errorf("a self-closing <script src=.../> produced %d script_include record(s) — "+
			"visitSelfClosingTag's \"script\" case has become REACHABLE (grammar.js routes "+
			"self_closing_tag through $._start_tag_name, which scanner.c never emits for a "+
			"SCRIPT tag), so a sixth anchoring producer is now live and this file's fixture "+
			"set does not drive it", script)
	}
	// The neighbours are the anti-vacuity half: without them "0 script_include"
	// is satisfied by a document that produced nothing at all.
	if img != 1 {
		t.Errorf("premise: want 1 image_include from the <img> beside the self-closing "+
			"<script>, got %d — the document did not parse as this test assumes", img)
	}
	if carrier != 1 {
		t.Errorf("premise: want exactly 1 carrier named %q (the <img> anchors), got %d",
			path, carrier)
	}
}

// TestHTML_CarrierShape_6852 pins what the carrier IS: stamped with the
// extractor's language token, anchored on the file it names, first in the
// record slice (#577), and owning NO relationships of its own — the
// script/link/img records still carry the IMPORTS edges, so re-homing them onto
// the carrier would double every edge.
//
// The Language assertion is load-bearing and not decorative, and it is why this
// test is named in nonTaggingCallers (internal/extractor/carrier_caller_set_6861_test.go):
// internal/extractors/html runs NO extractor.TagEntitiesLanguage, so the
// carrier keeps whatever token PrependFileCarrier is handed. A wrong OR EMPTY
// token would survive every other test in this file and in this package.
//
// Driven over every anchoring site so the shape is not pinned on one tag.
func TestHTML_CarrierShape_6852(t *testing.T) {
	const path = "src/pages/index.html"
	for site, src := range anchoringFixtures6852() {
		t.Run(site, func(t *testing.T) {
			recs := extractHTML6852(t, src, path)
			cs := htmlNamedExactly6852(recs, path)
			if len(cs) != 1 {
				t.Fatalf("premise: want 1 carrier, got %d", len(cs))
			}
			c := cs[0]
			if c.Kind != "SCOPE.Component" || c.Subtype != "file" {
				t.Errorf("carrier kind/subtype = %q/%q, want %q/%q",
					c.Kind, c.Subtype, "SCOPE.Component", "file")
			}
			if c.Language != "html" {
				t.Errorf("carrier Language = %q, want %q — the token comes from the "+
					"PrependFileCarrier argument, and internal/extractors/html runs no "+
					"TagEntitiesLanguage to repair an empty or wrong one", c.Language, "html")
			}
			if c.SourceFile != path {
				t.Errorf("carrier SourceFile = %q, want %q", c.SourceFile, path)
			}
			if n := len(c.Relationships); n != 0 {
				t.Errorf("the html file carrier must own no relationships, got %d", n)
			}
			if n := len(htmlPathAnchored6852(recs, path)); n != 1 {
				t.Errorf("the asset IMPORTS edge must still be emitted exactly once, got %d", n)
			}
			// #577 convention: the file entity is the FIRST record. python's
			// re_exports.go and prune_import_placeholders.go both rely on index 0.
			if recs[0].Name != path {
				t.Errorf("carrier must be record 0 (#577 convention), got %q at index 0", recs[0].Name)
			}
		})
	}
}
