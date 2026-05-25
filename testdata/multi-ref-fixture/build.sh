#!/usr/bin/env bash
# build.sh — creates the multi-ref test fixture on disk for manual inspection
# or debugging. The E2E tests in internal/daemon/e2e_multi_ref_test.go build
# this fixture automatically at test-time using exec.Command("git", ...) so
# you do NOT need to run this script before running tests.
#
# Usage:
#   ./testdata/multi-ref-fixture/build.sh [dest-dir]
#
# If dest-dir is omitted, the fixture is written to /tmp/archi-multi-ref-fixture.
#
# Layout produced:
#   <dest>/
#     repo/             — main git repository
#       main:           entities A, B, C
#       feature/foo:    entities A, B, C, D, E
#       feature/bar:    entities A, B, C', D
#     wt-feature-foo/   — linked worktree for feature/foo
#     wt-feature-bar/   — linked worktree for feature/bar
#
# Issue: #2223  Refs: #2087, #2098

set -euo pipefail

DEST="${1:-/tmp/archi-multi-ref-fixture}"

if [[ -e "$DEST" ]]; then
  echo "Removing existing fixture at $DEST …"
  rm -rf "$DEST"
fi

mkdir -p "$DEST"
REPO="$DEST/repo"
WT_FOO="$DEST/wt-feature-foo"
WT_BAR="$DEST/wt-feature-bar"

echo "Creating fixture in $DEST …"

# ── init ──────────────────────────────────────────────────────────────────────
mkdir "$REPO"
(cd "$REPO" &&
  git init -b main &&
  git config user.email "test@test.invalid" &&
  git config user.name "Test" &&
  cat > go.mod <<'GOMOD'
module example.com/multi-ref-fixture

go 1.21
GOMOD
)

# ── branch main: A, B, C ──────────────────────────────────────────────────────
(cd "$REPO" &&
  cat > entities.go <<'GO'
package fixture

func EntityA() {}
func EntityB() {}
func EntityC() {}
GO
  git add -A &&
  git commit -m "main: EntityA EntityB EntityC"
)

# ── branch feature/foo: A, B, C, D, E ────────────────────────────────────────
(cd "$REPO" &&
  git checkout -b feature/foo &&
  cat > entities.go <<'GO'
package fixture

func EntityA() {}
func EntityB() {}
func EntityC() {}
func EntityD() {}
func EntityE() {}
GO
  git add -A &&
  git commit -m "feature/foo: add EntityD EntityE"
)

# ── branch feature/bar: A, B, C', D ──────────────────────────────────────────
(cd "$REPO" &&
  git checkout main &&
  git checkout -b feature/bar &&
  cat > entities.go <<'GO'
package fixture

func EntityA()      {}
func EntityB()      {}
func EntityCPrime() {}
func EntityD()      {}
GO
  git add -A &&
  git commit -m "feature/bar: rename C→CPrime, add EntityD"
)

# back to main
(cd "$REPO" && git checkout main)

# ── linked worktrees ───────────────────────────────────────────────────────────
(cd "$REPO" && git worktree add "$WT_FOO" feature/foo)
(cd "$REPO" && git worktree add "$WT_BAR" feature/bar)

echo "Done."
echo ""
echo "Branches:"
git -C "$REPO" branch
echo ""
echo "Worktrees:"
git -C "$REPO" worktree list
echo ""
echo "Fixture layout:"
ls "$DEST"
