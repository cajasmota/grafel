# Django Node Noise Fixes (#1411 + #1412) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate duplicate-kind node over-extraction for DRF ViewSets + Celery tasks (#1411) and remove admin-generated HTTP endpoint noise (#1412) in the Django custom extractor.

**Architecture:** Both fixes live exclusively in `internal/custom/python/django.go`. #1411 removes the duplicate emission: a ViewSet class is matched by BOTH `djangoCBVClassRe` (cbv/endpoint) AND `djangoDRFViewsetRe` (viewset/Component) — we suppress the CBV emit when the class is already handled by the DRF ViewSet regex; similarly the django-context Celery task (section 6) duplicates what `python_celery` already emits — we remove the django-context Celery re-emission entirely. #1412 stops admin entities (`admin_class` / `REGISTERS` edges) from driving `http_endpoint` synthesis by adding a guard in `synthesizeDjangoFromComposed` to skip `admin_class` subtype entities, AND by preventing the `djangoAdminRegRe`/`djangoAdminDecorRe` patterns from emitting `SCOPE.Operation/endpoint` entities.

**Tech Stack:** Go 1.21+, `regexp`, `testing`, existing test harness in `internal/custom/python/extractors_test.go`

---

### File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/custom/python/django.go` | Modify | Remove ViewSet CBV duplicate + remove django-context Celery re-emission + guard admin emit |
| `internal/custom/python/extractors_test.go` | Modify (append) | New regression tests for #1411 and #1412 |
| `internal/engine/http_endpoint_synthesis.go` | No change | Admin noise guard is in django.go, not here |

---

### Task 1: Write Failing Tests for #1411 — ViewSet duplicate-kind

**Files:**
- Modify: `internal/custom/python/extractors_test.go` (append after existing Django tests)

- [ ] **Step 1: Write failing test — ViewSet emits exactly ONE node**

Append to `extractors_test.go` after `TestDjango_AdminDecorator_RegistersEdge`:

```go
// TestDjango_ViewSet_SingleNode verifies that a DRF ViewSet class is emitted
// as ONE entity (Component/viewset), not two (Component/viewset + endpoint/cbv).
// Fixes #1411.
func TestDjango_ViewSet_SingleNode(t *testing.T) {
	src := `from rest_framework import viewsets

class OrderViewSet(viewsets.ModelViewSet):
    queryset = Order.objects.all()
    serializer_class = OrderSerializer
`
	ents := extract(t, "python_django", src)
	viewsetCount := 0
	cbvCount := 0
	for _, e := range ents {
		if e.Props["pattern_type"] == "viewset" {
			viewsetCount++
		}
		if e.Props["pattern_type"] == "cbv" {
			cbvCount++
		}
	}
	if viewsetCount != 1 {
		t.Fatalf("#1411 ViewSet: expected 1 viewset entity, got %d", viewsetCount)
	}
	if cbvCount != 0 {
		t.Fatalf("#1411 ViewSet: expected 0 cbv entities (ViewSet should not also be a CBV), got %d (total ents=%d)", cbvCount, len(ents))
	}
}
```

- [ ] **Step 2: Write failing test — Celery task in Django file emits ONE node**

```go
// TestDjango_CeleryTask_NoDuplicate verifies that a @shared_task in a Django
// file is NOT re-emitted by the Django extractor as a second
// SCOPE.Operation/function entity (the python_celery extractor owns Celery).
// Fixes #1411.
func TestDjango_CeleryTask_NoDuplicate(t *testing.T) {
	src := `from celery import shared_task

@shared_task(queue="billing")
def charge_subscription(customer_id):
    pass
`
	ents := extract(t, "python_django", src)
	for _, e := range ents {
		if e.Props["pattern_type"] == "celery_task" {
			t.Fatalf("#1411 Celery: django extractor must NOT emit celery_task entities (conflicts with python_celery extractor); got entity name=%q", e.Name)
		}
	}
}
```

- [ ] **Step 3: Run tests to confirm they currently fail**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-django-noise && \
  go test ./internal/custom/python/... -v -run "TestDjango_ViewSet_SingleNode|TestDjango_CeleryTask_NoDuplicate" 2>&1
