// Package main — incremental_baseurl_fold_6450_test.go
//
// INCREMENTAL BASE-URL FOLD PARITY (#6450, review blocking finding 1).
//
// #6450 moved the substrate base-URL constant fold to index time so the
// per-repo call→definition matcher can see a folded path. The first cut
// derived the fold's symbol table from `merged`, which on an INCREMENTAL run
// holds only the re-extracted files. The consequence was not a missed
// improvement but a REGRESSION: touch the caller file with an unrelated edit
// and the declaring constants module becomes invisible, every folded path
// reverts to `/{BASE}/…`, and every FETCHES the previous full index resolved
// turns back into UNRESOLVED_FETCH — and stays that way across further
// incremental runs until the next full index.
//
// Measured on this fixture before the fix, via the same Path-B pipeline this
// test drives:
//
//	                              BEFORE the fix          AFTER the fix
//	full index                    folded=3 path=/api      folded=3 path=/api
//	append a comment to api.js    folded=0 path=/{BASE}   folded=3 path=/api
//	no-change incremental         folded=0 path=/{BASE}   folded=3 path=/api
//
// A full-index-only test cannot see any of that, which is why this file
// exists alongside the golden fixture. It asserts the property that matters —
// the incremental graph's FOLD STATE equals the full graph's — rather than a
// pass counter, so it stays honest if the fold's internals change again.
//
// WHAT THIS TEST DELIBERATELY DOES NOT ASSERT, AND WHY
// ─────────────────────────────────────────────────────
// It does NOT require the incremental graph to carry the same FETCHES /
// UNRESOLVED_FETCH counts as the full one. On the incremental path `merged`
// holds only the re-extracted CALL file; the http_endpoint_definition lives in
// an unchanged file and is therefore absent from the slice the matcher indexes
// (`synthetics=3 … calls_unresolved=3`, zero definitions). That is a
// pre-existing property of the incremental design and is orthogonal to #6450 —
// MEASURED, not assumed: with the fold compiled out entirely (pre-#6450
// behaviour) the same fixture gives FETCHES=3 / UNRESOLVED_FETCH=3 on the
// incremental run, identical to what the fixed fold gives. Asserting count
// parity here would gate #6450 on a defect it neither caused nor can fix.
//
// What IS asserted is exactly the surface the fold owns: the `path` and
// `url_kind` properties of every consumer call, the `path` stamped on any
// UNRESOLVED_FETCH edge, and entity-ID stability across the boundary.
package main

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// bfServerJS is the producer side: two literal routes.
const bfServerJS = `const express = require('express');
const app = express();

app.get('/api/things', (req, res) => {
  res.json([]);
});

app.post('/api/things', (req, res) => {
  res.status(201).json({});
});

module.exports = app;
`

// bfConfigJS declares the base URL. This file is NEVER edited by the test —
// that is the whole point: the fold must survive an edit that does not touch
// it.
const bfConfigJS = `export const BASE = '/api';
`

// bfClientJS is the consumer. `BASE` is imported, so the canonicaliser emits
// path=/{BASE}/things and only the substrate fold can resolve it.
const bfClientJS = `import { BASE } from './config';

export async function listThings() {
  const res = await fetch(` + "`${BASE}/things`" + `);
  return res.json();
}

export async function createThing(body) {
  const res = await fetch(` + "`${BASE}/things`" + `, {
    method: 'POST',
    body: JSON.stringify(body),
  });
  return res.json();
}
`

// bfFoldState is the observable this test compares across runs: for every
// consumer-side call, its canonical Name (which must never move) mapped to
// the folded `path` + `url_kind` the fold produced.
type bfFoldState struct {
	paths    map[string]string // call Name → path property
	urlKinds map[string]string // call Name → url_kind property
	fetches  int
	// unresPaths holds the `path` property stamped on every UNRESOLVED_FETCH
	// edge. The COUNT is not this test's business (see the header), but an
	// unresolved edge naming an UNFOLDED path is the #6450 symptom itself.
	unresPaths []string
	ids        map[string]string // "kind|name|sourceFile" → entity ID
	dynamic    []string          // call Names still classified dynamic_baseurl
}

