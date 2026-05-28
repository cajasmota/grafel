// audit2853_js — Marble.js slice. use(...) middleware effects piped into route.
import { r } from '@marblejs/http'

const getMe$ = r.pipe(
  r.matchPath('/marble/me'),
  r.matchType('GET'),
  r.use(logger$),
  r.use(validate$),
  r.useEffect((req$) => req$),
)

const getStatus$ = r.pipe(
  r.matchPath('/marble/status'),
  r.matchType('GET'),
  r.useEffect((req$) => req$),
)
