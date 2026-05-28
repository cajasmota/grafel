// Ionic React fixture (#2859) — Ionic builds on React under the hood, so the
// framework-agnostic JS/TS extractor already covers its Structure / Data Flow /
// Lifecycle capabilities. This fixture proves:
//   - Structure/context_extraction      → createContext() → SCOPE.Component subtype="context"
//   - Structure/hoc_wrapper_recognition → withIonLifeCycle() / memo() HOC → SCOPE.Operation
//   - Data Flow/state_management        → useState [value, setter] tuple
//   - Data Flow/branch_conditions       → discriminator comparisons in a body
//   - Lifecycle/state_setter_emission   → setter element subtype="state_setter"
import { createContext, useState, memo } from 'react';
import { IonPage, IonContent } from '@ionic/react';
import { withIonLifeCycle } from '@ionic/react';

// context_extraction
export const SessionContext = createContext(null);

// state_management + state_setter_emission
function SessionPanel() {
  const [authState, setAuthState] = useState('anonymous');
  const [retries, setRetries] = useState(0);

  // branch_conditions — discriminator comparisons on a bare identifier
  function classify() {
    if (authState === 'authenticated') {
      return 1;
    }
    if (retries === 3) {
      setAuthState('locked');
    }
    return 0;
  }

  return (
    <IonPage>
      <IonContent onClick={() => setRetries(retries + 1)}>{classify()}</IonContent>
    </IonPage>
  );
}

// hoc_wrapper_recognition — Ionic lifecycle HOC and React memo both produce
// a wrapped component bound to a name.
const LifecyclePanel = withIonLifeCycle(SessionPanel);
export const MemoPanel = memo(SessionPanel);

export default LifecyclePanel;
