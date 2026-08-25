package engine

import "testing"

// #6555: deriveOwningBackend degraded to naming a source-root directory
// ("app", "src") in a single-service repository, by two independent
// mechanisms. Each test below observes one mechanism and is red when the
// *other* half of the fix is applied alone, so neither half is unobserved.
//
// Mechanism 1 — manifests and framework markers were merged into one set in
// directoryHasManifest, so precedence was decided purely by depth: an
// `app/main.py` beat a real `pyproject.toml` above it.
//
// Mechanism 2 — the walk broke at "." *before* testing it, so a manifest at
// the repository root was never a candidate and the top-segment fallback ran.
//
// The repository root is not a service boundary that has a name reachable from
// a repo-relative path (filepath.Base(".") is "."), so a manifest winning at
// "." yields "" and lets the consumers' existing fallbacks run.

// TestDeriveOwningBackend_MarkerBelowManifest_6555 pins mechanism 1: a
// framework marker deeper in the tree must not beat a manifest above it.
// Red if manifests and markers are scanned as one depth-ordered set.
func TestDeriveOwningBackend_MarkerBelowManifest_6555(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root,
		"pyproject.toml",
		"app/main.py",
		"app/api/endpoints/notifications.py",
	)
	t.Chdir(root)

	const handler = "app/api/endpoints/notifications.py"
	got := deriveOwningBackend(handler)
	if got == "app" {
		t.Errorf("deriveOwningBackend(%q) = %q: the app/main.py marker beat the root pyproject.toml", handler, got)
	}
	if got != "" {
		t.Errorf("deriveOwningBackend(%q) = %q, want %q (manifest wins at the repository root)", handler, got, "")
	}
}

// TestDeriveOwningBackend_RootManifest_6555 pins mechanism 2: a manifest at
// the repository root is a candidate. Red if the walk breaks at "." before
// testing it.
func TestDeriveOwningBackend_RootManifest_6555(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root,
		"pyproject.toml",
		"src/service/api/routes.py",
	)
	t.Chdir(root)

	const handler = "src/service/api/routes.py"
	got := deriveOwningBackend(handler)
	if got == "src" {
		t.Errorf("deriveOwningBackend(%q) = %q: the root pyproject.toml was never tested, so the top-segment fallback ran", handler, got)
	}
	if got != "" {
		t.Errorf("deriveOwningBackend(%q) = %q, want %q (manifest wins at the repository root)", handler, got, "")
	}
}

// TestDeriveOwningBackend_MonorepoNotSwallowedByRoot_6555 is the permissive
// guard: a manifest at the repository root must NOT swallow per-service
// manifests below it. This is the case the property exists for, and both
// halves of the fix agree on it, which is exactly why it can break silently.
func TestDeriveOwningBackend_MonorepoNotSwallowedByRoot_6555(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root,
		"pyproject.toml",
		"svc-a/pyproject.toml",
		"svc-a/app/routers/x.py",
		"svc-b/pyproject.toml",
		"svc-b/app/routers/y.py",
	)
	t.Chdir(root)

	for handler, want := range map[string]string{
		"svc-a/app/routers/x.py": "svc-a",
		"svc-b/app/routers/y.py": "svc-b",
	} {
		if got := deriveOwningBackend(handler); got != want {
			t.Errorf("deriveOwningBackend(%q) = %q, want %q", handler, got, want)
		}
	}
}

// TestDeriveOwningBackend_RootMarkerIsNotABoundary_6555 pins the asymmetry
// between the two passes: a *manifest* at the repository root is a boundary
// (and yields ""), a framework *marker* there is not — main.py at the root is
// an intra-service entry point, not evidence that the repository is the
// service. With no manifest anywhere the top-segment fallback must still run.
func TestDeriveOwningBackend_RootMarkerIsNotABoundary_6555(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root,
		"main.py",
		"src/api/routes.py",
	)
	t.Chdir(root)

	const handler = "src/api/routes.py"
	if got := deriveOwningBackend(handler); got != "src" {
		t.Errorf("deriveOwningBackend(%q) = %q, want %q (a root marker is not a boundary; fallback applies)", handler, got, "src")
	}
}
