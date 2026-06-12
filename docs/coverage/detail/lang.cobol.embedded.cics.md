<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.cobol.embedded.cics` — COBOL CICS

Auto-generated. Back to [summary](../summary.md).

- **Language:** [COBOL](../by-language/cobol.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-06-12` | 4908 | `internal/extractors/cobol/depth.go`<br>`internal/extractors/cobol/extractor_test.go` | EXEC CICS program-transfer commands emit cross-program CALLS edges (previously only the abstract http_effect was recorded): extractCICSTransfers parses LINK/XCTL PROGRAM('NAME') and START TRANSID('TRAN') from the buffered EXEC CICS block, cicsCallEdge stamps via=EXEC-CICS-<VERB> external=true line=<n> + program/transid prop. Proven by TestExtractor_CICSProgramTransfer. WEAK (follow-up #4947): READQ/WRITEQ/DELETEQ TS/TD queue verbs carry fs_effect but no resolvable queue datastore entity; BMS SEND/RECEIVE MAP screen-transfer not modelled. |
| Fs effect | ✅ `full` | `2026-06-12` | 2838 | `internal/extractors/cobol/depth.go`<br>`internal/substrate/effect_sinks_cobol.go` | CICS file-control (READ/WRITE/REWRITE/DELETE FILE) and READQ/WRITEQ TS/TD queue verbs are sniffed as fs_read/fs_write by the COBOL effect sniffer and attributed to the enclosing paragraph. WEAK (follow-up #4947): CICS queue/file targets are not yet emitted as resolvable SCOPE.Datastore entities (unlike native VSAM SELECT, #4908), so cross-program queue coupling stays implicit. |
| HTTP effect | ✅ `full` | `2026-05-28` | 2838 | `internal/extractors/cobol/depth.go`<br>`internal/substrate/effect_sinks_cobol.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.cobol.embedded.cics ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
