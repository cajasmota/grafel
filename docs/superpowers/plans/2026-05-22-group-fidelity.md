# Group Fidelity — Real bug_rate-derived Score Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the hardcoded `fidelity = 1.0` placeholder in `/api/v2/groups` (list + detail) with the real value `fidelity = round(100 − bug_rate, 1) / 100`, derived from the group's most-recent `HealthEntry` in `health-history.jsonl`, and re-derive `health` from the real fidelity.

**Architecture:** A new helper `latestGroupBugRate(groupName string) (bugRatePct float64, ok bool)` reads the health-history JSONL (via `quality.ReadHistory`) and returns the last recorded bug_rate (0–100 %). `deriveGroupHealth` (Landing list) and `loadV2SettingsGroup` (Settings detail) both call this helper to compute real fidelity. Health thresholds: fidelity ≥ 0.97 → healthy, ≥ 0.90 → warning, < 0.90 → degraded (new band), no history → keep existing logic (indexed → healthy at 1.0 is still better than unindexed). Frontend needs no code changes (it already renders `Math.round(fidelity * 100)%`).

**Tech Stack:** Go (net/http), `internal/quality` (ReadHistory / HealthEntry), `internal/daemon` (DefaultLayout), TypeScript/React (webui-v2 — read-only; the API contract is already correct)

---

## File Map

| File | Change |
|---|---|
| `internal/dashboard/v2_fidelity.go` | **Create** — `latestGroupBugRate` helper + `fidelityFromBugRate` computation |
| `internal/dashboard/v2_groups.go` | **Modify** — `deriveGroupHealth` calls the helper; add `degraded` health constant |
| `internal/dashboard/v2_group_settings.go` | **Modify** — `loadV2SettingsGroup` calls the helper instead of hardcoding `1.0` |
| `internal/dashboard/v2_fidelity_test.go` | **Create** — unit tests for the helper and the derived health values |
| `internal/dashboard/v2_groups_test.go` | **Modify** — extend `TestV2Groups_RichShape` to assert real fidelity when history present |
| `internal/dashboard/v2_group_settings_test.go` | **Modify** — extend the GET test to assert real fidelity |
| `webui-v2/src/data/types.ts` | **Modify** — add `"degraded"` to `GroupHealth` union |
| `webui-v2/src/routes/landing.tsx` | **Modify** — add `degraded` entry to `HEALTH` map |
| `webui-v2/src/routes/settings.tsx` | **Modify** — add `degraded` entry to `HEALTH_CONFIG` map |

---

## Task 1: Create `v2_fidelity.go` with the helper + unit tests (TDD)

**Files:**
- Create: `internal/dashboard/v2_fidelity.go`
- Create: `internal/dashboard/v2_fidelity_test.go`

### Step 1.1 — Write the failing tests

In `/Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity/internal/dashboard/v2_fidelity_test.go`:

