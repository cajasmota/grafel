import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';

import { Thing } from './thing.model';

// Idiomatic Angular 16+: the client is obtained with inject(), not through a
// constructor parameter, so the file contains no `HttpService` token at all.
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
