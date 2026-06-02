<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.csharp.framework.winforms` — Windows Forms

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Desktop
- **Capability cells:** 15

## Capabilities


### Process

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| IPC extraction | 🟢 `partial` | — | — | `internal/custom/csharp/desktop_native.go`<br>`internal/custom/csharp/desktop_native_test.go` | Named pipe (NamedPipeServerStream/Client), MemoryMappedFile, WCF ServiceHost/ChannelFactory, Dispatcher.Invoke cross-thread dispatch, Process.Start child-process, and named EventWaitHandle/Mutex sync primitives detected. |
| Main renderer split | 🟢 `partial` | — | — | `internal/custom/csharp/desktop_native.go`<br>`internal/custom/csharp/desktop_native_test.go` | Application subclass declarations, Application.Run() entry points, static Main() methods, InitializeComponent() XAML wiring, and new XxxForm/XxxWindow() renderer-side construction detected. |

### Native

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Native module imports | 🟢 `partial` | — | — | `internal/custom/csharp/desktop_native.go`<br>`internal/custom/csharp/desktop_native_test.go` | [DllImport] P/Invoke, unsafe keyword pointer interop, [ComImport]/[Guid]/[InterfaceType] COM interop, using Windows.Win32./PInvoke. (CsWin32), Marshal interop helpers, and NativeLibrary.Load/GetExport detected. |

### Updates

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/csharp/config_consumer.go`<br>`internal/extractors/csharp/config_consumer_test.go` | IConfiguration indexer/GetValue/GetConnectionString + Environment.GetEnvironmentVariable -> config:<key> (issue #3641) |
| Constant propagation | ✅ `full` | `2026-05-29` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/csharp.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_csharp.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-29` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/csharp.go`<br>`internal/substrate/substrate.go` | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/constant_propagation.go`<br>`internal/substrate/csharp.go`<br>`internal/substrate/substrate.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_csharp.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.csharp.framework.winforms ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
