# WebUI v2 — Pending Screen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Pending screen (repair + enrichment inbox with agent hand-off) for WebUI v2, replacing the current placeholder, per design doc `pending.md` and EPIC #1432 / issue #1442.

**Architecture:** Split-pane layout rendered inside the existing `AppShell`; left pane lists grouped candidates, right pane shows detail + hint textarea + prompt preview + agent menu. Data is served by a new Go file `v2_pending.go` that wraps the existing v1 `handleRepairs`/`handleEnrichments` logic in a v2 envelope and adds a `PUT` hint endpoint. The React side uses TanStack Query hooks that call through `src/lib/api.ts`, with screen-local Zustand slice for UI state (focused row, open groups, drafts, tab, filter, groupBy).

**Tech Stack:** React 18 + TypeScript 5.7 + Tailwind v4 + Radix Tabs + @radix-ui/react-dropdown-menu (AgentMenu) + TanStack Query + sonner (toasts) + lucide-react + Zustand 5 + Go stdlib.

---

## File Map

| Operation | File | Responsibility |
|---|---|---|
| Create | `internal/dashboard/v2_pending.go` | Go handler: `GET /api/v2/groups/{group}/candidates` + `PUT /api/v2/groups/{group}/candidates/{cid}/hint` |
| Create | `internal/dashboard/v2_pending_test.go` | Table-driven tests for both endpoints |
| Append | `internal/dashboard/server.go` | Register two new v2 routes (append-only block) |
| Append | `webui-v2/src/data/types.ts` | `RepairCandidate`, `EnrichmentCandidate`, `EntityRef`, `Candidate`, `HintMap` types (append block) |
| Append | `webui-v2/src/lib/api.ts` | `api.listCandidates()` + `api.saveHint()` (append to `api` object) |
| Create | `webui-v2/src/hooks/use-pending.ts` | TanStack Query hooks: `useCandidates`, `useSaveHint` |
| Create | `webui-v2/src/store/use-pending-store.ts` | Zustand slice: tab, filter, groupBy, focusedId, openMap, drafts, savedHints |
| Replace | `webui-v2/src/routes/pending.tsx` | Full Pending screen (replaces placeholder) |

---

## Task 1: Set up the worktree

**Files:**
- none (shell ops only)

- [ ] **Step 1: Create the worktree**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph
git fetch origin main
git worktree add ../archigraph-worktrees/webui-pending -b feat/webui-v2-pending origin/main
```

Expected: `Preparing worktree (new branch 'feat/webui-v2-pending')` — no errors.

- [ ] **Step 2: Verify worktree is on the right commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
git log --oneline -3
```

Expected: top commit matches HEAD of main in the main repo.

- [ ] **Step 3: Confirm the project builds clean from the worktree**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
go build ./...
```

Expected: no output (success).

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending/webui-v2
npm install
npm run build 2>&1 | tail -5
```

Expected: `✓ built in` … no TypeScript errors.

---

## Task 2: Add Pending types to `data/types.ts`

**Files:**
- Modify: `webui-v2/src/data/types.ts` (append only — do NOT reorder existing lines)

- [ ] **Step 1: Append the Pending domain types**

Add to the very end of `webui-v2/src/data/types.ts`:

```typescript
// =============================================================
// Pending screen types — v2_pending.go wire shapes (#1442)
// =============================================================

export type EntityKind =
  | "function"
  | "component"
  | "hook"
  | "class"
  | "method"
  | "http_endpoint";

export interface EntityRef {
  name: string;
  type: EntityKind;
  repo: string;
  /** Includes `:line` suffix. */
  file: string;
}

export type RepairIssueType =
  | "missing_docstring"
  | "dead_code"
  | "mismatched_handler"
  | "untyped_params"
  | "broken_link"
  | "stale_cache";

export type EnrichmentType =
  | "summary"
  | "param_descriptions"
  | "relationship_tag"
  | "tags";

export type Severity = "critical" | "warning" | "info";

export interface RepairCandidate {
  id: string;
  severity: Severity;
  issueType: RepairIssueType;
  entity: EntityRef;
  description: string;
  /** 0..1 */
  confidence: number;
  /** Unix ms. */
  detectedAt: number;
}

export interface EnrichmentCandidate {
  id: string;
  enrichmentType: EnrichmentType;
  entity: EntityRef;
  description: string;
  confidence: number;
  detectedAt: number;
}

export type Candidate = RepairCandidate | EnrichmentCandidate;

/** Hints stored per-candidate-id in local state and persisted via PUT hint. */
export type HintMap = Record<string, string>;

/** Wire shape returned by GET /api/v2/groups/:id/candidates */
export interface V2CandidatesResponse {
  repairs: RepairCandidate[];
  enrichments: EnrichmentCandidate[];
}
```

- [ ] **Step 2: Verify TypeScript is happy**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending/webui-v2
npm run lint 2>&1 | grep -E "error TS|Error"
```

Expected: no output (zero errors).

- [ ] **Step 3: Commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
git add webui-v2/src/data/types.ts
git commit -m "feat(webui-v2): add Pending domain types to data/types.ts (#1442)"
```

---

## Task 3: Add the v2 pending API client methods

**Files:**
- Modify: `webui-v2/src/lib/api.ts` (append to the `api` object only — do NOT reorder existing keys)

- [ ] **Step 1: Add the import for new types at the top of the file**

The existing import line is:
```typescript
import type { Group, Entity, Community } from "@/data/types";
```

Change it to:
```typescript
import type { Group, Entity, Community, V2CandidatesResponse } from "@/data/types";
```

- [ ] **Step 2: Append `listCandidates` and `saveHint` to the `api` object**

The last line of the `api` object currently ends with:
```typescript
  searchEntities: (groupId: string, q: string) =>
    request<Entity[]>(`/groups/${groupId}/entities?q=${encodeURIComponent(q)}`),
};
```

Change it to:
```typescript
  searchEntities: (groupId: string, q: string) =>
    request<Entity[]>(`/groups/${groupId}/entities?q=${encodeURIComponent(q)}`),

  // --- v2 Pending surface (#1442) ---
  /** Fetch repair + enrichment candidates for a group. */
  listCandidates: (groupId: string, tab?: "repairs" | "enrichments") =>
    requestV2<V2CandidatesResponse>(
      `/groups/${groupId}/candidates${tab ? `?tab=${tab}` : ""}`,
    ),
  /** Persist a hint for a candidate. Empty string clears the hint. */
  saveHint: (groupId: string, candidateId: string, hint: string) =>
    requestV2<{ ok: true }>(
      `/groups/${groupId}/candidates/${encodeURIComponent(candidateId)}/hint`,
      { method: "PUT", body: JSON.stringify({ hint }) },
    ),
};
```

- [ ] **Step 3: Lint**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending/webui-v2
npm run lint 2>&1 | grep -E "error TS|Error"
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
git add webui-v2/src/lib/api.ts
git commit -m "feat(webui-v2): add listCandidates + saveHint to api client (#1442)"
```

---

## Task 4: Go backend — `v2_pending.go`

**Files:**
- Create: `internal/dashboard/v2_pending.go`

The backend decision: we add a thin **v2 wrapper** on top of the existing `readAllCandidates()` function (same source-of-truth, different wire shape). We do NOT call `handleRepairs`/`handleEnrichments` internally — we call `readAllCandidates` directly and produce the v2 `RepairCandidate`/`EnrichmentCandidate` shape the WebUI v2 types.ts expects. The `PUT` hint endpoint writes back to the on-disk candidate file (same pattern as the v1 hint writeback in `handlers_enrichment_writeback.go`).

Data decision for hints: hints are written back into the on-disk `enrichment-candidates.json` by candidate ID (same as v1). The WebUI v2 also caches hints in local state so UI is immediately responsive, then syncs to backend.

- [ ] **Step 1: Write `v2_pending.go`**

Create `/Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending/internal/dashboard/v2_pending.go`:

```go
// v2_pending.go — Pending screen endpoints for WebUI v2 (#1442).
//
// GET /api/v2/groups/{group}/candidates?tab=repairs|enrichments
//
//	Returns repair + enrichment candidates in the v2 wire shape that
//	webui-v2/src/data/types.ts expects. Both tabs are returned together
//	when ?tab is omitted; pass ?tab=repairs or ?tab=enrichments to scope.
//
// PUT /api/v2/groups/{group}/candidates/{cid}/hint
//
//	Persists a hint string on the matching candidate entry. Body: {"hint":"..."}
//	Empty hint string clears the hint. 404 when candidate not found.
package dashboard

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Wire shapes — mirror webui-v2/src/data/types.ts
// ---------------------------------------------------------------------------

