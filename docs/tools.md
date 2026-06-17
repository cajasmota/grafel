# Supported AI coding tools

`grafel install` wires the grafel knowledge graph into the AI coding tools you
use. For each tool grafel can write up to three kinds of artifact:

- **MCP entry** — registers the grafel MCP server in the tool's config so the
  agent can call the `grafel_*` tools (e.g. `grafel_find`, `grafel_inspect`,
  `grafel_traces`). One global entry per tool; the single daemon routes by the
  caller's working directory.
- **Rules file** — a marker-wrapped "prefer the grafel MCP over grep" guidance
  block written into the tool's per-repo rules file.
- **Skills + agent hook** — the grafel skill family (slash commands) and the
  opt-in `PreToolUse` grep-interceptor hook. **Claude Code only** today.

A tool that lacks a capability is a no-op for that artifact — grafel only
writes what the tool can actually consume.

---

## Supported-tools matrix

| Tool | MCP (config path) | Rules file | Skills | Agent hook | Detected? |
|------|-------------------|-----------|:------:|:----------:|:---------:|
| **Claude Code** (`claude`) | ✓ `~/.claude.json` | `CLAUDE.md` | ✓ | ✓ | ✓ (MCP config present) |
| **Codex** (`codex`) | ✓ `~/.codex/config.toml` (TOML, `[mcp_servers.grafel]`) | `AGENTS.md` | ✗ | ✗ | ✓ (MCP config present) |
| **Cursor** (`cursor`) | ✓ `~/.cursor/mcp.json` | `.cursorrules` | ✗ | ✗ | ✓ (MCP config present) |
| **Windsurf** (`windsurf`) | ✓ `~/.codeium/windsurf/mcp_config.json` | `.windsurfrules` | ✗ | ✗ | ✓ (MCP config present) |
| **Codeium** (`codeium`) | ✗ | `.codeium/instructions.md` | ✗ | ✗ | ✗ (rules-only) |
| **GitHub Copilot** (`copilot`) | ✗ | `.github/copilot-instructions.md` | ✗ | ✗ | ✗ (rules-only) |
| **Kiro** (`kiro`) | ✓ `~/.kiro/settings/mcp.json` | `.kiro/steering/grafel.md` | ✗ | ✗ | ✓ (MCP config present) |
| **Antigravity** (`antigravity`) | ✗ (rules-only today — see below) | `.agent/rules/grafel.md` | ✗ | ✗ | ✗ (rules-only) |

Notes:

- The parenthesised value (e.g. `claude`, `cursor`) is the **tool ID** — the
  stable key used by `--tools`, `grafel tools enable/disable`, and the web
  panel.
- Rules files are written **per repo** (relative to each repo root in the
  group). MCP entries are written once to the **user-global** config path shown.
- Config paths use `~` for the user's home directory. On Windows the same
  relative paths apply under the user profile.
- **Detected?** is a best-effort signal that the tool is present on this
  machine: for MCP-capable tools it checks whether the tool's MCP config file
  exists; the two rules-only tools (Codeium, Copilot, Antigravity) report "not
  detected" since there is no config file to probe. Detection is **advisory** —
  it only pre-checks tools in the wizard; install still honours your explicit
  selection regardless.
- **Codex** writes TOML (table `[mcp_servers.grafel]`), not JSON. Every other
  MCP-capable tool uses the JSON `{ "mcpServers": { "grafel": { ... } } }`
  shape.

### Antigravity — rules-only today