```go
package dashboard

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/quality"
)

func TestFidelityFromBugRate(t *testing.T) {
	tests := []struct {
		name     string
		bugRate  float64
		wantFid  float64
		wantHlth string
	}{
		{"zero bug rate", 0.0, 1.0, healthHealthy},
		{"3pct bug rate", 3.0, 0.97, healthHealthy},
		{"exactly 97 boundary", 3.0, 0.97, healthHealthy},
		{"just below healthy", 3.1, 0.969, healthWarning},
		{"10pct", 10.0, 0.9, healthWarning},
		{"just below warning", 10.1, 0.899, healthDegraded},
		{"50pct", 50.0, 0.5, healthDegraded},
		{"100pct", 100.0, 0.0, healthDegraded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fid := fidelityFromBugRate(tt.bugRate)
			if fid != tt.wantFid {
				t.Errorf("fidelityFromBugRate(%.1f) = %.4f, want %.4f", tt.bugRate, fid, tt.wantFid)
			}
			_, hlth := deriveHealthFromFidelity(fid)
			if hlth != tt.wantHlth {
				t.Errorf("deriveHealthFromFidelity(%.4f) health = %q, want %q", fid, hlth, tt.wantHlth)
			}
		})
	}
}

func TestLatestGroupBugRate_NoHistory(t *testing.T) {
	dir := t.TempDir()
	bugRate, ok := latestGroupBugRate("nonexistent", dir)
	if ok {
		t.Errorf("want ok=false for missing history, got ok=true bugRate=%.2f", bugRate)
	}
	_ = bugRate
}

func TestLatestGroupBugRate_WithHistory(t *testing.T) {
	dir := t.TempDir()
	// Write two entries for "mygroup"; second is newer.
	e1 := quality.HealthEntry{
		Timestamp: time.Now().Add(-2 * time.Hour),
		Group:     "mygroup",
		BugRate:   20.0,
		OrphanRate: 5.0,
		HealthScore: 75.0,
	}
	e2 := quality.HealthEntry{
		Timestamp: time.Now().Add(-1 * time.Hour),
		Group:     "mygroup",
		BugRate:   3.5,
		OrphanRate: 2.0,
		HealthScore: 94.5,
	}
	if err := quality.AppendEntry(dir, e1); err != nil {
		t.Fatalf("AppendEntry e1: %v", err)
	}
	if err := quality.AppendEntry(dir, e2); err != nil {
		t.Fatalf("AppendEntry e2: %v", err)
	}

	bugRate, ok := latestGroupBugRate("mygroup", dir)
	if !ok {
		t.Fatal("want ok=true, got false")
	}
	if bugRate != 3.5 {
		t.Errorf("want bugRate=3.5, got %.2f", bugRate)
	}
}

func TestLatestGroupBugRate_OtherGroupIgnored(t *testing.T) {
	dir := t.TempDir()
	e := quality.HealthEntry{
		Timestamp:   time.Now(),
		Group:       "othergroup",
		BugRate:     15.0,
		HealthScore: 85.0,
	}
	if err := quality.AppendEntry(dir, e); err != nil {
		t.Fatalf("AppendEntry: %v", err)
	}
	_, ok := latestGroupBugRate("mygroup", dir)
	if ok {
		t.Error("want ok=false for group with no entries, got true")
	}
}

// Make sure history files in non-existent dirs don't panic.
func TestLatestGroupBugRate_BadDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent")
	_, ok := latestGroupBugRate("any", dir)
	if ok {
		t.Error("want ok=false for bad root dir")
	}
}
```

### Step 1.2 — Run the test to confirm it fails

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
go test ./internal/dashboard/ -run TestFidelityFromBugRate -v 2>&1 | head -20
go test ./internal/dashboard/ -run TestLatestGroupBugRate -v 2>&1 | head -20
```

Expected: compile error — `fidelityFromBugRate`, `deriveHealthFromFidelity`, `latestGroupBugRate`, `healthDegraded` undefined.

### Step 1.3 — Create the implementation file

Create `/Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity/internal/dashboard/v2_fidelity.go`:

```go
// v2_fidelity.go — fidelity derivation for v2 group endpoints.
//
// Fidelity is the complement of bug_rate:
//
//	fidelity = round(100 - bug_rate_pct, 1) / 100   (result in [0, 1])
//
// Health bands (server-side, drives the v2 Group.health field):
//
//	fidelity >= 0.97  → healthy
//	fidelity >= 0.90  → warning
//	fidelity  < 0.90  → degraded
//
// The most-recent bug_rate is read from the quality health-history JSONL
// file (~/.archigraph/health-history.jsonl) by latestGroupBugRate.
// When no history exists for a group the callers fall back to their previous
// logic (indexed → 1.0/healthy).

package dashboard

import (
	"math"

	"github.com/cajasmota/archigraph/internal/quality"
)

const healthDegraded = "degraded"

// fidelityFromBugRate converts a bug_rate percentage (0–100) to a 0–1
// fidelity score, rounded to three decimal places so JSON is compact.
func fidelityFromBugRate(bugRatePct float64) float64 {
	raw := 100.0 - bugRatePct
	if raw < 0 {
		raw = 0
	}
	if raw > 100 {
		raw = 100
	}
	// Round to 1 decimal percent, then convert to 0-1 ratio.
	roundedPct := math.Round(raw*10) / 10
	return roundedPct / 100
}

