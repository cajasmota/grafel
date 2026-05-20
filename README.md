# archigraph

Multi-repo code knowledge graphs for AI agents.

## Status

Approaching v1.0.0. The full v1 post-mortem and migration runbook lives
at [`docs/migration/v1.md`](docs/migration/v1.md). Architectural
decisions are in [`docs/adrs/`](docs/adrs/). Track progress and roadmap
via the [issue tracker](https://github.com/cajasmota/archigraph/issues)
and [milestones](https://github.com/cajasmota/archigraph/milestones).

## Install

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/cajasmota/archigraph/main/install.sh | bash
```

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/cajasmota/archigraph/main/install.ps1 | iex
```

### Manual download

Pre-built binaries for every release are published at
https://github.com/cajasmota/archigraph/releases — pick the matching
`<os>_<arch>` archive (`linux_x86_64`, `linux_arm64`, `macos_x86_64`,
`macos_arm64`, or `windows_x86_64`).

### Build from source

Requires Go 1.22+. CGO is required (tree-sitter dependency).

```sh
git clone https://github.com/cajasmota/archigraph.git
cd archigraph
make build
./archigraph --version
```

## Usage

archigraph is a CLI plus a unified daemon process that manages indexing,
the MCP server, embedded dashboard, and file watchers. The common path:

```sh
# 1. Set up a group (interactive). Creates the group config and a
#    cross-repo links file scaffold.
archigraph wizard

# 2. One-shot setup: daemon, MCP, indexer, and dashboard.
archigraph install <group>

# 3. Confirm everything is wired.
archigraph status <group>

# 4. Open the dashboard in your browser (auto-starts daemon if needed).
archigraph dashboard
```

The daemon process manages everything — MCP server, indexing, live file
watchers, and the embedded dashboard on http://127.0.0.1:47274/. Control it
via:

```sh
archigraph start                # start the daemon
archigraph stop                 # stop the daemon
archigraph restart              # restart the daemon
archigraph dashboard serve      # run dashboard server standalone
```

Other useful commands:

```sh
archigraph index <repo>              # one-shot indexer (writes graph.json)
archigraph rebuild <group> [slug]    # force AST rebuild, no cache
archigraph reset <group> [slug]      # wipe .archigraph/ and rebuild
archigraph monorepo add <group> <p>  # opt a path inside a monorepo into indexing
archigraph doctor                    # smoke-check install + tools
archigraph uninstall <group>         # remove hooks/watchers from a group
archigraph patterns list             # inspect agent-learned patterns (ADR-0018)
archigraph patterns export --repo X  # write the CLAUDE.md marker block
archigraph patterns config           # show / set pattern thresholds
```

`archigraph help advanced` lists the full set.

## Contributing

If you're an AI agent contributing to archigraph, see [AGENTS.md](AGENTS.md) for conventions.
End-user-facing agent docs are delivered via the MCP `instructions` handshake — there is no per-user setup.

## Corpus & coverage

archigraph is validated against a curated corpus of small-to-medium
sample applications, one per supported language family. Framework
internals are deliberately excluded as primary fixtures — see
[ADR-0014](docs/adrs/0014-corpus-expansion-strategy.md). New language
support lands together with the sample apps that exercise it.

## License

MIT — see [LICENSE](LICENSE).
