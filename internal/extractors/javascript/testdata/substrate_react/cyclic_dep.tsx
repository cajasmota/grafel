// Cyclic-import partner for UserDashboard.tsx.
// Proves module_cycle_detection: UserDashboard imports from ./cyclic_dep AND
// cyclic_dep imports from ./UserDashboard — a deliberate 2-node import cycle.
// The module_cycle_pass (Tarjan SCC over IMPORTS edges) will surface this.
import { formatDisplayName } from "./UserDashboard";

export function useSharedState() {
  return { display: formatDisplayName("React", "User") };
}