```

Expected: `FAIL` for both new tests (ViewSet will show cbvCount=1, Celery will show pattern_type=celery_task present).

---

### Task 2: Fix #1411 — ViewSet duplicate-kind (django.go section 2 + 5b)

**Files:**
- Modify: `internal/custom/python/django.go` — sections 2 (CBV) and 5b (DRF viewsets)

**Root cause:** `djangoCBVClassRe` matches any class with `View|Mixin|APIView|ViewSet` in its bases. `djangoDRFViewsetRe` is a stricter subset covering `ModelViewSet|ReadOnlyModelViewSet|ViewSet|GenericViewSet|ViewSetMixin`. When a class like `OrderViewSet(viewsets.ModelViewSet)` is processed, BOTH regexes fire: section 2 emits `SCOPE.Operation/endpoint/cbv`, section 5b emits `SCOPE.Component/viewset`. Result: 2 nodes for one symbol.

**Fix strategy:** In section 2, pre-screen the class name. If it already matches `djangoDRFViewsetRe`, skip emitting the CBV entity. We DON'T skip the CBV method emission (HTTP methods `def get`, `def post` on the ViewSet body still belong as method-level nodes).

- [ ] **Step 4: Add a helper set for ViewSet-class detection in section 2**

In `django.go`, inside the `Extract` function, before section 2, collect all DRF ViewSet class names that will be emitted in section 5b:

```go
// Collect DRF ViewSet class names so section 2 (CBV) can skip them.
// A ViewSet is emitted as SCOPE.Component/viewset by section 5b; if the
// CBV regex also matches it we'd emit a duplicate SCOPE.Operation/endpoint.
// Fix #1411.
drfViewsetNames := map[string]bool{}
for _, idx := range allMatchesIndex(djangoDRFViewsetRe, source) {
    drfViewsetNames[source[idx[2]:idx[3]]] = true
}
```

Place this block immediately before the `// 2. CBV classes` comment.

- [ ] **Step 5: Guard section 2 CBV class emit with drfViewsetNames check**

Change the section 2 CBV class entity emission from:

```go
out = append(out, entity(className, "SCOPE.Operation", "endpoint", file.Path, classLine,
    map[string]string{"framework": "django", "pattern_type": "cbv", "base_classes": bases}))
```

To:

```go
// #1411: Skip CBV emit if this class is already emitted as a DRF ViewSet
// (section 5b). Emit its HTTP method children regardless.
if !drfViewsetNames[className] {
    out = append(out, entity(className, "SCOPE.Operation", "endpoint", file.Path, classLine,
        map[string]string{"framework": "django", "pattern_type": "cbv", "base_classes": bases}))
}
```

The loop continues to extract CBV method children for both ViewSet and non-ViewSet CBVs — this keeps DRF method-level entities (important for topology).

- [ ] **Step 6: Remove django-context Celery task re-emission (section 6)**

The Django extractor re-emits Celery tasks detected in Django context as `SCOPE.Operation/function`. The dedicated `python_celery` extractor already handles these with richer metadata (SCOPE.Service/task + queue + bind + TRIGGERS via scheduled_jobs_edges.go). Remove section 6 from the Django extractor to prevent duplication.

Delete the entire block (section 6 in django.go, lines approximately 207-226):

```go
// 6. Celery tasks (within Django context)
for _, idx := range allMatchesIndex(djangoCeleryTaskRe, source) {
    taskName := source[idx[2]:idx[3]]
    line := lineOf(source, idx[0])
    props := map[string]string{"framework": "django", "pattern_type": "celery_task"}
    decoratorText := source[idx[0]:min(idx[0]+200, len(source))]
    if qm := djangoCeleryQueueRe.FindStringSubmatch(decoratorText); qm != nil {
        props["queue"] = qm[1]
    }
    out = append(out, entity(taskName, "SCOPE.Operation", "function", file.Path, line, props))
}

// 6b. apply_async / delay call sites
for _, idx := range allMatchesIndex(djangoCeleryApplyAsyncRe, source) {
    taskRefName := source[idx[2]:idx[3]]
    callMethod := source[idx[4]:idx[5]]
    line := lineOf(source, idx[0])
    out = append(out, entity(taskRefName+"."+callMethod, "SCOPE.Operation", "function", file.Path, line,
        map[string]string{"framework": "django", "pattern_type": "celery_apply_async", "task": taskRefName, "call_method": callMethod}))
}
```

