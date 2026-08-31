# Setting up grafel in your AI coding tool

This is the step-by-step "how do I get grafel working in *my* tool" guide. For
the capability matrix (which artifact each tool gets) see [tools.md](tools.md);
for the full install matrix (script, binary, source) see [install.md](install.md).

The short version: **you almost never wire anything up by hand.** `grafel
install` writes the MCP config, the rules file, and (for Claude Code) the
skills and agent hook for every enabled tool, automatically. This page tells
you what each tool gets, where it lands, and how to verify it.

---

## 1. Install grafel and let it wire your tools

```sh
# 1. Install the binary + daemon
curl -fsSL https://raw.githubusercontent.com/cajasmota/grafel/main/install.sh | bash

# 2. Register your repos (creates the group config)
grafel wizard

# 3. Wire MCP + rules + skills into your AI coding tools and start the daemon
grafel install
```

`grafel install` does three things per enabled tool:

- **Writes the MCP entry** — registers the grafel MCP server in the tool's
  config so the agent can call the `grafel_*` tools (`grafel_find`,
  `grafel_inspect`, `grafel_trace`, …). One global entry per tool; the single
  daemon routes by the caller's working directory. The entry points at the
  `grafel` binary with args `["mcp-bridge"]` (a local **stdio** server).
- **Writes the rules file** — a marker-wrapped "prefer the grafel MCP over
  grep" guidance block, written **per repo** into the tool's rules file.
- **Writes skills + the agent hook** — the grafel skill family (slash commands)
  and the opt-in `PreToolUse` grep-interceptor hook. **Claude Code only** today.

A tool that lacks a capability is a no-op for that artifact — grafel only
writes what the tool can actually consume.

> **MCP tool names are `grafel_*`.** Once the MCP server is registered and the
> tool is restarted, you'll see tools like `grafel_find`, `grafel_inspect`,
> `grafel_related`, and `grafel_trace` in the tool's MCP/tool list.

---

## 2. Choose which tools grafel targets

By default grafel targets **every supported tool**. To pick a subset, use any of:

```sh
# Explicit allow-list (non-interactive)
grafel install --tools claude,cursor,windsurf

# Interactive multi-select wizard (on a TTY, with no --tools/--yes/--no-wizard)
grafel install

# Inspect / change the selection after install (in-process, no daemon restart)
grafel tools list                 # show every tool with enabled/detected state
grafel tools enable cursor kiro   # enable tools + write their artifacts
grafel tools disable codeium      # disable tools + remove their artifacts
```

Valid tool IDs: `claude`, `codex`, `cursor`, `windsurf`, `codeium`,
`copilot`, `kiro`, `antigravity`, `opencode`.

You can also edit the selection from the dashboard under **Settings → AI coding
tools** — a checklist with each tool's enabled and `(detected)` state. Saving
applies the delta in-process; the daemon stays up (no `grafel install`, no
restart).

See [tools.md](tools.md) for the full selection semantics (precedence, defaults,
web panel API).

---

## 3. Per-tool setup

Each section lists: what `grafel install` writes, and how to verify it. After
install, **restart the tool** so it re-reads its MCP config, then confirm the
`grafel` MCP server appears (for MCP-capable tools) and/or the rules file
exists.

### Claude Code (`claude`)

- **MCP**: `~/.claude.json` (global). Also registered into any
  `~/.claude-*/` config dirs that contain a `.claude.json`.
- **Rules**: `CLAUDE.md` (per repo).
- **Skills**: the grafel skill family under `~/.claude/skills/`.
- **Agent hook**: the opt-in `PreToolUse` grep-interceptor.

**Verify**

```sh
grep -A 6 '"grafel"' ~/.claude.json     # MCP entry present
ls CLAUDE.md                            # rules file in your repo
ls ~/.claude/skills/ | grep grafel      # skills installed
```

Restart Claude Code, then run `/mcp` (or check the MCP panel) — the `grafel`
server should be listed and its `grafel_*` tools available. The grafel slash
commands (e.g. `/grafel-graph-enrich`) should also appear.

### Codex (`codex`)

- **MCP**: `~/.codex/config.toml` — **TOML**, table `[mcp_servers.grafel]`
  (Codex uses TOML, not JSON; every other MCP tool uses JSON).
- **Rules**: `AGENTS.md` (per repo).

**Verify**

```sh
grep -A 4 '\[mcp_servers.grafel\]' ~/.codex/config.toml
ls AGENTS.md
```

Restart Codex; confirm the `grafel` MCP server loads and the `grafel_*` tools
are callable.

### Cursor (`cursor`)

- **MCP**: `~/.cursor/mcp.json` (JSON, `{ "mcpServers": { "grafel": … } }`).
- **Rules**: `.cursorrules` (per repo).

**Verify**

```sh
grep -A 6 '"grafel"' ~/.cursor/mcp.json
ls .cursorrules
```

Restart Cursor; in **Settings → MCP** the `grafel` server should show as
connected with its tools listed.

### Windsurf (`windsurf`)

- **MCP**: `~/.codeium/windsurf/mcp_config.json` (desktop app). The Windsurf
  JetBrains plugin uses `~/.codeium/mcp_config.json`.
- **Rules**: `.windsurfrules` (per repo).

