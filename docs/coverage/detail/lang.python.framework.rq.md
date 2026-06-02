<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.rq` — RQ (Redis Queue)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Task Queue
- **Capability cells:** 29

## Capabilities


### Tasks

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Task extraction | 🟢 `partial` | `2026-05-28` | — | `internal/custom/python/rq.go` | — |
| Task routing | 🟢 `partial` | `2026-05-28` | — | `internal/custom/python/rq.go` | — |

### Schedule

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Schedule extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3074) | `internal/custom/python/broker_binding_test.go`<br>`internal/custom/python/rq.go` | rq-scheduler enqueue_at/enqueue_in/cron patterns detected. Partial because periodic job re-scheduling (scheduler.schedule) and django-rq integration are not yet modelled. |

### Broker

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Broker binding | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3074) | `internal/custom/python/broker_binding_test.go`<br>`internal/custom/python/rq.go` | — |
| Result backend binding | 🟢 `partial` | — | [link](https://github.com/cajasmota/archigraph/issues/3074) | `internal/custom/python/rq.go` | RQ stores results in the same Redis instance used as broker; no separate result_backend URL is configured. Partial because the connection is inferred from the broker Redis conn, not a distinct field. |

### Reliability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Retry policy extraction | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3074) | `internal/custom/python/broker_binding_test.go`<br>`internal/custom/python/rq.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/pytest.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | `2026-05-29` | 3068 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go`<br>`internal/types/confidence.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractors/python/config_consumer.go`<br>`internal/extractors/python/config_consumer_test.go` | settings.X / os.environ.get(k) -> DEPENDS_ON_CONFIG (live pre-#3641; config-blast-radius) |
| Constant propagation | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go` | — |
| DB effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/effect_sinks_python.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-29` | 3047 | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_python.go` | dead code derived from reachability + entry-point sniffer; partial because RQ @job entry wiring is not modelled |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/def_use_python.go` | — |
| Env fallback recognition | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/effect_sinks_python.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/effect_sinks_python.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/python.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 3047 | `internal/links/module_cycle_pass.go` | language-agnostic Tarjan SCC over IMPORTS edges; fires on all Python modules; partial because RQ job module cycles are not distinguished from app-level cycles |
| Mutation effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/effect_sinks_python.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 3047 | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | derivative of effect propagation; fires on all Python entities; partial because RQ job functions may have side effects missed by heuristic sniffers |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | 3047 | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_python.go` | language-wide Python entry-point sniffer covers module-level __main__/test/lifecycle; partial because RQ @job entry wiring is not modelled |
| Request shape extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/payload_shapes_python.go` | — |
| Response shape extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/payload_shapes_python.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/taint_sites_python.go` | — |
| Schema drift detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/payload_shapes_python.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/taint_sites_python.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/taint_sites_python.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/template_pattern_python.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | 3047 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | derives from taint source+sink co-occurrence; fires on all Python; partial for RQ job context |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.rq ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
