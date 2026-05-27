package main

import (
	"strings"
	"testing"
)

// TestIssue2677_DRFEndpointAttribution covers the P0 attribution fix:
// every CRUD and @action endpoint emitted by ApplyDjangoDRFRoutes must
// attribute its source_file to the ViewSet's source file (and its StartLine
// to the def line of the handling method), NOT to the routers.py file that
// merely calls router.register(...).
//
// The fixture at testdata/drf_attribution_fixture defines:
//
//	myapp/urls.py    — path("api/v1/", include("myapp.routers"))
//	myapp/routers.py — router.register(r"things", views.ThingViewSet)
//	myapp/views.py   — class ThingViewSet(viewsets.ModelViewSet):
//	                       def list(...)               (line 12)
//	                       @action(... url_path="custom_action", methods=["post"])
//	                       def custom(...)             (line 17)
//
// Expectations:
//  1. GET /api/v1/things attributes to views.py at the `def list(` line.
//  2. POST /api/v1/things/custom_action attributes to views.py at the
//     `def custom(` line (the @action-decorated method).
//  3. The /api/v1/ mount-point declared in urls.py is still discoverable —
//     i.e. at least one http_endpoint whose canonical path starts with
//     /api/v1/ has SourceFile pointing at myapp/urls.py (the include site).
func TestIssue2677_DRFEndpointAttribution(t *testing.T) {
	doc := runIndexerOn(t, "testdata/drf_attribution_fixture", "drf_attribution_fixture", nil)

	type endpoint struct {
		path       string
		verb       string
		sourceFile string
		startLine  int
	}
	var got []endpoint
	for _, e := range doc.Entities {
		// The http-endpoint-split pass renames `http_endpoint` to either
		// `http_endpoint_definition` (server-side handlers) or
		// `http_endpoint_call` (client-side fetches). Definitions are what
		// this test cares about.
		if e.Kind != "http_endpoint" && e.Kind != "http_endpoint_definition" {
			continue
		}
		got = append(got, endpoint{
			path:       e.Properties["path"],
			verb:       e.Properties["verb"],
			sourceFile: e.SourceFile,
			startLine:  e.StartLine,
		})
	}
	if len(got) == 0 {
		t.Fatalf("no http_endpoint entities emitted by indexer")
	}

	findByVerbPath := func(verb, path string) *endpoint {
		for i := range got {
			if got[i].verb == verb && got[i].path == path {
				return &got[i]
			}
		}
		return nil
	}

	listEP := findByVerbPath("GET", "/api/v1/things")
	if listEP == nil {
		t.Fatalf("missing GET /api/v1/things — got endpoints: %+v", got)
	}
	if !strings.HasSuffix(listEP.sourceFile, "views.py") {
		t.Errorf("GET /api/v1/things attributes to %q; want views.py", listEP.sourceFile)
	}
	if listEP.startLine == 0 {
		t.Errorf("GET /api/v1/things has no StartLine; want the `def list(` line")
	}
	// The fixture's `def list(` lives on line 12; allow tolerance for whitespace
	// edits in the fixture by accepting any line in the body range [6, 16].
	if listEP.startLine < 6 || listEP.startLine > 16 {
		t.Errorf("GET /api/v1/things StartLine=%d; expected within def list() neighbourhood (6..16)",
			listEP.startLine)
	}

	customEP := findByVerbPath("POST", "/api/v1/things/custom_action")
	if customEP == nil {
		t.Fatalf("missing POST /api/v1/things/custom_action — got endpoints: %+v", got)
	}
	if !strings.HasSuffix(customEP.sourceFile, "views.py") {
		t.Errorf("POST /api/v1/things/custom_action attributes to %q; want views.py",
			customEP.sourceFile)
	}
	if customEP.startLine == 0 {
		t.Errorf("POST /api/v1/things/custom_action has no StartLine")
	}
	// The @action-decorated `def custom(` lives on line 17; accept [13, 22].
	if customEP.startLine < 13 || customEP.startLine > 22 {
		t.Errorf("POST /api/v1/things/custom_action StartLine=%d; expected within def custom() neighbourhood (13..22)",
			customEP.startLine)
	}

	// Mount-point discoverability: at least one http_endpoint whose path is
	// under /api/v1/ must attribute to the urls.py file that declared the
	// include() — otherwise the question "where is /api/v1/ declared?" loses
	// its anchor in the graph.
	mountFound := false
	for _, e := range got {
		if !strings.HasPrefix(e.path, "/api/v1") {
			continue
		}
		if strings.HasSuffix(e.sourceFile, "urls.py") {
			mountFound = true
			break
		}
	}
	if !mountFound {
		t.Errorf("no http_endpoint under /api/v1/ attributes to urls.py — mount-point discoverability lost")
	}

	// Spot-check: NO http_endpoint covering a /api/v1/things* path should
	// attribute to routers.py — that's the regression we are fixing.
	for _, e := range got {
		if !strings.HasPrefix(e.path, "/api/v1/things") {
			continue
		}
		if strings.HasSuffix(e.sourceFile, "routers.py") {
			t.Errorf("regression: endpoint %s %s still attributes to routers.py (#2677)",
				e.verb, e.path)
		}
	}
}
