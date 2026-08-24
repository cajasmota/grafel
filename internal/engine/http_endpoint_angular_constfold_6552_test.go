// Scope-safety tests for the bare-identifier constant fold added by #6551.
//
// The fold itself is right: `const PEOPLE = '/api/admin/v1/people';
// http.get(PEOPLE)` should resolve, and before #6551 it dangled at
// /{dynamic}/PEOPLE. What was wrong was the BINDING-SELECTION rule. The fold
// read buildJSConstantSymbolTable, a regex table over raw file text that keeps
// the FIRST `(?:const|let|var) NAME = '...'` match per name, never strips
// comments, and has no notion of scope.
//
// The older consumers of that table tolerated all of this because they only
// ever REFINED a path that was already partly literal. The bare-identifier arm
// makes the table the sole source of truth for a call that was otherwise an
// honest /{dynamic} marker, so a wrong binding ships as a confident
// runtime_dynamic=false endpoint and the cross-stack linker FETCHES-links it to
// a route the call never makes.
//
// Each test below is a shape where the regex table binds the wrong value, and
// where declining is strictly better than answering: `main` emitted
// /{dynamic}, which is honest.
package engine

import (
	"strings"
	"testing"
)

// requireNoStaticFold fails when any emitted id resolves to a concrete path
// under one of the given prefixes — i.e. the fold answered when it should have
// declined. A /{dynamic} id is the correct outcome for every caller here.
func requireNoStaticFold(t *testing.T, got []string, badPaths []string, label string) {
	t.Helper()
	for _, id := range got {
		for _, bad := range badPaths {
			if strings.Contains(id, bad) && !strings.Contains(id, "{dynamic}") {
				t.Errorf("%s: folded a binding it could not prove: %q (got=%v)", label, id, got)
			}
		}
	}
}

// requireAnyDynamic asserts the call site was still emitted, just honestly.
func requireAnyDynamic(t *testing.T, got []string, label string) {
	t.Helper()
	for _, id := range got {
		if strings.Contains(id, "{dynamic}") {
			return
		}
	}
	t.Errorf("%s: expected the unresolvable call to survive as a {dynamic} marker, got=%v", label, got)
}

// A — a commented-out declaration wins. tree-sitter never yields a `comment`
// node as a declaration; a first-match-wins regex over raw text does.
func TestSynth_AngularConstFold_CommentedOutDeclaration_6552(t *testing.T) {
	src := `
import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';

// const PEOPLE = '/api/OLD/people';        // dead, commented out
const PEOPLE = '/api/admin/v1/people';      // live

@Injectable({ providedIn: 'root' })
export class PeopleService {
  private http = inject(HttpClient);
  a() { return this.http.get<any>(PEOPLE); }
}
`
	got, _ := runDetect(t, "typescript", "people.service.ts", src)
	requireNoStaticFold(t, got, []string{"/api/OLD/people"}, "commented-out-decl")
	requireContains(t, got, []string{"http:GET:/api/admin/v1/people"}, "commented-out-decl-live")
}

// D — the identifier is not in scope at the call at all: declared inside an
// unrelated class's method body, referenced from a different class where the
// name is undefined. A per-file flat map cannot see that; a scope tree can.
func TestSynth_AngularConstFold_OutOfScopeDeclaration_6552(t *testing.T) {
	src := `
import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';

export class UnrelatedService {
  build() {
    const endpoint = '/api/unrelated/thing';
    return endpoint;
  }
}

@Injectable({ providedIn: 'root' })
export class CallerService {
  private http = inject(HttpClient);
  a() { return this.http.get<any>(endpoint); }
}
`
	got, _ := runDetect(t, "typescript", "caller.service.ts", src)
	requireNoStaticFold(t, got, []string{"/api/unrelated/thing"}, "out-of-scope-decl")
	requireAnyDynamic(t, got, "out-of-scope-decl")
}

// C — a reassignable binding. `let url = '/api/first'; url = '/api/second';`
// folds the stale first value. A `let` can never be folded safely, and
// jsConstStringRe literally alternates (?:const|let|var).
func TestSynth_AngularConstFold_ReassignedLet_6552(t *testing.T) {
	src := `
import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';

@Injectable({ providedIn: 'root' })
export class ReassignService {
  private http = inject(HttpClient);
  a() {
    let url = '/api/first';
    url = '/api/second';
    return this.http.get<any>(url);
  }
}
`
	got, _ := runDetect(t, "typescript", "reassign.service.ts", src)
	requireNoStaticFold(t, got, []string{"/api/first", "/api/second"}, "reassigned-let")
	requireAnyDynamic(t, got, "reassigned-let")
}

// H — a module-level const shadowed by a method PARAMETER of the same name.
// At the call site `path` is the parameter, whose value is a caller's runtime
// argument; the module const is not what is passed.
func TestSynth_AngularConstFold_ShadowedByParameter_6552(t *testing.T) {
	src := `
import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';

const path = '/api/module/level';

@Injectable({ providedIn: 'root' })
export class ShadowService {
  private http = inject(HttpClient);
  a(path: string) { return this.http.get<any>(path); }
}
`
	got, _ := runDetect(t, "typescript", "shadow.service.ts", src)
	requireNoStaticFold(t, got, []string{"/api/module/level"}, "shadowed-by-parameter")
	requireAnyDynamic(t, got, "shadowed-by-parameter")
}

// A class FIELD is not the module const of the same name. `this.url` and `url`
// are different bindings, and a class field's initialiser is not something this
// fold tracks. The review of #6552 named this exact move — trimming a `this.`
// prefix and retrying the lookup — as a permissive mutant that left the whole
// suite green while emitting a wrong value; this test is what kills it.
func TestSynth_AngularConstFold_ThisFieldIsNotTheModuleConst_6552(t *testing.T) {
	src := `
import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';

const url = '/api/module/const';

@Injectable({ providedIn: 'root' })
export class FieldService {
  private http = inject(HttpClient);
  private url = '/api/field/value';
  a() { return this.http.get<any>(this.url); }
}
`
	got, _ := runDetect(t, "typescript", "field.service.ts", src)
	requireNoStaticFold(t, got, []string{"/api/module/const", "/api/field/value"}, "this-field-not-module-const")
	requireAnyDynamic(t, got, "this-field-not-module-const")
}

// Regression guard for what already correctly DECLINES and must keep
// declining: a concatenation, and a same-named const in ANOTHER file (the
// table is per-file, built at http_endpoint_client_synthesis.go:1707).
func TestSynth_AngularConstFold_KeepsDeclining_6552(t *testing.T) {
	concat := `
import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';

const base = '/api/base';

@Injectable({ providedIn: 'root' })
export class ConcatService {
  private http = inject(HttpClient);
  a() { return this.http.get<any>(base + '/things'); }
}
`
	got, _ := runDetect(t, "typescript", "concat.service.ts", concat)
	requireNoStaticFold(t, got, []string{"/api/base"}, "concat-declines")
	requireAnyDynamic(t, got, "concat-declines")

	otherFile := `
import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';

@Injectable({ providedIn: 'root' })
export class OtherFileService {
  private http = inject(HttpClient);
  a() { return this.http.get<any>(PEOPLE); }
}
`
	got2, _ := runDetect(t, "typescript", "otherfile.service.ts", otherFile)
	requireNoStaticFold(t, got2, []string{"/api/admin/v1/people"}, "cross-file-declines")
	requireAnyDynamic(t, got2, "cross-file-declines")
}
