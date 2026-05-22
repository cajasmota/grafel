/* ============================================================
   NotFound — wildcard fallback route (`*`).

   Delegates to the full ErrorPage component (errors.tsx) with the
   notFound variant. Kept as a thin re-export so the router import
   stays consistent with the previous scaffold.
   ============================================================ */

import ErrorPage from "./errors";

export default function NotFound() {
  return <ErrorPage variant="notFound" />;
}