func bfCollect(doc *graph.Document) bfFoldState {
	st := bfFoldState{
		paths:    map[string]string{},
		urlKinds: map[string]string{},
		ids:      map[string]string{},
	}
	for _, e := range doc.Entities {
		st.ids[e.Kind+"|"+e.Name+"|"+e.SourceFile] = e.ID
		if e.Kind != "http_endpoint_call" {
			continue
		}
		st.paths[e.Name] = e.PropGet("path")
		st.urlKinds[e.Name] = e.PropGet("url_kind")
		if e.PropGet("url_kind") == "dynamic_baseurl" {
			st.dynamic = append(st.dynamic, e.Name)
		}
	}
	for _, r := range doc.Relationships {
		switch r.Kind {
		case "FETCHES":
			st.fetches++
		case "UNRESOLVED_FETCH":
			st.unresPaths = append(st.unresPaths, r.PropGet("path"))
		}
	}
	return st
}

func TestIncrementalDoesNotUnfoldBaseURLs_6450(t *testing.T) {
	repo := t.TempDir()
	state := t.TempDir()

	dvWriteFile(t, repo, "server/app.js", bfServerJS)
	dvWriteFile(t, repo, "client/config.js", bfConfigJS)
	dvWriteFile(t, repo, "client/api.js", bfClientJS)

	full := bfCollect(dvFullRebuild(t, repo, state))

	// The full index must actually fold, or every assertion below is vacuous.
	if len(full.paths) != 2 {
		t.Fatalf("full rebuild produced %d http_endpoint_call entities, want 2: %v",
			len(full.paths), full.paths)
	}
	for name, p := range full.paths {
		if p != "/api/things" {
			t.Fatalf("full rebuild did not fold %s: path=%q (want /api/things) — "+
				"the incremental comparison would be vacuous", name, p)
		}
	}
	if len(full.unresPaths) != 0 {
		t.Fatalf("full rebuild left %d UNRESOLVED_FETCH edges, want 0: %v",
			len(full.unresPaths), full.unresPaths)
	}
	if len(full.dynamic) != 0 {
		t.Fatalf("full rebuild left calls classified dynamic_baseurl: %v", full.dynamic)
	}
	if full.fetches == 0 {
		t.Fatal("full rebuild emitted no FETCHES edges — nothing to regress")
	}

	// An unrelated edit to the CALLER file, AFTER the manifest dvFullRebuild
	// seeded. client/config.js is untouched, so on the incremental run the
	// declaring module is reachable only through the carried-forward
	// prior-graph entities.
	//
	// Do NOT re-seed the manifest here: dvSeedManifest would record the EDITED
	// api.js as the baseline, the incremental run would then see zero changed
	// files, `merged` would be empty, and the carry-forward splice would
	// restore the previous (already folded) graph wholesale. That is a
	// vacuously green test — it was green even with the defect reintroduced.
	dvWriteFile(t, repo, "client/api.js", bfClientJS+"\n// unrelated edit\n")

	inc := bfCollect(cfPathBIncremental(t, repo, state))

	for name, want := range full.paths {
		got, ok := inc.paths[name]
		if !ok {
			t.Errorf("call %q vanished from the incremental graph", name)
			continue
		}
		if got != want {
			t.Errorf("call %q un-folded on the incremental path: path=%q, want %q "+
				"— an edit that never touched client/config.js must not lose the "+
				"base-URL binding (#6450 review blocking 1)", name, got, want)
		}
		if k := inc.urlKinds[name]; k != full.urlKinds[name] {
			t.Errorf("call %q url_kind=%q on the incremental path, want %q",
				name, k, full.urlKinds[name])
		}
	}
	// Second, independent detector of the same regression. The incremental path
	// legitimately re-emits UNRESOLVED_FETCH (the definition sits in an
	// unchanged file — see the header), but the `path` it stamps must be the
	// FOLDED one. An unfolded `/{BASE}/…` here means the symbol table lost the
	// declaring module again.
	for _, p := range inc.unresPaths {
		if strings.HasPrefix(p, "/{") {
			t.Errorf("incremental UNRESOLVED_FETCH stamped an UNFOLDED path %q — the "+
				"base-URL binding was lost on a run that never touched "+
				"client/config.js (#6450 review blocking 1)", p)
		}
	}
	if len(inc.dynamic) != 0 {
		t.Errorf("calls still classified url_kind=dynamic_baseurl after the "+
			"incremental run: %v — the full index had none", inc.dynamic)
	}
	if inc.fetches > full.fetches {
		t.Errorf("FETCHES count grew across the incremental run: full=%d incremental=%d",
			full.fetches, inc.fetches)
	}

	// IDENTITY CONTRACT across the incremental boundary: the fold rewrites the
	// `path` property only, so every entity ID present in the full graph must
	// carry the same ID in the incremental one.
	for key, wantID := range full.ids {
		if gotID, ok := inc.ids[key]; ok && gotID != wantID {
			t.Errorf("entity ID moved across the incremental run for %s: %s → %s",
				key, wantID, gotID)
		}
	}
}
