/* ============================================================
   AppShell — the shared chrome layout (Lego layer: screen layouts).

   NavRail (left) + TopBar (top) + scrollable <Outlet/> content area.
   Every in-group screen renders inside this shell. The surface label
   is derived from the route handle so screens stay declarative.
   ============================================================ */

import { Outlet, useParams, useMatches } from "react-router-dom";
import { NavRail } from "@/components/chrome/nav-rail";
import { TopBar } from "@/components/chrome/top-bar";

interface RouteHandle {
  surfaceLabel?: string;
}

export function AppShell() {
  const { groupId = "demo" } = useParams();
  const matches = useMatches();
  const surfaceLabel =
    [...matches].reverse().map((m) => (m.handle as RouteHandle | undefined)?.surfaceLabel).find(Boolean) ?? "Graph";

  return (
    <div className="flex h-full w-full">
      <NavRail />
      <div className="flex flex-col flex-1 min-w-0">
        <TopBar group={groupId} surfaceLabel={surfaceLabel} />
        <main className="flex-1 min-h-0 ag-scroll bg-bg">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
