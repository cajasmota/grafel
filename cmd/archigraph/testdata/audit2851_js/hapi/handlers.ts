// Hapi handler functions live HERE. Endpoint attribution must resolve to
// this file, not to server.ts where they are registered.

export function listBooks(request, h) {
  return [{ id: 1 }];
}

export function getBook(request, h) {
  return { id: request.params.id };
}
