# Provenance and licence — vbnet-mini fixture

Every file under `src/` is real VB.NET copied from an **MIT-licensed** upstream
project. Nothing here is hand-written pseudo-VB: a fixture built from invented
source grades the fixture author's model of the language, not the extractor.

LGPL/GPL corpora (e.g. `WakeOnLAN`) are deliberately excluded. Cloning one to
run the parser over it locally is fine; committing excerpts into this repo is
redistribution, and only the MIT sources permit that.

| `src/` file | Upstream | Licence | Extent |
|---|---|---|---|
| `Criteria.vb` | [staxrip/staxrip](https://github.com/staxrip/staxrip) — `Source/UI/Criteria/Criteria.vb` | MIT | verbatim, whole file |
| `FrameServer.vb` | staxrip/staxrip — `Source/Video/FrameServer.vb` | MIT | verbatim excerpt, lines 1–88 |
| `Win32Native.vb` | [Wagnard/display-drivers-uninstaller](https://github.com/Wagnard/display-drivers-uninstaller) — `display-driver-uninstaller/Display Driver Uninstaller/Win32/Win32.vb` | MIT | verbatim excerpts (header, `EvilInteger`, `IntPtrAdd`, `StructPtr`, `SYSTEMTIME`), reassembled under the original `Friend Module Win32Native` |

Copyright remains with the respective upstream authors under the MIT licence.

## Why these files

They between them exercise every shape S5 of #6327 emits, and none of the S7
gaps:

- **Containment** — Class (`Criteria`), Module (`Win32Native`), Structure
  (`EvilInteger`, `SYSTEMTIME`), Interface (`IFrameServer`,
  `INativeFrameServer`), Enum, plus a Class and a Structure nested in a Module.
- **`Inherits` → EXTENDS** — a three-deep in-tree chain
  (`IntCriteria` → `GenericCriteria` → `Criteria`) through a generic base whose
  type arguments must be stripped, and an interface inheriting an external one.
- **`Implements` → IMPLEMENTS** — a multi-target class-level clause with one
  in-tree and one external target.
- **`Imports` → IMPORTS** — on the per-file carrier: one folding onto the
  `System` external package, one staying a bare dotted namespace.
- **CALLS** — five edges, all gated on the paren disambiguator, plus four
  `forbidden_relationships` that pin the paren-less and keyword cases that must
  produce *no* edge.

`expected.json` asserts nothing that depends on partial-class merge, `Handles`,
`AddressOf`, With-block receivers, or method-level `Implements` — those are S7.
`FrameServer.vb` and `Win32Native.vb` do contain method-level `Implements`
clauses, which is fine precisely because no expectation reads them.