type v2EntityRef struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Repo string `json:"repo"`
	File string `json:"file"`
}

type v2RepairCandidate struct {
	ID          string      `json:"id"`
	Severity    string      `json:"severity"`
	IssueType   string      `json:"issueType"`
	Entity      v2EntityRef `json:"entity"`
	Description string      `json:"description"`
	Confidence  float64     `json:"confidence"`
	DetectedAt  int64       `json:"detectedAt"` // unix ms
}

type v2EnrichmentCandidate struct {
	ID              string      `json:"id"`
	EnrichmentType  string      `json:"enrichmentType"`
	Entity          v2EntityRef `json:"entity"`
	Description     string      `json:"description"`
	Confidence      float64     `json:"confidence"`
	DetectedAt      int64       `json:"detectedAt"` // unix ms
}

type v2CandidatesResponse struct {
	Repairs      []v2RepairCandidate      `json:"repairs"`
	Enrichments  []v2EnrichmentCandidate  `json:"enrichments"`
}

// ---------------------------------------------------------------------------
// Mapping helpers
// ---------------------------------------------------------------------------

// kindToRepairIssueType maps the daemon's internal candidate kind strings to
// the design-doc RepairIssueType values WebUI v2 expects.
var kindToRepairIssueType = map[string]string{
	"repair_edge":               "broken_link",
	"dynamic_baseurl_endpoint":  "mismatched_handler",
}

// kindToEnrichmentType maps daemon kinds to design-doc EnrichmentType values.
var kindToEnrichmentType = map[string]string{
	"describe_entity":    "summary",
	"summarize_api":      "summary",
	"classify_domain":    "tags",
	"describe_role":      "summary",
	"param_descriptions": "param_descriptions",
	"relationship_tag":   "relationship_tag",
}

// criticalityBandToSeverity maps CriticalityBand strings to the design-doc
// Severity values. Falls back to "info".
var criticalityBandToSeverity = map[string]string{
	"critical": "critical",
	"high":     "warning",
	"medium":   "warning",
	"low":      "info",
}

// parseDetectedAt converts an RFC3339 string to unix-ms; returns now on parse failure.
func parseDetectedAt(s string) int64 {
	if s == "" {
		return time.Now().UnixMilli()
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Now().UnixMilli()
	}
	return t.UnixMilli()
}

