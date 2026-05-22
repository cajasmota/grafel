/* ============================================================
   NavRail — left sidebar = PROJECT (group) SWITCHER (#1568).
   56px collapsed, 220px expanded on hover.

   Its sole job is selecting the current project (workspace-switcher
   pattern). It lists the indexed projects from /api/v2/groups, marks
   the current one (from the route :groupId), and switching keeps the
   SAME screen when the route supports it (else falls back to graph).

   The per-screen nav (Graph/Flows/…) now lives in the TopBar tab
   strip — see chrome/screen-tabs.tsx.

   Foot: All-groups / Landing affordance, Add-group, theme toggle.
   ============================================================ */

import { NavLink, useParams, useLocation, useNavigate } from "react-router-dom";
import { Sun, Moon, Home, Plus } from "lucide-react";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/store/use-app-store";
import { useGroups } from "@/hooks/use-groups";
import { SCREEN_SEGMENTS } from "./screens";
import type { GroupHealth } from "@/data/types";

const HEALTH_DOT: Record<GroupHealth, string> = {
  healthy: "var(--success)",
  warning: "var(--warning)",
  degraded: "var(--danger)",
  unindexed: "var(--text-4)",
};

function rowClass(active: boolean) {
  return cn(
    "group/nav relative flex items-center h-9 rounded-md px-2.5 mx-2 gap-3 w-[calc(100%-1rem)]",
    "text-text-2 transition-colors duration-[120ms]",
    active ? "bg-surface text-text shadow-[var(--shadow-1)]" : "hover:bg-surface-2",
  );
}

/** Segment after the group id in the current path (default graph). */
function currentSegment(pathname: string, groupId: string): string {
  const prefix = `/g/${groupId}/`;
  if (!pathname.startsWith(prefix)) return "graph";
  const seg = pathname.slice(prefix.length).split("/")[0] ?? "";
  return seg || "graph";
}

export function NavRail() {
  const { groupId = "demo" } = useParams();
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const theme = useAppStore((s) => s.theme);
  const toggleTheme = useAppStore((s) => s.toggleTheme);
  const { data: groups = [], isLoading } = useGroups();

  // Keep the same screen across a project switch when the route supports it.
  const seg = currentSegment(pathname, groupId);
  const targetScreen = SCREEN_SEGMENTS.has(seg) ? seg : "graph";

  function switchTo(id: string) {
    navigate(`/g/${id}/${targetScreen}`);
  }

  return (
    <aside
      className={cn(
        "group/rail flex flex-col shrink-0 h-full bg-bg-soft border-r border-border",
        "w-14 hover:w-[220px] transition-[width] duration-[180ms] ease-[var(--ease-out)] overflow-hidden",
      )}
    >
      {/* Brand */}
      <div className="flex items-center h-14 px-4 gap-2.5 shrink-0">
        <BrandMark />
        <span className="font-semibold text-text whitespace-nowrap opacity-0 group-hover/rail:opacity-100 transition-opacity">
          archigraph
        </span>
      </div>

      {/* Section label */}
      <div className="px-4 pt-1 pb-1.5 shrink-0">
        <span className="text-[10px] font-semibold uppercase tracking-wider text-text-4 whitespace-nowrap opacity-0 group-hover/rail:opacity-100 transition-opacity">
          Projects
        </span>
      </div>

      {/* Project list */}
      <nav aria-label="Projects" className="flex flex-col gap-0.5 py-0.5 flex-1 min-h-0 ag-scroll">
        {isLoading && (
          <span className="px-3 mx-2 h-9 flex items-center text-text-4 text-md whitespace-nowrap opacity-0 group-hover/rail:opacity-100 transition-opacity">
            Loading…
          </span>
        )}
        {groups.map((g) => {
          const active = g.id === groupId;
          return (
            <button
              key={g.id}
              type="button"
              onClick={() => switchTo(g.id)}
              aria-current={active ? "true" : undefined}
              className={cn(rowClass(active), "text-left")}
              title={g.name}
            >
              <span
                className="size-2.5 rounded-full shrink-0 ml-[3px] mr-[3px]"
                style={{ background: HEALTH_DOT[g.health] }}
                aria-hidden
              />
              <span
                className={cn(
                  "flex-1 whitespace-nowrap text-md truncate opacity-0 group-hover/rail:opacity-100 transition-opacity",
                  active && "font-medium",
                )}
              >
                {g.name}
              </span>
            </button>
          );
        })}
      </nav>

      {/* Foot */}
      <div className="mt-auto flex flex-col gap-0.5 py-2 shrink-0 border-t border-border">
        <NavLink to="/" className={({ isActive }) => rowClass(isActive)} title="All groups / Landing">
          <Home size={18} className="shrink-0 ml-px" />
          <span className="flex-1 whitespace-nowrap text-md opacity-0 group-hover/rail:opacity-100 transition-opacity">
            All groups
          </span>
        </NavLink>

        <NavLink to="/?new=1" className={rowClass(false)} title="Add group">
          <Plus size={18} className="shrink-0 ml-px" />
          <span className="flex-1 whitespace-nowrap text-md opacity-0 group-hover/rail:opacity-100 transition-opacity">
            Add group
          </span>
        </NavLink>

        <button className={rowClass(false)} onClick={toggleTheme} title={theme === "dark" ? "Light mode" : "Dark mode"}>
          {theme === "dark" ? <Sun size={18} className="shrink-0 ml-px" /> : <Moon size={18} className="shrink-0 ml-px" />}
          <span className="flex-1 text-left whitespace-nowrap text-md opacity-0 group-hover/rail:opacity-100 transition-opacity">
            {theme === "dark" ? "Light" : "Dark"} mode
          </span>
        </button>
      </div>
    </aside>
  );
}

function BrandMark() {
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" className="shrink-0" aria-hidden>
      <defs>
        <linearGradient id="ag-lg" x1="0" x2="1" y1="0" y2="1">
          <stop offset="0" stopColor="var(--accent)" />
          <stop offset="1" stopColor="var(--accent-strong)" />
        </linearGradient>
      </defs>
      <circle cx="6" cy="6" r="2.6" fill="url(#ag-lg)" />
      <circle cx="18" cy="6" r="2.0" fill="var(--accent)" opacity=".7" />
      <circle cx="12" cy="18" r="2.6" fill="var(--accent-strong)" />
      <path d="M7.6 7.6l3 8M16 7.6l-3 8M8 6h8" stroke="var(--accent)" strokeWidth="1.4" fill="none" />
    </svg>
  );
}
