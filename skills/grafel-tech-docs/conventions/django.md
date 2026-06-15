# Django convention

Required reading: `_graph-searchability.md`.

This convention applies when the repo's primary framework is Django (with or without Django REST Framework). It covers Django 4.x and 5.x; older versions are best-effort.

## Public surface

Treat the following as the **public API** of a Django repo, in this order:

1. **URL routes** — anything reachable by HTTP. Pull from the root `URLConf` and recursively from `include()` calls.
2. **Management commands** — every `BaseCommand` subclass under `<app>/management/commands/`.
3. **Celery tasks** — every `@shared_task` or `app.task` decorated callable.
4. **DRF serializers and viewsets** — these are not technically "exported" but they define the wire format.
5. **Signals** — `post_save`, `pre_delete`, custom signals; receivers wired via `@receiver` or `Signal.connect()`.

Internal helpers (anything under `_private/`, anything prefixed with `_`, anything in a `utils.py` not imported across apps) are not public surface.

## Module shape

A Django "app" is the natural module boundary. For each app:

- `models.py` (or `models/`) defines the data layer.
- `views.py` / `viewsets.py` defines HTTP handlers.
- `serializers.py` defines DRF wire format.
- `admin.py` registers admin pages (often skipped in docs unless customized).
- `signals.py` connects post-save / pre-delete handlers.
- `tasks.py` defines Celery tasks.
- `management/commands/*.py` defines CLI subcommands of `manage.py`.
- `migrations/*.py` are usually summarized, not described per-file.
- `tests/` is excluded from generated docs.

A community detected by `grafel_clusters` that mixes two apps is rare in idiomatic Django. When it happens, split it back along app boundaries.

## Entry points (Pass 3)

- `wsgi.py` and `asgi.py` — runtime entry.
- The root `urls.py` — request entry.
- `manage.py` — CLI entry.
- `celery.py` (typical filename) — worker entry.
- A `Procfile`, `Dockerfile`, or `docker-compose.yml` if present.

## Dynamic edges (Pass 4)

Django's runtime couplings that static analysis misses:

- **Signal connections** — a sender/receiver pair declared in `signals.py` or in `apps.py` `ready()`. Encode in `flows.md` with a heading like:
  ```markdown
  ## How `post_save` on `Order` triggers `send_receipt`
  ```
- **Middleware ordering** — `MIDDLEWARE` is a list whose order is semantically meaningful. List middlewares in order in `cross-cutting/auth.md` or `cross-cutting/observability.md`.
- **Async tasks via Celery** — a view enqueues a task by name; the task lives in another app. The link is real but invisible to import analysis. Encode the bridge in a heading.
- **Database routers** — `DATABASE_ROUTERS` setting silently routes models to specific databases.
- **Custom user model** — `AUTH_USER_MODEL` is a string reference resolved at runtime; treat it as a public-API decision worth its own section.

## Deployment signals (Pass 5)

Look for, in priority order:

1. `Dockerfile`, `Containerfile`, `compose.yml`, `docker-compose.yml`.
2. Process declarations: `Procfile`, `gunicorn.conf.py`, `uvicorn` invocations.
3. Settings module split: `settings/base.py`, `settings/prod.py`, etc.
4. CI files (`.github/workflows/*.yml`, `.gitlab-ci.yml`) that show what the deploy step actually runs.

## Manifest files (Pass 5 — `dependencies.md`)

In order: `pyproject.toml`, `poetry.lock`, `requirements.txt`, `requirements/*.txt`, `Pipfile`, `setup.cfg`, `setup.py`. Use the lockfile (when present) for the version pin column; use the manifest for the "direct vs transitive" distinction.

## Cross-cutting pitfalls

- Management commands skip middleware. Auth/logging assumptions baked into middleware do not apply. Note this explicitly in `cross-cutting/auth.md`.
- Django admin has its own permission system; document it separately from app-level permissions.
- Atomic blocks and `select_for_update()` — list every place these appear under `cross-cutting/errors.md` because they directly affect retry behavior.

## Cross-repo signals

A Django repo most often connects to other repos through:

- Outbound HTTP via `requests`, `httpx`, or a generated client.
- Outbound message bus via Celery or a direct boto3 SQS publish.
- Shared database — another repo runs migrations against the same Postgres. Note this in `dependencies.md`.

When `grafel_cross_links(action=list)` returns a candidate from this repo, the connection method is usually one of those three. Pass 8 should accept HTTP and message-bus candidates with high confidence; shared-DB candidates need user confirmation because the static signal is weaker.
