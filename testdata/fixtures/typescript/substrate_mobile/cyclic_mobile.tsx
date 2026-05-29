// Cyclic dependency fixture for module_cycle_detection proving test (#3059).
// This file imports from substrate_mobile.tsx which imports from this file,
// forming a deliberate cycle to prove module_cycle_detection fires on mobile.
import { formatLabel } from './substrate_mobile';

export function cyclic_dep() {
  return formatLabel('test', 1);
}
