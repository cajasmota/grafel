<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.c-cpp.framework.ros` — ROS (Robot Operating System)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C/C++](../by-language/c-cpp.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Desktop
- **Capability cells:** 14

## Capabilities


### Process

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| IPC extraction | 🟢 `partial` | — | — | `internal/custom/cpp/ros_extractor.go` | ROS1 advertise/subscribe/advertiseService/serviceClient and ROS2 create_publisher/create_subscription/create_service/create_client topic/service names extracted; regex/partial |
| Main renderer split | — `not_applicable` | — | — | `internal/custom/cpp/ros_extractor.go` | ROS is a robotics pub-sub middleware; it has no main-process/renderer-process split concept |

### Native

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Native module imports | 🟢 `partial` | — | — | `internal/custom/cpp/ros_extractor.go` | ROS #include headers (ros/, sensor_msgs/, geometry_msgs/, etc.) and package.xml <depend> entries extracted as native module imports; regex/partial |

### Updates

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | ✅ `full` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/c_cpp.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_c_cpp.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_c_cpp.go` | — |
| Env fallback recognition | ✅ `full` | — | — | `internal/substrate/c_cpp.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_c_cpp.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_c_cpp.go` | — |
| Import resolution quality | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/c_cpp.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_c_cpp.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_c_cpp.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.c-cpp.framework.ros ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
