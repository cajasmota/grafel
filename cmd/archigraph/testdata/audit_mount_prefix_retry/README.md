# audit_mount_prefix_retry

Fixture for [#2702] — consumer-side mount-prefix retry in the cross-repo HTTP
linker.

## What the scenario models

A Django backend mounts a router at `path("internal/v3/", include(...))`
(producer) and a JS frontend (consumer) calls the routes WITHOUT the
`/internal/v3/` prefix because its HTTP client prepends a `baseURL` at
runtime. The cross-repo HTTP linker must:

1. Harvest the `/internal/v3/` prefix from the `url_mount_point` synthetic
   the indexer emits for the `include()` declaration (#2677).
2. Retry the byPath probe for the consumer's `/things` call after prepending
   the discovered prefix, finding the `/internal/v3/things` producer endpoint.
3. Stamp the resolved link with `resolve_strategy=mount_prefix_added` and
   `applied_mount_prefix=/internal/v3/`.

## Why `/internal/v3/` rather than the spec's `/api/v1/`

The issue (#2702) uses `/api/v1/` as the worked example. In practice that
exact prefix is already absorbed by two pre-existing aliasing passes — the
generic-strip pass (#1409) and the `url_prefix`-keyed strip (#819) — so the
consumer's raw `/things` would match the producer's `/api/v1/things` via
the standard byPath probe labelled `exact`, never reaching the new retry.

To prove the new code path actually runs, the fixture uses a non-canonical
mount prefix (`/internal/v3/`) that is invisible to both the generic-strip
pass and the hardcoded prefix-injection retry (#2569, candidates =
`/api,/api/vN,/vN`). The only resolution path left is the new
`mount_prefix_added` strategy this PR adds.

## Why `graph.json` instead of `*.py` / `*.js`

`producer/graph.json` and `consumer/graph.json` are hand-crafted because the
real indexer attaches `url_prefix` to every DRF-expanded endpoint
(`internal/engine/django_drf_router.go` lines #800/#811), which in turn
makes `http_pass.go` register a prefix-stripped byPath alias (#819). That
alias short-circuits the retry the same way `/api/v1/` does.

A hand-crafted graph lets the test force the exact precondition the issue
targets: a producer endpoint WITHOUT `url_prefix` populated, paired with a
sibling `url_mount_point` synthetic that provides the discovered prefix.
This is the same shape produced by hand-written `urls.py` files that
register a fully-qualified path side-by-side with an `include()` declaration
— a pattern observed in the upvate codebase.

## Source-level reference (for human readers)

These files are illustrative ONLY — they are not loaded by
`TestIssue2702_MountPrefixRetry`, which reads the `graph.json` files directly:

```python
# producer/myapp/urls.py
from django.urls import path, include
from . import views

urlpatterns = [
    path("internal/v3/", include("other.routes")),               # url_mount_point
    path("internal/v3/things", views.list_things, name="things"), # endpoint
]
```

```javascript
// consumer/src/client.js
export async function listThings() {
  const r = await fetch("/things");   // NO /internal/v3/ prefix
  return r.json();
}
```
