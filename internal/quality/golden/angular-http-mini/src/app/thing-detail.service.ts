import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';

import { Thing, ThingPatch } from './thing.model';

// Constructor injection with an explicit HttpClient type annotation, and a
// template literal whose leading segment is a static path.
@Injectable({ providedIn: 'root' })
export class ThingDetailService {
  constructor(private http: HttpClient) {}

  byId(id: string): Observable<Thing> {
    return this.http.get<Thing>(`/api/things/${id}`);
  }

  rename(id: string, body: ThingPatch): Observable<Thing> {
    return this.http.patch<Thing>(`/api/things/${id}`, body);
  }
}
