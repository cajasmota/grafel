/* ============================================================
   chrome/screens.ts — the per-project screen registry.

   Single source of truth for the screens that live inside a
   project (group). Consumed by the LEFT SIDEBAR (nav-rail) — the
   PRIMARY screen nav — and by the project switcher to preserve
   the current screen across a project switch (#1572).

   The left sidebar lists these screens for the current project;
   the top-right control is the PROJECT switcher.
   ============================================================ */

import {
  Network,
  Workflow,
  Radio,
  Route as RouteIcon,
  FileText,
  ShieldCheck,
  Wrench,
  Inbox,
  Settings,
} from "lucide-react";

export interface ScreenDef {
  /** Route segment under /g/:groupId/. */
  to: string;
  label: string;
  Icon: typeof Network;
  shortcut: string;
}

/** Primary screens — shown in the left sidebar rail. */
export const SCREENS: ScreenDef[] = [
  { to: "graph", label: "Graph", Icon: Network, shortcut: "G" },
  { to: "topology", label: "Topology", Icon: Radio, shortcut: "T" },
  { to: "paths", label: "Paths", Icon: RouteIcon, shortcut: "P" },
  { to: "flows", label: "Flows", Icon: Workflow, shortcut: "F" },
  { to: "docs", label: "Docs", Icon: FileText, shortcut: "D" },
  { to: "security", label: "Security", Icon: ShieldCheck, shortcut: "S" },
  { to: "operations", label: "Operations", Icon: Wrench, shortcut: "O" },
];

/** Pending lives below the divider in the rail (carries a badge). */
export const PENDING_SCREEN: ScreenDef = {
  to: "pending",
  label: "Pending",
  Icon: Inbox,
  shortcut: "I",
};

/** Settings — foot of the rail. */
export const SETTINGS_SCREEN: ScreenDef = {
  to: "settings",
  label: "Group settings",
  Icon: Settings,
  shortcut: ",",
};

/** Set of route segments that a project switch can preserve. */
export const SCREEN_SEGMENTS = new Set(
  [...SCREENS, PENDING_SCREEN, SETTINGS_SCREEN].map((s) => s.to),
);
