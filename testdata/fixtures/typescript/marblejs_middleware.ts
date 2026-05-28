// Marble.js middleware_coverage fixture (#2853). Hand-written, manifest-free.
// Marble pipes middleware effects into a route Effect via use(...).
import { r } from '@marblejs/http'

const getMe$ = r.pipe(
  r.matchPath('/me'),
  r.matchType('GET'),
  r.use(logger$),
  r.use(validate$),
  r.useEffect((req$) => req$),
)

const getStatus$ = r.pipe(
  r.matchPath('/status'),
  r.matchType('GET'),
  r.useEffect((req$) => req$),
)
