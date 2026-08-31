import { TestBed } from '@angular/core/testing';
import { HttpClientTestingModule } from '@angular/common/http/testing';
import { HttpClient, HttpClientModule } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';

// Negative control for the test-source exclusion (#6446). HttpClientTestingModule
// carries `HttpClient` as a substring, so every Angular spec file opened the old
// substring gate -- and the cross extractor, unlike the engine, had no test-file
// exclusion, so spec scaffolding inflated SCOPE.ExternalEndpoint: the exact metric
// #6433 was reported on.
//
// The in-spec stub service below is written in the same receiver shape as the
// production services on purpose. It is indistinguishable from one by content;
// only the file path says it is scaffolding.
@Injectable()
class StubThingService {
  private http = inject(HttpClient);

  load() {
    return this.http.get<unknown>('/api/spec-never-extracted');
  }
}

describe('ThingService', () => {
  it('stubs the backend', () => {
    TestBed.configureTestingModule({ imports: [HttpClientTestingModule, HttpClientModule] });
    return new StubThingService().load();
  });
});
