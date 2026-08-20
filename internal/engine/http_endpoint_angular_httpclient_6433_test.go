// Unit tests for #6433 — Angular HttpClient consumer-side extraction.
//
// Reported by @auxmedrano: SCOPE.ExternalAPI = 98 across a monorepo, all of
// them backend, against 42 frontend files making concrete
// `this.http.get/post/patch(...)` calls. Nothing on the frontend existed for a
// cross-stack link to attach to.
//
// The JS/TS consumer pass (http_endpoint_jsts_client_1483.go) already carried a
// regex that matches `this.http.<verb>(...)` — including the TS generic
// parameter — but synthesizeNestHttpService early-returned unless the file text
// contained the literal token `httpService` / `HttpService`. Idiomatic Angular
//
//	private http = inject(HttpClient);
//	this.http.get<readonly Thing[]>('/api/things')
//
// contains neither, so the pass exited before the regex ever ran.
//
// Separately, the URL argument had to be a quoted literal opening `http` or `/`,
// or a template literal containing `${...}`. A call expression (`base(code)`)
// or a template literal opening with an interpolation (`${BASE}/${id}`) was
// dropped outright — zero nodes, which is precisely why the frontend side was
// empty.
package engine

import (
	"testing"
)

// angularServiceSrc is idiomatic Angular 16+: `inject(HttpClient)` with a
// generic-parameterised call. It contains neither `httpService` nor
// `HttpService`.
const angularServiceSrc = `
import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';

@Injectable({ providedIn: 'root' })
export class ThingService {
  private http = inject(HttpClient);

  list(): Observable<readonly Thing[]> {
    return this.http.get<readonly Thing[]>('/api/things');
  }

  create(body: Thing): Observable<Thing> {
    return this.http.post<Thing>('/api/things', body);
  }
}
`

// TestSynth_AngularHttpClient_StaticURL_6433 is the headline case: a static
// quoted URL behind a generic parameter, in a file whose only HTTP-client
// token is `HttpClient`.
func TestSynth_AngularHttpClient_StaticURL_6433(t *testing.T) {
	got, _ := runDetect(t, "typescript", "thing.service.ts", angularServiceSrc)
	requireContains(t, got, []string{
		"http:GET:/api/things",
		"http:POST:/api/things",
	}, "angular-httpclient-static")
}

// TestSynth_AngularHttpClient_TemplateLiteral_6433 covers a template literal
// whose leading segment IS statically knowable — the canonical path keeps the
// interpolation as a `{name}` route parameter, so it can bind to the producer.
func TestSynth_AngularHttpClient_TemplateLiteral_6433(t *testing.T) {
	src := `
import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';

@Injectable({ providedIn: 'root' })
export class ThingDetailService {
  private http = inject(HttpClient);

  byId(id: string) {
    return this.http.get<Thing>(` + "`/api/things/${id}`" + `);
  }
}
`
	got, _ := runDetect(t, "typescript", "thing-detail.service.ts", src)
	requireContains(t, got, []string{"http:GET:/api/things/{id}"}, "angular-httpclient-template")
}

// TestSynth_AngularHttpClient_DynamicArg_6433 is the "0 nodes" case the
// reporter actually hit. Neither argument is a statically-resolvable URL:
//
//	base(code)      — a call expression
//	`${BASE}/${id}` — a template literal opening with an interpolation
//
// Resolving either would require base-URL constant folding, which is
// deliberately out of scope. What is NOT acceptable is dropping the call site:
// a node marked dynamic is useful to the repair flow and to cross-stack
// review; zero nodes is why the frontend side of the link was empty.
//
// The marker is the existing one (#721 / #732): runtime_dynamic=true, which
// urlKindFromPath turns into url_kind=dynamic_baseurl.
func TestSynth_AngularHttpClient_DynamicArg_6433(t *testing.T) {
	src := `
import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';

const BASE = environment.apiBase;
const base = (code: string) => environment.apiBase + '/' + code;

@Injectable({ providedIn: 'root' })
export class DynamicService {
  private http = inject(HttpClient);

  fetchByCode(code: string) {
    return this.http.get<Thing>(base(code));
  }

  patchById(id: string, body: Partial<Thing>) {
    return this.http.patch<Thing>(` + "`${BASE}/${id}`" + `, body);
  }
}
`
	_, res := runDetect(t, "typescript", "dynamic.service.ts", src)

	// The call expression carries no knowable path at all: it lands on the
	// canonical placeholder with runtime_dynamic=true.
	var dynamicGet bool
	// The interpolation-leading template literal keeps its shape
	// (`/{BASE}/{id}`) — that is NOT base-URL folding, the first segment is
	// still a placeholder — and is classified dynamic by url_kind.
	var dynamicPatch bool

	for _, e := range res.Entities {
		if e.Kind != httpEndpointCallKind {
			continue
		}
		if e.Properties["url_kind"] != "dynamic_baseurl" {
			continue
		}
		switch e.Properties["verb"] {
		case "GET":
			if e.Properties["runtime_dynamic"] == "true" && e.ID == "http:GET:"+dynamicClientPath {
				dynamicGet = true
			}
		case "PATCH":
			dynamicPatch = true
		}
	}
	if !dynamicGet {
		t.Errorf("no runtime_dynamic GET call site emitted for this.http.get(base(code)); entities=%s",
			summarizeHTTPCallEntities(res))
	}
	if !dynamicPatch {
		t.Errorf("no dynamic-marked PATCH call site emitted for this.http.patch(`${BASE}/${id}`); entities=%s",
			summarizeHTTPCallEntities(res))
	}
}

// TestSynth_AngularHttpClient_GateIsNotOverBroad_6433 pins the replacement
// gate. Widening the guard to "any file mentioning HttpClient / HttpService"
// must NOT degenerate into "any file at all": a `this.http.get(...)` call on
// some unrelated `http` member of a class that never touches Angular's
// HttpClient stays unextracted, because the receiver name alone is far too
// common a field name to key on.
func TestSynth_AngularHttpClient_GateIsNotOverBroad_6433(t *testing.T) {
	src := `
// Deliberately carries none of the Angular / NestJS client class tokens.
export class NotAnHttpConsumer {
  private http = { get: (_: string) => null, post: (_: string) => null };

  probe() {
    return this.http.get('/api/definitely-not-extracted');
  }
}
`
	got, _ := runDetect(t, "typescript", "not-a-consumer.ts", src)
	for _, id := range got {
		if id == "http:GET:/api/definitely-not-extracted" {
			t.Errorf("gate is over-broad: emitted %q from a file with no HttpClient/HttpService token (got=%v)", id, got)
		}
	}
}

// summarizeHTTPCallEntities renders every http_endpoint_call entity for a
// failure message.
func summarizeHTTPCallEntities(res *DetectResult) string {
	out := "["
	for _, e := range res.Entities {
		if e.Kind != httpEndpointCallKind {
			continue
		}
		out += " " + e.ID + "(runtime_dynamic=" + e.Properties["runtime_dynamic"] +
			",url_kind=" + e.Properties["url_kind"] + ")"
	}
	return out + " ]"
}