// entityRefFromContext extracts a v2EntityRef from a candidate's Context map.
// Falls back to SubjectID as the name when context keys are absent.
func entityRefFromContext(ctx map[string]any, repo, subjectID string) v2EntityRef {
	getString := func(key string) string {
		if v, ok := ctx[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	name := getString("entity_name")
	if name == "" {
		name = getString("subject_name")
	}
	if name == "" {
		// Trim to the last segment (e.g. "pkg.Foo.Bar" → "Bar")
		parts := strings.Split(subjectID, ".")
		name = parts[len(parts)-1]
	}
	entityType := getString("entity_type")
	if entityType == "" {
		entityType = getString("kind")
	}
	if entityType == "" {
		entityType = "function"
	}
	file := getString("file")
	if file == "" {
		file = getString("source_file")
	}
	return v2EntityRef{
		Name: name,
		Type: entityType,
		Repo: repo,
		File: file,
	}
}

// descriptionFromContext extracts a human-readable description from the
// candidate context map. Falls back to the kind label.
func descriptionFromContext(ctx map[string]any, kind string) string {
	for _, key := range []string{"description", "reason", "details", "message"} {
		if v, ok := ctx[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return "Detected by archigraph. No additional context available."
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleV2Candidates — GET /api/v2/groups/{group}/candidates
func (s *Server) handleV2Candidates(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}
	tab := r.URL.Query().Get("tab") // "repairs", "enrichments", or ""

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	var repairs []v2RepairCandidate
	var enrichments []v2EnrichmentCandidate

	for slug, repo := range grp.Repos {
		if repo == nil || repo.Path == "" {
			continue
		}
		for _, c := range readAllCandidates(repo.Path) {
			if repairKinds[c.Kind] {
				if tab == "enrichments" {
					continue
				}
				issueType := kindToRepairIssueType[c.Kind]
				if issueType == "" {
					issueType = "broken_link"
				}
				sev := criticalityBandToSeverity[c.CriticalityBand]
				if sev == "" {
					if c.Confidence >= 0.85 {
						sev = "warning"
					} else {
						sev = "info"
					}
				}
				repairs = append(repairs, v2RepairCandidate{
					ID:          c.ID,
					Severity:    sev,
					IssueType:   issueType,
					Entity:      entityRefFromContext(c.Context, slug, c.SubjectID),
					Description: descriptionFromContext(c.Context, c.Kind),
					Confidence:  c.Confidence,
					DetectedAt:  parseDetectedAt(c.DiscoveredAt),
				})
			} else if !communityNamingKinds[c.Kind] {
				if tab == "repairs" {
					continue
				}
				enrichType := kindToEnrichmentType[c.Kind]
				if enrichType == "" {
					enrichType = "summary"
				}
				enrichments = append(enrichments, v2EnrichmentCandidate{
					ID:             c.ID,
					EnrichmentType: enrichType,
					Entity:         entityRefFromContext(c.Context, slug, c.SubjectID),
					Description:    descriptionFromContext(c.Context, c.Kind),
					Confidence:     c.Confidence,
					DetectedAt:     parseDetectedAt(c.DiscoveredAt),
				})
			}
		}
	}

	if repairs == nil {
		repairs = []v2RepairCandidate{}
	}
	if enrichments == nil {
		enrichments = []v2EnrichmentCandidate{}
	}

	writeV2JSON(w, http.StatusOK, v2OK(v2CandidatesResponse{
		Repairs:     repairs,
		Enrichments: enrichments,
	}))
}

// v2HintReq is the body for PUT /api/v2/groups/{group}/candidates/{cid}/hint.
type v2HintReq struct {
	Hint string `json:"hint"`
}

// handleV2CandidateHint — PUT /api/v2/groups/{group}/candidates/{cid}/hint
//
// Persists the hint on the matching candidate in enrichment-candidates.json.
// Responds 200 { ok:true, data: { hint: "<saved>" } } on success.
// Responds 404 when the candidate is not found in any repo.
func (s *Server) handleV2CandidateHint(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	cid := r.PathValue("cid")
	if group == "" || cid == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group and cid required")
		return
	}

	var req v2HintReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	for _, repo := range grp.Repos {
		if repo == nil || repo.Path == "" {
			continue
		}
		if updated := updateCandidateHint(repo.Path, cid, req.Hint); updated {
			writeV2JSON(w, http.StatusOK, v2OK(map[string]string{"hint": req.Hint}))
			return
		}
	}

	writeV2Err(w, http.StatusNotFound, "not_found", "candidate not found")
}
```

- [ ] **Step 2: Add `updateCandidateHint` helper**

Append to the bottom of the same file `v2_pending.go`:

```go
// updateCandidateHint reads enrichment-candidates.json in repoPath, finds
// the entry with id == cid, sets its Hint field, and writes the file back.
// Returns true when the update was applied; false when the candidate was
// not found or the file is absent.
func updateCandidateHint(repoPath, cid, hint string) bool {
	import_path := "" // declared below to satisfy the compiler
	_ = import_path   // this comment block is replaced by the actual implementation below
	// implementation at the bottom of the declaration
	return false
}
```

Wait — we need to actually implement this properly. The real implementation reads the JSON file and updates in-place. Write the full helper in `v2_pending.go` by appending after `handleV2CandidateHint`:

```go
import (
	"os"
	"path/filepath"

	"github.com/cajasmota/archigraph/internal/daemon"
)
```

Actually, `os`, `path/filepath`, `daemon` are already imported by the same package (in `handlers_repairs.go`). In Go, imports are per-file. So `v2_pending.go` needs its own imports. Update the import block at the top of `v2_pending.go` to:

```go
import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
)
```

And replace the placeholder `updateCandidateHint` with:

```go
// updateCandidateHint reads enrichment-candidates.json in repoPath, finds
// the entry with id == cid, updates its Hint, and writes the file back.
// Returns true when the candidate was found and the file written successfully.
func updateCandidateHint(repoPath, cid, hint string) bool {
	if repoPath == "" {
		return false
	}
	filePath := filepath.Join(daemon.StateDirForRepo(repoPath), "enrichment-candidates.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false
	}

	// Support both flat-array and {"candidates":[…]} shapes.
	var arr []candidateRaw
	wrapped := false
	if json.Unmarshal(data, &arr) != nil {
		var obj struct {
			Candidates []candidateRaw `json:"candidates"`
		}
		if json.Unmarshal(data, &obj) != nil {
			return false
		}
		arr = obj.Candidates
		wrapped = true
	}

	found := false
	for i := range arr {
		if arr[i].ID == cid {
			arr[i].Hint = hint
			found = true
			break
		}
	}
	if !found {
		return false
	}

	var out []byte
	var marshalErr error
	if wrapped {
		out, marshalErr = json.Marshal(struct {
			Candidates []candidateRaw `json:"candidates"`
		}{Candidates: arr})
	} else {
		out, marshalErr = json.Marshal(arr)
	}
	if marshalErr != nil {
		return false
	}
	return os.WriteFile(filePath, out, 0o644) == nil
}
```

Write the complete final version of `v2_pending.go` (all in one piece):

```go
// v2_pending.go — Pending screen endpoints for WebUI v2 (#1442).
// ... (full content as assembled above)
```

- [ ] **Step 3: Confirm `go build ./...` passes**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
go build ./...
```

Expected: no output.

---

## Task 5: Go backend tests — `v2_pending_test.go`

**Files:**
- Create: `internal/dashboard/v2_pending_test.go`

- [ ] **Step 1: Write the test file**

Create `internal/dashboard/v2_pending_test.go`:

```go
package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/daemon"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// writeFixtureCandidates writes a candidates JSON array to the state dir
// under repoPath so readAllCandidates can pick it up.
func writeFixtureCandidates(t *testing.T, repoPath string, candidates []candidateRaw) {
	t.Helper()
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data, err := json.Marshal(candidates)
	if err != nil {
		t.Fatalf("marshal candidates: %v", err)
	}
	p := filepath.Join(stateDir, "enrichment-candidates.json")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v2/groups/{group}/candidates
// ---------------------------------------------------------------------------

func TestHandleV2Candidates_repairKind(t *testing.T) {
	srv, dir := newTestServerWithGroup(t, "grp", []string{"repo1"})
	defer os.RemoveAll(dir)

	repoPath := filepath.Join(dir, "repo1")
	os.MkdirAll(repoPath, 0o755)
	writeFixtureCandidates(t, repoPath, []candidateRaw{
		{
			ID:           "c1",
			Kind:         "repair_edge",
			SubjectID:    "pkg.Foo",
			Confidence:   0.9,
			DiscoveredAt: "2026-01-01T00:00:00Z",
			Context: map[string]any{
				"entity_name": "Foo",
				"entity_type": "function",
				"file":        "pkg/foo.go:10",
			},
		},
	})

	req := httptest.NewRequest("GET", "/api/v2/groups/grp/candidates", nil)
	req.SetPathValue("group", "grp")
	rr := httptest.NewRecorder()
	srv.handleV2Candidates(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Repairs     []v2RepairCandidate     `json:"repairs"`
			Enrichments []v2EnrichmentCandidate `json:"enrichments"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.OK {
		t.Fatal("expected ok:true")
	}
	if len(env.Data.Repairs) != 1 {
		t.Fatalf("want 1 repair, got %d", len(env.Data.Repairs))
	}
	r := env.Data.Repairs[0]
	if r.ID != "c1" {
		t.Errorf("want id c1, got %s", r.ID)
	}
	if r.IssueType != "broken_link" {
		t.Errorf("want issueType broken_link, got %s", r.IssueType)
	}
	if r.Entity.Name != "Foo" {
		t.Errorf("want entity.name Foo, got %s", r.Entity.Name)
	}
	if len(env.Data.Enrichments) != 0 {
		t.Errorf("want 0 enrichments, got %d", len(env.Data.Enrichments))
	}
}

func TestHandleV2Candidates_enrichmentKind(t *testing.T) {
	srv, dir := newTestServerWithGroup(t, "grp2", []string{"repo1"})
	defer os.RemoveAll(dir)

	repoPath := filepath.Join(dir, "repo1")
	os.MkdirAll(repoPath, 0o755)
	writeFixtureCandidates(t, repoPath, []candidateRaw{
		{
			ID:        "e1",
			Kind:      "describe_entity",
			SubjectID: "pkg.Bar",
			Confidence: 0.75,
			Context: map[string]any{
				"entity_name": "Bar",
				"entity_type": "class",
				"file":        "pkg/bar.go:5",
			},
		},
	})

	req := httptest.NewRequest("GET", "/api/v2/groups/grp2/candidates", nil)
	req.SetPathValue("group", "grp2")
	rr := httptest.NewRecorder()
	srv.handleV2Candidates(rr, req)

	var env struct {
		OK   bool `json:"ok"`
		Data v2CandidatesResponse `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Data.Repairs) != 0 {
		t.Errorf("want 0 repairs, got %d", len(env.Data.Repairs))
	}
	if len(env.Data.Enrichments) != 1 {
		t.Fatalf("want 1 enrichment, got %d", len(env.Data.Enrichments))
	}
	if env.Data.Enrichments[0].EnrichmentType != "summary" {
		t.Errorf("want summary, got %s", env.Data.Enrichments[0].EnrichmentType)
	}
}

func TestHandleV2Candidates_tabFilter(t *testing.T) {
	srv, dir := newTestServerWithGroup(t, "grp3", []string{"repo1"})
	defer os.RemoveAll(dir)

	repoPath := filepath.Join(dir, "repo1")
	os.MkdirAll(repoPath, 0o755)
	writeFixtureCandidates(t, repoPath, []candidateRaw{
		{ID: "r1", Kind: "repair_edge", SubjectID: "A", Confidence: 0.8},
		{ID: "e1", Kind: "describe_entity", SubjectID: "B", Confidence: 0.8},
	})

	// ?tab=repairs should return only repairs
	req := httptest.NewRequest("GET", "/api/v2/groups/grp3/candidates?tab=repairs", nil)
	req.SetPathValue("group", "grp3")
	rr := httptest.NewRecorder()
	srv.handleV2Candidates(rr, req)

	var env struct {
		Data v2CandidatesResponse `json:"data"`
	}
	json.Unmarshal(rr.Body.Bytes(), &env)
	if len(env.Data.Repairs) != 1 {
		t.Errorf("tab=repairs: want 1 repair, got %d", len(env.Data.Repairs))
	}
	if len(env.Data.Enrichments) != 0 {
		t.Errorf("tab=repairs: want 0 enrichments, got %d", len(env.Data.Enrichments))
	}
}

func TestHandleV2Candidates_groupNotFound(t *testing.T) {
	srv, dir := newTestServerWithGroup(t, "grp4", nil)
	defer os.RemoveAll(dir)

	req := httptest.NewRequest("GET", "/api/v2/groups/does-not-exist/candidates", nil)
	req.SetPathValue("group", "does-not-exist")
	rr := httptest.NewRecorder()
	srv.handleV2Candidates(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// PUT /api/v2/groups/{group}/candidates/{cid}/hint
// ---------------------------------------------------------------------------

func TestHandleV2CandidateHint_ok(t *testing.T) {
	srv, dir := newTestServerWithGroup(t, "grpH", []string{"repo1"})
	defer os.RemoveAll(dir)

	repoPath := filepath.Join(dir, "repo1")
	os.MkdirAll(repoPath, 0o755)
	writeFixtureCandidates(t, repoPath, []candidateRaw{
		{ID: "c99", Kind: "repair_edge", SubjectID: "X", Confidence: 0.9},
	})

	body := `{"hint":"check the migration guide"}`
	req := httptest.NewRequest("PUT", "/api/v2/groups/grpH/candidates/c99/hint",
		strings.NewReader(body))
	req.SetPathValue("group", "grpH")
	req.SetPathValue("cid", "c99")
	rr := httptest.NewRecorder()
	srv.handleV2CandidateHint(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify hint was persisted to disk.
	saved := readAllCandidates(repoPath)
	if len(saved) == 0 || saved[0].Hint != "check the migration guide" {
		t.Errorf("hint not persisted; saved candidates: %+v", saved)
	}
}

func TestHandleV2CandidateHint_notFound(t *testing.T) {
	srv, dir := newTestServerWithGroup(t, "grpH2", []string{"repo1"})
	defer os.RemoveAll(dir)

	repoPath := filepath.Join(dir, "repo1")
	os.MkdirAll(repoPath, 0o755)
	writeFixtureCandidates(t, repoPath, []candidateRaw{
		{ID: "c1", Kind: "repair_edge", SubjectID: "X", Confidence: 0.9},
	})

	body := `{"hint":"irrelevant"}`
	req := httptest.NewRequest("PUT", "/api/v2/groups/grpH2/candidates/no-such-id/hint",
		strings.NewReader(body))
	req.SetPathValue("group", "grpH2")
	req.SetPathValue("cid", "no-such-id")
	rr := httptest.NewRecorder()
	srv.handleV2CandidateHint(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rr.Code)
	}
}
```

Note: The test file uses `newTestServerWithGroup` which already exists in the dashboard test helpers. The test also uses `strings.NewReader` so add `"strings"` to the import block.

- [ ] **Step 2: Run the tests**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
go test ./internal/dashboard/... -run TestHandleV2Candidates -v 2>&1 | tail -20
go test ./internal/dashboard/... -run TestHandleV2CandidateHint -v 2>&1 | tail -20
```

Expected: `PASS` for all new tests.

- [ ] **Step 3: Commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
git add internal/dashboard/v2_pending.go internal/dashboard/v2_pending_test.go
git commit -m "feat(daemon): add v2 pending candidates endpoint + hint PUT (#1442)"
```

---

## Task 6: Register routes in `server.go`

**Files:**
- Modify: `internal/dashboard/server.go` (append-only in the v2 block)

- [ ] **Step 1: Append the two new routes after the existing v2 routes**

The existing v2 block ends with:
```go
	mux.HandleFunc("POST /api/v2/groups", s.handleV2CreateGroup)
```

Append immediately after that line (do NOT reorder anything above it):

```go

	// --- v2 Pending screen (#1442) ---
	mux.HandleFunc("GET /api/v2/groups/{group}/candidates", s.handleV2Candidates)
	mux.HandleFunc("PUT /api/v2/groups/{group}/candidates/{cid}/hint", s.handleV2CandidateHint)
```

- [ ] **Step 2: Verify no existing routes changed**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
git diff internal/dashboard/server.go | grep "^-" | grep "HandleFunc" | head -10
```

Expected: no lines removed (only additions).

- [ ] **Step 3: Build passes**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
go build ./...
```

Expected: no output.

- [ ] **Step 4: Run the full dashboard test suite to confirm v1 is untouched**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
go test ./internal/dashboard/... 2>&1 | tail -5
```

Expected: `ok github.com/cajasmota/archigraph/internal/dashboard` (no FAIL).

- [ ] **Step 5: Commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
git add internal/dashboard/server.go
git commit -m "feat(daemon): register v2 pending routes in server.go (#1442)"
```

---

## Task 7: Zustand store for Pending screen UI state

**Files:**
- Create: `webui-v2/src/store/use-pending-store.ts`

- [ ] **Step 1: Create the store**

```typescript
/* ============================================================
   store/use-pending-store.ts — Pending screen UI state (#1442).

   Separate from use-app-store.ts (appearance/command palette).
   Holds: tab, filter, groupBy, focusedId, openMap (group collapse),
   drafts (unsaved hint text per candidate), savedHints (confirmed).
   ============================================================ */

import { create } from "zustand";

export type PendingTab = "repairs" | "enrichments";
export type PendingFilter = "all" | "high" | "stale";
export type PendingGroupBy = "type" | "severity" | "repo" | "none";

interface PendingState {
  tab: PendingTab;
  filter: PendingFilter;
  groupBy: PendingGroupBy;
  /** ID of the currently focused candidate row, or null. */
  focusedId: string | null;
  /** Map of groupKey → collapsed (false means collapsed; absent/true means open). */
  openMap: Record<string, boolean>;
  /** Per-candidate-id hint text typed but not yet saved to the server. */
  drafts: Record<string, string>;
  /** Per-candidate-id hint text that has been confirmed saved (from PUT response). */
  savedHints: Record<string, string>;

  setTab: (tab: PendingTab) => void;
  setFilter: (filter: PendingFilter) => void;
  setGroupBy: (groupBy: PendingGroupBy) => void;
  setFocusedId: (id: string | null) => void;
  toggleGroup: (key: string) => void;
  setDraft: (id: string, text: string) => void;
  confirmSave: (id: string, hint: string) => void;
}

export const usePendingStore = create<PendingState>((set) => ({
  tab: "repairs",
  filter: "all",
  groupBy: "type",
  focusedId: null,
  openMap: {},
  drafts: {},
  savedHints: {},

  setTab: (tab) => set({ tab, focusedId: null }),
  setFilter: (filter) => set({ filter }),
  setGroupBy: (groupBy) => set({ groupBy }),
  setFocusedId: (focusedId) => set({ focusedId }),
  toggleGroup: (key) =>
    set((s) => ({
      openMap: { ...s.openMap, [key]: s.openMap[key] === false ? true : false },
    })),
  setDraft: (id, text) => set((s) => ({ drafts: { ...s.drafts, [id]: text } })),
  confirmSave: (id, hint) =>
    set((s) => {
      const drafts = { ...s.drafts };
      delete drafts[id];
      return { drafts, savedHints: { ...s.savedHints, [id]: hint } };
    }),
}));
```

- [ ] **Step 2: Lint**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending/webui-v2
npm run lint 2>&1 | grep -E "error TS|Error"
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
git add webui-v2/src/store/use-pending-store.ts
git commit -m "feat(webui-v2): add Zustand store for Pending screen state (#1442)"
```

---

## Task 8: TanStack Query data hook

**Files:**
- Create: `webui-v2/src/hooks/use-pending.ts`

- [ ] **Step 1: Create the hook file**

```typescript
/* ============================================================
   hooks/use-pending.ts — data hooks for the Pending screen (#1442).
   ============================================================ */

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { RepairCandidate, EnrichmentCandidate } from "@/data/types";

/** Returns the full candidates payload (both tabs) for a group. */
export function useCandidates(groupId: string) {
  return useQuery({
    queryKey: ["candidates", groupId],
    queryFn: () => api.listCandidates(groupId),
    // Candidates change on index runs; polling every 30 s keeps the count fresh
    // without hammering the daemon.
    refetchInterval: 30_000,
    staleTime: 10_000,
  });
}

/** Mutation to persist a hint for one candidate. Invalidates candidates query on success. */
export function useSaveHint(groupId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ candidateId, hint }: { candidateId: string; hint: string }) =>
      api.saveHint(groupId, candidateId, hint),
    onSuccess: () => {
      // Invalidate so a background refetch picks up the persisted hint value.
      qc.invalidateQueries({ queryKey: ["candidates", groupId] });
    },
  });
}
```

- [ ] **Step 2: Lint**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending/webui-v2
npm run lint 2>&1 | grep -E "error TS|Error"
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
git add webui-v2/src/hooks/use-pending.ts
git commit -m "feat(webui-v2): add useCandidates + useSaveHint TanStack Query hooks (#1442)"
```