Google Antigravity gets the rules file (`.agent/rules/grafel.md`) but **no MCP
entry yet**. Antigravity's MCP config path is not confidently verifiable
(public sources disagree on the location, and it uses a non-standard
`serverUrl` JSON key), so grafel ships the rules adapter only rather than write
a path it cannot justify. MCP support is tracked in
[#5280](https://github.com/cajasmota/grafel/issues/5280); once the path and
JSON shape are confirmed, Antigravity will register an MCP entry like the other
tools.

---

## Choosing which tools grafel targets

### Default behaviour

When you don't make an explicit selection, the effective set is **every
supported tool** (all rows above). This is the historical default and keeps CI
and existing installs working unchanged: a group with no `tools` field behaves
exactly as before (all rules files + all supported MCP entries). An explicit
selection becomes an allow-list — only the named tools get artifacts.

> Selection is stored in the group config as `GroupConfig.Tools`. An absent or
> empty list means "use the default (all tools)". Unknown IDs are dropped; a
> selection that names *only* unknown IDs falls back to the default rather than
> installing nothing.

### CLI — `grafel install --tools`

Pass a comma-separated list of tool IDs to target exactly those tools
(non-interactive):

```sh
grafel install --tools claude,cursor,windsurf
```

Valid IDs: `claude`, `codex`, `cursor`, `windsurf`, `codeium`, `copilot`,
`kiro`, `antigravity`. Run `grafel tools list` to see them with current state.

### CLI — the interactive wizard

When you run `grafel install` on an interactive terminal **without** `--tools`,
`--no-wizard`, or `--yes`, grafel shows a multi-select checklist of every
supported tool. Tools detected on your machine are pre-checked; toggle with
**space**, confirm with **enter**.

Precedence:

1. `--tools a,b,c` → explicit, **non-interactive** (wins over the wizard).
2. Interactive wizard → only when stdin is a TTY **and** neither `--tools` nor
   `--yes`/`--no-wizard` was given.
3. Otherwise (no flag, no TTY, or `--yes`/`--no-wizard`) → leave the existing
   selection alone. **CI is never blocked.**

```sh
grafel install --no-wizard   # skip the wizard even on a TTY; keep current/default set
grafel install --yes         # assume defaults for all prompts (alias for --no-wizard here)
```

Selecting nothing in the wizard is treated as "keep the default (all tools)" to
avoid the footgun of disabling everything.

### CLI — `grafel tools list | enable | disable`

Inspect or change the selection **after** install, without re-running
`grafel install` and without restarting the daemon — the artifact delta is
applied in-process:

```sh
grafel tools                       # list all tools with enabled/detected state
grafel tools list                  # same as above
grafel tools enable cursor kiro    # enable tools and write their artifacts
grafel tools disable codeium       # disable tools and remove their artifacts
```

- `grafel tools list` marks each tool `enabled`/`disabled` for the resolved
  group and appends `(detected)` when present on the machine. If the group has
  no explicit selection it notes "all tools enabled by default".
- `enable`/`disable` update `GroupConfig.Tools`, persist it, and re-apply only
  the **changed** tools' artifacts (rules files written/removed, MCP entries
  registered/unregistered) in-process. They never shell out to
  `grafel install` and never stop/start the daemon.
- Use `--group <name>` to target a specific group (defaults to the only
  registered group).

### Web — Settings → "AI coding tools"

The dashboard exposes the same selection in **Settings → AI coding tools**: a
checklist of every supported tool with its enabled and `(detected)` state.
Toggle the tools you want and click **Save tools**.

- Saving applies the delta **in-process** via `PUT /api/v2/groups/{group}/tools`
  — the daemon stays up across the change (no `grafel install`, no restart).
- The panel reads the current state from `GET /api/v2/groups/{group}/tools`,
  which returns one `{ id, displayName, enabled, detected }` row per adapter
  plus an `explicit` flag (whether the group has an explicit selection vs the
  all-tools default).
- The save response includes a per-tool summary with an action of `written`
  (newly enabled, artifacts rewritten), `removed` (newly disabled, artifacts
  removed), `unchanged`, or `error` (the failure detail is reported per tool and
  is not fatal to the whole save).

---

## See also

- [install.md](install.md) — full install matrix (script, binary, source).
- [agent-hosts.md](agent-hosts.md) — per-agent model/session setup for the
  enrichment skills.
- [mcp-tools.md](mcp-tools.md) — the `grafel_*` MCP tool catalogue.
