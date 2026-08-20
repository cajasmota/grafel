import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';

import { Thing } from './thing.model';

const BASE = '/api/v2';
const base = (code: string) => BASE + '/things/' + code;
const legacyBase = (code: string) => BASE + '/legacy/' + code;

// Neither call site carries a statically-resolvable URL. Resolving them would
// require base-URL constant folding, which grafel deliberately does not
// attempt. What it must NOT do is drop the call site: a node marked dynamic is
// reviewable, a missing node is invisible (#6433).
//
// The two sites exist to pin the dedup key (#6446): the marker used to be one
// shared path, so N dynamic sites in a file collapsed into ONE entity.
@Injectable({ providedIn: 'root' })
export class DynamicThingService {
  private http = inject(HttpClient);

  byCode(code: string): Observable<Thing> {
    return this.http.get<Thing>(base(code));
  }

  legacyByCode(code: string): Observable<Thing> {
    return this.http.get<Thing>(legacyBase(code));
  }
}
