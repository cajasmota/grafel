# Quickstart

This page gets you from zero to a running graph in five commands. For a full install matrix (pre-built binaries, dev mode, Windows) see [install.md](install.md).

---

## Prerequisites

- **Go 1.26+** with CGO enabled. CGO is required because the tree-sitter extractor uses a C library.
  - macOS: `xcode-select --install` (Xcode Command Line Tools)
  - Debian/Ubuntu: `apt install build-essential`
- **Node.js 20+** and npm (used to build the embedded dashboard)
- **git**

> The fastest path is the one-line installer (below). If you prefer to build
> the binary yourself, see the **Build from source** alternative under step 1.

---

## 1. Install

macOS / Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/cajasmota/grafel/main/install.sh | bash
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/cajasmota/grafel/main/install.ps1 | iex
```

The installer places `grafel` under `~/.grafel/bin`. Confirm with `grafel --version`.

**Build from source** (alternative):

```sh
git clone https://github.com/cajasmota/grafel.git
cd grafel
make build          # builds dashboard + binary -> ./grafel
./grafel --version
```

Optional — add the source build to `PATH`:

```sh
go install -ldflags="-X main.commit=$(git rev-parse --short HEAD)" ./cmd/grafel
# installs to ~/go/bin -- make sure ~/go/bin is on your PATH
```

---

## 2. Index your code

```sh
./grafel wizard
```

The wizard asks you to point at a folder. It accepts:
- A single git repo
- A folder containing several git repos (they become one multi-repo group)
- A monorepo (auto-split into modules)

You can also use the **Add group** button in the dashboard after step 3.

---

## 3. Start the daemon + register MCP

```sh
./grafel install
```

This starts the daemon as a background service, registers the MCP server in your AI agent's config (Claude Code's `~/.claude/claude.json`, or equivalent for other clients), and installs the skill family to `~/.claude/skills/`.

To verify:

```sh
./grafel status
```

Output shows `MCP: connected` when the wiring is complete.

---

## 4. Open the dashboard

```sh
./grafel dashboard    # opens http://127.0.0.1:47274 in your browser
```

The dashboard is embedded in the daemon binary — no separate server needed. Deep links and browser reloads work on every surface.

---

## 5. Query from your agent

Open a new Claude Code session in one of your indexed repos. The MCP server is auto-registered, so you can call grafel tools immediately:

```
grafel_whoami()      -- confirm group + repo
grafel_stats()       -- entity counts, any unavailable repos
grafel_clusters()    -- module map (Louvain communities)
```

For a complete guide to navigating with the MCP tools, see [../skills/using-grafel/SKILL.md](../skills/using-grafel/SKILL.md).

---

## Daemon control

```sh
grafel start          # start daemon in background
grafel stop           # stop daemon
grafel restart        # restart daemon
grafel status         # health check all groups
grafel doctor         # full install smoke-check
```

After upgrading grafel:

```sh
grafel rebuild <group>    # force AST rebuild, no cache
```

---

## Uninstall

```sh
grafel uninstall          # removes skills, MCP entry, stops daemon
grafel uninstall --purge  # also removes ~/.grafel/store/ (your graphs)
```
