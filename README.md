# archigraph

Multi-repo code knowledge graphs for AI agents.

## Status

This project is in early scaffolding. Track progress and roadmap via the
[issue tracker](https://github.com/cajasmota/archigraph/issues) and
[milestones](https://github.com/cajasmota/archigraph/milestones).

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

## License

MIT — see [LICENSE](LICENSE).