**Verify**

```sh
grep -A 6 '"grafel"' ~/.codeium/windsurf/mcp_config.json
ls .windsurfrules
```

Restart Windsurf; the Cascade MCP panel should list the `grafel` server.

### Kiro (`kiro`)

- **MCP**: `~/.kiro/settings/mcp.json` (JSON, user-global). Kiro also supports a
  workspace-level `<repo>/.kiro/settings/mcp.json`, but grafel writes the
  user-global file.
- **Rules**: `.kiro/steering/grafel.md` (per repo — Kiro reads steering markdown
  from `<repo>/.kiro/steering/*.md`).

**Verify**

```sh
grep -A 6 '"grafel"' ~/.kiro/settings/mcp.json
ls .kiro/steering/grafel.md
```

Restart Kiro; confirm the `grafel` MCP server and its `grafel_*` tools.

### Antigravity (`antigravity`)

- **MCP**: `~/.gemini/antigravity/mcp_config.json` (JSON, same
  `{ "mcpServers": { "grafel": … } }` stdio shape as Cursor/Kiro).
- **Rules**: `.agent/rules/grafel.md` (per repo — Antigravity reads rules
  markdown from `<repo>/.agent/rules/*.md`).

**Verify**

```sh
grep -A 6 '"grafel"' ~/.gemini/antigravity/mcp_config.json
ls .agent/rules/grafel.md
```

Restart Antigravity; confirm the `grafel` MCP server loads.

### opencode (`opencode`)

- **MCP**: `~/.config/opencode/opencode.json` (honours `$XDG_CONFIG_HOME`).
  JSON, but **not** the `mcpServers` shape the other JSON hosts use — opencode
  puts servers under a top-level **`mcp`** key, wants `type: "local"`, and takes
  the whole argv as a `command` **array** with no `args` key:

  ```json
  { "mcp": { "grafel": { "type": "local",
                         "command": ["/path/to/grafel", "mcp-bridge"] } } }
  ```

  grafel writes this shape for you. It matters because opencode **ignores**
  config keys it does not recognise rather than erroring, so a file written in
  the `mcpServers` shape looks perfectly valid and simply never loads the
  server. The file is edited as JSONC — your comments and trailing commas
  survive, and other servers are left untouched.
- **Rules**: `AGENTS.md` (per repo) — the **same file Codex uses**. opencode
  checks the project `AGENTS.md` first (walking up from the working directory),
  then `~/.config/opencode/AGENTS.md`, then `~/.claude/CLAUDE.md`. Because the
  file has two owners, `grafel tools disable codex` leaves `AGENTS.md` in place
  while opencode is still enabled — it is removed only when the last owner goes.
- **Skills**: nothing opencode-specific is written, but opencode still **sees**
  grafel's skills if you have Claude Code enabled: it reads
  `~/.claude/skills/<name>/SKILL.md` through its Claude-compat fallback, and
  that is the directory grafel populates for Claude.

**Verify**

```sh
grep -A 6 '"grafel"' ~/.config/opencode/opencode.json   # under "mcp", not "mcpServers"
ls AGENTS.md
```

Restart opencode; the `grafel` MCP server should load and the `grafel_*` tools
be callable. If the tools are missing but the file looks right, check that the
entry is under `"mcp"` — an entry under `"mcpServers"` is silently ignored.

### Codeium (`codeium`) — rules only

- **MCP**: none. grafel does **not** register an MCP server for Codeium, so the
  `grafel_*` tools are not callable here.
- **Rules**: `.codeium/instructions.md` (per repo).

**Verify**

```sh
ls .codeium/instructions.md
```

The rules file steers the agent toward grafel's conventions, but there is no
MCP surface. For graph queries, use an MCP-capable host (Claude Code, Cursor,
Windsurf, Kiro, Antigravity, Codex, opencode).

### GitHub Copilot (`copilot`) — rules only

- **MCP**: none.
- **Rules**: `.github/copilot-instructions.md` (per repo).

**Verify**

```sh
ls .github/copilot-instructions.md
```

Same as Codeium: rules-only, no `grafel_*` MCP tools. Use an MCP-capable host
for graph queries.

---

## 4. Notes

- **Rules files are per-repo**; MCP entries are written once to the
  **user-global** path shown above. On Windows the same relative paths apply
  under the user profile.
- **There are three MCP config shapes.** Claude Code, Cursor, Windsurf, Kiro and
  Antigravity write JSON `{ "mcpServers": { "grafel": { ... } } }`; **Codex**
  writes TOML (`[mcp_servers.grafel]`); **opencode** writes JSON under a
  top-level `"mcp"` key with `command` as an array — see its section above.
- **`AGENTS.md` is shared by Codex and opencode.** It is written once, and
  removed only when the last of the two is disabled.
- After enabling/disabling tools with `grafel tools enable|disable` or the web
  panel, restart the affected tool so it re-reads its config.

---

## See also

- [tools.md](tools.md) — supported-tools matrix + enable/disable reference.
- [install.md](install.md) — full install matrix (script, binary, source).
- [agent-hosts.md](agent-hosts.md) — per-agent model/session setup for the
  enrichment skills.
- [mcp-tools.md](mcp-tools.md) — the `grafel_*` MCP tool catalogue.