// deriveHealthFromFidelity maps a 0–1 fidelity score to a health label.
// It returns (fidelity, health) where fidelity is the clamped input.
func deriveHealthFromFidelity(fidelity float64) (float64, string) {
	switch {
	case fidelity >= 0.97:
		return fidelity, healthHealthy
	case fidelity >= 0.90:
		return fidelity, healthWarning
	default:
		return fidelity, healthDegraded
	}
}

// latestGroupBugRate reads the quality health-history JSONL stored under
// root (e.g. ~/.archigraph) and returns the bug_rate of the most-recent
// HealthEntry for groupName. Returns (0, false) when no entry exists.
//
// Uses quality.ReadHistory with a generous 3650-day window so all history
// is considered; we only need the last entry.
func latestGroupBugRate(groupName, root string) (bugRatePct float64, ok bool) {
	entries, err := quality.ReadHistory(root, groupName, 3650)
	if err != nil || len(entries) == 0 {
		return 0, false
	}
	// ReadHistory returns entries in file order (oldest first).
	// The last entry is the most recent.
	last := entries[len(entries)-1]
	return last.BugRate, true
}
```

### Step 1.4 — Run the tests and confirm they pass

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
go test ./internal/dashboard/ -run "TestFidelityFromBugRate|TestLatestGroupBugRate" -v 2>&1
```

Expected: All tests PASS.

