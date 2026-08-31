# Supported AI coding tools

`grafel install` wires the grafel knowledge graph into the AI coding tools you
use. For each tool grafel can write up to three kinds of artifact:

- **MCP entry** — registers the grafel MCP server in the tool's config so the
  agent can call the `grafel_*` tools (e.g. `grafel_find`, `grafel_inspect`,
  `grafel_trace`). One global entry per tool; the single daemon routes by the
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
| **Antigravity** (`antigravity`) | ✓ `~/.gemini/antigravity/mcp_config.json` | `.agent/rules/grafel.md` | ✗ | ✗ | ✓ (MCP config present) |
| **opencode** (`opencode`) | ✓ `~/.config/opencode/opencode.json` (JSON, top-level `mcp` — **not** `mcpServers`) | `AGENTS.md` (shared with Codex) | ✗ (see note) | ✗ | ✓ (MCP config present) |

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
  exists; the two rules-only tools (Codeium, Copilot) report "not
  detected" since there is no config file to probe. Detection is **advisory** —
  it only pre-checks tools in the wizard; install still honours your explicit
  selection regardless.
- **Three MCP config shapes, not one.** Claude Code, Cursor, Windsurf, Kiro and
  Antigravity share the JSON `{ "mcpServers": { "grafel": { "command": …,
  "args": ["mcp-bridge"] } } }` shape. **Codex** writes TOML (table
  `[mcp_servers.grafel]`). **opencode** writes JSON, but a *different* JSON —
  see its section below.
- **AGENTS.md has two owners**: Codex and opencode both read it. grafel writes
  the block once (it is marker-wrapped and idempotent) and removes the file's
  block only when the **last** owner is disabled — so `grafel tools disable
  codex` leaves `AGENTS.md` in place while opencode is still enabled, and vice
  versa. Enabling *either* tool alone is enough to get `AGENTS.md` written.

### Antigravity — MCP + rules

Google Antigravity gets both the rules file (`.agent/rules/grafel.md`) and an
MCP entry at `~/.gemini/antigravity/mcp_config.json` (#5280). grafel is a local
**stdio** server, so the entry uses the standard JSON
`{ "mcpServers": { "grafel": { "command": ..., "args": ["mcp-bridge"] } } }`
shape — identical to Cursor/Kiro. (The `serverUrl` key applies only to remote
HTTP MCP servers and is not used here.)

### opencode — a different JSON shape, and a shared rules file

opencode's config is JSON but **not** the `mcpServers` shape every other JSON
host uses. grafel writes `~/.config/opencode/opencode.json` (honouring
`$XDG_CONFIG_HOME`) in opencode's own schema:

```json
{
  "mcp": {
    "grafel": {
      "type": "local",
      "command": ["/path/to/grafel", "mcp-bridge"]
    }
  }
}
```

Four things differ from the `mcpServers` shape, and **none of them fails
loudly**:

- the top-level key is **`mcp`**, not `mcpServers`;
- `type` is **`"local"`**, not `"stdio"`;
- the whole argv goes in **`command` as an array** — there is no `args` key, and
  the schema sets `additionalProperties: false`, so an `args` sibling is invalid
  rather than ignored;
- since v1.18.16 opencode **ignores unknown top-level config fields instead of
  failing to parse**. Writing `mcpServers` there therefore succeeds, leaves a
  valid file on disk, satisfies any existence check — and the server simply
  never loads, with no error anywhere to find. That is why grafel has a
  dedicated writer for this format (#6730) rather than reusing the JSON one.

The file is edited as JSONC: your comments and trailing commas survive, foreign
servers and unrelated keys are left alone, and a file grafel did not create is
never reflowed.

**Rules**: opencode reads `AGENTS.md` — the project file first (walking up from
the working directory), then `~/.config/opencode/AGENTS.md`, then
`~/.claude/CLAUDE.md` as a Claude-compat fallback. grafel writes the per-repo
`AGENTS.md`, the **same file Codex uses**; see the shared-ownership note above
for what that means when you disable one of the two.

**Skills**: the matrix says ✗, which needs a word of explanation, because
opencode *does* read skills. It reads them from
`~/.claude/skills/<name>/SKILL.md` — the directory grafel populates for Claude
Code — via the same Claude-compat fallback. So if you have Claude Code enabled,
grafel's skills are already visible in opencode. The ✗ means "grafel writes no
opencode-specific skills artifact", not "skills are unavailable here": the
skills copy is Claude-pathed and global, so there is nothing per-tool to write.

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
`kiro`, `antigravity`, `opencode`. Run `grafel tools list` to see them with
current state.

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
