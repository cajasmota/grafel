// Remix server-only module (.server suffix) — stripped from client bundle.
export function getUserSession(request: Request) {
  return request.headers.get('Cookie')
}
