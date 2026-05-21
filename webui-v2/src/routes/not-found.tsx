/* ============================================================
   NotFound — shared error shell (404 + status pages).
   Scaffold only; daemon-down / indexer-failed / upgrading / offline
   variants land in the errors ticket per docs/screens/errors.md.
   ============================================================ */

import { Link } from "react-router-dom";
import { Button } from "@/components/ui";

export default function NotFound() {
  return (
    <div className="h-full grid place-items-center bg-bg">
      <div className="text-center">
        <p className="font-mono text-text-3 text-md">404</p>
        <h1 className="mt-2 text-2xl font-semibold text-text">Page not found</h1>
        <p className="mt-1 text-md text-text-3">The surface you’re looking for doesn’t exist.</p>
        <Button asChild className="mt-5">
          <Link to="/">Back to all groups</Link>
        </Button>
      </div>
    </div>
  );
}
