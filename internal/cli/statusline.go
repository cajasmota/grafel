package cli

import (
	"github.com/spf13/cobra"
)

// newStatuslineCmd returns the cobra command for `grafel statusline`.
//
// This command INSTALLS NOTHING and edits no files — it only prints an
// explainer + copy-paste bash snippets to stdout so a user can wire
// grafel's live status-plane (internal/statusfile) into their own shell
// prompt or editor statusline by hand. See internal/statusfile.go doc
// comment for the on-disk schema this command documents.
func newStatuslineCmd() *cobra.Command {
	var snippetOnly bool
	cmd := &cobra.Command{
		Use:   "statusline [--snippet]",
		Short: "Explain how to surface grafel's live status in a shell/editor statusline (prints only; installs nothing)",
		Long: `Print a guide to grafel's live per-repo status-plane file and ready-to-copy
example statusline segments.

This command does not install or edit anything — it only prints. Copy
whichever example segment fits your setup into your own statusline script
or config.

--snippet prints ONLY the icon-based bash segment (state precedence: engine
down > indexing > reindex required > error > idle > not indexed), suitable
for piping straight into a file.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if snippetOnly {
				_, err := cmd.OutOrStdout().Write([]byte(statuslineIconSnippet()))
				return err
			}
			_, err := cmd.OutOrStdout().Write([]byte(statuslineGuideText()))
			return err
		},
	}
	cmd.Flags().BoolVar(&snippetOnly, "snippet", false,
		"print only the icon-based bash segment (no prose)")
	return cmd
}

// statuslineMinimalSnippet is example 3a: the smallest possible segment —
// just "grafel <freshness>". Self-contained: resolves the repo root, hashes
// it, reads the status file, and prints nothing if the repo isn't indexed.
const statuslineMinimalSnippet = `# grafel statusline segment — minimal ("grafel 3m ago")
g_root=$(git rev-parse --show-toplevel 2>/dev/null)
[ -z "$g_root" ] && g_root=$(pwd)
g_hash=$(printf '%s' "$g_root" | shasum -a 256 | cut -c1-16)
g_sfile="${GRAFEL_HOME:-$HOME/.grafel}/status/$g_hash.json"
[ -f "$g_sfile" ] || exit 0
g_json=$(cat "$g_sfile")
g_mtime_ns=$(printf '%s' "$g_json" | jq -r '.graph_fb_mtime // 0')
if [ "$g_mtime_ns" = "0" ]; then
  echo "grafel not indexed"
  exit 0
fi
g_mtime=$((g_mtime_ns / 1000000000))
g_now=$(date +%s)
g_age=$((g_now - g_mtime))
if [ "$g_age" -lt 60 ]; then
  echo "grafel just now"
elif [ "$g_age" -lt 3600 ]; then
  echo "grafel $((g_age / 60))m ago"
elif [ "$g_age" -lt 86400 ]; then
  echo "grafel $((g_age / 3600))h ago"
else
  echo "grafel $((g_age / 86400))d ago"
fi
`

// statuslineIconSnippet is example 3b: the canonical icon-state segment.
//
// State precedence: engine down (heartbeat >15s stale) > indexing >
// reindex_required > last_err set > idle (graph_fb_mtime>0, "X ago") >
// not indexed (graph_fb_mtime == 0).
//
// reindex_required sits between indexing and last_err (#reindex-required
// PR1) — this is the fix for the "silent-green lie": before this field
// existed, a repo whose on-disk graph.fb was an incompatible old format
// version rendered as a plain green "✓ 3m ago" (graph_fb_mtime was still
// stamped from the last successful index at the OLD format), even though
// grafel can no longer actually read that graph. Placement rationale:
//   - BELOW indexing: an index currently in flight (possibly the user's own
//     remediation, e.g. they just ran `grafel index`) is more temporally
//     relevant right now than a static flag that is about to self-clear the
//     moment that very index completes.
//   - ABOVE last_err: reindex_required is recomputed FRESH from the actual
//     graph.fb bytes on every ~5s heartbeat (internal/daemon/statuswriter.go,
//     writeRepoStatusFile -> graph.ReindexRequiredReason), so it is always
//     the current, authoritative truth about whether THIS repo's graph is
//     servable. last_err is a snapshot of the most recently COMPLETED index
//     attempt — potentially stale (e.g. a transient failure hours ago that a
//     later successful index superseded, if last_err is ever not cleared on
//     success) — so a live, freshly-recomputed signal outranks a snapshot.
//
// Prints nothing if this isn't a grafel-indexed repo (no status file).
// Self-contained: no external color/helper dependency, works when piped or
// run non-interactively.
func statuslineIconSnippet() string {
	return `#!/usr/bin/env bash
# grafel statusline segment — icon states
# Precedence: engine down > indexing > reindex required > error > idle (age) > not indexed.
# Prints nothing if this isn't a grafel-indexed repo.
c_red=$'\033[31m'; c_yellow=$'\033[33m'; c_green=$'\033[32m'; c_dim=$'\033[2m'; c_reset=$'\033[0m'

g_root=$(git rev-parse --show-toplevel 2>/dev/null)
[ -z "$g_root" ] && g_root=$(pwd)
g_hash=$(printf '%s' "$g_root" | shasum -a 256 | cut -c1-16)
g_sfile="${GRAFEL_HOME:-$HOME/.grafel}/status/$g_hash.json"
[ -f "$g_sfile" ] || exit 0

g_json=$(cat "$g_sfile")
indexing=$(printf '%s' "$g_json" | jq -r '.indexing // false')
reindex_required=$(printf '%s' "$g_json" | jq -r '.reindex_required // false')
err=$(printf '%s' "$g_json" | jq -r '.last_err // empty')
hb=$(printf '%s' "$g_json" | jq -r '.heartbeat_at // empty')
mtime_ns=$(printf '%s' "$g_json" | jq -r '.graph_fb_mtime // 0')
now=$(date +%s)

down=0
if [ -n "$hb" ]; then
  # BSD/macOS date parsing. On GNU/Linux, replace this line with:
  #   hbep=$(date -d "$hb" +%s 2>/dev/null)
  hbep=$(date -j -u -f "%Y-%m-%dT%H:%M:%S" "${hb%%.*}" +%s 2>/dev/null)
  # 15s = grafel's EngineHeartbeatStaleAfter (3 × 5s heartbeat); matches 'grafel doctor'
  [ -n "$hbep" ] && [ $((now - hbep)) -gt 15 ] && down=1
fi

if [ "$down" = "1" ]; then
  echo "${c_red}⚠ down${c_reset}"
elif [ "$indexing" = "true" ]; then
  echo "${c_yellow}⟳ indexing${c_reset}"
elif [ "$reindex_required" = "true" ]; then
  echo "${c_yellow}⟲ reindex required${c_reset}"
elif [ -n "$err" ]; then
  echo "${c_red}✗ error${c_reset}"
elif [ "$mtime_ns" != "0" ] && [ -n "$mtime_ns" ]; then
  mtime=$((mtime_ns / 1000000000))
  age=$((now - mtime))
  if [ "$age" -lt 60 ]; then
    ago="just now"
  elif [ "$age" -lt 3600 ]; then
    ago="$((age / 60))m ago"
  elif [ "$age" -lt 86400 ]; then
    ago="$((age / 3600))h ago"
  else
    ago="$((age / 86400))d ago"
  fi
  echo "${c_green}✓ ${ago}${c_reset}"
else
  echo "${c_dim}○ not indexed${c_reset}"
fi
`
}

// statuslineDetailedSnippet is example 3c: adds the indexed commit (and,
// with a caveat, entity count) to the freshness line.
const statuslineDetailedSnippet = `# grafel statusline segment — detailed ("grafel ✓ 3m ago @ a1b2c3d")
g_root=$(git rev-parse --show-toplevel 2>/dev/null)
[ -z "$g_root" ] && g_root=$(pwd)
g_hash=$(printf '%s' "$g_root" | shasum -a 256 | cut -c1-16)
g_sfile="${GRAFEL_HOME:-$HOME/.grafel}/status/$g_hash.json"
[ -f "$g_sfile" ] || exit 0
g_json=$(cat "$g_sfile")
g_mtime_ns=$(printf '%s' "$g_json" | jq -r '.graph_fb_mtime // 0')
g_commit=$(printf '%s' "$g_json" | jq -r '.indexed_commit // empty')
g_entities=$(printf '%s' "$g_json" | jq -r '.entities // 0')
if [ "$g_mtime_ns" = "0" ]; then
  echo "grafel ○ not indexed"
  exit 0
fi
g_mtime=$((g_mtime_ns / 1000000000))
g_now=$(date +%s)
g_age=$((g_now - g_mtime))
if [ "$g_age" -lt 60 ]; then g_ago="just now"
elif [ "$g_age" -lt 3600 ]; then g_ago="$((g_age / 60))m ago"
elif [ "$g_age" -lt 86400 ]; then g_ago="$((g_age / 3600))h ago"
else g_ago="$((g_age / 86400))d ago"
fi
g_short=${g_commit:0:7}
# NOTE: entities can be 0 for repos indexed via the wizard/rebuild path (a
# known separate limitation — the graph-stats sidecar isn't written there),
# so don't rely on it as a freshness signal; graph_fb_mtime already is one.
if [ -n "$g_short" ]; then
  echo "grafel ✓ ${g_ago} @ ${g_short}"
else
  echo "grafel ✓ ${g_ago}"
fi
`

// statuslineGuideText assembles the full printed guide.
func statuslineGuideText() string {
	return `grafel statusline — surface live index status in your shell/editor

This command installs and edits NOTHING. It only prints. Copy whatever
fits your setup into your own statusline script or config.

1. What's available
--------------------
grafel's daemon writes a small per-repo JSON status file as it indexes, so
a statusline can read live index state cheaply (no daemon RPC needed):

  File:  ${GRAFEL_HOME:-$HOME/.grafel}/status/<sha256(abs_repo_root)[:16]>.json

The filename is the first 16 hex characters of sha256(absolute repo root
path) — a statusline resolves the repo root (git rev-parse --show-toplevel),
hashes it the same way, and reads that file directly.

Key fields:
  engine_pid       int      pid of the daemon that wrote this file
  heartbeat_at     RFC3339  rewritten every ~5s, even when idle — older
                            than 15s (3 missed = grafel's
                            EngineHeartbeatStaleAfter, same threshold
                            'grafel doctor' uses) means the engine is DOWN
  version          string   engine's self-reported version
  repo_path        string   absolute path this file describes
  indexed_ref      string   git ref the on-disk graph reflects
  indexed_commit   string   commit SHA the on-disk graph reflects
  entities         int64    NOTE: 0 for repos indexed via the wizard/rebuild
                            path (a known limitation) — don't rely on this
                            for freshness
  relationships    int64    same caveat as entities
  graph_fb_mtime   int64    nanoseconds since epoch the graph was last
                            written — THE reliable freshness signal
                            ("indexed X ago"); divide by 1e9 for unix secs
  indexing         bool     true while an index is running right now
  queue_len        int      jobs queued behind this repo (omitempty)
  last_err         string   most recent index error, if any (omitempty)
  reindex_required bool     true when the on-disk graph.fb this repo is
                            SERVING was written by an older grafel build
                            than this engine's format version supports
                            (omitempty). Recomputed fresh from the actual
                            graph.fb bytes on every heartbeat — this is
                            what a repo looks like when it needs a
                            ` + "`grafel index <repo>`" + ` re-run before grafel can
                            trust what it's serving. Detection-only in
                            this release: nothing auto-reindexes or
                            prompts you yet.
  reindex_reason   string   human-readable explanation set whenever
                            reindex_required is true, naming both the
                            found and required graph.fb format versions
                            (omitempty)

2. Two ways to read it
-----------------------
  a) Direct file read (recommended for statuslines): a few milliseconds,
     no process spawn. Resolve the repo root, hash it, read the JSON with
     jq. See the example segments below.
  b) grafel status --json: poll-safe, cwd-scoped, doesn't dial the daemon,
     but spawns the grafel binary (~200ms) — fine for scripts, on the slow
     side for a per-render statusline.

3. Example segments (copy any)
-------------------------------

--- 3a. Minimal ("grafel 3m ago") ---
` + statuslineMinimalSnippet + `
--- 3b. Icon states (recommended) ---
` + statuslineIconSnippet() + `
--- 3c. Detailed (with commit) ---
` + statuslineDetailedSnippet + `
4. Wiring it in
----------------
This command does not wire anything automatically — pick whichever applies:

  Claude Code:
    - If you already have ~/.claude/statusline.sh, paste one of the
      segments above into it (or call it from there).
    - Otherwise, set statusLine.command in ~/.claude/settings.json, e.g.:
        "statusLine": {"type": "command", "command": "~/.grafel/statusline-segment.sh"}
      pointing at a script containing one of the segments above.

  Generic shell prompt / editor statusline:
    - Any prompt/statusline that can run a shell command and capture its
      stdout can call grafel status --json, or read the status file
      directly as shown above.

  Shortcut:
    grafel statusline --snippet
      prints ONLY the icon-based segment (3b) so you can pipe or copy it
      straight into a file, e.g.:
        grafel statusline --snippet > ~/.grafel/statusline-segment.sh
`
}
