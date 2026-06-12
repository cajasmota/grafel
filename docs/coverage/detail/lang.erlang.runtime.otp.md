<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.erlang.runtime.otp` — Erlang/OTP behaviours

Auto-generated. Back to [summary](../summary.md).

- **Language:** [erlang](../by-language/erlang.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Core extraction | 🟢 `partial` | `2026-06-12` | 4903 | `internal/extractors/erlang/extractor.go`<br>`internal/extractors/erlang/extractor_test.go` | #4903: OTP behaviour detection. -behaviour(gen_server|gen_statem|gen_event|gen_fsm|supervisor|application). (both British -behaviour and American -behavior spellings; behaviourRE) is detected and stamps the module entity — Subtype refined to gen_server_module / supervisor_module / application_module (or otp_module for multi-behaviour modules; otpModuleSubtype), Properties["otp_behaviour"]=comma-list, Tags ["otp","otp:<behaviour>"]. The behaviour's canonical callbacks (handle_call/handle_cast/handle_info/init/terminate/code_change for gen_server; init for supervisor; start/stop for application; etc.; otpCallbacks table) are re-typed Subtype="otp_callback" with Properties["otp_callback_of"]. Proven by TestErlangExtractor_OTPBehaviour, TestErlangExtractor_OTPCallbacks, TestErlangExtractor_SupervisorBehaviour, TestErlangExtractor_GenServerRecall. Partial: supervision-tree child_spec edges (supervisor:init → {ok,{SupFlags,[ChildSpec]}}) and per-message-tag handle_call/cast dispatch are not yet resolved into edges — tracked in follow-up #4929. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.erlang.runtime.otp ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