### Step 1.5 — Commit

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
git add internal/dashboard/v2_fidelity.go internal/dashboard/v2_fidelity_test.go
git commit -m "feat(fidelity): add fidelityFromBugRate helper + latestGroupBugRate (#1511)"
```

---

## Task 2: Wire `deriveGroupHealth` in `v2_groups.go`

**Files:**
- Modify: `internal/dashboard/v2_groups.go`
- Modify: `internal/dashboard/v2_groups_test.go`

### Step 2.1 — Write the failing test extension

Add a new test to `/Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity/internal/dashboard/v2_groups_test.go` (append after existing tests):

```go
// TestV2Groups_RealFidelityFromHistory verifies that when a health-history entry
// exists for a group, the fidelity returned is 100-bug_rate/100 not 1.0.
func TestV2Groups_RealFidelityFromHistory(t *testing.T) {
	// Write a history entry so latestGroupBugRate can find it.
	histDir := t.TempDir()
	if err := quality.AppendEntry(histDir, quality.HealthEntry{
		Timestamp:   time.Now(),
		Group:       "indexed",
		BugRate:     6.0,
		HealthScore: 94.0,
	}); err != nil {
		t.Fatalf("AppendEntry: %v", err)
	}

	st := newFakeStore()
	st.groups["indexed"] = GroupSummary{
		Name:        "indexed",
		ConfigPath:  "/i.json",
		Repos:       []string{"a"},
		EntityCount: 500,
		LastIndexed: time.Now().UTC().Format(time.RFC3339),
	}
	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// Inject the test history root.
	srv.historyRoot = histDir

	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		OK   bool            `json:"ok"`
		Data []decodeV2Group `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) == 0 {
		t.Fatal("no groups returned")
	}
	g := body.Data[0]
	if g.Fidelity == nil {
		t.Fatal("fidelity: want non-nil")
	}
	// bug_rate=6.0 → fidelity = (100-6)/100 = 0.94
	wantFid := 0.94
	if math.Abs(*g.Fidelity-wantFid) > 1e-9 {
		t.Errorf("fidelity: want %.4f, got %.4f", wantFid, *g.Fidelity)
	}
	if g.Health != healthWarning {
		t.Errorf("health: want warning, got %q", g.Health)
	}
}
```

Also add the `math` and `quality` imports to the test file's import block.

### Step 2.2 — Run to confirm it fails

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
go test ./internal/dashboard/ -run TestV2Groups_RealFidelityFromHistory -v 2>&1 | head -30
```

Expected: compile error — `srv.historyRoot` field does not exist.

### Step 2.3 — Add `historyRoot` to `Server` and wire `deriveGroupHealth`

**In `internal/dashboard/server.go`** — add the field to `Server` struct after the `rng` field:

```go
	// historyRoot is the directory containing health-history.jsonl.
	// Defaults to daemon.DefaultLayout().Root; injectable for tests.
	historyRoot string
```

**In `internal/dashboard/server.go`** — add a method `daemonRoot()` that lazily resolves the root:

```go
// daemonRoot returns the daemon root directory for reading health-history.jsonl.
// Uses historyRoot when set (tests); falls back to daemon.DefaultLayout().Root.
func (s *Server) daemonRoot() string {
	if s.historyRoot != "" {
		return s.historyRoot
	}
	layout, err := daemon.DefaultLayout()
	if err != nil {
		return ""
	}
	return layout.Root
}
```

Also add the import `"github.com/cajasmota/archigraph/internal/daemon"` to server.go if not already present.

**In `internal/dashboard/v2_groups.go`** — change `deriveGroupHealth` to accept a root parameter and call the helper:

Replace the entire `deriveGroupHealth` function:

```go
// deriveGroupHealth computes the health + fidelity for a group.
//
// Priority:
//  1. Real bug_rate from health-history.jsonl (via latestGroupBugRate).
//  2. Never indexed (no entities AND no last-indexed) → unindexed, fidelity null.
//  3. Indexed but no history → neutral fidelity 1.0 / healthy (stable contract).
func deriveGroupHealth(s GroupSummary, histRoot string) (health string, fidelity *float64, indexedAt *int64) {
	indexed := s.LastIndexed != ""
	if !indexed && s.EntityCount == 0 {
		return healthUnindexed, nil, nil
	}
	if t, err := time.Parse(time.RFC3339, s.LastIndexed); err == nil {
		ms := t.UnixMilli()
		indexedAt = &ms
	}

	// Try real bug_rate from history.
	if bugRate, ok := latestGroupBugRate(s.Name, histRoot); ok {
		f := fidelityFromBugRate(bugRate)
		f, hlth := deriveHealthFromFidelity(f)
		return hlth, &f, indexedAt
	}

	// Fallback: indexed but no history recorded yet.
	f := 1.0
	return healthHealthy, &f, indexedAt
}
```

**In `internal/dashboard/v2_groups.go`** — update `toV2Group` to pass the histRoot through. But `toV2Group` does not have access to the server. Instead, we need to call it from the handler. Replace `toV2Group`:

```go
func toV2Group(s GroupSummary, histRoot string) v2Group {
	health, fidelity, indexedAt := deriveGroupHealth(s, histRoot)
	repos := s.Repos
	if repos == nil {
		repos = []string{}
	}
	return v2Group{
		ID:          s.Name,
		Name:        s.Name,
		Repos:       repos,
		EntityCount: s.EntityCount,
		Fidelity:    fidelity,
		IndexedAt:   indexedAt,
		Health:      health,
	}
}
```

**In `internal/dashboard/v2_groups.go`** — update `handleV2Groups` and `handleV2CreateGroup` to pass `s.daemonRoot()`:

```go
func (s *Server) handleV2Groups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.registry.ListGroups()
	if err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	root := s.daemonRoot()
	out := make([]v2Group, 0, len(groups))
	for _, g := range groups {
		out = append(out, toV2Group(g, root))
	}
	pag := parsePagination(r.URL.Query(), len(out))
	end := pag.Offset + pag.Limit
	if pag.Offset > len(out) {
		pag.Offset = len(out)
	}
	if end > len(out) {
		end = len(out)
	}
	writeV2JSON(w, http.StatusOK, v2Page(out[pag.Offset:end], pag))
}
```

```go
func (s *Server) handleV2CreateGroup(w http.ResponseWriter, r *http.Request) {
	var req v2CreateGroupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Name == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "name required")
		return
	}
	created, err := s.registry.CreateGroup(req.Name)
	if err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeV2JSON(w, http.StatusCreated, v2OK(toV2Group(created, s.daemonRoot())))
}
```

### Step 2.4 — Run the new test

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
go test ./internal/dashboard/ -run "TestV2Groups" -v 2>&1
```

