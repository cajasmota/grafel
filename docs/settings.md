# Settings file reference

archigraph persists user preferences in `~/.archigraph/settings.json`.
The file is created automatically on first write (via the Settings surface
or the `PUT /api/settings` endpoint). All fields are optional; missing keys
fall back to the defaults below.

## Schema

```json
{
  "theme": "light",
  "default_group": "",
  "auto_check_updates": true,
  "update_channel": "stable",
  "refresh_schedule": "",
  "telemetry_enabled": false,
  "daemon_rss_budget_mb": 512,
  "watcher_debounce_secs": 2,
  "indexer_parallelism": 4,
  "log_level": "info"
}
```

## Field reference

| Field | Type | Default | Allowed values | Notes |
|-------|------|---------|----------------|-------|
| `theme` | string | `"light"` | `"light"` \| `"dark"` \| `"auto"` | `"auto"` follows the OS dark-mode preference |
| `default_group` | string | `""` | Any registered group slug | Group shown on first dashboard load; empty = most recently used |
| `auto_check_updates` | bool | `true` | `true` \| `false` | Polls GitHub releases on daemon start |
| `update_channel` | string | `"stable"` | `"stable"` \| `"dev"` | `"dev"` includes release candidates |
| `refresh_schedule` | string | `""` | cron expression or `""` | Empty = manual-only refresh; e.g. `"0 3 * * *"` for 03:00 daily |
| `telemetry_enabled` | bool | `false` | `true` \| `false` | Opt-in anonymous usage metrics |
| `daemon_rss_budget_mb` | int | `512` | 100–2000 | Maximum RSS the daemon may use before it sheds loaded graphs. **Requires daemon restart.** |
| `watcher_debounce_secs` | int | `2` | 1–60 | How long to wait after a file change before triggering a re-index |
| `indexer_parallelism` | int | `4` | 1–32 | Number of parallel goroutines used during indexing. **Requires daemon restart.** |
| `log_level` | string | `"info"` | `"debug"` \| `"info"` \| `"warn"` \| `"error"` | Daemon log verbosity |

## API endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/settings` | Return current settings + defaults |
| `PUT` | `/api/settings` | Partial or full update; returns which fields require a restart |
| `POST` | `/api/settings/reset` | Restore factory defaults |

### `GET /api/settings` response

```json
{
  "settings": { ... },
  "defaults": { ... }
}
```

### `PUT /api/settings` response

The server merges the request body onto the current settings (partial PUT is
safe). Fields that require a daemon restart are listed in `restart_required`:

```json
{
  "settings": { ... },
  "defaults": { ... },
  "restart_required": ["daemon_rss_budget_mb"]
}
```

## Dashboard

The Settings surface at `/settings` in the dashboard provides a GUI for all
fields above and displays a "restart required" banner when applicable.
