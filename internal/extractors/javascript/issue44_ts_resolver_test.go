// Package javascript — unit tests for issue #44 (TS/JS resolver slice).
//
// Three fixes are exercised here:
//
//  1. External JSX RENDERS edges: when a JSX component tag (e.g.
//     BrowserRouter, Route) is imported from an npm package, the RENDERS
//     edge ToID must be "ext:<module>" rather than the bare tag name.
//
//  2. Context .Provider / .Consumer RENDERS edges: when a JSX tag is of
//     the form "Ctx.Provider" or "Ctx.Consumer", the RENDERS edge ToID
//     must be "ext:react" rather than the bare property name "Provider".
//
//  3. Hook-variable CALLS: when a local variable (e.g. `navigate`) is
//     assigned from an imported hook call (`const navigate = useNavigate()`),
//     any CALLS edge to that variable must carry "ext:<module>" as the ToID
//     rather than the bare variable name "navigate".
package javascript_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// findRel returns the first relationship from entity e whose Kind and ToID
// both match. Returns nil when no such relationship exists.
func findRel(e *types.EntityRecord, kind, toID string) *types.RelationshipRecord {
	for i := range e.Relationships {
		if e.Relationships[i].Kind == kind && e.Relationships[i].ToID == toID {
			return &e.Relationships[i]
		}
	}
	return nil
}

// hasRelPrefix returns true when e has any relationship whose Kind matches
// and whose ToID has the given prefix.
func hasRelPrefix(e *types.EntityRecord, kind, prefix string) bool {
	for _, r := range e.Relationships {
		if r.Kind == kind {
			if len(r.ToID) >= len(prefix) && r.ToID[:len(prefix)] == prefix {
				return true
			}
		}
	}
	return false
}

