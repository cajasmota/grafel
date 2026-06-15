# Generic Python convention

Required reading: `_graph-searchability.md`.

Use for Python repos that do not match a more specific framework convention (`django.md`, `fastapi.md`). Examples: a Click/Typer CLI, a data-pipeline package, a library, a Lambda handler bundle.

## Public surface

1. **Top-level package exports** — anything in `<package>/__init__.py`'s `__all__` (if defined) or every public name otherwise.
2. **CLI entry points** — `console_scripts` in `pyproject.toml` (or `setup.cfg`/`setup.py`). Each becomes a documented command.
3. **Lambda / FaaS handlers** — any function whose name matches the configured handler in IaC.
4. **Plugin entry points** — `entry_points` declared in `pyproject.toml` for plugin systems (pytest, Click, etc.).

## Module shape

```
src/<package>/
  __init__.py
  <feature>/
    __init__.py
    <module>.py
  cli.py            # if Click/Typer
tests/
pyproject.toml
```

Communities usually map to feature subpackages.

## Entry points (Pass 3)

- `pyproject.toml` `[project]` and `[project.scripts]`.
- Module-level `if __name__ == "__main__":` blocks.
- Lambda handler functions (the IaC repo points at them by dotted path).

## Dynamic edges (Pass 4)

- **Plugin discovery** — `importlib.metadata.entry_points()` finds plugins at runtime. The producer is in another package; document the contract.
- **Decorator registration** — frameworks like Click, Typer, pytest, and Celery register callables via decorators. The decorator side-effects mean discovery is lazy; document the import order required for registration to fire.
- **`__init__.py` re-exports** — `from .x import Y` in `__init__.py` flattens the public path. Document the canonical import path users should use.

## Deployment signals (Pass 5)

- `Dockerfile` if container.
- `serverless.yml` / `samconfig.toml` if SAM/Serverless Framework.
- `pyproject.toml` `[project]` `requires-python` — minimum runtime.

## Manifest files

`pyproject.toml`, `poetry.lock` / `uv.lock`, `requirements*.txt`.

## Cross-cutting pitfalls

- **Import-time side effects** — top-level code runs on first import; surprising in test contexts.
- **Logging configuration** — libraries should not call `logging.basicConfig`; document if the repo does anyway.
- **Threading vs multiprocessing vs async** — pick one model and document the choice.

## Cross-repo signals

For libraries, the cross-repo edge is "consumer pins this version". For Lambda handler bundles, the edge is from the IaC stack that targets the handler. Both are real but require manual confirmation since the static signal is weak.
