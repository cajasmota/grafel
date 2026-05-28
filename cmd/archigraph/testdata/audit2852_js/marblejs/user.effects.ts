// Marble.js auth corpus file (#2852 real-data verification).
import { r } from '@marblejs/http'

const getMe$ = r.pipe(
  r.matchPath('/me'),
  r.matchType('GET'),
  use(authorize$),
  r.useEffect(req$ => req$),
)

const getStatus$ = r.pipe(
  r.matchPath('/status'),
  r.matchType('GET'),
  r.useEffect(req$ => req$),
)
