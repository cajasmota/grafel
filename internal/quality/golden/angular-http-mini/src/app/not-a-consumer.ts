// Negative control. `http` is an extremely common member name; on its own it is
// not evidence of an HTTP client. This file carries no Angular / NestJS client
// class token, so its `this.http.get(...)` must stay unextracted.
export class LocalCache {
  private http = { get: (_key: string) => null };

  probe(): unknown {
    return this.http.get('/cache/never-extracted');
  }
}
