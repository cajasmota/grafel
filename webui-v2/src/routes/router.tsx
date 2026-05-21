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
import TopologyScreen from "./topology";
import PathsScreen from "./paths";
import DocsScreen from "./docs";
import SettingsScreen from "./settings";
import PendingScreen from "./pending";
import OperationsScreen from "./operations";

export const router = createBrowserRouter([
  { path: "/", element: <Landing /> },
  { path: "/showcase", element: <PrimitivesShowcase /> },
  {
    path: "/g/:groupId",
    element: <AppShell />,
    children: [
      { index: true, element: <GraphScreen />, handle: { surfaceLabel: "Graph" } },
      { path: "graph", element: <GraphScreen />, handle: { surfaceLabel: "Graph" } },
      { path: "flows", element: <FlowsScreen />, handle: { surfaceLabel: "Flows" } },
      { path: "topology", element: <TopologyScreen />, handle: { surfaceLabel: "Topology" } },
      { path: "paths", element: <PathsScreen />, handle: { surfaceLabel: "Paths" } },
      { path: "docs", element: <DocsScreen />, handle: { surfaceLabel: "Docs" } },
      { path: "settings", element: <SettingsScreen />, handle: { surfaceLabel: "Group settings" } },
      { path: "pending", element: <PendingScreen />, handle: { surfaceLabel: "Pending" } },
      { path: "operations", element: <OperationsScreen />, handle: { surfaceLabel: "Operations" } },
    ],
  },
  { path: "*", element: <NotFound /> },
]);
