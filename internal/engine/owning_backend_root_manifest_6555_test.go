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

// TestDeriveOwningBackend_RootManifestBeatsDeeperMarker_6555 pins the
// consequence of the two-pass order that this PR's review surfaced: once a
// manifest exists anywhere in the chain — including at "." — pass 2 never
// runs, so a framework marker below a *root* manifest is not a boundary
// either, and these shapes return "" where the parent returned a directory
// name.
//
// This is a deliberate decision, not a side effect. It cannot be decided
// per-shape, because the shape here is byte-identical to the issue's row 1
// (root pyproject.toml + app/main.py + app/api/endpoints/notifications.py,
// which must NOT be "app"). Only the directory names differ, so no rule over
// tree shape can return "" for `app` and `svc-a` for `svc-a`. MEASURED against
// the parent:
//
//	root pyproject.toml, app/main.py    | app/api/endpoints/notifications.py | "app"   -> ""
//	root pyproject.toml, svc-a/main.py  | svc-a/api/x.py                     | "svc-a" -> ""
//	root package.json,   svc-a/server.js| svc-a/routes/x.js                  | "svc-a" -> ""
//	root go.mod,         cmd/api/main.go| cmd/api/handlers/x.go              | "api"   -> ""
//
// #6593 FLIPPED THE TWO GO ROWS BACK, deliberately, and they are pinned here
// in their new direction rather than deleted. The reason is not the tree shape
// — that argument above still stands — it is a Go language convention that has
// no analogue in the Python and Node rows: each cmd/<name>/ directory is its
// own main package and builds a binary named <name> (verified with `go list
// -f '{{.Name}} {{.Target}}'`). So cmd/api and cmd/worker are two services in
// a single-module repository, and returning "" for both collapsed them onto
// one value. The Python and Node rows are unchanged and are what keeps the
// special case from being read as a general "a marker below a root manifest
// wins" rule; see owning_backend_go_cmd_6593_test.go for its guards.
func TestDeriveOwningBackend_RootManifestBeatsDeeperMarker_6555(t *testing.T) {
	cases := []struct {
		name string
		tree []string
		// want maps handler path -> expected owning_backend.
		want map[string]string
	}{
		{
			name: "python: root pyproject.toml over svc-a/main.py",
			tree: []string{"pyproject.toml", "svc-a/main.py", "svc-a/api/x.py"},
			want: map[string]string{"svc-a/api/x.py": ""},
		},
		{
			name: "node: root package.json over svc-a/server.js",
			tree: []string{"package.json", "svc-a/server.js", "svc-a/routes/x.js"},
			want: map[string]string{"svc-a/routes/x.js": ""},
		},
		{
			name: "go: root go.mod over cmd/api/main.go names the binary (#6593)",
			tree: []string{"go.mod", "cmd/api/main.go", "cmd/api/handlers/x.go"},
			want: map[string]string{"cmd/api/handlers/x.go": "api"},
		},
		{
			// Both binaries are asserted, so this subtest measures what its
			// name says: several cmd/ binaries stay *distinct* rather than
			// merely being non-empty.
			name: "go: root go.mod with several cmd/ binaries stay distinct (#6593)",
			tree: []string{"go.mod", "cmd/api/main.go", "cmd/worker/main.go", "cmd/api/handlers/x.go", "cmd/worker/handlers/y.go"},
			want: map[string]string{
				"cmd/api/handlers/x.go":    "api",
				"cmd/worker/handlers/y.go": "worker",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTree(t, root, tc.tree...)
			t.Chdir(root)
			for handler, want := range tc.want {
				if got := deriveOwningBackend(handler); got != want {
					t.Errorf("deriveOwningBackend(%q) = %q, want %q", handler, got, want)
				}
			}
		})
	}
}

// TestDeriveOwningBackend_ExhaustedWalkIsNotEmpty_6555 pins the invariant the
// doc comment states: "" is returned ONLY for a manifest found at ".", never
// for an exhausted walk. "./x.py" reaches the fallback with an unusable top
// segment, and must still return the "unknown" sentinel rather than "" —
// otherwise `owning_backend == ""` stops meaning "the repository is the
// boundary" and the consumers' fallbacks fire on a path that never decided
// anything. Red if the final `return "unknown"` becomes `return ""`.
func TestDeriveOwningBackend_ExhaustedWalkIsNotEmpty_6555(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "x.py")
	t.Chdir(root)

	const handler = "./x.py"
	if got := deriveOwningBackend(handler); got != "unknown" {
		t.Errorf("deriveOwningBackend(%q) = %q, want %q (an exhausted walk must not yield the root-manifest signal)", handler, got, "unknown")
	}
}
