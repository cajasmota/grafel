/* ============================================================
   chrome/screens.ts — the per-project screen registry.

   Single source of truth for the screens that live inside a
   project (group). Consumed by the TopBar tab strip (#1568) and
   the command palette's Navigate group. The left sidebar is the
   PROJECT switcher and no longer lists these.
   ============================================================ */

import {
  Network,
  Workflow,
  Radio,
  Route as RouteIcon,
  FileText,
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

/** Primary screens — shown as top tabs. */
export const SCREENS: ScreenDef[] = [
  { to: "graph", label: "Graph", Icon: Network, shortcut: "G" },
  { to: "flows", label: "Flows", Icon: Workflow, shortcut: "F" },
  { to: "topology", label: "Topology", Icon: Radio, shortcut: "T" },
  { to: "paths", label: "Paths", Icon: RouteIcon, shortcut: "P" },
  { to: "docs", label: "Docs", Icon: FileText, shortcut: "D" },
  { to: "operations", label: "Operations", Icon: Wrench, shortcut: "O" },
  { to: "pending", label: "Pending", Icon: Inbox, shortcut: "I" },
];

/** Settings lives off the tab strip (gear in the breadcrumb bar). */
export const SETTINGS_SCREEN: ScreenDef = {
  to: "settings",
  label: "Group settings",
  Icon: Settings,
  shortcut: ",",
};

/** Set of route segments that a project switch can preserve. */
export const SCREEN_SEGMENTS = new Set(SCREENS.map((s) => s.to));