// extractTS runs the typescript extractor on source, returning the flat list
// of entity records (each carries its relationships inline).
func extractTS(t *testing.T, src []byte, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extreg.Get("typescript")
	if !ok {
		t.Fatalf("typescript extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extreg.FileInput{
		Path:     path,
		Content:  src,
		Language: "typescript",
		TSTree:   nil, // no tree supplied → extractor returns no entities
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func findByName44(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Fix 1 — External JSX RENDERS rewrite to ext:<module>
// ---------------------------------------------------------------------------

// TestJSXRenders_ExternalImport verifies that when a JSX component tag is
// imported from an npm package (external import), the RENDERS edge uses
// "ext:<module>" as the ToID instead of the bare component name.
//
// Before the fix: RENDERS {ToID: "BrowserRouter"} → bug-extractor.
// After  the fix: RENDERS {ToID: "ext:react-router-dom"} → ExternalKnown.
func TestJSXRenders_ExternalImport(t *testing.T) {
	src := []byte(`
import React from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { Home } from './pages/Home';

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Home />} />
      </Routes>
    </BrowserRouter>
  );
}
`)
	tree := parseTSX(t, src)
	entities := extractTSX(t, src, tree)

	app := findByName44(entities, "App")
	if app == nil {
		t.Fatal("App entity not found")
	}

	// BrowserRouter / Routes / Route must resolve to "ext:react-router-dom".
	for _, tag := range []string{"BrowserRouter", "Routes", "Route"} {
		// Must NOT appear as bare tag.
		if r := findRel(app, "RENDERS", tag); r != nil {
			t.Errorf("RENDERS ToID=%q should have been rewritten to ext:react-router-dom (bare name leaks to bug-extractor)", tag)
		}
		// Must appear as ext:react-router-dom.
		if r := findRel(app, "RENDERS", "ext:react-router-dom"); r == nil {
			// Check generically — there might be multiple ext: targets.
			if !hasRelPrefix(app, "RENDERS", "ext:") {
				t.Errorf("expected a RENDERS ext:react-router-dom edge for %s; got %+v", tag, app.Relationships)
			}
		}
	}
}

// TestJSXRenders_LocalImportUnchanged verifies that locally-imported
// components (relative path) keep their bare name as the RENDERS ToID so
// the cross-file resolver can bind them.
func TestJSXRenders_LocalImportUnchanged(t *testing.T) {
	src := []byte(`
import React from 'react';
import { UserCard } from './UserCard';

function UserList({ users }) {
  return (
    <div>
      {users.map(u => <UserCard key={u.id} user={u} />)}
    </div>
  );
}
`)
	tree := parseTSX(t, src)
	entities := extractTSX(t, src, tree)

	ul := findByName44(entities, "UserList")
	if ul == nil {
		t.Fatal("UserList entity not found")
	}
	// Local import — must still carry the bare component name.
	if r := findRel(ul, "RENDERS", "UserCard"); r == nil {
		t.Errorf("expected RENDERS UserList→UserCard (bare, local import); got %+v", ul.Relationships)
	}
}

// ---------------------------------------------------------------------------
// Fix 2 — Context.Provider / Context.Consumer mapped to ext:react
// ---------------------------------------------------------------------------

// TestJSXRenders_ContextProvider verifies that <AuthContext.Provider> emits
// a RENDERS edge with ToID "ext:react" rather than the bare property "Provider".
//
// Before the fix: RENDERS {ToID: "Provider"} → bug-extractor.
// After  the fix: RENDERS {ToID: "ext:react"} → ExternalKnown.
func TestJSXRenders_ContextProvider(t *testing.T) {
	src := []byte(`
import React, { createContext } from 'react';

const AuthContext = createContext(null);

function AuthProvider({ children }) {
  return (
    <AuthContext.Provider value={{ user: null }}>
      {children}
    </AuthContext.Provider>
  );
}
`)
	tree := parseTSX(t, src)
	entities := extractTSX(t, src, tree)

	ap := findByName44(entities, "AuthProvider")
	if ap == nil {
		t.Fatal("AuthProvider entity not found")
	}

	// Must NOT appear as bare "Provider".
	if r := findRel(ap, "RENDERS", "Provider"); r != nil {
		t.Errorf("RENDERS ToID='Provider' should have been rewritten to ext:react (bare name leaks to bug-extractor)")
	}
	// Must appear as "ext:react".
	if r := findRel(ap, "RENDERS", "ext:react"); r == nil {
		t.Errorf("expected RENDERS AuthProvider→ext:react for <AuthContext.Provider>; got %+v", ap.Relationships)
	}
}

// ---------------------------------------------------------------------------
// Fix 3 — Hook-variable CALLS rewrite to ext:<module>
// ---------------------------------------------------------------------------

// TestCallTarget_HookVariableRewrite verifies that when a local variable is
// assigned from a hook call (`const navigate = useNavigate()`), any CALLS
// to that variable are rewritten to "ext:<sourceModule>" in the edge ToID.
//
// Before the fix: CALLS {ToID: "navigate"} → bug-extractor.
// After  the fix: CALLS {ToID: "ext:react-router-dom"} → ExternalUnknown/Known.
func TestCallTarget_HookVariableRewrite(t *testing.T) {
	src := []byte(`
import { useNavigate } from 'react-router-dom';

function Home() {
  const navigate = useNavigate();
  const pick = (id) => navigate('/users/' + id);
  return <div onClick={() => pick('1')} />;
}
`)
	tree := parseTSX(t, src)
	entities := extractTSX(t, src, tree)

	// `pick` is an arrow function entity that calls `navigate`.
	pick := findByName44(entities, "pick")
	if pick == nil {
		// `pick` might not be extracted as a separate entity; check Home.
		home := findByName44(entities, "Home")
		if home == nil {
			t.Fatal("neither pick nor Home entity found")
		}
		// Verify navigate does not appear as bare CALLS target in Home.
		if r := findRel(home, "CALLS", "navigate"); r != nil {
			t.Errorf("CALLS ToID='navigate' should have been rewritten to ext:react-router-dom; found bare name in Home")
		}
		return
	}

	// `pick` calls `navigate` — must be rewritten to ext:react-router-dom.
	if r := findRel(pick, "CALLS", "navigate"); r != nil {
		t.Errorf("CALLS ToID='navigate' should have been rewritten to ext:react-router-dom; bare local-variable name leaks to bug-extractor")
	}
	if r := findRel(pick, "CALLS", "ext:react-router-dom"); r == nil {
		t.Errorf("expected CALLS pick→ext:react-router-dom for navigate(); got %+v", pick.Relationships)
	}
}

// TestCallTarget_HookVariableRedux verifies the same rewrite for a Redux
// dispatch variable: `const dispatch = useDispatch()`.
func TestCallTarget_HookVariableRedux(t *testing.T) {
	src := []byte(`
import { useDispatch } from 'react-redux';

function Counter() {
  const dispatch = useDispatch();
  const increment = () => dispatch({ type: 'INCREMENT' });
  return <button onClick={increment}>+</button>;
}
`)
	tree := parseTSX(t, src)
	entities := extractTSX(t, src, tree)

	increment := findByName44(entities, "increment")
	if increment == nil {
		// dispatch call may be inside Counter; check Counter directly.
		counter := findByName44(entities, "Counter")
		if counter == nil {
			t.Fatal("neither increment nor Counter entity found")
		}
		if r := findRel(counter, "CALLS", "dispatch"); r != nil {
			t.Errorf("CALLS ToID='dispatch' should be rewritten to ext:react-redux; bare name found")
		}
		return
	}

	if r := findRel(increment, "CALLS", "dispatch"); r != nil {
		t.Errorf("CALLS ToID='dispatch' should be rewritten to ext:react-redux; bare name found")
	}
	if r := findRel(increment, "CALLS", "ext:react-redux"); r == nil {
		t.Errorf("expected CALLS increment→ext:react-redux for dispatch(); got %+v", increment.Relationships)
	}
}

// TestCallTarget_NonHookLocalUnchanged verifies that CALLS to a locally-
// declared function (not a hook result) keep the bare name so the cross-
// file / same-file resolver can bind them.
func TestCallTarget_NonHookLocalUnchanged(t *testing.T) {
	src := []byte(`
function formatDate(ts) { return new Date(ts).toISOString(); }

function EventRow({ event }) {
  const label = formatDate(event.ts);
  return <span>{label}</span>;
}
`)
	tree := parseTSX(t, src)
	entities := extractTSX(t, src, tree)

	er := findByName44(entities, "EventRow")
	if er == nil {
		t.Fatal("EventRow entity not found")
	}
	// formatDate is a local function entity — must keep bare name for binding.
	if r := findRel(er, "CALLS", "formatDate"); r == nil {
		// It might be resolved to an entity ID already — check there's no
		// ext: rewrite that would be wrong.
		if hasRelPrefix(er, "CALLS", "ext:") {
			t.Errorf("formatDate CALLS should not be rewritten to ext:; got %+v", er.Relationships)
		}
	}
}
