<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.cobol.embedded.cics` — COBOL CICS

Auto-generated. Back to [summary](../summary.md).

- **Language:** [COBOL](../by-language/cobol.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-06-12` | 4908 | `internal/extractors/cobol/depth.go`<br>`internal/extractors/cobol/extractor.go`<br>`internal/extractors/cobol/extractor_test.go`<br>`internal/extractors/cobol/testdata/orderui.cbl` | EXEC CICS program-transfer commands emit cross-program CALLS edges (previously only the abstract http_effect was recorded): extractCICSTransfers parses LINK/XCTL PROGRAM('NAME') and START TRANSID('TRAN') from the buffered EXEC CICS block, cicsCallEdge stamps via=EXEC-CICS-<VERB> external=true line=<n> + program/transid prop. Proven by TestExtractor_CICSProgramTransfer. #4947 extends this beyond program transfer: BMS/MFS SEND/RECEIVE MAP('NAME') now surface a SCOPE.View/screen entity (buildCICSMapEntity, subtype=screen, ui=bms, mapset prop) with a RENDERS edge for SEND (terminal output) and a REFERENCES edge for RECEIVE (operator input), so the online-transaction presentation layer is a first-class node. Proven by TestExtractor_CICSScreenMaps / _CICSScreenMapSend. |
| Fs effect | ✅ `full` | `2026-06-12` | 2838 | `internal/extractors/cobol/depth.go`<br>`internal/extractors/cobol/extractor.go`<br>`internal/extractors/cobol/extractor_test.go`<br>`internal/extractors/cobol/testdata/orderui.cbl`<br>`internal/substrate/effect_sinks_cobol.go` | CICS file-control (READ/WRITE/REWRITE/DELETE FILE) and READQ/WRITEQ TS/TD queue verbs are sniffed as fs_read/fs_write by the COBOL effect sniffer and attributed to the enclosing paragraph. #4947 makes the queue target resolvable: extractCICSQueues parses READQ/WRITEQ/DELETEQ TS|TD QUEUE('NAME'|data-item) from the buffered EXEC CICS block and buildCICSQueueEntity emits one deduped SCOPE.Datastore/queue per queue (queue_type=TS|TD, storage=cics-<ts|td>-queue, dynamic_ref when the operand is a data-item) with a READS_FROM (READQ) / WRITES_TO (WRITEQ, DELETEQ) edge FromID=enclosing paragraph (via=EXEC-CICS-<VERB>), so cross-program queue coupling is now explicit (mirrors native VSAM SELECT, #4908). Proven by TestExtractor_CICSQueues / _CICSQueueLiteralAndTD. |
| HTTP effect | ✅ `full` | `2026-05-28` | 2838 | `internal/extractors/cobol/depth.go`<br>`internal/substrate/effect_sinks_cobol.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.cobol.embedded.cics ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
