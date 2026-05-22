/* ============================================================
   ScreenTabs — horizontal segmented nav of the per-project screens
   (Graph · Flows · Topology · Paths · Docs · Operations · Pending).
   Lives in the TopBar, scoped to the current project (#1568).

   The active screen is derived from the URL segment after the
   group id, so it stays correct on deep links (e.g. /docs/<key>).
   ============================================================ */

import { NavLink, useParams, useLocation } from "react-router-dom";
import { cn } from "@/lib/utils";
import { SCREENS } from "./screens";

function activeSegment(pathname: string, groupId: string): string {
  // /g/:groupId/<segment>/...  → <segment> (default: graph)
  const prefix = `/g/${groupId}/`;
  if (!pathname.startsWith(prefix)) return "graph";
  const rest = pathname.slice(prefix.length);
  const seg = rest.split("/")[0] ?? "";
  return seg || "graph";
}

export function ScreenTabs() {
  const { groupId = "demo" } = useParams();
  const { pathname } = useLocation();
  const base = `/g/${groupId}`;
  const current = activeSegment(pathname, groupId);

  return (
    <nav
      aria-label="Project screens"
      className="flex items-center gap-0.5 overflow-x-auto"
    >
      {SCREENS.map(({ to, label, Icon }) => {
        const isActive = current === to;
        return (
          <NavLink
            key={to}
            to={`${base}/${to}`}
            aria-current={isActive ? "page" : undefined}
            className={cn(
              "group/tab inline-flex items-center gap-2 h-8 px-3 rounded-md text-md whitespace-nowrap",
              "transition-colors duration-[120ms]",
              isActive
                ? "bg-surface text-text shadow-[var(--shadow-1)]"
                : "text-text-3 hover:bg-surface-2 hover:text-text-2",
            )}
          >
            <Icon size={15} className="shrink-0" />
            {label}
          </NavLink>
        );
      })}
    </nav>
  );
}
