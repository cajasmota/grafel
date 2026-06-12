# Agent host configuration — Haiku for graph enrichment

The graph-enrichment pass (`/archigraph-graph-enrich`) runs hundreds to
thousands of LLM calls in batches. Using the wrong model (Sonnet or Opus)
inflates cost by 10–20× without meaningfully improving enrichment quality for
most entities. This page shows how to set `claude-3-haiku-20240307` as the
active model in each supported agent host **before** starting enrichment.

> **Model selection rule** (from [`skills/archigraph-graph-enrich/SKILL.md`](../skills/archigraph-graph-enrich/SKILL.md)):
> Haiku for `high`, `medium`, and `low` criticality bands (the vast majority
> of a corpus). Sonnet only for the small `critical` tier (score ≥ 80).
> The skill enforces this automatically — but only when the host allows
> per-call model overrides (see the comparison table below).

---

## Host comparison table

| Host | Can set model? | Supports MCP? | Per-call override? | Notes |
|------|---------------|--------------|-------------------|-------|
| [Claude Code](#claude-code) | Yes — CLI flag, slash command, or `settings.json` | Yes (native) | Yes — skill drives model selection per batch | Full support; recommended |
| [Cursor](#cursor) | Yes — model picker per session | Yes (via MCP JSON config) | No — one model for the whole session | Switch to Haiku before invoking `/archigraph-graph-enrich` |
| [Windsurf](#windsurf-codeium) | Yes — Cascade model picker | Yes (via MCP JSON config) | No — one model for the whole session | Switch to Haiku before invoking `/archigraph-graph-enrich` |
| [Continue](#continue) | Yes — `config.json` or inline picker | Yes (via MCP JSON config) | No | Set `defaultModel` to Haiku in config |
| [Aider](#aider) | Yes — `--model` CLI flag or `.aider.conf.yml` | No (no MCP client) | No | Run enrichment outside Aider; use Claude Code instead |
| [Cline](#cline) | Yes — model picker in VS Code sidebar | Yes (via MCP JSON config) | No — one model per task | Switch to Haiku before starting the task |

---

## Claude Code

Enrichment runs inside Claude Code and the `/archigraph-graph-enrich` skill drives model
selection automatically (Haiku for non-critical batches, Sonnet for the
critical tier). You can still lock the session to Haiku to prevent accidental
Sonnet fallback.

### Set model at session start (recommended)

```sh
claude --model claude-3-haiku-20240307
```

Then invoke:

```
/archigraph-graph-enrich
```

The skill's enrichment will use Haiku for all non-critical batches and will
prompt you before switching to Sonnet for the critical tier.

### Switch model mid-session

In the Claude Code chat:

```
/model claude-3-haiku-20240307
```

### Per-project default

Add to `.claude/settings.json` in your repo (or `~/.claude/settings.json`
for a machine-wide default):

```json
{
  "model": "claude-3-haiku-20240307"
}
```

The project-level file takes precedence over the machine-wide file.

### Confirm the active model

The model name appears in the Claude Code status bar and in the `/model`
command output. You can also check at any time:

```
/model
```

Expected output: `Current model: claude-3-haiku-20240307`

### Recommended workflow for enrichment

1. `claude --model claude-3-haiku-20240307` — start the session locked to Haiku.
2. Run `archigraph status <group>` to confirm the daemon is up and MCP is connected.
3. Invoke `/archigraph-graph-enrich` — the skill fetches the pending enrichment
   queue, then prints a cost estimate and asks for confirmation before
   dispatching batches. The non-critical batches go to Haiku; the skill will ask
   you to confirm the model switch to Sonnet for the critical tier before sending
   those batches.
4. After enrichment completes, run `/model claude-3-5-sonnet-20241022` (or whichever
   model you prefer for interactive coding) to restore your normal session model.

### Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| Enrichment cost far higher than expected | Session model was Sonnet or Opus | Verify with `/model`; restart with `--model claude-3-haiku-20240307` and re-run |
| `/model` command not found | Claude Code version too old | Upgrade: `npm i -g @anthropic-ai/claude-code@latest` |
| Skill ignores `/model` change mid-run | Session model is advisory; the skill's per-batch override still applies | No action needed — the skill manages model selection per batch |
| `settings.json` model ignored | Project file path wrong | Must be `.claude/settings.json` relative to the repo root you opened |

---

## Cursor

Cursor selects the model per chat session. It does not support mid-run model
switching inside a single task.

### Set model before starting enrichment

1. Open the AI panel: `Cmd+L` (macOS) / `Ctrl+L` (Linux/Windows).
2. Click the **model selector** at the top of the panel.
3. Choose **claude-3-haiku-20240307** (or the display name "Claude 3 Haiku").

### Confirm the active model

The model name is shown in the panel header while a request is in flight.
There is no CLI command to query it.

### Recommended workflow for enrichment

Because Cursor does not allow mid-run model switching, all batches — including
the critical tier — will use the model active when you invoke `/archigraph-graph-enrich`.
Choose one of:

- **Option A (preferred):** use Claude Code for enrichment (supports per-batch
  model selection).
- **Option B:** set Haiku in Cursor, accept that the critical tier also runs
  Haiku, and re-run critical-tier entities in a Claude Code session afterward
  if deeper analysis is needed.

### Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| "Claude 3 Haiku" not in model list | Anthropic API key not set in Cursor settings | Add key under **Cursor → Settings → Models → Anthropic** |
| Model resets after closing the panel | Expected — Cursor does not persist per-chat model | Re-select before each enrichment run |

---

## Windsurf (Codeium)

Windsurf uses Cascade for AI interactions. Model selection is per-session
and does not persist across sessions.

### Set model before starting enrichment

1. Open Cascade: `Cmd+L` (macOS) / `Ctrl+L` (Linux/Windows).
2. Click the **model picker** (usually a small label near the top-right of
   the Cascade panel).
3. Select **Claude 3 Haiku** (maps to `claude-3-haiku-20240307`).

### Confirm the active model

The model name is shown in the Cascade panel header.

### Recommended workflow for enrichment

Same constraint as Cursor — no per-batch model switching. Prefer Claude Code
for enrichment if your corpus has a significant critical tier. If you must use
Windsurf, set Haiku before invoking the skill and accept that the critical
tier runs at Haiku quality.

### Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| "Claude 3 Haiku" missing from picker | Codeium account plan does not include Haiku | Check your Codeium plan; Claude models require Codeium Pro or API key mode |
| Cascade panel not opening | Windsurf extension needs restart | `Cmd+Shift+P` → "Reload Window" |

---

## Continue

Continue (VS Code / JetBrains extension) reads its model config from
`~/.continue/config.json`.

### Set Haiku as default model

Edit `~/.continue/config.json`:

```json
{
  "models": [
    {
      "title": "Claude 3 Haiku",
      "provider": "anthropic",
      "model": "claude-3-haiku-20240307",
      "apiKey": "<your-anthropic-api-key>"
    }
  ],
  "defaultModel": "Claude 3 Haiku"
}
```

Reload the Continue extension after saving (`Cmd+Shift+P` → "Continue: Reload").

### Switch model inline

Click the model label at the bottom of the Continue chat panel and choose
**Claude 3 Haiku** from the dropdown.

### Confirm the active model

The current model is shown in the Continue chat panel footer.

### Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| Model not listed | API key missing or wrong | Verify `apiKey` in `config.json`; check for trailing spaces |
| `defaultModel` ignored | Title mismatch | `defaultModel` must exactly match the `title` field in `models` |

---

## Aider

Aider is a terminal-based AI coding tool. It does not have an MCP client, so
it cannot call `archigraph_*` MCP tools directly. **enrichment cannot run inside
Aider.** Use Claude Code for enrichment.

If you use Aider for your normal coding sessions but want to run enrichment,
the recommended workflow is:

1. Finish your Aider session and commit your work.
2. Open Claude Code in the same directory.
3. Run enrichment inside Claude Code as described in the [Claude Code](#claude-code) section.
4. Return to Aider after enrichment is complete.

### Setting the model in Aider (for reference)

If you do use Aider for any Claude work:

```sh
aider --model claude-3-haiku-20240307
```

Or add to `.aider.conf.yml`:

```yaml
model: claude-3-haiku-20240307
```

### Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `archigraph_*` tools not found in Aider | Aider has no MCP client | Use Claude Code for enrichment |
| Aider rejects the model name | Aider version too old | `pip install --upgrade aider-chat` |

---

## Cline

Cline is a VS Code extension with MCP client support. Model selection is
per-task (set before starting the task).

### Set model before starting enrichment

1. Open the Cline sidebar in VS Code.
2. Click the **model selector** (gear icon or model name label near the top).
3. Choose **claude-3-haiku-20240307**.

### Wire up MCP (required for `archigraph_*` tools)

Cline reads MCP server config from its VS Code extension settings.
`archigraph install <group>` writes the server entry to `~/.claude/claude.json`,
but Cline uses its own config file. Copy the server entry:

```sh
# After archigraph install, inspect the generated entry:
cat ~/.claude/claude.json | grep -A 10 '"archigraph"'
```

Then add the equivalent entry to the Cline MCP config (VS Code settings →
**Cline → MCP Servers**):

```json
{
  "archigraph": {
    "command": "archigraph",
    "args": ["mcp"],
    "type": "stdio"
  }
}
```

### Confirm the active model

The model is shown in the Cline task panel header before each task run.

### Recommended workflow for enrichment

Set Haiku before clicking "Start Task". The same no-per-batch-switching
constraint applies as for Cursor and Windsurf — prefer Claude Code for full
tier-aware enrichment.

### Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `archigraph_*` tools not available | MCP entry not in Cline's config | Add the server entry as shown above |
| Model selector not showing Haiku | Anthropic API key not configured in Cline | VS Code settings → **Cline → API Provider** → set Anthropic key |
| Task spins with no progress | MCP server not started | Run `archigraph start` and verify `archigraph status` shows "running" |

---

## Recommended minimal setup

If you are onboarding to archigraph enrichment for the first time, this is
the fastest path to a working, cost-safe enrichment environment:

```sh
# 1. Install archigraph
curl -fsSL https://raw.githubusercontent.com/cajasmota/archigraph/main/install.sh | bash

# 2. Register your repos and start the daemon
archigraph wizard          # creates group config
archigraph install <group> # starts daemon, wires MCP, writes ~/.claude/claude.json

# 3. Confirm MCP is connected
archigraph status <group>  # should show "MCP: connected"

# 4. Open Claude Code locked to Haiku
claude --model claude-3-haiku-20240307

# 5. Run the enrichment pass
/archigraph-graph-enrich
```

Total setup time: ~5 minutes. Enrichment will then run at Haiku rates for most
entities, with a cost-estimate confirmation gate before any Sonnet batches.

---

## Related

- [`skills/archigraph-graph-enrich/SKILL.md`](../skills/archigraph-graph-enrich/SKILL.md) — full enrichment procedure, model selection table, batching rules, and resume semantics.
- [`docs/settings.md`](settings.md) — archigraph daemon settings reference.
- [MCP Activity surface (`/mcp-activity`)](http://127.0.0.1:47274/mcp-activity) — live view of MCP tool calls; useful to confirm the daemon is receiving archigraph calls from your agent host.
