// Marble.js routing fixture (#2851). Hand-written, dependency-manifest-free.
import { r } from '@marblejs/http'
import { mapTo } from 'rxjs/operators'

const getUsers$ = r.pipe(
  r.matchPath('/users'),
  r.matchType('GET'),
  r.useEffect(req$ => req$.pipe(mapTo({ body: [] })))
)

const getUser$ = r.pipe(
  r.matchPath('/users/:id'),
  r.matchType('GET'),
  r.useEffect(req$ => req$.pipe(mapTo({ body: {} })))
)

const createUser$ = r.pipe(
  r.matchPath('/users'),
  r.matchType('POST'),
  r.useEffect(req$ => req$.pipe(mapTo({ status: 201 })))
)
