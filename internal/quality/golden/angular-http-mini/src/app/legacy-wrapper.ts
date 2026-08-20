// TODO(migration): replace this wrapper with Angular's HttpClient.
//
// Negative control for the token gate (#6446). A legacy wrapper carrying a
// migration TODO is the most common shape in a mid-migration Angular codebase,
// and a `strings.Contains` gate is opened by the comment alone. `http` here is a
// plain object; nothing is injected, imported as a value, or type-annotated.
export class LegacyHttpWrapper {
  private http = { get: (_key: string) => null };

  probe(): unknown {
    return this.http.get('/legacy/never-extracted');
  }
}
