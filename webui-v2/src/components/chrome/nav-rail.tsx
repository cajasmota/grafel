/* ============================================================
   NavRail — left sidebar = SCREEN nav. 56px collapsed, 220px
   expanded on hover. (prototype `.ag-sidebar`, stack-guide → <NavRail>)

   This is the PRIMARY navigation: it navigates the screens of the
   CURRENT project (Graph · Topology · Paths · Flows · Docs ·
   Operations · Pending · Settings). The active screen is the
   highlighted row. Screens come from the chrome/screens.ts registry.

   Brand mark at top, screen nav in the middle, divider + Pending,
   then theme toggle / All-groups / Settings at the foot.
   Active row = filled surface card (no left accent bar).

   The PROJECT (group) switcher lives in the TopBar (top-right) —
   the rail no longer switches projects (#1572).
   ============================================================ */

import { NavLink, useParams } from "react-router-dom";
import { Sun, Moon, Home } from "lucide-react";
import { cn } from "@/lib/utils";
import { Kbd } from "@/components/ui";
import { useAppStore } from "@/store/use-app-store";
import { usePendingCount } from "@/hooks/use-pending";
import { SCREENS, PENDING_SCREEN, SETTINGS_SCREEN } from "./screens";
import { WorktreeList } from "./worktree-list";
import { MonorepoModuleList } from "./monorepo-module-list";

function rowClass(active: boolean) {
  return cn(
    "group/nav relative flex items-center h-9 rounded-md px-2.5 mx-2 gap-3",
    "text-text-2 transition-colors duration-[120ms]",
    active ? "bg-surface text-text shadow-[var(--shadow-1)]" : "hover:bg-surface-2",
  );
}

export function NavRail() {
  const { groupId = "demo" } = useParams();
  const theme = useAppStore((s) => s.theme);
  const toggleTheme = useAppStore((s) => s.toggleTheme);
  const base = `/g/${groupId}`;

  const { Icon: PendingIcon } = PENDING_SCREEN;
  const { Icon: SettingsIcon } = SETTINGS_SCREEN;
  const pendingCount = usePendingCount(groupId);

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
          Grafel
        </span>
      </div>

      {/* Screen nav — the scrollable middle (#5614). Brand (above) and the
          foot (below) stay pinned; this list scrolls when worktree/module
          subtrees overflow a short viewport. `min-h-0` lets a flex child shrink
          below its content height so overflow actually kicks in. */}
      <nav
        aria-label="Screens"
        className="flex flex-col gap-0.5 py-1 flex-1 min-h-0 overflow-y-auto ag-scroll-mac"
      >
        {SCREENS.map(({ to, label, Icon, shortcut }) => (
          <NavLink key={to} to={`${base}/${to}`} className={({ isActive }) => rowClass(isActive)} title={label}>
            <Icon size={18} className="shrink-0" />
            <span className="flex-1 whitespace-nowrap text-md opacity-0 group-hover/rail:opacity-100 transition-opacity">
              {label}
            </span>
            <Kbd className="opacity-0 group-hover/rail:opacity-100 transition-opacity">{shortcut}</Kbd>
          </NavLink>
        ))}

        <div className="my-1.5 mx-3 border-t border-border" />

        <NavLink to={`${base}/${PENDING_SCREEN.to}`} className={({ isActive }) => rowClass(isActive)} title="Pending suggestions">
          <PendingIcon size={18} className="shrink-0" />
          <span className="flex-1 whitespace-nowrap text-md opacity-0 group-hover/rail:opacity-100 transition-opacity">
            {PENDING_SCREEN.label}
          </span>
          {pendingCount > 0 && (
            <span className="inline-flex items-center justify-center min-w-[18px] h-[18px] px-1 rounded-full bg-accent text-accent-text text-[10px] tabular-nums">
              {pendingCount}
            </span>
          )}
        </NavLink>

        {/* PH4: worktree subtree — visible when group has worktree refs */}
        <WorktreeList groupId={groupId} />

        {/* M3: monorepo module tree — visible when group has repos with declared modules */}
        <MonorepoModuleList groupId={groupId} />
      </nav>

      {/* Foot — pinned below the scrollable nav (#5614) */}
      <div className="shrink-0 flex flex-col gap-0.5 py-2">
        <NavLink to={`${base}/${SETTINGS_SCREEN.to}`} className={({ isActive }) => rowClass(isActive)} title={SETTINGS_SCREEN.label}>
          <SettingsIcon size={18} className="shrink-0" />
          <span className="flex-1 whitespace-nowrap text-md opacity-0 group-hover/rail:opacity-100 transition-opacity">
            {SETTINGS_SCREEN.label}
          </span>
        </NavLink>

        <button className={rowClass(false)} onClick={toggleTheme} title={theme === "dark" ? "Light mode" : "Dark mode"}>
          {theme === "dark" ? <Sun size={18} className="shrink-0" /> : <Moon size={18} className="shrink-0" />}
          <span className="flex-1 text-left whitespace-nowrap text-md opacity-0 group-hover/rail:opacity-100 transition-opacity">
            {theme === "dark" ? "Light" : "Dark"} mode
          </span>
        </button>

        <NavLink to="/" className={rowClass(false)} title="All groups">
          <Home size={18} className="shrink-0" />
          <span className="flex-1 whitespace-nowrap text-md opacity-0 group-hover/rail:opacity-100 transition-opacity">
            All groups
          </span>
        </NavLink>
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
