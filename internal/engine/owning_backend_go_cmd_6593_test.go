package engine

import "testing"

// #6593: #6555 made a manifest beat a framework marker and made a manifest at
// "." the repository-root boundary (yielding ""). Every single-module Go
// repository carries go.mod at the root, so once that rule applied, every
// cmd/<name> binary lost its name: cmd/api/handlers/x.go went from "api" to
// "", and cmd/api and cmd/worker — two binaries, i.e. two services —
// collapsed onto the same value.
//
// The distinction that restores them is NOT structural. root pyproject.toml +
// app/main.py + app/api/endpoints/notifications.py is byte-identically shaped
// and must stay "" (#6555). It is a *language convention*: in Go, each
// cmd/<name>/ directory is its own main package and builds a binary named
// <name>. VERIFIED empirically with the toolchain:
//
//	module example.com/m, cmd/api/main.go + cmd/worker/main.go
//	go list -f '{{.ImportPath}} {{.Name}} {{.Target}}' ./...
//	  example.com/m/cmd/api    main  .../go/bin/api
//	  example.com/m/cmd/worker main  .../go/bin/worker
//
// `app/main.py` under a Python project carries no such meaning, so the special
// case is Go-and-cmd-only. The permissive direction — a cmd/ rule firing where
// cmd/ is not a Go binary directory — is what the guards below exist for.

// TestDeriveOwningBackend_GoCmdBinaryIsABoundary_6593 pins the restored
// behaviour: a top-level cmd/<name>/ holding main.go in a Go module names the
// binary, and distinct binaries stay distinct.
func TestDeriveOwningBackend_GoCmdBinaryIsABoundary_6593(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root,
		"go.mod",
		"cmd/api/main.go",
		"cmd/api/handlers/x.go",
		"cmd/worker/main.go",
		"cmd/worker/handlers/y.go",
	)
	t.Chdir(root)

	for handler, want := range map[string]string{
		"cmd/api/handlers/x.go":    "api",
		"cmd/worker/handlers/y.go": "worker",
		"cmd/api/main.go":          "api",
	} {
		if got := deriveOwningBackend(handler); got != want {
			t.Errorf("deriveOwningBackend(%q) = %q, want %q (cmd/<name> is a Go binary boundary)", handler, got, want)
		}
	}
}

// TestDeriveOwningBackend_CmdRuleIsGoOnly_6593 is the first permissive guard:
// a cmd/ directory in a repository that is not a Go module carries no such
// convention. Red if the rule keys on the directory name alone.
func TestDeriveOwningBackend_CmdRuleIsGoOnly_6593(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root,
		"package.json",
		"cmd/api/index.js",
		"cmd/api/routes/x.js",
	)
	t.Chdir(root)

	const handler = "cmd/api/routes/x.js"
	if got := deriveOwningBackend(handler); got != "" {
		t.Errorf("deriveOwningBackend(%q) = %q, want %q (cmd/ is not a binary convention outside Go)", handler, got, "")
	}
}

// TestDeriveOwningBackend_CmdRuleIsGoFilesOnly_6593 is the second permissive
// guard: even inside a Go module, a non-Go handler under cmd/<name>/ is not
// covered by the convention. This is also what keeps spring_routes_kotlin.go's
// call site (Kotlin handlers) outside the special case entirely.
func TestDeriveOwningBackend_CmdRuleIsGoFilesOnly_6593(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root,
		"go.mod",
		"cmd/api/main.go",
		"cmd/api/web/Routes.kt",
	)
	t.Chdir(root)

	const handler = "cmd/api/web/Routes.kt"
	if got := deriveOwningBackend(handler); got != "" {
		t.Errorf("deriveOwningBackend(%q) = %q, want %q (the Go cmd convention covers Go sources only)", handler, got, "")
	}
}

// TestDeriveOwningBackend_CmdRuleNeedsAGoModule_6593 isolates the go.mod
// condition, which no other test here reaches: the Go-file guard already
// blocks the non-Go repository above, because that repository's handlers are
// .js. This one uses a *Go* handler under cmd/ in a repository that is not a
// Go module — a Go helper binary living inside a Python service — where the
// convention has no module to be a convention of, so #6555's root-manifest
// answer stands. MEASURED: without the go.mod condition this returns "api".
func TestDeriveOwningBackend_CmdRuleNeedsAGoModule_6593(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root,
		"pyproject.toml",
		"cmd/api/main.go",
		"cmd/api/handlers/x.go",
	)
	t.Chdir(root)

	const handler = "cmd/api/handlers/x.go"
	if got := deriveOwningBackend(handler); got != "" {
		t.Errorf("deriveOwningBackend(%q) = %q, want %q (no go.mod: the repository is not a Go module)", handler, got, "")
	}
}

