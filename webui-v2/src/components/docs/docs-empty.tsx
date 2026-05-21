/* ============================================================
   docs-empty.tsx — Right-pane empty state.
   Shown when no entity is selected (no :entityId in URL).
   ============================================================ */

import { BookOpen } from "lucide-react";

export function DocsEmpty() {
  return (
    <div className="flex flex-col items-center justify-center h-full gap-3 px-6 text-center">
      <span className="text-text-4" aria-hidden="true">
        <BookOpen size={32} strokeWidth={1.25} />
      </span>
      <h2 className="text-base font-medium text-text">Pick an entity</h2>
      <p className="text-sm text-text-3 max-w-xs">
        Browse the index on the left or search by name above. Each entity gets a
        page with its signature, parameters, callers, and dependencies.
      </p>
    </div>
  );
}
