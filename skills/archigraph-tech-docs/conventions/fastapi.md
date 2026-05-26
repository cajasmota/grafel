# FastAPI convention

Required reading: `_graph-searchability.md`.

Applies to FastAPI applications. For Django, see `django.md`. For non-FastAPI Python, see `python-generic.md`.

## Public surface

1. **Routes** — every `@router.get`, `@router.post`, etc. plus every `app.include_router(...)`.
2. **Dependencies** — every callable used in `Depends(...)`. These are not "endpoints" but they define the auth/loading contract for every endpoint that uses them.
3. **Pydantic models used as request/response bodies** — wire format.
4. **Background tasks** — `BackgroundTasks.add_task(...)` and any external worker (Celery, RQ, arq) tasks.
5. **WebSocket endpoints** — `@router.websocket(...)`.
6. **Lifespan handlers** — `lifespan=` callables on `FastAPI(...)` or legacy `@app.on_event(...)`.

## Module shape

A typical FastAPI repo:

```
app/
  main.py             # FastAPI() instance + include_router calls
  api/
    v1/
      <feature>.py    # APIRouter
  core/
    config.py         # pydantic-settings Settings class
    security.py
  models/             # SQLAlchemy or SQLModel
  schemas/            # Pydantic
  services/
  workers/            # Celery/arq if used
```

Communities usually map to a feature router under `api/v1/`. When the indexer surfaces a community spanning `services/` and `workers/`, that is the runtime job pipeline — document it as one module.

## Entry points (Pass 3)

- `app/main.py` (or wherever `FastAPI()` is instantiated).
- The ASGI runner: `uvicorn` invocation, `gunicorn` with a uvicorn worker, or a `Procfile`.
- `core/config.py` — settings entry; documents which env vars matter.

## Dynamic edges (Pass 4)

- **`Depends` chains** — a route says `Depends(get_current_user)`, which itself depends on `Depends(get_db)`. Encode the chain in the route's section in `api.md` or in `flows.md` when the chain is non-trivial.
- **Middleware** — `app.add_middleware(...)` calls in order. Encode in `cross-cutting/auth.md` or `cross-cutting/errors.md`.
- **Exception handlers** — `@app.exception_handler(...)` maps an exception class to a response. Document each in `cross-cutting/errors.md`.
- **Background workers** — a route enqueues; a worker consumes. Cross-module bridge.

## Deployment signals (Pass 5)

- ASGI server config: `uvicorn.run(...)` or a `gunicorn.conf.py`.
- `Dockerfile` / `compose.yml`.
- Reverse proxy / TLS termination — only mention if defined in this repo (otherwise belongs to the gateway repo).
- Health-check endpoints — typically `/health` and `/ready`; list them in `reference/deployment.md`.

## Manifest files

`pyproject.toml`, `poetry.lock` / `uv.lock`, `requirements.txt`. Same precedence as `django.md`.

## Cross-cutting pitfalls

- **CORS** — `CORSMiddleware` configuration belongs in `cross-cutting/auth.md` not in scattered route files.
- **OpenAPI schema** — FastAPI generates one automatically; flag deviations (custom `openapi_url`, hidden routes via `include_in_schema=False`) explicitly.
- **Sync vs async** — a sync route in an async app blocks the event loop. List sync routes explicitly.

## Cross-repo signals

Outbound HTTP via `httpx`; outbound message bus via Celery/arq/redis; shared DB via SQLAlchemy. Same accept/confirm rules as `django.md`.
