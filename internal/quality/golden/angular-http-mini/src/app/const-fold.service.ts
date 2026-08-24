import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';

import { Thing } from './thing.model';

// The bare-identifier fold (#6551). `list()` below passes PEOPLE, not a
// literal, and before #6551 that call dangled at /{dynamic}/PEOPLE even though
// its value is right here in the file.
//
// The commented-out declaration is the point of the file, not decoration
// (#6552 shape A): a first-match-wins regex over raw source binds PEOPLE to
// /api/OLD/people, because a regex cannot tell a comment from a declaration.
// The AST table never sees it -- tree-sitter yields a `comment` node, and a
// comment is not a lexical_declaration.
//
// const PEOPLE = '/api/OLD/people';
const PEOPLE = '/api/admin/v1/people';

// #6552 shape H: a module-level const shadowed by a method PARAMETER of the
// same name. At the call site `shadowed` is whatever the caller passed, so no
// path is knowable and the call must stay under the dynamic marker. Folding
// the module const here would publish a confident runtime_dynamic=false
// endpoint that the cross-stack linker would attach to a route this call never
// makes -- strictly worse than the honest /{dynamic} the pass emitted before.
const shadowed = '/api/module/level';

@Injectable({ providedIn: 'root' })
export class ConstFoldService {
  private http = inject(HttpClient);

  // Deliberately NOT named `list` — app/thing.service.ts already has a
  // SCOPE.Operation by that name, and its FETCHES row is what grades caller
  // attribution (#6447). Two same-named operations in one fixture make that
  // row ambiguous rather than falsifiable.
  people(): Observable<readonly Thing[]> {
    return this.http.get<readonly Thing[]>(PEOPLE);
  }

  byPath(shadowed: string): Observable<Thing> {
    return this.http.get<Thing>(shadowed);
  }
}
