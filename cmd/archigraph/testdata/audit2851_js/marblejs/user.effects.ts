// Marble.js EffectRoutes. Handler effects are defined inline in this file.
import { r } from "@marblejs/http";
import { mapTo } from "rxjs/operators";

export const getUsers$ = r.pipe(
  r.matchPath("/users"),
  r.matchType("GET"),
  r.useEffect((req$) => req$.pipe(mapTo({ body: [] })))
);

export const getUser$ = r.pipe(
  r.matchPath("/users/:id"),
  r.matchType("GET"),
  r.useEffect((req$) => req$.pipe(mapTo({ body: {} })))
);

export const createUser$ = r.pipe(
  r.matchPath("/users"),
  r.matchType("POST"),
  r.useEffect((req$) => req$.pipe(mapTo({ status: 201 })))
);
