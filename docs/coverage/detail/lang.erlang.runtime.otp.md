<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.erlang.runtime.otp` — Erlang/OTP behaviours

Auto-generated. Back to [summary](../summary.md).

- **Language:** [erlang](../by-language/erlang.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Core extraction | ✅ `full` | `2026-06-12` | 4929 | `internal/extractors/erlang/extractor.go`<br>`internal/extractors/erlang/extractor_test.go`<br>`internal/types/kinds.go` | #4903: OTP behaviour detection. -behaviour(gen_server|gen_statem|gen_event|gen_fsm|supervisor|application). (both British -behaviour and American -behavior spellings; behaviourRE) is detected and stamps the module entity — Subtype refined to gen_server_module / supervisor_module / application_module (or otp_module for multi-behaviour modules; otpModuleSubtype), Properties["otp_behaviour"]=comma-list, Tags ["otp","otp:<behaviour>"]. The behaviour's canonical callbacks (handle_call/handle_cast/handle_info/init/terminate/code_change for gen_server; init for supervisor; start/stop for application; etc.; otpCallbacks table) are re-typed Subtype="otp_callback" with Properties["otp_callback_of"]. #4929 deepens this into two semantic layers, closing the previously-partial gap. (1) SUPERVISION-TREE CHILD_SPEC EDGES: when a module is a supervisor (behaviourSet["supervisor"]) its init/1 body is parsed for the child spec list and a SUPERVISES edge (new RelationshipKindSupervises in internal/types/kinds.go, registered in AllRelationshipKinds and proven by the producer-kinds guardrail in internal/types) is emitted from the supervisor module entity to each child module; parseChildSpecs handles BOTH spec shapes after stripCommentsAndStrings scrubbing: the modern map form (childSpecMapStartRE for the start-MFA module, childSpecMapIDRE for the id; map literals isolated by mapSegments so each start is paired with its own id) and the legacy tuple form {Id, {M, F, A}, ...} (childSpecTupleRE); children de-duplicated by module; edge Properties: child_id, provenance="otp_child_spec". (2) PER-MESSAGE-TAG DISPATCH: for the request-dispatching callbacks (handle_call/handle_cast/handle_info/handle_event/handle_sync_event; isDispatchCallback) the message TAG of each clause is recovered (first pattern element of the first arg; a {get, Key} tuple yields 'get', a bare atom 'flush' yields 'flush') from per-clause argument heads (funcInfo.argHeads; clauseMatch.args is funcHeadRE group 2) via firstArgTag + splitTopLevelFirst (nesting-aware top-level comma split over (){}[]) + isAtom; variable/wildcard catch-all clauses (_Request,_Msg,_) carry no concrete tag and are skipped; tags emitted in clause order, de-duplicated, onto the callback as Properties["otp_dispatch_tags"]=comma-list and Tags ["otp_msg:TAG", ...]. Proven by TestErlangExtractor_OTPBehaviour, TestErlangExtractor_OTPCallbacks, TestErlangExtractor_SupervisorBehaviour, TestErlangExtractor_SupervisionTreeEdges (cache_server + logger_srv maps + db_pool tuple), TestErlangExtractor_MessageTagDispatch (handle_call yields 'get'; handle_cast yields 'put,delete,flush'), TestErlangExtractor_GenServerRecall. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.erlang.runtime.otp ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
