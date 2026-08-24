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
	"strings"
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

// TestSynth_AngularHttpClient_StringConstURL_6551 — the URL argument is a bare
// identifier bound to a same-file string-literal constant, the idiomatic Angular
// api-service shape:
//
//	const PEOPLE = '/api/admin/v1/people';
//	this.http.get<Person>(PEOPLE, opts);
//	this.http.post<Person>(PEOPLE, body);
//
// Reported by @auxmedrano (#6551): #6433 got the call site extracted, but a bare
// identifier fell straight through to the /{dynamic} marker even when its value
// was a statically-known path, so every call routed through a path constant
// dangled instead of linking to its producer endpoint. The symbol table
// (buildJSConstantSymbolTable) already captures such consts and is already
// threaded into the receiver residual pass; this folds the identifier through it.
// A non-literal const (const BASE = environment.apiBase) is NOT captured and
// stays dynamic — see TestSynth_AngularHttpClient_DynamicArg_6433.
func TestSynth_AngularHttpClient_StringConstURL_6551(t *testing.T) {
	src := `
import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';

const PEOPLE = '/api/admin/v1/people';

@Injectable({ providedIn: 'root' })
export class PeopleService {
  private http = inject(HttpClient);

  byDpi(dpi: string) {
    return this.http.get<Person>(PEOPLE, { params: new HttpParams().set('dpi', dpi) });
  }

  create(body: Person) {
    return this.http.post<Person>(PEOPLE, body);
  }
}
`
	got, _ := runDetect(t, "typescript", "people.service.ts", src)
	requireContains(t, got, []string{
		"http:GET:/api/admin/v1/people",
		"http:POST:/api/admin/v1/people",
	}, "angular-httpclient-string-const")
	for _, id := range got {
		if strings.Contains(id, "{dynamic}") && strings.Contains(id, "PEOPLE") {
			t.Errorf("bare string-const identifier left dynamic instead of folded: %q (got=%v)", id, got)
		}
	}
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
			if e.Properties["runtime_dynamic"] == "true" &&
				strings.HasPrefix(e.ID, "http:GET:"+dynamicClientPathPrefix) {
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

// ---------------------------------------------------------------------------
// Review round 2 (#6446) — the token gate was a bare strings.Contains over raw
// file text, so a MENTION of the class name opened it.
// ---------------------------------------------------------------------------

// tokenMentionCases are the three shapes that must NOT count as evidence that a
// file consumes an HTTP client. Each carries a `this.http.<verb>('/…')` call on
// a plain-object member named `http`; none of them injects, imports as a value,
// or type-annotates against the real client.
var tokenMentionCases = []struct {
	name    string
	file    string
	mention string
}{
	{
		// The single most common shape in a mid-migration Angular codebase.
		name:    "comment",
		file:    "legacy-wrapper.ts",
		mention: "// TODO(migration): replace this wrapper with Angular's HttpClient.",
	},
	{
		// Erased at compile time; a type-only import cannot be an Angular DI
		// token, so on its own it is not evidence of consumption.
		name:    "type-only-import",
		file:    "type-only.ts",
		mention: "import type { HttpClient } from '@angular/common/http';",
	},
	{
		name:    "string-literal",
		file:    "doc.ts",
		mention: "export const DOC = 'use HttpClient instead';",
	},
}

func TestSynth_AngularHttpClient_MentionIsNotEvidence_6433(t *testing.T) {
	for _, tc := range tokenMentionCases {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.mention + `
export class LegacyWrapper {
  private http = { get: (_: string) => null };

  probe() {
    return this.http.get('/api/mention-only');
  }
}
`
			got, _ := runDetect(t, "typescript", tc.file, src)
			for _, id := range got {
				if id == "http:GET:/api/mention-only" {
					t.Errorf("a %s mention of the client class opened the gate: emitted %q (got=%v)",
						tc.name, id, got)
				}
			}
		})
	}
}

