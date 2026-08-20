import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';

// A statically-known RELATIVE URL, served behind a baseHref — routine in
// Angular. The path is right there in the source; the only reason the static
// pass declines it is the missing leading slash, so calling it "dynamic" would
// claim unresolvable about a URL the pass is holding (#6446).
//
// Also exercises a NESTED generic argument: Record<string, string> matched
// nothing at all before #6446, not even the dynamic marker.
@Injectable({ providedIn: 'root' })
export class I18nService {
  private http = inject(HttpClient);

  strings(): Observable<Record<string, string>> {
    return this.http.get<Record<string, string>>('assets/i18n/en.json');
  }
}
