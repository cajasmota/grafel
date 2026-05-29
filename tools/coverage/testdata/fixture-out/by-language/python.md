<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# python

**Frameworks**: 4 · **Tools**: 0 · **ORMs**: 0 · **Other**: 1

Back to [summary](../summary.md).

### Legend

Each group column shows `glyph covered/applicable` — **covered** = capabilities with extraction, **applicable** = covered + missing (not-applicable capabilities are excluded from both). The glyph is the group's **support level**:

| Glyph | Level | Meaning |
|---|---|---|
| ✅ | **Comprehensive** | every applicable capability is `full` — fixture-proven, resolves the general case |
| 🟢 | **Supported** | every applicable capability is extracted; some only *heuristically* (detected by pattern, not full AST/data-flow resolution) |
| 🟡 | **Partial** | some capabilities extracted, some still missing |
| 🔴 | **Not extracted** | nothing extracted yet |
| — | **N/A** | capability does not apply to this framework |

Examples: `🟢 20/20` = fully supported, some capabilities heuristic · `🟡 12/20` = 8 not yet extracted. Detail pages use the same palette **per cell** (✅ full · 🟢 heuristic/partial · 🔴 missing · — n/a).

## Frameworks

| Name | Auth coverage | Endpoint synthesis | Handler attribution | Middleware coverage | Notes |
|---|---|---|---|---|---|
| [Django REST Framework](../detail/lang.python.framework.django-drf.md) | 🟢 | ✅ | ✅ | — | |
| [FastAPI](../detail/lang.python.framework.fastapi.md) | — | ✅ | — | — | |
| [Flask](../detail/lang.python.framework.flask.md) | — | ✅ | — | — | |
| [Starlette](../detail/lang.python.framework.starlette.md) | — | 🔴 | 🔴 | — | |

## Other


### Validation

| Name | Testing | Other capabilities | Notes |
|---|---|---|---|
| [Pydantic](../detail/lang.python.validation.pydantic.md) | 🔴 0/1 | 🟡 1/5 | |