// TestSynth_AngularHttpClient_RelativeURLIsNotMarkedDynamic_6433 covers a
// statically-known RELATIVE URL. normalizeRawClientPath declines it only because
// it lacks a leading slash — the path is right there in the source. Emitting it
// as runtime_dynamic claims "unresolvable" about a URL the pass is holding, and
// contradicts the cross extractor, which mints the real path from the same call
// site in the same file.
func TestSynth_AngularHttpClient_RelativeURLIsNotMarkedDynamic_6433(t *testing.T) {
	src := `
import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';

@Injectable({ providedIn: 'root' })
export class I18nService {
  private http = inject(HttpClient);

  strings() {
    return this.http.get<Record<string, string>>('assets/i18n/en.json');
  }
}
`
	got, res := runDetect(t, "typescript", "i18n.service.ts", src)
	requireContains(t, got, []string{"http:GET:/assets/i18n/en.json"}, "angular-httpclient-relative")

	for _, e := range res.Entities {
		if e.Kind == httpEndpointCallKind && e.Properties["runtime_dynamic"] == "true" {
			t.Errorf("relative literal URL was mislabelled runtime_dynamic: %q (entities=%s)",
				e.ID, summarizeHTTPCallEntities(res))
		}
	}
}

// TestSynth_AngularHttpClient_DynamicSitesDoNotCollide_6433 pins the dedup key.
// Every dynamic site used to land on the single id http:GET:/{dynamic}, and
// makeEmit dedups on (patternType, id), so N dynamic GETs in one file collapsed
// to one entity. The reporter's headline complaint was an undercount; a
// collapsing marker reintroduces one.
func TestSynth_AngularHttpClient_DynamicSitesDoNotCollide_6433(t *testing.T) {
	src := `
import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';

const one = (c: string) => resolveOne(c);
const two = (c: string) => resolveTwo(c);

@Injectable({ providedIn: 'root' })
export class TwoDynamicService {
  private http = inject(HttpClient);

  first(c: string) {
    return this.http.get<Thing>(one(c));
  }

  second(c: string) {
    return this.http.get<Thing>(two(c));
  }
}
`
	_, res := runDetect(t, "typescript", "two-dynamic.service.ts", src)

	ids := map[string]bool{}
	for _, e := range res.Entities {
		if e.Kind == httpEndpointCallKind && e.Properties["runtime_dynamic"] == "true" {
			ids[e.ID] = true
		}
	}
	if len(ids) != 2 {
		t.Errorf("two distinct dynamic GET call sites produced %d entity/entities, want 2 (entities=%s)",
			len(ids), summarizeHTTPCallEntities(res))
	}
}

// TestSynth_AngularHttpClient_StaticURLIsNotAlsoDynamic_6433 pins the claim the
// residual pass makes in prose — "arguments the static / template passes DO
// resolve are skipped here, so no call site is emitted twice". Deleting that
// skip previously survived every test, the fixture and the baseline while the
// graph silently grew a bogus dynamic node per static call.
func TestSynth_AngularHttpClient_StaticURLIsNotAlsoDynamic_6433(t *testing.T) {
	_, res := runDetect(t, "typescript", "thing.service.ts", angularServiceSrc)
	for _, e := range res.Entities {
		if e.Kind != httpEndpointCallKind {
			continue
		}
		if e.Properties["runtime_dynamic"] == "true" {
			t.Errorf("statically-resolved call site also emitted a dynamic twin: %q (entities=%s)",
				e.ID, summarizeHTTPCallEntities(res))
		}
	}
}

// TestSynth_AngularHttpClient_NestedGeneric_6433 — Array<T>, HttpResponse<T> and
// Record<string,T> are routine Angular response types. The generic-argument
// group excluded < and >, so a nested generic matched NOTHING: not the static
// pass, not even the dynamic marker.
func TestSynth_AngularHttpClient_NestedGeneric_6433(t *testing.T) {
	src := `
import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpResponse } from '@angular/common/http';

@Injectable({ providedIn: 'root' })
export class NestedGenericService {
  private http = inject(HttpClient);

  all() {
    return this.http.get<Array<Thing>>('/api/nested-array');
  }

  raw() {
    return this.http.get<HttpResponse<Array<Thing>>>('/api/nested-twice');
  }
}
`
	got, _ := runDetect(t, "typescript", "nested-generic.service.ts", src)
	requireContains(t, got, []string{"http:GET:/api/nested-array"}, "angular-httpclient-nested-generic")
}
