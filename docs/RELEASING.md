# Releasing archigraph

## Versioning policy

archigraph follows [Semantic Versioning](https://semver.org/) under a
`v0.MINOR.PATCH` scheme until v1.0.

| Component | Rule |
|-----------|------|
| **MINOR** | May break MCP tool APIs, graph schema wire format, or CLI flags. Always documented in CHANGELOG.md under "Changed" or "Known Limitations". |
| **PATCH** | Bug fixes and additive changes only. No breaking changes to public APIs, MCP tool signatures, or the `graph.fb` wire format. |

## v1.0 ship criteria

v1.0 ships when all of the following are true:

1. **Schema stability commitment** — `graph.fb` wire format version has been
   stable (no breaking field changes) for at least two consecutive MINOR
   releases.
2. **All 3 platforms verified end-to-end** — macOS, Linux, and Windows
   binaries each pass the full integration test suite in CI.
3. **MCP API stable for 3+ minor releases** — no tool renames, param renames,
   or response-shape changes across the most recent three MINOR versions.
4. **Documented migration path** — `docs/migration/v1.md` covers every
   breaking change since v0.1.0 with before/after examples.

Until v1.0, the "This is a pre-release" checkbox on every GitHub Release
must be ticked.

## Release process

### 1. Prepare the release commit

1. Update `CHANGELOG.md` — move entries from `[Unreleased]` to a new
   `[X.Y.Z] — YYYY-MM-DD` section.
2. Verify `go build ./...` and `go vet ./...` are clean.
3. Open a PR titled `release: vX.Y.Z prep` and merge it.

### 2. Tag

```bash
git checkout main
git pull
git tag vX.Y.Z
git push origin vX.Y.Z
```

### 3. GitHub Release

1. Go to **Releases → Draft a new release**.
2. Select the tag `vX.Y.Z`.
3. Title: `vX.Y.Z`.
4. Body: paste the CHANGELOG section for this version.
5. Tick **"This is a pre-release"** for every release until v1.0.
6. Attach platform binaries as release assets:
   - `archigraph-darwin-arm64`
   - `archigraph-darwin-amd64`
   - `archigraph-linux-amd64`
   - `archigraph-linux-arm64`
   - `archigraph.exe` (Windows, CGO/MinGW build)

### 4. Post-release

- Update `wire_version` constant in `internal/mcp/tools.go` for the next
  MINOR release.
- Bump the `graph.fb` schema `version` field if any wire-format changes
  landed (see [graph-format.md](graph-format.md)).

## Graph schema versioning

The on-disk `graph.fb` FlatBuffers schema carries a `version` integer field
(currently `2`). See [graph-format.md](graph-format.md) for the full policy.

## MCP wire_version

`archigraph_whoami` returns a `wire_version` field (e.g. `"0.1.0"`). Agents
can use this to detect incompatible daemon versions. The value is a constant
in `internal/mcp/tools.go` and must be bumped on every MINOR release.
