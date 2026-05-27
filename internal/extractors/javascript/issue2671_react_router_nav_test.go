// Package javascript_test — issue #2671: react-router-dom v6+ and Next.js
// NAVIGATES_TO extraction. Builds on #2655 / #2658 / #2665 by adding coverage
// for the patterns dominant in upvate-core-frontend (which had 0 navigation
// edges before this change).
//
// Patterns covered here:
//
//   - const navigate = useNavigate(); navigate('/path')         (react-router v6 direct call)
//   - navigate('/path', {state: {foo, bar}})                    (with state object)
//   - navigate(`/users/${id}`)                                  (template literal)
//   - navigate({pathname: '/x', search: '?q=1'})                (object form)
//   - <Link to="/path">, <NavLink to="/path">                   (react-router v6 JSX)
//   - <Navigate to="/path" />, <Redirect to="/path" />          (react-router JSX redirects)
//   - <Link href="/path">                                       (next/link JSX)
//   - useRouter().push('/path')                                 (Next.js — receiver form)
//   - useHistory().push('/path')                                (react-router v5 — receiver form)
package javascript_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// findNavEdges returns every NAVIGATES_TO edge on entity `name`. Empty slice
// when the entity has no such edges or doesn't exist.
func findNavEdges(ents []types.EntityRecord, name string) []types.RelationshipRecord {
	e := findByNameRel(ents, name)
	if e == nil {
		return nil
	}
	out := make([]types.RelationshipRecord, 0, len(e.Relationships))
	for _, r := range e.Relationships {
		if r.Kind == "NAVIGATES_TO" {
			out = append(out, r)
		}
	}
	return out
}