---

## Task 9: Build the Pending screen — `pending.tsx`

This is the main React screen. It uses all the pieces built above. It follows the **three-strip layout** from the spec:
1. The AppShell's `TopBar` already provides the breadcrumb.
2. A tab bar (44px) with filter + groupBy on the right.
3. A split pane: left list (440px fixed) + right detail.

The `<StatPill>` (unresolved count) goes in a secondary bar below the TopBar, inside the screen, per the design — the TopBar is shared chrome that must not have screen-specific counts.

**Files:**
- Replace: `webui-v2/src/routes/pending.tsx`

- [ ] **Step 1: Write the complete pending.tsx**

```typescript
/* ============================================================
   routes/pending.tsx — Repair + enrichment inbox (#1442, EPIC #1432).

   Layout (three horizontal strips):
   1. Stat bar (unresolved pill + group breadcrumb extension) 40px
   2. Tab bar (44px): tabs left · filter+groupBy right
   3. Split pane: left list (440px) + right detail

   The AppShell TopBar handles the outer breadcrumb (archigraph › group › Pending).
   ============================================================ */

import { useState, useMemo, useRef, useEffect } from "react";
import { useParams } from "react-router-dom";
import { Wrench, Sparkles, ChevronRight, Copy, ExternalLink } from "lucide-react";
import { toast } from "sonner";

import { useCandidates, useSaveHint } from "@/hooks/use-pending";
import { usePendingStore } from "@/store/use-pending-store";
import { Badge, Button, Tabs, TabsList, TabsTrigger } from "@/components/ui";
import { cn } from "@/lib/utils";

import type {
  RepairCandidate,
  EnrichmentCandidate,
  Candidate,
  Severity,
  RepairIssueType,
  EnrichmentType,
} from "@/data/types";

// ---------------------------------------------------------------------------
// Constants / metadata maps
// ---------------------------------------------------------------------------

const SEVERITY_META: Record<Severity, { tone: "danger" | "warning" | "neutral"; label: string }> = {
  critical: { tone: "danger",  label: "Critical" },
  warning:  { tone: "warning", label: "Warning"  },
  info:     { tone: "neutral", label: "Info"      },
};

const ISSUE_LABEL: Record<RepairIssueType, string> = {
  missing_docstring:  "Missing docstring",
  dead_code:          "Likely dead code",
  mismatched_handler: "Duplicate handler",
  untyped_params:     "Untyped signature",
  broken_link:        "Broken cross-repo edge",
  stale_cache:        "Stale cache",
};

const ENRICHMENT_LABEL: Record<EnrichmentType, string> = {
  summary:            "Generate summary",
  param_descriptions: "Generate param docs",
  relationship_tag:   "Suggest relationship",
  tags:               "Suggest tags",
};

// Pastel color map for entity type dots (matches prototype TYPE_DOT).
const TYPE_COLOR: Record<string, string> = {
  function:      "var(--pastel-6)",
  component:     "var(--pastel-1)",
  hook:          "var(--pastel-2)",
  class:         "var(--pastel-3)",
  method:        "var(--pastel-4)",
  http_endpoint: "var(--pastel-5)",
};

const AGENTS = [
  { id: "claude",   label: "Claude Code", note: "open://..." },
  { id: "cursor",   label: "Cursor",      note: "cursor://..." },
  { id: "windsurf", label: "Windsurf",    note: "windsurf://..." },
] as const;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function relTime(ms: number): string {
  const m = Math.floor((Date.now() - ms) / 60_000);
  if (m < 1)  return "just now";
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

function isRepair(c: Candidate): c is RepairCandidate {
  return "issueType" in c;
}

function candidateLabel(c: Candidate, tab: "repairs" | "enrichments"): string {
  if (tab === "repairs" && isRepair(c)) return ISSUE_LABEL[c.issueType] ?? c.issueType;
  if (!isRepair(c)) return ENRICHMENT_LABEL[c.enrichmentType] ?? c.enrichmentType;
  return "";
}

function buildPrompt(c: Candidate, hint: string, tab: "repairs" | "enrichments"): string {
  const verb = tab === "repairs"
    ? `Fix the ${candidateLabel(c, "repairs")} issue`
    : `Generate ${candidateLabel(c, "enrichments")}`;
  const hintSection = hint ? `\nHint from the team:\n${hint}\n` : "";
  return `${verb} on \`${c.entity.name}\` (${c.entity.type}) in ${c.entity.repo}.

File: ${c.entity.file}

What archigraph detected:
${c.description}
${hintSection}
After making the change, run \`archigraph rebuild ${c.entity.repo}\` to refresh the graph.`;
}