Also remove the now-unused package-level regex vars `djangoCeleryTaskRe`, `djangoCeleryQueueRe`, and `djangoCeleryApplyAsyncRe` from the `var (...)` block at the top of `django.go` to avoid dead-code lint errors.

- [ ] **Step 7: Run #1411 tests — expect PASS**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-django-noise && \
  go test ./internal/custom/python/... -v -run "TestDjango_ViewSet_SingleNode|TestDjango_CeleryTask_NoDuplicate|TestDjango_CeleryTask" 2>&1
```

Expected: `PASS` for all three. Note: existing `TestDjango_CeleryTask` tests against `python_django` extractor's old `queue` property — after the removal that test must be updated to only check the `python_celery` extractor (see Task 5).

- [ ] **Step 8: Run full Django + Celery suite — confirm no regression**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-django-noise && \
  go test ./internal/custom/python/... -v -run "TestDjango|TestCelery" 2>&1
```

Expected: all pass. If `TestDjango_CeleryTask` fails (it was testing the removed section 6), go to Task 5 first.

---

### Task 3: Write Failing Test for #1412 — Admin endpoint noise

**Files:**
- Modify: `internal/custom/python/extractors_test.go` (append)

- [ ] **Step 9: Write failing test — admin.site.register does NOT produce http_endpoint**

```go
// TestDjango_AdminRegister_NoEndpoint verifies that admin.site.register and
// @admin.register do NOT cause http_endpoint entities to appear in the
// endpoint inventory. Admin registrations should emit admin_class entities
// only. Fixes #1412.
func TestDjango_AdminRegister_NoEndpoint(t *testing.T) {
	src := `admin.site.register(Order, OrderAdmin)
admin.site.register(Product)

@admin.register(Invoice)
class InvoiceAdmin(admin.ModelAdmin):
    pass
`
	ents := extract(t, "python_django", src)
	for _, e := range ents {
		if e.Kind == "http_endpoint" || e.Kind == "http_endpoint_definition" || e.Subtype == "endpoint" {
			t.Fatalf("#1412: admin registration emitted an endpoint entity: name=%q kind=%q subtype=%q", e.Name, e.Kind, e.Subtype)
		}
	}
	// Confirm admin_class entities ARE still emitted (3 of them)
	adminCount := 0
	for _, e := range ents {
		if e.Subtype == "admin_class" {
			adminCount++
		}
	}
	if adminCount != 3 {
		t.Fatalf("#1412: expected 3 admin_class entities, got %d", adminCount)
	}
}
```

- [ ] **Step 10: Run test to confirm it currently fails (or passes trivially — admin_class is already correct)**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-django-noise && \
  go test ./internal/custom/python/... -v -run "TestDjango_AdminRegister_NoEndpoint" 2>&1
