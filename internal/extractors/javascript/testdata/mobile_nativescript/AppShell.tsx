// React-NativeScript fixture (#2859) — the React flavor of NativeScript
// (`react-nativescript`) reuses React's context + HOC primitives, which the
// framework-agnostic extractor already covers. Proves for NativeScript:
//   - Structure/context_extraction      → createContext()
//   - Structure/hoc_wrapper_recognition → withOrientation() HOC + memo()
import { createContext, memo } from 'react';
import { withOrientation } from 'react-nativescript';

// context_extraction
export const DeviceContext = createContext({ orientation: 'portrait' });

function ShellRoot() {
  return null;
}

// hoc_wrapper_recognition — NativeScript orientation HOC + React memo.
export const OrientationShell = withOrientation(ShellRoot);
export const MemoShell = memo(ShellRoot);
