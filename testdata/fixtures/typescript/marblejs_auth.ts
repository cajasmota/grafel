// Marble.js auth_coverage fixture (#2852). Hand-written.
// Auth is an Effect (`authorize$`) piped into the route via `use(...)`.
import { r } from '@marblejs/http'

const getMe$ = r.pipe(
  r.matchPath('/me'),
  r.matchType('GET'),
  use(authorize$),
  r.useEffect(req$ => req$),
)

// Public route — no authorize$ in the pipe.
const getStatus$ = r.pipe(
  r.matchPath('/status'),
  r.matchType('GET'),
  r.useEffect(req$ => req$),
)