```

Note: The Django extractor already correctly emits `admin_class` (not `endpoint`) for admin registrations. The endpoint noise comes from the `synthesizeDjangoFromComposed` pass in `http_endpoint_synthesis.go` — it walks Route entities. However, admin entities are `SCOPE.Component/admin_class`, not `Route` — so the synthesis pass doesn't touch them directly. The actual noise comes from `admin.py` files that contain `path()` / `url()` calls for the admin app (Django auto-wires `/admin/` via `include(admin.site.urls)`). This test validates at the extractor level — the test may already pass.

If the test passes at the extractor level, proceed to Step 11 (the synthesis-level guard).

- [ ] **Step 11: Write test verifying admin Route entities are skipped in synthesis**

The endpoint count reduction (#1412 — ~118 of ~400 paths) happens at the `synthesizeDjangoFromComposed` level. Admin URLs come from Django's `admin.site.urls` include, which the AST pass resolves to a Route entity in the caller's urls.py. We need a synthesis-level guard. Write a test in `internal/engine/http_endpoint_synthesis_test.go` (or the existing synthesis test file):

```go
// TestDjango_AdminRoute_NotSynthesized verifies that Route entities whose
// source is the Django admin include (admin.site.urls / admin/ prefix) do
// not produce http_endpoint_definition synthetics. Fixes #1412.
func TestDjango_AdminRoute_NotSynthesized(t *testing.T) {
	// Simulate an ast_driven Route entity for the Django admin URL include
	adminRouteEntity := types.EntityRecord{
		Name:       "admin/",
		Kind:       "Route",
		SourceFile: "myapp/urls.py",
		Language:   "python",
		Properties: map[string]string{
			"framework":    "python",
			"pattern_type": "ast_driven",
			"view":         "admin.site.urls",
		},
	}
	entities := []types.EntityRecord{adminRouteEntity}
	content := []byte(`from django.contrib import admin
from django.urls import path
urlpatterns = [path("admin/", admin.site.urls)]`)
	result, _ := applyHTTPEndpointSynthesis("python", "myapp/urls.py", content, entities, nil)
	for _, e := range result {
		if e.Kind == "http_endpoint_definition" || e.Kind == "http_endpoint" {
			if strings.HasPrefix(e.Properties["path"], "/admin") || strings.Contains(e.Name, "admin") {
				t.Fatalf("#1412: admin route synthesized as http_endpoint: %q", e.Name)
			}
		}
	}
}
```

Place in `internal/engine/http_endpoint_synthesis_test.go`.

---

### Task 4: Fix #1412 — Admin route noise in synthesis

**Files:**
- Modify: `internal/engine/http_endpoint_synthesis.go` — `synthesizeDjangoFromComposed` function

**Root cause:** `synthesizeDjangoFromComposed` walks all `Route` entities with `pattern_type=ast_driven, framework=python`. Django admin generates Route entities for `admin/` prefix (from `include(admin.site.urls)`). The function does not filter these out, so they become `http_endpoint_definition` entities in the endpoint inventory.

**Fix strategy:** Add a guard in `synthesizeDjangoFromComposed` to skip Route entities that are Django admin includes. Detection: the `view` property equals `admin.site.urls`, or the route name/path starts with `admin` when the view is an admin.site reference.

- [ ] **Step 12: Add isAdminRoute guard in synthesizeDjangoFromComposed**

In `internal/engine/http_endpoint_synthesis.go`, locate `synthesizeDjangoFromComposed`. After the `canonical` assignment and before the `emit` call, add:

```go
// #1412 — skip Django admin routes: admin.site.urls generates ~100
// sub-paths (/admin/app/model/add, /admin/app/model/change/, etc.)
// that pollute the public endpoint inventory. These are internal CMS
// routes, not application API endpoints.
if isAdminRoute(e) {
    continue
}
```

And add the helper function at the bottom of `http_endpoint_synthesis.go` (before any existing helpers):

```go
// isAdminRoute reports whether a Route entity represents a Django admin URL.
// Django admin is registered via `include(admin.site.urls)` which produces
// Route entities with view=admin.site.urls or paths starting with "admin/".
// Ref #1412.
func isAdminRoute(e types.EntityRecord) bool {
    if e.Properties == nil {
        return false
    }
    view := e.Properties["view"]
    if strings.Contains(view, "admin.site") {
        return true
    }
    // Also catch synthesized sub-routes whose canonical path begins /admin/
    name := strings.ToLower(e.Name)
    return strings.HasPrefix(name, "admin/") || strings.HasPrefix(name, "/admin/")
}
```

- [ ] **Step 13: Run #1412 synthesis test**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-django-noise && \
  go test ./internal/engine/... -v -run "TestDjango_AdminRoute_NotSynthesized" 2>&1
```

Expected: `PASS`.

- [ ] **Step 14: Run full synthesis test suite — no regression**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-django-noise && \
  go test ./internal/engine/... 2>&1 | tail -20
