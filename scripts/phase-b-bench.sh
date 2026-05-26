#!/usr/bin/env bash
# Phase B daemon benchmark — measures RSS at 3 lifecycle points:
#   1) idle daemon (no repos)
#   2) idle daemon with N repos registered
#   3) peak RSS during one reactive reindex
#   4) peak RSS during concurrent reindex of all repos
#
# Uses ARCHIGRAPH_DAEMON_ROOT + ARCHIGRAPH_HOME to point at a tempdir
# so the real ~/.archigraph state is never touched.
set -euo pipefail

BIN="${BIN:-./build/archigraph}"
ROOT="$(mktemp -d /tmp/archi-pb-XXXX)"
trap 'pkill -f "archigraph daemon" 2>/dev/null || true; rm -rf "$ROOT"' EXIT

export ARCHIGRAPH_DAEMON_ROOT="$ROOT/daemon"
export ARCHIGRAPH_HOME="$ROOT/home"
mkdir -p "$ARCHIGRAPH_HOME"

# Make 3 small repos from existing fixtures.
mkdir -p "$ROOT/repos"
cp -R testdata/fixtures/real-world/go "$ROOT/repos/repo-a"
cp -R testdata/fixtures/real-world/javascript "$ROOT/repos/repo-b"
cp -R testdata/fixtures/real-world/python "$ROOT/repos/repo-c"

REPO_A="$ROOT/repos/repo-a"
REPO_B="$ROOT/repos/repo-b"
REPO_C="$ROOT/repos/repo-c"

# Write registry + per-group config so daemonReposToWatch() picks them up.
mkdir -p "$ARCHIGRAPH_HOME/groups/bench"
cat > "$ARCHIGRAPH_HOME/groups/bench/group.json" <<EOF
{
  "name": "bench",
  "repos": [
    {"slug": "repo-a", "path": "$REPO_A"},
    {"slug": "repo-b", "path": "$REPO_B"},
    {"slug": "repo-c", "path": "$REPO_C"}
  ],
  "features": {"watchers": true, "git_hooks": false}
}
EOF
cat > "$ARCHIGRAPH_HOME/registry.json" <<EOF
{"version":1,"groups":[{"name":"bench","config_path":"$ARCHIGRAPH_HOME/groups/bench/group.json"}]}
EOF

echo "=== Starting daemon (root=$ROOT) ==="
"$BIN" daemon &
DAEMON_PID=$!
sleep 2

rss() {
  # ps reports RSS in KB on macOS/Linux.
  ps -o rss= -p "$DAEMON_PID" 2>/dev/null | tr -d ' ' || echo "0"
}

human() {
  local kb=$1
  awk -v k="$kb" 'BEGIN { printf "%.1f MB", k/1024 }'
}

echo "--- (1) idle daemon, 3 repos registered & watched, 5s settle ---"
sleep 5
RSS_IDLE=$(rss)
echo "idle RSS: $(human "$RSS_IDLE") (kb=$RSS_IDLE)"

echo "--- (2) trigger reindex on repo-a (touch one file) ---"
# Background sampler.
( for _ in $(seq 1 40); do rss; sleep 0.25; done ) > "$ROOT/peak-single.txt" &
SAMPLER=$!
echo "// bench tick $(date +%s)" >> "$REPO_A/main.go"
wait $SAMPLER
PEAK_SINGLE=$(sort -n "$ROOT/peak-single.txt" | tail -1)
echo "single-reindex peak RSS: $(human "$PEAK_SINGLE") (kb=$PEAK_SINGLE)"

echo "--- (3) concurrent: trigger reindex on all 3 repos ---"
sleep 8 # let scheduler settle
( for _ in $(seq 1 60); do rss; sleep 0.25; done ) > "$ROOT/peak-concurrent.txt" &
SAMPLER=$!
echo "// bench tick $(date +%s)" >> "$REPO_A/main.go"
echo "// bench tick $(date +%s)" >> "$REPO_B/index.js"
echo "# bench tick $(date +%s)" >> "$REPO_C/main.py"
wait $SAMPLER
PEAK_CONCURRENT=$(sort -n "$ROOT/peak-concurrent.txt" | tail -1)
echo "concurrent-reindex peak RSS: $(human "$PEAK_CONCURRENT") (kb=$PEAK_CONCURRENT)"

echo "--- daemon status snapshot ---"
"$BIN" status || true

echo "--- shutting down ---"
"$BIN" stop || kill "$DAEMON_PID" 2>/dev/null || true
wait "$DAEMON_PID" 2>/dev/null || true

echo
echo "================ Summary ================"
echo "Idle (3 repos watched):  $(human "$RSS_IDLE")"
echo "Single reindex peak:     $(human "$PEAK_SINGLE")"
echo "Concurrent reindex peak: $(human "$PEAK_CONCURRENT")"
echo "========================================="