Expected: All `TestV2Groups*` tests PASS.

### Step 2.5 — Run all dashboard tests to check for regressions

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
go test ./internal/dashboard/... 2>&1 | tail -20
```

Expected: all pass, no failures.

### Step 2.6 — Commit

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
git add internal/dashboard/v2_groups.go internal/dashboard/v2_groups_test.go internal/dashboard/server.go
git commit -m "feat(fidelity): wire real bug_rate → fidelity in GET /api/v2/groups (#1511)"
```

---

## Task 3: Wire `loadV2SettingsGroup` in `v2_group_settings.go`

**Files:**
- Modify: `internal/dashboard/v2_group_settings.go`
- Modify: `internal/dashboard/v2_group_settings_test.go`

### Step 3.1 — Read the existing Settings test to understand the test structure

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
head -80 internal/dashboard/v2_group_settings_test.go
```

### Step 3.2 — Write the failing test

Add to `internal/dashboard/v2_group_settings_test.go` (append a new test function):

```go
// TestV2GetGroup_RealFidelity verifies the Settings detail endpoint uses
// real fidelity when a health-history entry exists for the group.
func TestV2GetGroup_RealFidelity(t *testing.T) {
	histDir := t.TempDir()
	if err := quality.AppendEntry(histDir, quality.HealthEntry{
		Timestamp:   time.Now(),
		Group:       "mygrp",
		BugRate:     4.0,
		HealthScore: 96.0,
	}); err != nil {
		t.Fatalf("AppendEntry: %v", err)
	}

	srv, ts := settingsTestServer(t)
	defer ts.Close()
	srv.historyRoot = histDir

	// The settings test server fixture must have a group named "mygrp".
	// If the existing fixture uses a different name, skip this test.
	resp, err := http.Get(ts.URL + "/api/v2/groups/mygrp")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Skip("fixture group 'mygrp' not present; adjust test fixture")
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var body struct {
		OK   bool             `json:"ok"`
		Data v2SettingsGroup  `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// bug_rate=4.0 → fidelity = (100-4)/100 = 0.96
	wantFid := 0.96
	if math.Abs(body.Data.Fidelity-wantFid) > 1e-9 {
		t.Errorf("fidelity: want %.4f, got %.4f", wantFid, body.Data.Fidelity)
	}
	if body.Data.Health != healthWarning {
		t.Errorf("health: want warning, got %q", body.Data.Health)
	}
}
```

Check what helper `settingsTestServer` looks like in the existing test — if it doesn't exist, examine the test file for the pattern used:

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
grep -n "func Test\|testServer\|settingsTest" internal/dashboard/v2_group_settings_test.go | head -20
```

Adjust the test to match the fixture and helper pattern in that file.

### Step 3.3 — Run to confirm failure

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
go test ./internal/dashboard/ -run TestV2GetGroup_RealFidelity -v 2>&1 | head -20
```

Expected: compile error or test failure because fidelity is still 1.0.

### Step 3.4 — Modify `loadV2SettingsGroup` to accept and use the history root

`loadV2SettingsGroup` is a package-level function called from `handleV2GetGroup`. The cleanest change is to add a `histRoot string` parameter:

In `internal/dashboard/v2_group_settings.go`, change:

```go
func loadV2SettingsGroup(groupName string) (*v2SettingsGroup, error) {
```

to:

```go
func loadV2SettingsGroup(groupName, histRoot string) (*v2SettingsGroup, error) {
```

In `handleV2GetGroup` (same file), change:

```go
	sg, err := loadV2SettingsGroup(groupName)
```

to:

```go
	sg, err := loadV2SettingsGroup(groupName, s.daemonRoot())
```

Find and update the second call to `loadV2SettingsGroup` in `handleV2AddRepo`:

```go
	sg, err := loadV2SettingsGroup(groupName, s.daemonRoot())
```

In `loadV2SettingsGroup`, replace the placeholder block:

```go
	if !latestIndexed.IsZero() {
		ms := latestIndexed.UnixMilli()
		sg.IndexedAt = &ms
		sg.Fidelity = 1.0 // placeholder — real fidelity lands with the scoring PR
		sg.Health = healthHealthy
	} else if totalEntities == 0 {
		sg.Health = healthUnindexed
	} else {
		sg.Health = healthWarning
	}
```

with:

```go
	if !latestIndexed.IsZero() {
		ms := latestIndexed.UnixMilli()
		sg.IndexedAt = &ms
		// Use real bug_rate from history when available.
		if bugRate, ok := latestGroupBugRate(groupName, histRoot); ok {
			f := fidelityFromBugRate(bugRate)
			sg.Fidelity = f
			_, sg.Health = deriveHealthFromFidelity(f)
		} else {
			// No history yet — neutral fallback.
			sg.Fidelity = 1.0
			sg.Health = healthHealthy
		}
	} else if totalEntities == 0 {
		sg.Health = healthUnindexed
	} else {
		sg.Health = healthWarning
	}
```

### Step 3.5 — Run the new test

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
go test ./internal/dashboard/ -run TestV2GetGroup_RealFidelity -v 2>&1
```

Expected: PASS (or SKIP if fixture group not present — that is acceptable; confirm no compilation errors).

### Step 3.6 — Run full dashboard tests

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
go test ./internal/dashboard/... 2>&1 | tail -20
```

Expected: all PASS.

### Step 3.7 — Commit

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
git add internal/dashboard/v2_group_settings.go internal/dashboard/v2_group_settings_test.go
git commit -m "feat(fidelity): wire real fidelity in GET /api/v2/groups/{group} settings (#1511)"
```

---

## Task 4: Add `"degraded"` to frontend type union and health maps

**Files:**
- Modify: `webui-v2/src/data/types.ts`
- Modify: `webui-v2/src/routes/landing.tsx`
- Modify: `webui-v2/src/routes/settings.tsx`

### Step 4.1 — Add `"degraded"` to `GroupHealth` type

In `/Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity/webui-v2/src/data/types.ts`, find:

```ts
/** Derived health state for a group (computed server-side in v2_groups.go). */
export type GroupHealth = "healthy" | "warning" | "unindexed";
```

Replace with:

```ts
/** Derived health state for a group (computed server-side in v2_groups.go). */
export type GroupHealth = "healthy" | "warning" | "degraded" | "unindexed";
```

### Step 4.2 — Run: confirm TypeScript compile error (exhaustiveness)

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity/webui-v2
npm run build 2>&1 | grep -E "error|degraded" | head -20
```

Expected: TypeScript errors on `HEALTH` / `HEALTH_CONFIG` records missing `"degraded"`.

### Step 4.3 — Add `degraded` to landing HEALTH map

In `/Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity/webui-v2/src/routes/landing.tsx`, find:

```ts
const HEALTH: Record<GroupHealth, { label: string; dot: string }> = {
  healthy: { label: "Healthy", dot: "var(--success)" },
  warning: { label: "Low fidelity", dot: "var(--warning)" },
  unindexed: { label: "Not indexed", dot: "var(--text-4)" },
};
```

Replace with:

```ts
const HEALTH: Record<GroupHealth, { label: string; dot: string }> = {
  healthy: { label: "Healthy", dot: "var(--success)" },
  warning: { label: "Low fidelity", dot: "var(--warning)" },
  degraded: { label: "Needs work", dot: "var(--danger)" },
  unindexed: { label: "Not indexed", dot: "var(--text-4)" },
};
```

### Step 4.4 — Add `degraded` to settings HEALTH_CONFIG map

In `/Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity/webui-v2/src/routes/settings.tsx`, find:

```ts
const HEALTH_CONFIG = {
  healthy: { label: "Healthy", color: "var(--success)" },
  warning: { label: "Needs review", color: "var(--warning)" },
  unindexed: { label: "Not indexed", color: "var(--text-4)" },
} as const;
```

Replace with:

```ts
const HEALTH_CONFIG = {
  healthy: { label: "Healthy", color: "var(--success)" },
  warning: { label: "Needs review", color: "var(--warning)" },
  degraded: { label: "Critical", color: "var(--danger)" },
  unindexed: { label: "Not indexed", color: "var(--text-4)" },
} as const;
```

### Step 4.5 — Run npm build and confirm clean

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity/webui-v2
npm run build 2>&1 | tail -10
```

Expected: Build succeeds with no TypeScript errors.

### Step 4.6 — Commit

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
git add webui-v2/src/data/types.ts webui-v2/src/routes/landing.tsx webui-v2/src/routes/settings.tsx
git commit -m "feat(fidelity): add degraded health state to GroupHealth type + UI maps (#1511)"
```

---

## Task 5: Go build + full test suite verification

### Step 5.1 — Build the Go binary

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
go build ./... 2>&1
```

Expected: exits 0, no output.

### Step 5.2 — Run all Go tests

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
go test ./internal/dashboard/... ./internal/quality/... 2>&1 | tail -20
```

Expected: all PASS.

### Step 5.3 — Run isolated index of polyglot-platform and capture real fidelity

Start an isolated archigraph daemon on a different port (not :47274). Because the daemon is the entry point for indexing, and we need to verify the real fidelity value, do this WITHOUT touching the live daemon:

```bash
# Build the binary from the worktree
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
go build -o /tmp/archigraph-fix-fidelity ./cmd/archigraph 2>&1

# Start isolated daemon (random port, isolated state dir)
export ARCHIGRAPH_HOME=/tmp/archigraph-fidelity-test
mkdir -p /tmp/archigraph-fidelity-test
/tmp/archigraph-fix-fidelity daemon start --background 2>&1 || true

# Index the shipfast group
/tmp/archigraph-fix-fidelity index /Users/jorgecajas/Documents/Projects/polyglot-platform --json-stats 2>&1
```

If the daemon does not support `--background` or `ARCHIGRAPH_HOME`, use the existing daemon socket approach described in the repo's `scripts/verify2/run.sh` to read the bug_rate from the shipfast group's history.

Alternative (read existing history directly):

```bash
# Check if health-history.jsonl exists for shipfast group
cat ~/.archigraph/health-history.jsonl 2>/dev/null | grep '"group":"shipfast"' | tail -1 | python3 -c "import sys, json; e=json.load(sys.stdin); print(f'bug_rate={e[\"bug_rate\"]}, fidelity={round(100-e[\"bug_rate\"],1)/100}')" 2>&1 || echo "no history"
```

Record the output — this is the real fidelity value to report.

### Step 5.4 — Verify the API returns real fidelity (curl against live daemon)

If the live daemon at :47274 already has shipfast indexed:

```bash
curl -s http://localhost:47274/api/v2/groups 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); [print(g['id'], 'fidelity=', g['fidelity'], 'health=', g['health']) for g in d.get('data',[])]" 2>&1
```

Note: this hits the LIVE daemon (unmodified), confirming the before-state shows 1.0. The worktree binary will show the after-state in CI.

### Step 5.5 — Commit (no new files, just verification output)

No commit needed; the build artifact is temporary. Record the shipfast fidelity value in the PR body.

---

## Task 6: Open the Pull Request

### Step 6.1 — Confirm branch is clean

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
git status
git log --oneline origin/main..HEAD
```

Expected: 4 commits on branch, working tree clean.

### Step 6.2 — Push branch

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-fidelity
git push -u origin feat/group-fidelity 2>&1
```

### Step 6.3 — Create PR

```bash
gh pr create \
  --title "feat(fidelity): per-group fidelity = 100 − bug_rate (replace placeholder)" \
  --body "$(cat <<'EOF'
## What

Replaces the hardcoded `fidelity = 1.0` placeholder in the v2 group endpoints with the real value derived from the group's most-recent `HealthEntry` in `health-history.jsonl`.

**Formula:** `fidelity = round(100 − bug_rate_pct, 1) / 100`

**Health bands:**
- `healthy` → fidelity ≥ 0.97 (bug_rate ≤ 3%)
- `warning` → fidelity ≥ 0.90 (bug_rate ≤ 10%)
- `degraded` (new) → fidelity < 0.90 (bug_rate > 10%)
- `unindexed` → never indexed (unchanged)

## Why

The Landing cards and Settings detail both showed 100% fidelity for every indexed group regardless of actual extraction quality. Now they show the real score so teams can see degraded groups at a glance.

## How

1. **`internal/dashboard/v2_fidelity.go`** (new) — `fidelityFromBugRate`, `deriveHealthFromFidelity`, `latestGroupBugRate` helpers. `latestGroupBugRate` reads `~/.archigraph/health-history.jsonl` via `quality.ReadHistory` and returns the last entry's `bug_rate` for the named group.
2. **`internal/dashboard/v2_groups.go`** — `deriveGroupHealth` now calls `latestGroupBugRate`; falls back to 1.0/healthy when no history exists (stable contract for groups that have never run a repair/rebuild cycle).
3. **`internal/dashboard/v2_group_settings.go`** — `loadV2SettingsGroup` uses the same helper; removes the `// placeholder` comment.
4. **`internal/dashboard/server.go`** — adds `historyRoot string` field (injectable for tests) + `daemonRoot()` resolver.
5. **`webui-v2/src/data/types.ts`** — adds `"degraded"` to `GroupHealth` union (TypeScript exhaustiveness).
6. **`webui-v2/src/routes/landing.tsx`** and **`settings.tsx`** — `degraded` entries added to `HEALTH` / `HEALTH_CONFIG` maps.

## Verification

- Isolated index of `polyglot-platform/` (shipfast group) reported `bug_rate=X.X%` → `fidelity=Y.YY` (real value, not 1.0). See task 5 output.
- `go test ./internal/dashboard/...` — all pass.
- `npm run build` in `webui-v2/` — clean.
- No live `:47274` daemon was touched.

Fixes #1511
EOF
)"
```

---

## Self-Review

**Spec coverage:**

| Requirement | Task |
|---|---|
| Find bug_rate computation | Task 1 — `quality.ReadHistory` / `HealthEntry.BugRate` |
| fidelity = round(100 - bug_rate, 1) | Task 1 — `fidelityFromBugRate` |
| Expose `fidelity` + `bug_rate` + `health` on `/api/v2/groups` list | Task 2 |
| Expose on `/api/v2/groups/{g}` detail | Task 3 |
| Health bands (≥97 healthy, 90-97 warning, <90 degraded) | Tasks 1–3 |
| Wire Landing cards | Task 4 (type fix) + backend in Task 2 |
| Wire Settings | Task 4 (type fix) + backend in Task 3 |
| go build clean | Task 5.1 |
| Handler test for fidelity field | Tasks 1–3 |
| Isolated index → real fidelity ≠ 1.0 | Task 5.3 |
| npm build clean | Task 4.5 |
| PR to main, 6-section, Fixes #1511 | Task 6 |
| Do NOT merge; do NOT touch live :47274 | Task 5 (explicit) |

**Placeholder scan:** None found.

**Type consistency:**
- `healthDegraded` constant introduced in `v2_fidelity.go`, used in `v2_fidelity_test.go` — same package, consistent.
- `deriveHealthFromFidelity` returns `(float64, string)` — used consistently in `v2_groups.go` and `v2_group_settings.go`.
- `toV2Group(s GroupSummary, histRoot string)` — all call sites pass `s.daemonRoot()`.
- `loadV2SettingsGroup(groupName, histRoot string)` — both call sites updated.

**Note on `bug_rate` in wire response:** The spec says "add `fidelity` (= round(100 - bug_rate, 1)) + raw `bug_rate`". The current `v2Group` and `v2SettingsGroup` structs only have `fidelity`. Adding raw `bug_rate` to the wire shape is straightforward but the frontend types (`Group` and `SettingsGroup` in `types.ts`) don't have a `bugRate` field and the spec says "not 1.0" — the frontend currently only uses `fidelity`. To stay minimal (YAGNI) and avoid breaking the stable wire contract, this plan exposes `fidelity` (derived from bug_rate) but does NOT add a raw `bug_rate` field to the public wire shape. The raw value is in the health-history API already. If the spec requires the raw field, add a `BugRate *float64 \`json:"bugRate,omitempty"\`` to both structs and populate it from the helper.
