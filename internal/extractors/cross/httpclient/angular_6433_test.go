package httpclient

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
)

// #6433 — @auxmedrano measured SCOPE.ExternalAPI = 98 (pre-#6451 name) across a monorepo, every
// one of them backend, against 42 frontend files making concrete
// `this.http.get/post/patch(...)` calls.
//
// Every JS/TS pattern in this extractor anchors on the BARE identifiers `fetch`
// or `axios`. Angular's idiom is a receiver — `this.http.<verb><T>(...)` on an
// injected HttpClient — which matches under none of them, so the frontend
// contributed zero SCOPE.ExternalEndpoint nodes and there was nothing for a
// cross-stack link to attach to.

const angularSrc = `
import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';

@Injectable({ providedIn: 'root' })
export class ThingService {
  private http = inject(HttpClient);

  list() {
    return this.http.get<readonly Thing[]>('/api/things');
  }

  create(body: Thing) {
    return this.http.post<Thing>("/api/things", body);
  }

  byId(id: string) {
    return this.http.get<Thing>(` + "`/api/things/${id}`" + `);
  }
}
`

func extractAngular(t *testing.T, path, src string) []string {
	t.Helper()
	e := &Extractor{}
	recs, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "typescript",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var urls []string
	for _, r := range recs {
		if r.Kind == "SCOPE.ExternalEndpoint" {
			urls = append(urls, r.Name)
		}
	}
	return urls
}

func hasURL(urls []string, want string) bool {
	for _, u := range urls {
		if u == want {
			return true
		}
	}
	return false
}

// TestAngularHttpClient_EmitsExternalEndpoint_6433 is the reporter's metric: an
// Angular service must contribute SCOPE.ExternalEndpoint nodes.
func TestAngularHttpClient_EmitsExternalEndpoint_6433(t *testing.T) {
	urls := extractAngular(t, "src/app/thing.service.ts", angularSrc)
	for _, want := range []string{"/api/things", "/api/things/{*}"} {
		if !hasURL(urls, want) {
			t.Errorf("missing SCOPE.ExternalEndpoint %q (got %v)", want, urls)
		}
	}
}

// TestAngularHttpClient_MethodIsRecorded_6433 pins the http_method property on
// the CALLS edge, so a POST call site is distinguishable from a GET to the same
// URL — both of which this fixture makes to /api/things.
func TestAngularHttpClient_MethodIsRecorded_6433(t *testing.T) {
	e := &Extractor{}
	recs, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "src/app/thing.service.ts",
		Content:  []byte(angularSrc),
		Language: "typescript",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	methods := map[string]bool{}
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Properties.Get("url") == "/api/things" {
				if m := rel.Properties.Get("http_method"); m != "" {
					methods[m] = true
				}
			}
		}
	}
	for _, want := range []string{"GET", "POST"} {
		if !methods[want] {
			t.Errorf("no CALLS edge to /api/things with http_method=%s (got %v)", want, methods)
		}
	}
}

// TestAngularHttpClient_ReceiverGateIsNotOverBroad_6433 keeps the new receiver
// pattern behind the same token gate the engine pass uses. `this.http.get(...)`
// on some unrelated member named `http`, in a file that never mentions the
// Angular / NestJS client class, must stay unextracted — the field name alone
// is far too common to treat as evidence.
func TestAngularHttpClient_ReceiverGateIsNotOverBroad_6433(t *testing.T) {
	src := `
export class NotAConsumer {
  private http = { get: (_: string) => null };
  probe() { return this.http.get('/api/definitely-not-extracted'); }
}
`
	urls := extractAngular(t, "src/app/not-a-consumer.ts", src)
	if hasURL(urls, "/api/definitely-not-extracted") {
		t.Errorf("gate is over-broad: emitted /api/definitely-not-extracted (got %v)", urls)
	}
}

// ---------------------------------------------------------------------------
// Review round 2 (#6446)
// ---------------------------------------------------------------------------

// TestAngularHttpClient_MentionIsNotEvidence_6433 — the gate was a bare
// strings.Contains over raw file text, so a MENTION of the client class name
// opened it and minted a SCOPE.ExternalEndpoint from a plain-object member named
// `http`. That is the reporter's exact metric, inflated with non-calls.
func TestAngularHttpClient_MentionIsNotEvidence_6433(t *testing.T) {
	cases := []struct{ name, mention string }{
		{"comment", "// TODO(migration): replace this wrapper with Angular's HttpClient."},
		{"type-only-import", "import type { HttpClient } from '@angular/common/http';"},
		{"string-literal", "export const DOC = 'use HttpClient instead';"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.mention + `
export class LegacyWrapper {
  private http = { get: (_: string) => null };
  probe() { return this.http.get('/api/mention-only'); }
}
`
			urls := extractAngular(t, "src/app/legacy-wrapper.ts", src)
			if hasURL(urls, "/api/mention-only") {
				t.Errorf("a %s mention of the client class opened the gate (got %v)", tc.name, urls)
			}
		})
	}
}

// TestAngularHttpClient_SpecFileIsExcluded_6433 — the engine's consumer pass
// skips test sources; this extractor had no such exclusion, so the two passes
// disagreed and only the SCOPE.ExternalEndpoint side (the counted one) picked up test
// scaffolding. HttpClientTestingModule carries `HttpClient` as a substring, so
// every Angular spec file opened the old gate.
func TestAngularHttpClient_SpecFileIsExcluded_6433(t *testing.T) {
	src := `
import { TestBed } from '@angular/core/testing';
import { HttpClientTestingModule } from '@angular/common/http/testing';
import { HttpClient } from '@angular/common/http';

describe('ThingService', () => {
  it('stubs', () => {
    const http = TestBed.inject(HttpClient);
    return this.http.get('/api/spec-only');
  });
});
`
	urls := extractAngular(t, "src/app/thing.service.spec.ts", src)
	if hasURL(urls, "/api/spec-only") {
		t.Errorf("spec file contributed a SCOPE.ExternalEndpoint (got %v)", urls)
	}
}

// TestAngularHttpClient_NestedGeneric_6433 — Array<T> / HttpResponse<T> are
// routine; the generic-argument group excluded < and >, so they matched nothing.
func TestAngularHttpClient_NestedGeneric_6433(t *testing.T) {
	src := `
import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';

@Injectable({ providedIn: 'root' })
export class NestedGenericService {
  private http = inject(HttpClient);
  all() { return this.http.get<Array<Thing>>('/api/nested-array'); }
}
`
	urls := extractAngular(t, "src/app/nested-generic.service.ts", src)
	if !hasURL(urls, "/api/nested-array") {
		t.Errorf("nested generic argument matched nothing (got %v)", urls)
	}
}
