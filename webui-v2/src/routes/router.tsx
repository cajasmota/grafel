/* ============================================================
   router.tsx — route table. One placeholder route per screen so the
   screen tickets slot straight in. In-group screens nest under the
   AppShell at /g/:groupId/*; landing + showcase + errors are top-level.

   To add a screen: create routes/<screen>.tsx (default-export a
   component), then add one <Route> below with a `handle.surfaceLabel`.
   ============================================================ */

import { createBrowserRouter } from "react-router-dom";
import { AppShell } from "@/layouts/app-shell";

import Landing from "./landing";
import NotFound from "./not-found";
import PrimitivesShowcase from "@/components/showcase/primitives-showcase";

import GraphScreen from "./graph";
import FlowsScreen from "./flows";
import EventFlowsScreen from "./event-flows";
import TopologyScreen from "./topology";
import PathsScreen from "./paths";
import DocsScreen from "./docs";
import SecurityScreen from "./security";
import SettingsScreen from "./settings";
import PendingScreen from "./pending";
import OperationsScreen from "./operations";
// PH5 (#2093): graph diff compare view
import CompareScreen from "./compare";

// Errors screen — #1443, epic #1432
import { NotFoundPage, GroupGonePage, DaemonDownPage, UpgradingPage, AppErrorPage } from "./errors";

export const router = createBrowserRouter([
  {
    path: "/",
    errorElement: <AppErrorPage />,
    children: [
      { index: true, element: <Landing /> },
      { path: "showcase", element: <PrimitivesShowcase /> },
      {
        path: "/g/:groupId",
        element: <AppShell />,
        errorElement: <AppErrorPage />,
        children: [
          { index: true, element: <GraphScreen />, handle: { surfaceLabel: "Graph" } },
          { path: "graph", element: <GraphScreen />, handle: { surfaceLabel: "Graph" } },
          { path: "flows", element: <FlowsScreen />, handle: { surfaceLabel: "Flows" } },
          { path: "event-flows", element: <EventFlowsScreen />, handle: { surfaceLabel: "Event Flows" } },
          { path: "topology", element: <TopologyScreen />, handle: { surfaceLabel: "Topology" } },
          { path: "paths", element: <PathsScreen />, handle: { surfaceLabel: "Paths" } },
          { path: "docs", element: <DocsScreen />, handle: { surfaceLabel: "Docs" } },
          // Wildcard: the doc key (repoSlug/rel/path.md) may contain slashes.
          { path: "docs/*", element: <DocsScreen />, handle: { surfaceLabel: "Docs" } },
          { path: "security", element: <SecurityScreen />, handle: { surfaceLabel: "Security" } },
          { path: "settings", element: <SettingsScreen />, handle: { surfaceLabel: "Group settings" } },
          { path: "pending", element: <PendingScreen />, handle: { surfaceLabel: "Pending" } },
          { path: "operations", element: <OperationsScreen />, handle: { surfaceLabel: "Operations" } },
          // PH5 (#2093): graph diff compare view
          { path: "compare", element: <CompareScreen />, handle: { surfaceLabel: "Compare" } },
          // Errors — in-group variants (full chrome provided by AppShell)
          { path: "missing", element: <GroupGonePage />, handle: { surfaceLabel: "Group not found" } },
          { path: "error/daemon-down", element: <DaemonDownPage />, handle: { surfaceLabel: "Daemon unreachable" } },
          { path: "error/upgrading", element: <UpgradingPage />, handle: { surfaceLabel: "Upgrading" } },
        ],
      },
      // Errors — top-level routes (minimal chrome, no group context)
      { path: "/error/404", element: <NotFoundPage /> },
      { path: "*", element: <NotFound /> },
    ],
  },
]);