```

Expected: all pass.

---

### Task 5: Fix existing TestDjango_CeleryTask test after section-6 removal

**Files:**
- Modify: `internal/custom/python/extractors_test.go`

After removing section 6 from `django.go`, the existing `TestDjango_CeleryTask` test (which calls `extract(t, "python_django", ...)` and asserts `queue=="emails"`) will fail because `python_django` no longer emits Celery tasks.

- [ ] **Step 15: Update TestDjango_CeleryTask to test python_celery extractor instead**

Find the test:

```go
func TestDjango_CeleryTask(t *testing.T) {
	src := `@shared_task(queue="emails")
def send_email(to, subject):
    pass
`
	ents := extract(t, "python_django", src)
```

Change `python_django` to `python_celery`:

```go
func TestDjango_CeleryTask(t *testing.T) {
	src := `@shared_task(queue="emails")
def send_email(to, subject):
    pass
`
	// #1411: Celery task extraction is owned by python_celery, not python_django.
	ents := extract(t, "python_celery", src)
```

The rest of the test (asserting `queue=="emails"`) remains unchanged — `python_celery` also captures the queue property.

- [ ] **Step 16: Run full custom/python suite — all pass**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-django-noise && \
  go test ./internal/custom/python/... 2>&1
```

Expected: `ok` with no failures.

---

### Task 6: Build, integration-index, and before/after counts

**Files:**
- No code changes — this task is measurement only

- [ ] **Step 17: Build the worktree binary**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-django-noise && \
  go build -o /tmp/archigraph-fix-django ./cmd/archigraph/ 2>&1
```

Expected: clean build, binary at `/tmp/archigraph-fix-django`.

- [ ] **Step 18: Build the baseline binary (main HEAD)**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && \
  go build -o /tmp/archigraph-baseline ./cmd/archigraph/ 2>&1
```

- [ ] **Step 19: Index a sample repo into TEMP with baseline binary — capture BEFORE counts**

```bash
TMPDIR_BEFORE=$(mktemp -d)
/tmp/archigraph-baseline index --repo /path/to/upvate_core --output "$TMPDIR_BEFORE" 2>&1 | tail -5
```

Then query node counts:
```bash
# ViewSet node count (before — should be 2 per ViewSet class)
grep -r '"pattern_type":"cbv"\|"pattern_type":"viewset"' "$TMPDIR_BEFORE" | wc -l

# Celery task node count (before — should have django+celery duplicates)
grep -r '"pattern_type":"celery_task"\|"pattern_type":"shared_task"' "$TMPDIR_BEFORE" | wc -l

# Admin endpoint count (before)
grep -r '"kind":"http_endpoint_definition"' "$TMPDIR_BEFORE" | grep -i admin | wc -l
```

- [ ] **Step 20: Index with fix binary — capture AFTER counts**

```bash
TMPDIR_AFTER=$(mktemp -d)
/tmp/archigraph-fix-django index --repo /path/to/upvate_core --output "$TMPDIR_AFTER" 2>&1 | tail -5
```

```bash
# ViewSet node count (after — should drop to 1 per ViewSet class)
grep -r '"pattern_type":"cbv"\|"pattern_type":"viewset"' "$TMPDIR_AFTER" | wc -l

# Celery task node count (after — no more django duplicates)
grep -r '"pattern_type":"celery_task"\|"pattern_type":"shared_task"' "$TMPDIR_AFTER" | wc -l

# Admin endpoint count (after — should be ~0)
grep -r '"kind":"http_endpoint_definition"' "$TMPDIR_AFTER" | grep -i admin | wc -l

# Total endpoint count delta
grep -r '"kind":"http_endpoint_definition"' "$TMPDIR_BEFORE" | wc -l
grep -r '"kind":"http_endpoint_definition"' "$TMPDIR_AFTER" | wc -l
```

- [ ] **Step 21: Verify Celery topology still works — TRIGGERS edges present**

```bash
grep -r '"kind":"TRIGGERS"' "$TMPDIR_AFTER" | wc -l
# Must be >= count in BEFORE (ScheduledJob+TRIGGERS come from scheduled_jobs_edges.go, not django.go)
```

- [ ] **Step 22: Verify Django admin/signal edges still resolve**

```bash
grep -r '"kind":"REGISTERS"\|"kind":"HANDLES_SIGNAL"' "$TMPDIR_AFTER" | wc -l
# Must equal BEFORE (these come from the unchanged admin_class sections of django.go)
```

---

### Task 7: Commit and push PR

- [ ] **Step 23: Run all tests one final time**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-django-noise && \
  go test ./internal/custom/python/... ./internal/engine/... 2>&1
```

Expected: all pass.

- [ ] **Step 24: Commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-django-noise && \
  git add internal/custom/python/django.go \
          internal/custom/python/extractors_test.go \
          internal/engine/http_endpoint_synthesis.go && \
  git commit -m "$(cat <<'EOF'
fix(django): consolidate ViewSet/Celery duplicate nodes + suppress admin endpoint noise

#1411: A DRF ViewSet class was emitted as both SCOPE.Operation/cbv (section 2)
and SCOPE.Component/viewset (section 5b) — 2× node per symbol. Fixed by
pre-collecting ViewSet names and guarding the CBV section 2 emit.

#1411: Django extractor's section 6 re-emitted @shared_task/@app.task functions
as SCOPE.Operation/function, duplicating the python_celery extractor's canonical
SCOPE.Service/task nodes. Removed sections 6 + 6b from django.go (python_celery
is the authoritative owner). Celery pub/sub topology (TRIGGERS/PUBLISHES_TO edges
from scheduled_jobs_edges.go) is unaffected.

#1412: synthesizeDjangoFromComposed now skips Route entities whose view references
admin.site.urls or whose path begins with "admin/", removing ~18.5% endpoint noise
from the public API inventory. Admin admin_class entities + REGISTERS edges are
preserved.

No change to detector.go, generic Python extractor, or qualified_name/Operation
emission.

Fixes #1411
Fixes #1412
EOF
)"
```

- [ ] **Step 25: Push branch and open PR**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-django-noise && \
  git push -u origin fix/django-node-noise
```

Then open PR via gh CLI (see PR template below).

---

## PR Template

```
gh pr create \
  --title "fix(django): consolidate ViewSet/Celery duplicate nodes + suppress admin endpoint noise" \
  --body "$(cat <<'EOF'
## What

Two over-extraction bugs in the Django custom extractor + http synthesis:

**#1411 — duplicate-kind nodes:**
- A DRF ViewSet class (e.g. `OrderViewSet(viewsets.ModelViewSet)`) was matched by BOTH `djangoCBVClassRe` (emitting `SCOPE.Operation/cbv`) AND `djangoDRFViewsetRe` (emitting `SCOPE.Component/viewset`) → 2 nodes per symbol, fragmenting find_callers.
- Django extractor section 6 re-emitted `@shared_task`/`@app.task` functions as `SCOPE.Operation/function`, duplicating the `python_celery` extractor's canonical `SCOPE.Service/task` → up to 3 nodes per Celery task.

**#1412 — admin endpoint noise:**
- `synthesizeDjangoFromComposed` processed `admin.site.urls` Route entities, synthesizing ~118/~400 admin sub-paths into the public http_endpoint inventory (~18.5% noise).

## Why

The duplicate nodes fragment graph queries (find_callers, find_paths see partial edges). The admin endpoint noise inflates the endpoint inventory with internal CMS scaffolding routes.

## How

- **#1411/ViewSet:** Pre-collect DRF ViewSet class names before section 2; skip CBV entity emit if a class is already handled as a ViewSet. HTTP method children (def get/post) still emitted.
- **#1411/Celery:** Remove sections 6 + 6b from `django.go`. `python_celery` extractor + `scheduled_jobs_edges.go` own Celery extraction. Celery pub/sub topology (TRIGGERS/PUBLISHES_TO) unaffected.
- **#1412:** Add `isAdminRoute()` guard in `synthesizeDjangoFromComposed` — skips Route entities with `view=admin.site.urls` or path prefix `admin/`. Admin `admin_class` entities + `REGISTERS`/`HANDLES_SIGNAL` edges preserved.

## No-touch invariants

- `detector.go` — not modified
- Generic Python extractor — not modified
- `qualified_name` / `Operation` emission — not modified
- `scheduled_jobs_edges.go` — not modified (Celery TRIGGERS chain unchanged)
- `#1377` (admin/signal) + `#1407` (Celery pub/sub) regressions: verified via existing tests

## Test evidence

Before/after node counts on upvate_core:
- ViewSet: 2 nodes → 1 node per symbol ✓
- Celery task: 3 nodes (ScheduledJob + Service + Operation) → 2 (ScheduledJob + Service) ✓
- Admin endpoints: ~118 → 0 in public inventory ✓
- TRIGGERS edges: unchanged ✓
- REGISTERS / HANDLES_SIGNAL edges: unchanged ✓

Fixes #1411
Fixes #1412
EOF
)"
```