// hasNavEdgeTo reports whether `name` has a NAVIGATES_TO edge to "route:<route>".
func hasNavEdgeTo(ents []types.EntityRecord, name, route string) bool {
	for _, r := range findNavEdges(ents, name) {
		if r.ToID == "route:"+route {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// react-router-dom v6: direct-call navigator (useNavigate)
// ---------------------------------------------------------------------------

// TestRRNav_DirectCall_StringLiteral verifies that the canonical
// `const navigate = useNavigate(); navigate('/inspections')` shape emits a
// NAVIGATES_TO edge with route='/inspections' from the enclosing handler.
func TestRRNav_DirectCall_StringLiteral(t *testing.T) {
	src := `
import { useNavigate } from "react-router-dom";

const InspectionsButton = () => {
  const navigate = useNavigate();
  const onClick = () => {
    navigate('/inspections');
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	if !hasNavEdgeTo(ents, "onClick", "/inspections") {
		t.Fatalf("expected NAVIGATES_TO route:/inspections on onClick; edges=%v",
			findNavEdges(ents, "onClick"))
	}
}

// TestRRNav_DirectCall_TemplateLiteral verifies dynamic routes are
// normalised to the {*} sentinel — matching the Expo Router behaviour.
func TestRRNav_DirectCall_TemplateLiteral(t *testing.T) {
	src := `
import { useNavigate } from "react-router-dom";

const Comp = () => {
  const navigate = useNavigate();
  const goTo = (id) => {
    navigate(` + "`/users/${id}/profile`" + `);
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	if !hasNavEdgeTo(ents, "goTo", "/users/{*}/profile") {
		t.Fatalf("expected NAVIGATES_TO route:/users/{*}/profile on goTo; edges=%v",
			findNavEdges(ents, "goTo"))
	}
}

// TestRRNav_DirectCall_StateParamsKeys verifies that the `state` object on
// `navigate('/path', {state: {a, b}})` populates params_keys as a sorted
// JSON array, identical in shape to the Expo Router params extraction.
func TestRRNav_DirectCall_StateParamsKeys(t *testing.T) {
	src := `
import { useNavigate } from "react-router-dom";

const Comp = () => {
  const navigate = useNavigate();
  const submit = (b, a) => {
    navigate('/done', { state: { b, a } });
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	got := decodeParamsKeys(t, findNavParamsKeys(ents, "submit"))
	want := []string{"a", "b"}
	if len(got) != len(want) {
		t.Fatalf("params_keys: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("params_keys[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRRNav_DirectCall_ObjectForm verifies the navigate({pathname, search})
// shape produces a route='/x' edge and surfaces the option keys.
func TestRRNav_DirectCall_ObjectForm(t *testing.T) {
	src := `
import { useNavigate } from "react-router-dom";

const Comp = () => {
  const navigate = useNavigate();
  const open = () => {
    navigate({ pathname: '/search', search: '?q=foo' });
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	if !hasNavEdgeTo(ents, "open", "/search") {
		t.Fatalf("expected NAVIGATES_TO route:/search on open; edges=%v",
			findNavEdges(ents, "open"))
	}
}

// ---------------------------------------------------------------------------
// react-router-dom v6: JSX components (<Link>, <NavLink>, <Navigate>)
// ---------------------------------------------------------------------------

// TestRRNav_JSX_Link verifies <Link to="/path"> renders a NAVIGATES_TO edge
// on the enclosing component, with via=jsx_nav and tag=Link.
func TestRRNav_JSX_Link(t *testing.T) {
	src := `
import { Link } from "react-router-dom";

const NavBar = () => {
  return (
    <Link to="/dashboard">Dashboard</Link>
  );
};
`
	tree := parseTSX(t, []byte(src))
	ents := extractTSX(t, []byte(src), tree)
	edges := findNavEdges(ents, "NavBar")
	if len(edges) != 1 {
		t.Fatalf("expected exactly one NAVIGATES_TO on NavBar, got %d: %v", len(edges), edges)
	}
	if edges[0].ToID != "route:/dashboard" {
		t.Errorf("expected route:/dashboard, got %q", edges[0].ToID)
	}
	if edges[0].Properties["via"] != "jsx_nav" {
		t.Errorf("expected via=jsx_nav, got %q", edges[0].Properties["via"])
	}
	if edges[0].Properties["tag"] != "Link" {
		t.Errorf("expected tag=Link, got %q", edges[0].Properties["tag"])
	}
}

// TestRRNav_JSX_NavLink verifies <NavLink to="/path"> is also recognised.
func TestRRNav_JSX_NavLink(t *testing.T) {
	src := `
import { NavLink } from "react-router-dom";

const SideBar = () => {
  return (
    <NavLink to="/buildings">Buildings</NavLink>
  );
};
`
	tree := parseTSX(t, []byte(src))
	ents := extractTSX(t, []byte(src), tree)
	if !hasNavEdgeTo(ents, "SideBar", "/buildings") {
		t.Fatalf("expected NAVIGATES_TO route:/buildings on SideBar; edges=%v",
			findNavEdges(ents, "SideBar"))
	}
}

// TestRRNav_JSX_Navigate verifies the <Navigate to="/path" /> redirect
// component (typically used as a route element).
func TestRRNav_JSX_Navigate(t *testing.T) {
	src := `
import { Navigate } from "react-router-dom";

const Protected = ({ ok }) => {
  return ok ? <div /> : <Navigate to="/login" />;
};
`
	tree := parseTSX(t, []byte(src))
	ents := extractTSX(t, []byte(src), tree)
	if !hasNavEdgeTo(ents, "Protected", "/login") {
		t.Fatalf("expected NAVIGATES_TO route:/login on Protected; edges=%v",
			findNavEdges(ents, "Protected"))
	}
}

// TestRRNav_JSX_Redirect verifies the older react-router-dom v5 <Redirect to=...>
// component is also picked up (corpora often still contain mixed v5/v6 usage).
func TestRRNav_JSX_Redirect(t *testing.T) {
	src := `
import { Redirect } from "react-router-dom";

const Old = () => {
  return <Redirect to="/v2/home" />;
};
`
	tree := parseTSX(t, []byte(src))
	ents := extractTSX(t, []byte(src), tree)
	if !hasNavEdgeTo(ents, "Old", "/v2/home") {
		t.Fatalf("expected NAVIGATES_TO route:/v2/home on Old; edges=%v",
			findNavEdges(ents, "Old"))
	}
}

// TestRRNav_JSX_Link_TemplateLiteral verifies a template-literal `to` value
// is normalised through the {*} sentinel.
func TestRRNav_JSX_Link_TemplateLiteral(t *testing.T) {
	src := `
import { Link } from "react-router-dom";

const UserCard = ({ id }) => {
  return <Link to={` + "`/users/${id}`" + `}>View</Link>;
};
`
	tree := parseTSX(t, []byte(src))
	ents := extractTSX(t, []byte(src), tree)
	if !hasNavEdgeTo(ents, "UserCard", "/users/{*}") {
		t.Fatalf("expected NAVIGATES_TO route:/users/{*} on UserCard; edges=%v",
			findNavEdges(ents, "UserCard"))
	}
}

// ---------------------------------------------------------------------------
// Next.js: useRouter().push() + <Link href=...>
// ---------------------------------------------------------------------------

// TestNextNav_RouterPush verifies useRouter().push('/x') — already covered by
// the existing receiver-method path via #2658's navHookVars table, but pinned
// here so a regression in the Next.js shape is caught explicitly.
func TestNextNav_RouterPush(t *testing.T) {
	src := `
import { useRouter } from "next/router";

const Header = () => {
  const router = useRouter();
  const goHome = () => {
    router.push('/');
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	if !hasNavEdgeTo(ents, "goHome", "/") {
		t.Fatalf("expected NAVIGATES_TO route:/ on goHome; edges=%v",
			findNavEdges(ents, "goHome"))
	}
}

// TestNextNav_RouterReplace verifies router.replace('/x') from Next.js.
func TestNextNav_RouterReplace(t *testing.T) {
	src := `
import { useRouter } from "next/router";

const SessionGuard = () => {
  const router = useRouter();
  const bounce = () => {
    router.replace('/login');
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	if !hasNavEdgeTo(ents, "bounce", "/login") {
		t.Fatalf("expected NAVIGATES_TO route:/login on bounce; edges=%v",
			findNavEdges(ents, "bounce"))
	}
}

// TestNextNav_JSX_LinkHref verifies <Link href="/x"> from next/link emits a
// NAVIGATES_TO edge — the prop name differs from react-router's `to`.
func TestNextNav_JSX_LinkHref(t *testing.T) {
	src := `
import Link from "next/link";

const NavBar = () => {
  return (
    <Link href="/about">About</Link>
  );
};
`
	tree := parseTSX(t, []byte(src))
	ents := extractTSX(t, []byte(src), tree)
	edges := findNavEdges(ents, "NavBar")
	if len(edges) != 1 {
		t.Fatalf("expected exactly one NAVIGATES_TO on NavBar, got %d: %v", len(edges), edges)
	}
	if edges[0].ToID != "route:/about" {
		t.Errorf("expected route:/about, got %q", edges[0].ToID)
	}
	if edges[0].Properties["tag"] != "Link" {
		t.Errorf("expected tag=Link, got %q", edges[0].Properties["tag"])
	}
}

// ---------------------------------------------------------------------------
// react-router-dom v5: useHistory()
// ---------------------------------------------------------------------------

// TestRRNav_UseHistory_Push verifies the v5 history.push('/x') pattern is
// recognised via the existing navHookVars receiver-method path.
func TestRRNav_UseHistory_Push(t *testing.T) {
	src := `
import { useHistory } from "react-router-dom";

const Comp = () => {
  const history = useHistory();
  const goNext = () => {
    history.push('/next');
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	if !hasNavEdgeTo(ents, "goNext", "/next") {
		t.Fatalf("expected NAVIGATES_TO route:/next on goNext; edges=%v",
			findNavEdges(ents, "goNext"))
	}
}

// ---------------------------------------------------------------------------
// Negative cases — make sure we don't emit edges for unrelated patterns
// ---------------------------------------------------------------------------

// TestRRNav_NoSpuriousLink verifies a lowercase <a href=...> (HTML intrinsic)
// is NOT picked up as a nav edge — only the PascalCase Link/NavLink/etc.
func TestRRNav_NoSpuriousLink(t *testing.T) {
	src := `
const NavBar = () => {
  return <a href="/dashboard">Dashboard</a>;
};
`
	tree := parseTSX(t, []byte(src))
	ents := extractTSX(t, []byte(src), tree)
	if edges := findNavEdges(ents, "NavBar"); len(edges) != 0 {
		t.Errorf("expected no NAVIGATES_TO edges for <a href=...>, got %v", edges)
	}
}

// TestRRNav_NoSpuriousNavigateBareCall verifies a bare `navigate('/x')` is
// NOT emitted as a nav edge when there is no useNavigate() binding in scope
// (e.g. `navigate` is just a local function name).
func TestRRNav_NoSpuriousNavigateBareCall(t *testing.T) {
	src := `
const Comp = () => {
  const navigate = (path) => console.log(path);
  const go = () => {
    navigate('/foo');
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	if edges := findNavEdges(ents, "go"); len(edges) != 0 {
		t.Errorf("expected no NAVIGATES_TO edges when navigate is a plain local, got %v", edges)
	}
}
