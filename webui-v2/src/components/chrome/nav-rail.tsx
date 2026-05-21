/* ============================================================
   NavRail — left sidebar. 56px collapsed, 220px expanded on hover.
   (prototype `.ag-sidebar`, stack-guide → <NavRail>)

   Brand mark at top, nav items in the middle, divider + Pending,
   then theme toggle / All-groups / group selector at the foot.
   Active row = filled surface card (no left accent bar).
   ============================================================ */

import { NavLink, useParams } from "react-router-dom";
import {
  Network,
  Workflow,
  Radio,
  Route as RouteIcon,
  FileText,
  Settings,
  Inbox,
  Sun,
  Moon,
  Home,
  ChevronDown,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Kbd } from "@/components/ui";
import { useAppStore } from "@/store/use-app-store";

interface NavItem {
  to: string;
  label: string;
  Icon: typeof Network;
  shortcut: string;
}

const NAV_ITEMS: NavItem[] = [
  { to: "graph", label: "Graph", Icon: Network, shortcut: "G" },
  { to: "flows", label: "Flows", Icon: Workflow, shortcut: "F" },
  { to: "topology", label: "Topology", Icon: Radio, shortcut: "T" },
  { to: "paths", label: "Paths", Icon: RouteIcon, shortcut: "P" },
  { to: "docs", label: "Docs", Icon: FileText, shortcut: "D" },
  { to: "settings", label: "Group settings", Icon: Settings, shortcut: "," },
];

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

      {/* Nav */}
      <nav className="flex flex-col gap-0.5 py-1">
        {NAV_ITEMS.map(({ to, label, Icon, shortcut }) => (
          <NavLink key={to} to={`${base}/${to}`} className={({ isActive }) => rowClass(isActive)} title={label}>
            <Icon size={18} className="shrink-0" />
            <span className="flex-1 whitespace-nowrap text-md opacity-0 group-hover/rail:opacity-100 transition-opacity">
              {label}
            </span>
            <Kbd className="opacity-0 group-hover/rail:opacity-100 transition-opacity">{shortcut}</Kbd>
          </NavLink>
        ))}

        <div className="my-1.5 mx-3 border-t border-border" />

        <NavLink to={`${base}/pending`} className={({ isActive }) => rowClass(isActive)} title="Pending suggestions">
          <Inbox size={18} className="shrink-0" />
          <span className="flex-1 whitespace-nowrap text-md opacity-0 group-hover/rail:opacity-100 transition-opacity">
            Pending
          </span>
          <span className="inline-flex items-center justify-center min-w-[18px] h-[18px] px-1 rounded-full bg-accent text-accent-text text-[10px] tabular-nums">
            12
          </span>
        </NavLink>
      </nav>

      {/* Foot */}
      <div className="mt-auto flex flex-col gap-0.5 py-2">
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

        <button className={rowClass(false)} title="Switch group">
          <span className="size-2.5 rounded-full bg-accent shrink-0 ml-[3px] mr-[3px]" aria-hidden />
          <span className="flex-1 text-left whitespace-nowrap font-medium text-text opacity-0 group-hover/rail:opacity-100 transition-opacity">
            {groupId}
          </span>
          <ChevronDown size={14} className="opacity-0 group-hover/rail:opacity-100 transition-opacity" />
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
