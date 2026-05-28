// Proving fixture for issue #2875 — React Internals framework_specific cells:
//   - lazy_code_splitting: React.lazy + dynamic import() split point.
//   - suspense_error_boundary: <Suspense> boundary + ErrorBoundary class.
//   - portal_recognition: ReactDOM.createPortal target.
//
// Hand-written, dependency-manifest-free (no node_modules / lockfile).
import React, { lazy, Suspense, Component } from 'react';
import { createPortal } from 'react-dom';

// lazy_code_splitting — the dynamic import('./SettingsPanel') is the code-split
// point. The extractor decorates this entity react_lazy + lazy_module.
const SettingsPanel = lazy(() => import('./SettingsPanel'));

// suspense_error_boundary — class error boundary: declares the React contract
// (static getDerivedStateFromError + instance componentDidCatch).
class ErrorBoundary extends Component<{ children: React.ReactNode }, { hasError: boolean }> {
  state = { hasError: false };

  static getDerivedStateFromError() {
    return { hasError: true };
  }

  componentDidCatch(error: Error) {
    console.error(error);
  }

  render() {
    if (this.state.hasError) {
      return <p>Something went wrong.</p>;
    }
    return this.props.children;
  }
}

// suspense_error_boundary — function component renders a <Suspense> boundary.
export function AppShell() {
  return (
    <ErrorBoundary>
      <Suspense fallback={<p>Loading…</p>}>
        <SettingsPanel />
      </Suspense>
    </ErrorBoundary>
  );
}

// portal_recognition — function component renders into a portal via createPortal.
export function Modal({ children }: { children: React.ReactNode }) {
  return createPortal(
    <div className="modal">{children}</div>,
    document.body,
  );
}

// Plain component — must NOT pick up any React Internals markers (negative case).
export function PlainCard() {
  return <div className="card">plain</div>;
}