// TestDeriveOwningBackend_NestedModuleBelowCmdWins_6593 pins the third
// behaviour flip this change carries, in both directions. Pass 0 runs ahead of
// #6555's manifest pass, so it could preempt a *real* manifest below
// cmd/<name> — contradicting the principle #6555 established, that a manifest
// is a service boundary. It defers instead: a go.mod under cmd/api/sub is its
// own module and names the boundary, while a nested manifest that is not on
// the handler's ancestor chain (a cousin under cmd/api) does not defer,
// matching how pass 1 anchors its own walk.
func TestDeriveOwningBackend_NestedModuleBelowCmdWins_6593(t *testing.T) {
	t.Run("nested module on the chain wins", func(t *testing.T) {
		root := t.TempDir()
		writeTree(t, root,
			"go.mod",
			"cmd/api/main.go",
			"cmd/api/sub/go.mod",
			"cmd/api/sub/h/x.go",
		)
		t.Chdir(root)

		const handler = "cmd/api/sub/h/x.go"
		if got := deriveOwningBackend(handler); got != "sub" {
			t.Errorf("deriveOwningBackend(%q) = %q, want %q (a real module below cmd/<name> is the boundary)", handler, got, "sub")
		}
	})

	t.Run("nested module off the chain does not defer", func(t *testing.T) {
		root := t.TempDir()
		writeTree(t, root,
			"go.mod",
			"cmd/api/main.go",
			"cmd/api/internal/go.mod",
			"cmd/api/handlers/x.go",
		)
		t.Chdir(root)

		const handler = "cmd/api/handlers/x.go"
		if got := deriveOwningBackend(handler); got != "api" {
			t.Errorf("deriveOwningBackend(%q) = %q, want %q (a cousin module is not on the handler chain)", handler, got, "api")
		}
	})
}

// TestDeriveOwningBackend_CmdWithNonGoMarkerIsNotABinary_6593 closes the other
// half of the main.go condition. CmdWithoutMainGoIsNotABinary_6593 pins only
// the coarsest case — cmd/<name> holding nothing — which leaves
// `[]string{"main.go"}` widened to frameworkMarkerFiles alive: an index.js in
// cmd/api would then make it a "binary" directory. It is not a Go binary
// directory, so #6555's root-manifest answer stands.
func TestDeriveOwningBackend_CmdWithNonGoMarkerIsNotABinary_6593(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root,
		"go.mod",
		"cmd/api/index.js",
		"cmd/api/h/x.go",
	)
	t.Chdir(root)

	const handler = "cmd/api/h/x.go"
	if got := deriveOwningBackend(handler); got != "" {
		t.Errorf("deriveOwningBackend(%q) = %q, want %q (a non-main.go marker does not make cmd/api a Go binary)", handler, got, "")
	}
}

// TestDeriveOwningBackend_CmdRuleIsTopLevelOnly_6593 is the third permissive
// guard: a nested cmd/ belongs to the service above it, and that service's own
// manifest must still win. Red if the rule matches "cmd" anywhere in the path,
// which would re-collapse a Go monorepo onto per-binary names and lose svc-a.
func TestDeriveOwningBackend_CmdRuleIsTopLevelOnly_6593(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root,
		"go.mod",
		"svc-a/go.mod",
		"svc-a/cmd/api/main.go",
		"svc-a/cmd/api/handlers/x.go",
	)
	t.Chdir(root)

	const handler = "svc-a/cmd/api/handlers/x.go"
	if got := deriveOwningBackend(handler); got != "svc-a" {
		t.Errorf("deriveOwningBackend(%q) = %q, want %q (a nested cmd/ does not outrank the service manifest above it)", handler, got, "svc-a")
	}
}

// TestDeriveOwningBackend_CmdWithoutMainGoIsNotABinary_6593 is the fourth
// permissive guard: cmd/<name>/ without a main.go is not a binary directory,
// so the #6555 root-manifest answer stands. Red if the rule fires on the
// directory name without checking that a main package is actually there.
func TestDeriveOwningBackend_CmdWithoutMainGoIsNotABinary_6593(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root,
		"go.mod",
		"cmd/api/handlers/x.go",
	)
	t.Chdir(root)

	const handler = "cmd/api/handlers/x.go"
	if got := deriveOwningBackend(handler); got != "" {
		t.Errorf("deriveOwningBackend(%q) = %q, want %q (cmd/api holds no main.go, so it is not a binary)", handler, got, "")
	}
}