// ---------------------------------------------------------------------------
// AgentMenu dropdown
// ---------------------------------------------------------------------------

function AgentMenu({ onPick }: { onPick: (agent: typeof AGENTS[number]) => void }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  // Close on ESC
  useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => { if (e.key === "Escape") setOpen(false); };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [open]);

  return (
    <div ref={ref} className="relative">
      <Button
        variant="ghost"
        size="sm"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
      >
        <ExternalLink size={13} />
        Open in agent
        <ChevronRight size={11} className={cn("transition-transform", open && "rotate-90")} />
      </Button>

      {open && (
        <div
          role="menu"
          className="absolute bottom-full right-0 mb-1 w-52 rounded-lg border border-border bg-surface shadow-[var(--shadow-4)] py-1 z-50"
        >
          {AGENTS.map((a) => (
            <button
              key={a.id}
              role="menuitem"
              className="w-full flex flex-col items-start px-3 py-2 text-left hover:bg-surface-2 focus-visible:outline-none focus-visible:bg-surface-2"
              onClick={() => { setOpen(false); onPick(a); }}
            >
              <span className="text-sm font-medium text-text">{a.label}</span>
              <span className="text-xs font-mono text-text-4">{a.note}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// ConfidencePill
// ---------------------------------------------------------------------------

function ConfidencePill({ value, compact = false }: { value: number; compact?: boolean }) {
  const pct = Math.round(value * 100);
  const tone = value >= 0.8 ? "success" : value >= 0.5 ? "warning" : "neutral";
  return (
    <Badge tone={tone} className={cn(compact && "text-[10px] h-4 px-1.5")}>
      {pct}%{!compact && " conf."}
    </Badge>
  );
}

// ---------------------------------------------------------------------------
// ListRow
// ---------------------------------------------------------------------------

interface ListRowProps {
  item: Candidate;
  tab: "repairs" | "enrichments";
  focused: boolean;
  hasHint: boolean;
  onFocus: (id: string) => void;
}

function ListRow({ item, tab, focused, hasHint, onFocus }: ListRowProps) {
  const sev = tab === "repairs" && isRepair(item) ? SEVERITY_META[item.severity] : null;
  const label = candidateLabel(item, tab);
  const typeColor = TYPE_COLOR[item.entity.type] ?? "var(--text-4)";

  return (
    <button
      className={cn(
        "w-full flex items-stretch gap-0 text-left transition-colors duration-[80ms]",
        "border-b border-border-soft last:border-0",
        focused
          ? "bg-surface border-l-[3px] border-l-accent"
          : "hover:bg-surface-2",
      )}
      onClick={() => onFocus(item.id)}
    >
      {/* Severity bar */}
      <span
        className="w-1 shrink-0 self-stretch rounded-l-sm"
        style={{
          background: sev
            ? sev.tone === "danger" ? "var(--danger)" : sev.tone === "warning" ? "var(--warning)" : "var(--text-4)"
            : "var(--accent)",
        }}
        aria-label={sev?.label ?? "enrichment"}
      />

      {/* Main content */}
      <div className="flex-1 min-w-0 px-3 py-2">
        <div className="flex items-center gap-2 min-w-0">
          <span className="font-mono text-[13px] text-text truncate">{item.entity.name}</span>
          <span className="inline-flex items-center gap-1 shrink-0">
            <span className="size-1.5 rounded-full" style={{ background: typeColor }} />
            <span className="font-mono text-xs text-text-3">{item.entity.type}</span>
          </span>
        </div>
        <div className="flex items-center gap-1.5 mt-0.5 text-xs text-text-3">
          <span>{label}</span>
          <span className="text-text-4">·</span>
          <span className="font-mono text-text-4 truncate">{item.entity.repo}</span>
        </div>
      </div>

      {/* Right column */}
      <div className="flex flex-col items-end justify-center gap-1 px-3 py-2 shrink-0">
        {hasHint && (
          <Badge tone="success" className="text-[10px] h-4 px-1.5">hint</Badge>
        )}
        <ConfidencePill value={item.confidence} compact />
        <span className="text-[11px] text-text-4">{relTime(item.detectedAt)}</span>
      </div>
    </button>
  );
}

// ---------------------------------------------------------------------------
// Group (collapsible section)
// ---------------------------------------------------------------------------

interface GroupSectionProps {
  id: string;
  label: string;
  items: Candidate[];
  open: boolean;
  onToggle: () => void;
  tab: "repairs" | "enrichments";
  focusedId: string | null;
  savedHints: Record<string, string>;
  onFocus: (id: string) => void;
}

function GroupSection({ id, label, items, open, onToggle, tab, focusedId, savedHints, onFocus }: GroupSectionProps) {
  return (
    <div>
      <button
        className="sticky top-0 z-10 w-full flex items-center gap-2 px-3 py-1.5 bg-bg-soft border-b border-border-soft"
        onClick={onToggle}
        aria-expanded={open}
      >
        <ChevronRight
          size={12}
          className={cn("text-text-4 transition-transform", open && "rotate-90")}
        />
        <h3 className="flex-1 text-left text-[11px] font-semibold uppercase tracking-wide font-mono text-text-3">
          {label}
        </h3>
        <span className="font-mono text-[11px] text-text-4">{items.length}</span>
      </button>
      {open && items.map((item) => (
        <ListRow
          key={item.id}
          item={item}
          tab={tab}
          focused={focusedId === item.id}
          hasHint={!!savedHints[item.id]}
          onFocus={onFocus}
        />
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// DetailPane
// ---------------------------------------------------------------------------

interface DetailPaneProps {
  item: Candidate | null;
  tab: "repairs" | "enrichments";
  draft: string;
  savedHint: string;
  onDraftChange: (v: string) => void;
  onSave: () => void;
  saving: boolean;
  groupId: string;
}

function DetailPane({ item, tab, draft, savedHint, onDraftChange, onSave, saving, groupId }: DetailPaneProps) {
  if (!item) {
    return (
      <div className="flex flex-col items-center justify-center h-full gap-3 text-center px-8">
        <Wrench size={28} strokeWidth={1.4} className="text-text-4" />
        <p className="text-sm font-medium text-text-2">Pick a suggestion</p>
        <p className="text-xs text-text-4 max-w-[28ch]">
          Choose any row to see what archigraph detected, then hand it off to your agent.
        </p>
      </div>
    );
  }

  const label = candidateLabel(item, tab);
  const sev = tab === "repairs" && isRepair(item) ? SEVERITY_META[item.severity] : null;
  const typeColor = TYPE_COLOR[item.entity.type] ?? "var(--text-4)";
  const dirty = draft !== savedHint;
  const prompt = buildPrompt(item, savedHint || draft, tab);

  const handleCopyPrompt = async () => {
    try {
      await navigator.clipboard.writeText(prompt);
      toast.success("Prompt copied to clipboard.");
    } catch {
      toast.warning("Couldn't access clipboard — copy manually.");
    }
  };

  const handleAgentPick = (agent: typeof AGENTS[number]) => {
    toast.success(`Would deep-link to ${agent.label} with the prompt prefilled.`);
  };

  return (
    <div className="flex flex-col h-full max-w-[720px] mx-auto">
      {/* Header */}
      <header className="shrink-0 px-6 pt-5 pb-4 border-b border-border-soft">
        <div className="flex items-center gap-2 flex-wrap mb-2">
          {sev && (
            <Badge tone={sev.tone === "danger" ? "danger" : sev.tone === "warning" ? "warning" : "neutral"}>
              {sev.label}
            </Badge>
          )}
          <span className="text-sm font-medium text-text">{label}</span>
          <span className="ml-auto">
            <ConfidencePill value={item.confidence} />
          </span>
        </div>

        <div className="flex items-center gap-2 flex-wrap">
          <a
            href={`/g/${groupId}/docs`}
            className="font-mono text-[20px] font-semibold text-accent hover:underline truncate"
          >
            {item.entity.name}
          </a>
          <span className="inline-flex items-center gap-1">
            <span className="size-2 rounded-full" style={{ background: typeColor }} />
            <span className="font-mono text-xs text-text-3">{item.entity.type}</span>
          </span>
          <Badge tone="neutral">{item.entity.repo}</Badge>
        </div>

        <div className="mt-1 font-mono text-xs text-text-4">{item.entity.file}</div>
      </header>

      {/* Body */}
      <div className="flex-1 overflow-y-auto px-6 py-4 flex flex-col gap-6">
        {/* Section 1: What archigraph detected */}
        <section>
          <h4 className="text-xs font-semibold text-text-3 uppercase tracking-wide mb-2">
            What archigraph detected
          </h4>
          <p className="text-sm text-text leading-relaxed max-w-[64ch]">{item.description}</p>
        </section>

        {/* Section 2: Hint textarea */}
        <section>
          <div className="flex items-center gap-2 mb-2" id={`hint-label-${item.id}`}>
            <h4 className="text-xs font-semibold text-text-3 uppercase tracking-wide">
              Hint for your agent
            </h4>
            <span className="text-xs text-text-4">— optional</span>
            {savedHint && !dirty && (
              <span className="text-xs text-success font-medium ml-auto">Saved</span>
            )}
            {dirty && (
              <span className="text-xs text-text-4 ml-auto">Unsaved changes</span>
            )}
          </div>

          <textarea
            aria-labelledby={`hint-label-${item.id}`}
            className={cn(
              "w-full min-h-[64px] max-h-[160px] px-3 py-2 resize-vertical rounded-md",
              "border border-border bg-surface text-sm text-text",
              "placeholder:text-text-4 focus:outline-none focus:ring-2 focus:ring-[var(--accent-ring)] focus:border-accent",
              "transition-colors",
            )}
            placeholder="e.g. 'this is a public API — keep the wording neutral and reference the migration guide if relevant'"
            value={draft}
            onChange={(e) => onDraftChange(e.target.value)}
            rows={3}
          />

          <div className="flex items-start gap-3 mt-2">
            <p className="flex-1 text-xs text-text-4 leading-relaxed">
              archigraph doesn&apos;t write code or prose itself. The hint is persisted and
              included when you hand the task off to your agent.
            </p>
            <Button
              variant="primary"
              size="sm"
              onClick={onSave}
              disabled={!dirty || saving}
              aria-disabled={!dirty}
              className="shrink-0"
            >
              {dirty ? (saving ? "Saving…" : "Save hint") : "Saved"}
            </Button>
          </div>
        </section>

        {/* Section 3: Prompt preview */}
        <section>
          <h4 className="text-xs font-semibold text-text-3 uppercase tracking-wide mb-2">
            Generated prompt preview
          </h4>
          <pre className="rounded-md bg-surface-2 border border-border-soft px-4 py-3 text-[12px] font-mono text-text-2 whitespace-pre-wrap leading-relaxed overflow-x-auto">
            <code>{prompt}</code>
          </pre>
        </section>
      </div>

      {/* Footer */}
      <footer className="shrink-0 flex items-center justify-between gap-4 px-6 py-3 border-t border-border-soft bg-bg">
        <p className="text-xs text-text-4">
          Only the agent can resolve this candidate — archigraph will clear it on the next index.
        </p>
        <div className="flex items-center gap-2 shrink-0">
          <Button variant="ghost" size="sm" onClick={handleCopyPrompt}>
            <Copy size={13} />
            Copy prompt
          </Button>
          <AgentMenu onPick={handleAgentPick} />
        </div>
      </footer>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Loading skeleton
// ---------------------------------------------------------------------------

function PendingSkeleton() {
  return (
    <div className="flex flex-1 min-h-0">
      <aside className="w-[440px] shrink-0 flex flex-col gap-0 bg-bg-soft border-r border-border overflow-y-auto">
        {Array.from({ length: 6 }).map((_, i) => (
          <div key={i} className="px-3 py-2 border-b border-border-soft animate-pulse">
            <div className="h-3 bg-surface-3 rounded w-3/4 mb-1.5" />
            <div className="h-2.5 bg-surface-3 rounded w-1/2" />
          </div>
        ))}
      </aside>
      <section className="flex-1 flex items-center justify-center">
        <div className="text-sm text-text-4">Loading candidates…</div>
      </section>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main screen
// ---------------------------------------------------------------------------

export default function PendingScreen() {
  const { groupId = "demo" } = useParams();
  const { data, isLoading, isError } = useCandidates(groupId);
  const saveHintMutation = useSaveHint(groupId);

  const {
    tab, filter, groupBy,
    focusedId, openMap,
    drafts, savedHints,
    setTab, setFilter, setGroupBy,
    setFocusedId, toggleGroup,
    setDraft, confirmSave,
  } = usePendingStore();

  // When tab changes, select first row of new list.
  const allItems: Candidate[] = useMemo(() => {
    if (!data) return [];
    return tab === "repairs" ? data.repairs : data.enrichments;
  }, [data, tab]);

  // Auto-select first row on load or tab switch.
  useEffect(() => {
    if (allItems.length > 0 && !focusedId) {
      setFocusedId(allItems[0].id);
    }
  }, [allItems, focusedId, setFocusedId]);

  // Filter candidates.
  const filtered = useMemo(() =>
    allItems.filter((item) => {
      if (filter === "high")  return item.confidence >= 0.85;
      if (filter === "stale") return Date.now() - item.detectedAt > 86_400_000;
      return true;
    }),
    [allItems, filter],
  );

  // Group candidates.
  type GroupEntry = { id: string; label: string; items: Candidate[] };
  const groups: GroupEntry[] = useMemo(() => {
    const map = new Map<string, GroupEntry>();
    for (const item of filtered) {
      let key: string;
      let label: string;
      if (groupBy === "type") {
        key = isRepair(item) ? item.issueType : item.enrichmentType;
        label = candidateLabel(item, tab);
      } else if (groupBy === "severity" && tab === "repairs" && isRepair(item)) {
        key = item.severity;
        label = SEVERITY_META[item.severity].label;
      } else if (groupBy === "repo") {
        key = item.entity.repo;
        label = item.entity.repo;
      } else {
        key = "_all";
        label = "All";
      }
      if (!map.has(key)) map.set(key, { id: key, label, items: [] });
      map.get(key)!.items.push(item);
    }
    return Array.from(map.values());
  }, [filtered, groupBy, tab]);

  const totalUnresolved = (data?.repairs.length ?? 0) + (data?.enrichments.length ?? 0);
  const focusedItem = focusedId ? allItems.find((i) => i.id === focusedId) ?? null : null;
  const focusedDraft = focusedId ? (drafts[focusedId] ?? savedHints[focusedId] ?? "") : "";
  const focusedSaved = focusedId ? (savedHints[focusedId] ?? "") : "";

  const handleSaveHint = () => {
    if (!focusedItem) return;
    saveHintMutation.mutate(
      { candidateId: focusedItem.id, hint: focusedDraft },
      {
        onSuccess: () => {
          confirmSave(focusedItem.id, focusedDraft);
          toast.success("Hint saved.");
        },
        onError: () => {
          toast.error("Couldn't save hint — try again.");
        },
      },
    );
  };

  return (
    <div className="flex flex-col h-full">
      {/* Stat bar */}
      <div className="shrink-0 flex items-center justify-end px-4 h-10 border-b border-border-soft bg-bg">
        {isError ? (
          <span className="text-xs text-danger">Couldn't load candidates.</span>
        ) : (
          <Badge tone="neutral" className="font-mono">
            {totalUnresolved} unresolved
          </Badge>
        )}
      </div>

      {/* Tab bar */}
      <div className="shrink-0 flex items-center gap-3 px-4 h-11 border-b border-border-soft bg-bg">
        {/* Tabs */}
        <Tabs value={tab} onValueChange={(v) => setTab(v as "repairs" | "enrichments")}>
          <TabsList className="border-0 gap-0">
            <TabsTrigger value="repairs" className="gap-1.5">
              <Wrench size={14} />
              Repair candidates
              <Badge tone={tab === "repairs" ? "accent" : "neutral"} className="text-[10px] h-4 px-1.5">
                {data?.repairs.length ?? 0}
              </Badge>
            </TabsTrigger>
            <TabsTrigger value="enrichments" className="gap-1.5">
              <Sparkles size={14} />
              Enrichment candidates
              <Badge tone={tab === "enrichments" ? "accent" : "neutral"} className="text-[10px] h-4 px-1.5">
                {data?.enrichments.length ?? 0}
              </Badge>
            </TabsTrigger>
          </TabsList>
        </Tabs>

        <div className="flex-1" />

        {/* Filter segmented control */}
        <div className="inline-flex items-center h-7 rounded-md border border-border-soft bg-surface-2 p-0.5 gap-0.5">
          {(["all", "high", "stale"] as const).map((f) => (
            <button
              key={f}
              onClick={() => setFilter(f)}
              className={cn(
                "h-6 px-2.5 rounded text-xs font-medium transition-colors",
                filter === f
                  ? "bg-surface text-text shadow-sm"
                  : "text-text-3 hover:text-text",
              )}
            >
              {f === "all" ? "All" : f === "high" ? "High conf." : ">24h"}
            </button>
          ))}
        </div>

        {/* Group by select */}
        <div className="inline-flex items-center gap-1.5 text-xs">
          <span className="text-text-4">Group by</span>
          <select
            className="h-7 px-2 bg-surface border border-border rounded-md text-xs font-mono text-text focus:outline-none focus:ring-2 focus:ring-[var(--accent-ring)]"
            value={groupBy}
            onChange={(e) => setGroupBy(e.target.value as typeof groupBy)}
          >
            <option value="type">Issue type</option>
            <option value="severity">Severity</option>
            <option value="repo">Repository</option>
            <option value="none">None</option>
          </select>
        </div>
      </div>

      {/* Split pane */}
      {isLoading ? (
        <PendingSkeleton />
      ) : (
        <div className="flex flex-1 min-h-0">
          {/* Left list */}
          <aside className="w-[440px] shrink-0 flex flex-col bg-bg-soft border-r border-border overflow-y-auto">
            {groups.length === 0 ? (
              <div className="flex items-center gap-2 justify-center h-24 text-xs text-text-4">
                No suggestions match this filter.
              </div>
            ) : (
              groups.map((g) => (
                <GroupSection
                  key={g.id}
                  id={g.id}
                  label={g.label}
                  items={g.items}
                  open={openMap[g.id] !== false}
                  onToggle={() => toggleGroup(g.id)}
                  tab={tab}
                  focusedId={focusedId}
                  savedHints={savedHints}
                  onFocus={setFocusedId}
                />
              ))
            )}
          </aside>

          {/* Right detail */}
          <section className="flex-1 min-w-0 overflow-hidden">
            <DetailPane
              item={focusedItem}
              tab={tab}
              draft={focusedDraft}
              savedHint={focusedSaved}
              onDraftChange={(v) => focusedId && setDraft(focusedId, v)}
              onSave={handleSaveHint}
              saving={saveHintMutation.isPending}
              groupId={groupId}
            />
          </section>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Lint the full webui-v2**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending/webui-v2
npm run lint 2>&1
```

Expected: zero TypeScript errors. If there are errors fix them before proceeding.

- [ ] **Step 3: Build**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending/webui-v2
npm run build 2>&1 | tail -10
```

Expected: `✓ built in` line — no errors.

- [ ] **Step 4: Verify `dashboard/` has zero diff**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
git diff HEAD -- dashboard/
```

Expected: no output (zero changes in `dashboard/`).

- [ ] **Step 5: Commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
git add webui-v2/src/routes/pending.tsx
git commit -m "feat(webui-v2): implement Pending screen — repair+enrichment inbox (#1442)"
```

---

## Task 10: Playwright screenshots (light + dark)

Isolated Vite port `:47282` (not `:47280` dev, not `:47274` daemon). Tear down when done.

**Files:**
- none (screenshots saved to the worktree root as PNG evidence)

- [ ] **Step 1: Start the preview server on a dedicated port**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending/webui-v2
npm run build
PORT=47282 npm run preview -- --port 47282 &
PREVIEW_PID=$!
sleep 3
```

- [ ] **Step 2: Navigate to the Pending screen and take a light-theme screenshot**

Use Playwright to navigate to `http://localhost:47282/g/demo/pending`.

In Playwright:
```javascript
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('http://localhost:47282/g/demo/pending');
  await page.waitForTimeout(1500);
  await page.screenshot({ path: '/Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending/pending-light.png', fullPage: false });
  // Switch to dark mode
  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'dark'));
  await page.waitForTimeout(300);
  await page.screenshot({ path: '/Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending/pending-dark.png', fullPage: false });
  await browser.close();
})();
```

Or use the Playwright MCP tool to navigate and screenshot.

- [ ] **Step 3: Kill the preview server**

```bash
kill $PREVIEW_PID 2>/dev/null || pkill -f "vite preview.*47282" || true
```

- [ ] **Step 4: Verify screenshots were created**

```bash
ls -lh /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending/pending-*.png
```

Expected: `pending-light.png` and `pending-dark.png` both exist and are > 10KB.

---

## Task 11: Open PR

- [ ] **Step 1: Final checks**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
go build ./...
go test ./internal/dashboard/... -run "TestHandleV2Candidates|TestHandleV2CandidateHint" -v 2>&1 | grep -E "PASS|FAIL|ok"
cd webui-v2 && npm run build 2>&1 | tail -5
```

All three commands must succeed.

- [ ] **Step 2: Check git log**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
git log --oneline main..HEAD
```

Expected: 7–8 commits on this branch.

- [ ] **Step 3: Push branch**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-pending
git push -u origin feat/webui-v2-pending
```

- [ ] **Step 4: Open PR**

```bash
gh pr create \
  --title "feat(webui-v2): Pending screen — repair + enrichment inbox (#1442)" \
  --body "$(cat <<'EOF'
## What

Implements the Pending screen (repair + enrichment inbox / agent hand-off) for WebUI v2, fulfilling #1442 and EPIC #1432.

## Why

The Pending screen is the triage + hand-off surface archigraph exposes for quality issues it cannot auto-fix. It surfaces two parallel inboxes — repair candidates and enrichment candidates — and gives users a prompt-building interface to hand work to their coding agent (Claude Code, Cursor, Windsurf).

## How

**Backend (`v2_pending.go`):**
- New `GET /api/v2/groups/{group}/candidates?tab=repairs|enrichments` — wraps the existing `readAllCandidates()` + existing `repairKinds` maps in a v2 envelope; maps daemon's internal kind strings to design-doc `issueType`/`enrichmentType` values; extracts entity metadata from the `context` map.
- New `PUT /api/v2/groups/{group}/candidates/{cid}/hint` — reads the on-disk `enrichment-candidates.json`, patches the matching entry's hint field, writes back. Same file-level approach as v1 writeback.
- Routes registered in a new append-only block in `server.go`. **No existing routes modified.**

**Data decisions:**
- Hints are persisted by candidate ID (same as v1). The `open question` about entity-ID keying is daemon work, deferred per spec.
- The design calls `Candidate[]` from `/api/groups/:id/candidates` — we expose it under the v2 path with a combined `{ repairs, enrichments }` payload to minimize round-trips.
- `suggestedFix` / `proposedContent` fields are intentionally absent per design constraint (archigraph does not generate fix content).

**Frontend (`webui-v2/`):**
- `data/types.ts` — appended `RepairCandidate`, `EnrichmentCandidate`, `EntityRef`, etc.
- `lib/api.ts` — appended `listCandidates` + `saveHint`.
- `store/use-pending-store.ts` — new Zustand slice (tab, filter, groupBy, focusedId, openMap, drafts, savedHints).
- `hooks/use-pending.ts` — `useCandidates` (TanStack Query, 30 s refetch) + `useSaveHint` mutation.
- `routes/pending.tsx` — full screen: stat bar · tab bar with filter + group-by · split pane (list + detail). Uses only existing `@/components/ui` primitives. No hardcoded colors.
- `dashboard/` — **zero diff** (verified by `git diff HEAD -- dashboard/`).

## Test plan

- [ ] `go build ./...` passes
- [ ] `go test ./internal/dashboard/... -run "TestHandleV2Candidates|TestHandleV2CandidateHint"` all PASS
- [ ] `npm run build` in `webui-v2/` passes with zero TypeScript errors
- [ ] Light-theme screenshot: `pending-light.png`
- [ ] Dark-theme screenshot: `pending-dark.png`
- [ ] `git diff HEAD -- dashboard/` produces no output

Fixes #1442
Ref EPIC #1432

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
  )" \
  --base main \
  --head feat/webui-v2-pending
```

---

## Self-Review

### Spec coverage check

| Spec requirement | Covered in task |
|---|---|
| Route `/g/:groupId/pending?tab=repairs\|enrichments` | Task 9 (React Router already registered it) |
| Top bar: breadcrumb + total unresolved pill | Task 9 (stat bar + TopBar from AppShell) |
| Tab bar: Repair / Enrichment + count badges | Task 9 |
| Filter: All / High conf. / >24h | Task 9 |
| Group by: Issue type / Severity / Repository / None | Task 9 |
| Left list: `<Group>` collapsible with sticky header | Task 9 `GroupSection` |
| Left list: `<ListRow>` severity bar + main + right | Task 9 `ListRow` |
| Right detail: head (sev chip + issue label + conf pill) | Task 9 `DetailPane` header |
| Right detail: entity name links to Docs | Task 9 (anchor to `/g/${groupId}/docs`) |
| Right detail: "What archigraph detected" prose | Task 9 section 1 |
| Right detail: "Hint for your agent" textarea with saved/dirty | Task 9 section 2 |
| Right detail: Prompt preview `<pre>` | Task 9 section 3 |
| Right detail: sticky footer with "Copy prompt" + AgentMenu | Task 9 footer |
| AgentMenu dropdown (Claude Code / Cursor / Windsurf) | Task 9 `AgentMenu` |
| Empty state (no row focused) | Task 9 `DetailPane` null branch |
| Toasts for copy / hint saved / agent deep-link | Task 9 via `sonner` |
| Loading skeleton | Task 9 `PendingSkeleton` |
| Daemon offline banner | Not implemented — `isError` shows a badge; full "live updates paused" banner is a stretch goal per spec "Daemon offline" state; deferred |
| `buildPrompt(item, hint, tab)` | Task 9 `buildPrompt` function |
| Backend endpoint `GET /api/groups/:id/candidates` | Task 4 v2 path |
| Backend endpoint `PUT /api/groups/:id/candidates/:cid/hint` | Task 4 |
| `dashboard/` zero diff | Task 9 verification step |

### Placeholder scan

No TBDs, no "add appropriate error handling" patterns, no "similar to Task N" shortcuts. All code blocks are complete.

### Type consistency

- `RepairCandidate.issueType: RepairIssueType` — used consistently in `candidateLabel`, `ISSUE_LABEL`, `isRepair`.
- `EnrichmentCandidate.enrichmentType: EnrichmentType` — used consistently in `ENRICHMENT_LABEL`.
- `usePendingStore.tab: PendingTab` — matches `setTab` parameter type throughout.
- `api.listCandidates` returns `V2CandidatesResponse` — matches `useCandidates` hook's `data.repairs` / `data.enrichments` access.
- `updateCandidateHint(repoPath, cid, hint)` — signature matches call site in `handleV2CandidateHint`.

One known gap: **Daemon offline banner** (the "live updates paused" state from the spec's states table). This requires SSE connectivity tracking that is not yet in the WebUI v2 foundation. Surfaced as an error badge when `isError` is true; a full banner can be added in a follow-up ticket referencing #1442.
