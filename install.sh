#!/usr/bin/env bash
# grafel one-line installer for macOS and Linux.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/cajasmota/grafel/main/install.sh | bash
#
# Environment variables:
#   GRAFEL_VERSION   Release tag to install (default: latest, e.g. v0.1.0)
#   GRAFEL_FORCE     If set to 1, overwrite an existing install without warning.
#   GRAFEL_PREFIX    Install prefix (default: $HOME/.grafel)

set -eu

REPO="cajasmota/grafel"
PREFIX="${GRAFEL_PREFIX:-$HOME/.grafel}"
BIN_DIR="$PREFIX/bin"
TMP_DIR="${TMPDIR:-/tmp}/grafel-install.$$"

err() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

info() {
  printf '%s\n' "$*"
}

cleanup() {
  rm -rf "$TMP_DIR" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

detect_os() {
  uname_s=$(uname -s)
  case "$uname_s" in
    Linux)  echo "linux" ;;
    Darwin) echo "macos" ;;
    *) err "unsupported OS: $uname_s (supported: Linux, Darwin)" ;;
  esac
}

detect_arch() {
  uname_m=$(uname -m)
  case "$uname_m" in
    x86_64|amd64) echo "x86_64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) err "unsupported architecture: $uname_m (supported: x86_64, arm64)" ;;
  esac
}

resolve_version() {
  v="${GRAFEL_VERSION:-latest}"
  if [ "$v" = "latest" ]; then
    # Follow the redirect from /releases/latest to /releases/tag/<version>
    redirect_url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
      "https://github.com/${REPO}/releases/latest" 2>/dev/null) || \
      err "failed to resolve latest release (network error)"
    v=$(printf '%s' "$redirect_url" | sed -E 's@.*/tag/([^/]+)$@\1@')
    if [ -z "$v" ] || [ "$v" = "$redirect_url" ]; then
      err "failed to parse latest release tag from $redirect_url"
    fi
  fi
  echo "$v"
}

download() {
  src=$1
  dst=$2
  if ! curl -fsSL --retry 3 -o "$dst" "$src"; then
    err "failed to download $src"
  fi
}

verify_checksum() {
  archive_path=$1
  archive_name=$2
  checksums_path=$3

  expected=$(grep -E "[[:space:]]\*?${archive_name}\$" "$checksums_path" | awk '{print $1}' | head -n1)
  if [ -z "$expected" ]; then
    err "checksum for $archive_name not found in checksums.txt"
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$archive_path" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$archive_path" | awk '{print $1}')
  else
    err "no sha256 tool found (need sha256sum or shasum)"
  fi

  if [ "$expected" != "$actual" ]; then
    err "checksum mismatch for $archive_name (expected $expected, got $actual)"
  fi
}

update_path_block() {
  rc_file=$1
  marker_start="# grafel PATH (auto-installed)"
  marker_end="# end grafel"

  [ -f "$rc_file" ] || touch "$rc_file"

  if grep -Fq "$marker_start" "$rc_file" 2>/dev/null; then
    return 0
  fi

  {
    printf '\n%s\n' "$marker_start"
    printf 'export PATH="%s:$PATH"\n' "$BIN_DIR"
    printf '%s\n' "$marker_end"
  } >> "$rc_file"
}

update_path_fish() {
  rc_file=$1
  marker_start="# grafel PATH (auto-installed)"
  marker_end="# end grafel"

  mkdir -p "$(dirname "$rc_file")"
  [ -f "$rc_file" ] || touch "$rc_file"

  if grep -Fq "$marker_start" "$rc_file" 2>/dev/null; then
    return 0
  fi

  {
    printf '\n%s\n' "$marker_start"
    printf 'set -gx PATH %s $PATH\n' "$BIN_DIR"
    printf '%s\n' "$marker_end"
  } >> "$rc_file"
}

# normalize_version strips a single leading 'v' so a v-prefixed version
# ("v0.1.9") compares equal to a bare one ("0.1.9"). This matters because
# `grafel status` prints the daemon version VERBATIM, WITH the leading 'v'
# (version.Version is baked as ${GITHUB_REF_NAME}, e.g. "v0.1.9"), whereas the
# installer's wanted version is the bare tag ($version_no_v). Without this the
# compare was always "v0.1.9" = "0.1.9" → false, so every correct update
# escalated needlessly and warned "still old version" forever (#5850).
normalize_version() {
  printf '%s' "${1#v}"
}

# parse_status_version extracts the daemon version token from `grafel status`
# output supplied on stdin (the "  version:" line). Echoes the raw token (with
# whatever prefix the daemon printed) or nothing if not present.
parse_status_version() {
  awk '/^  version:/ {print $2; exit}'
}

# status_version_matches reports (exit 0) whether the `grafel status` output in
# $1 names a running daemon version equivalent to the wanted version $2,
# tolerating the leading-'v' difference between the daemon's v-prefixed print
# and the installer's bare wanted version. Extracted as its own helper so it is
# unit-testable without a live daemon (see installsh_restart_verify_test.go).
status_version_matches() {
  _seen=$(printf '%s\n' "$1" | parse_status_version)
  [ -n "$_seen" ] && [ "$(normalize_version "$_seen")" = "$(normalize_version "$2")" ]
}

# wait_for_daemon_version polls the JUST-INSTALLED binary's `grafel status`
# output until the RUNNING daemon reports a version equivalent to $1 (the
# just-installed version) or ~10s elapse. `grafel status` dials the daemon's
# RPC socket directly — the same reliable, non-HTTP channel `grafel install`'s
# Go-side version probe uses (internal/install/copy.go's
# defaultDaemonVersionProbe) — rather than any dashboard HTTP route. The
# dashboard has no dedicated /healthz-style version endpoint: an unmatched GET
# falls through to the SPA catch-all and returns an HTML document instead of a
# version string (#5596), so an HTTP-based check here would risk the exact
# HTML-shadowing hazard this fix is guarding against. Echoes the LAST version
# observed (possibly empty/stale, normalized) on stdout; returns non-zero if it
# never matched $1 within budget.
wait_for_daemon_version() {
  want=$1
  seen=""
  status=""
  i=0
  while [ "$i" -lt 20 ]; do
    status=$("$BIN_DIR/grafel" status 2>/dev/null || true)
    seen=$(printf '%s\n' "$status" | parse_status_version)
    seen=$(normalize_version "$seen")
    if [ -n "$seen" ] && status_version_matches "$status" "$want"; then
      echo "$seen"
      return 0
    fi
    i=$((i + 1))
    sleep 0.5
  done
  echo "$seen"
  return 1
}

# restart_daemon restarts an already-registered grafel daemon so it runs the
# freshly-installed binary, then VERIFIES the running daemon actually reports
# the newly-installed version before declaring success (#5850: a stale
# daemon that stayed bound to the socket through a kickstart/restart was
# previously reported as a successful restart just because the OS-level
# restart command exited 0). If the first restart leaves a stale daemon
# running, it escalates to a HARD restart — macOS: `launchctl bootout`
# (forces the old process to fully exit; a bootout of an already-unloaded
# label is a harmless no-op) followed by bootstrap/kickstart; Linux:
# `systemctl --user stop` then `start` (a plain "restart" can race a unit
# that never actually reloaded the new binary) — and re-checks.
#
# It is best-effort: a missing tool, a failed restart, or a daemon that is
# STILL stale after escalation prints a warning/hint and returns non-zero,
# but never aborts the installer (no call to the fatal err() helper).
# Echoes "restarted" on stdout only when the running daemon was CONFIRMED to
# report the just-installed version; echoes "stale" when a daemon is running
# but never reached that version, so the caller can print an accurate final
# message.
restart_daemon() {
  os=$1
  want_version=$2
  hint="re-run 'grafel install' or restart the daemon to finish the update"

  case "$os" in
    macos)
      command -v launchctl >/dev/null 2>&1 || return 1
      uid=$(id -u)
      target="gui/${uid}/com.grafel.daemon"
      plist="$HOME/Library/LaunchAgents/com.grafel.daemon.plist"
      # Detect a registered launchd agent (print, falling back to list).
      if ! launchctl print "$target" >/dev/null 2>&1 &&
         ! launchctl list 2>/dev/null | grep -q "com.grafel.daemon"; then
        return 1
      fi
      if ! launchctl kickstart -k "$target" >/dev/null 2>&1; then
        info "warning: failed to restart the grafel daemon; $hint" >&2
        return 1
      fi
      ;;
    linux)
      command -v systemctl >/dev/null 2>&1 || return 1
      # Match the user unit grafel install registers (grafel-daemon.service).
      unit=$(systemctl --user list-unit-files 2>/dev/null \
        | awk '/grafel/ {print $1; exit}')
      [ -n "$unit" ] || return 1
      if ! systemctl --user restart "$unit" >/dev/null 2>&1; then
        info "warning: failed to restart the grafel daemon; $hint" >&2
        return 1
      fi
      ;;
    *)
      return 1
      ;;
  esac

  # The OS-level restart command exited 0 — that only proves SOME daemon is
  # now bound to the socket, not that it is the one we just installed.
  # Verify, and escalate to a hard restart if it's still stale.
  running=$(wait_for_daemon_version "$want_version") || {
    info "note: daemon restarted but still reports version '${running:-unknown}' (want '$want_version'); forcing a hard restart" >&2
    case "$os" in
      macos)
        # bootout forces the currently-loaded (possibly stale) daemon to
        # fully exit before we reload it, unlike kickstart alone.
        launchctl bootout "$target" >/dev/null 2>&1 || true
        if [ -f "$plist" ]; then
          launchctl bootstrap "gui/${uid}" "$plist" >/dev/null 2>&1 ||
            launchctl kickstart -k "$target" >/dev/null 2>&1 || true
        else
          launchctl kickstart -k "$target" >/dev/null 2>&1 || true
        fi
        ;;
      linux)
        systemctl --user stop "$unit" >/dev/null 2>&1 || true
        systemctl --user start "$unit" >/dev/null 2>&1 || true
        ;;
    esac
    running=$(wait_for_daemon_version "$want_version") || {
      info "warning: daemon still running version '${running:-unknown}' after a hard restart (want '$want_version'); $hint" >&2
      echo "stale"
      return 1
    }
  }
  echo "restarted"
  return 0
}

configure_path() {
  shell_name=""
  if [ -n "${SHELL:-}" ]; then
    shell_name=$(basename "$SHELL")
  fi
  if [ -z "$shell_name" ]; then
    shell_name=$(ps -p $$ -o comm= 2>/dev/null | sed 's@^-@@' | xargs basename 2>/dev/null || echo "")
  fi

  case "$shell_name" in
    zsh)  update_path_block "$HOME/.zshrc" ;;
    bash)
      if [ -f "$HOME/.bashrc" ]; then
        update_path_block "$HOME/.bashrc"
      else
        update_path_block "$HOME/.bash_profile"
      fi
      ;;
    fish) update_path_fish "$HOME/.config/fish/config.fish" ;;
    *)
      info "note: unknown shell '$shell_name'; add $BIN_DIR to your PATH manually."
      ;;
  esac
}

main() {
  os=$(detect_os)
  arch=$(detect_arch)
  version=$(resolve_version)
  version_no_v=${version#v}

  archive_name="grafel_${version_no_v}_${os}_${arch}.tar.gz"
  archive_url="https://github.com/${REPO}/releases/download/${version}/${archive_name}"
  checksums_url="https://github.com/${REPO}/releases/download/${version}/checksums.txt"

  info "grafel installer"
  info "  version: $version"
  info "  target:  ${os}/${arch}"
  info "  prefix:  $PREFIX"

  if [ -x "$BIN_DIR/grafel" ] && [ "${GRAFEL_FORCE:-0}" != "1" ]; then
    info "  upgrading existing install at $BIN_DIR"
  fi

  mkdir -p "$TMP_DIR" "$BIN_DIR"

  info "downloading $archive_url"
  download "$archive_url" "$TMP_DIR/$archive_name"

  info "downloading checksums.txt"
  download "$checksums_url" "$TMP_DIR/checksums.txt"

  info "verifying SHA256"
  verify_checksum "$TMP_DIR/$archive_name" "$archive_name" "$TMP_DIR/checksums.txt"

  info "extracting"
  tar -xzf "$TMP_DIR/$archive_name" -C "$TMP_DIR"

  if [ ! -f "$TMP_DIR/grafel" ]; then
    err "archive did not contain an 'grafel' binary"
  fi

  install -m 0755 "$TMP_DIR/grafel" "$BIN_DIR/grafel" 2>/dev/null || {
    cp "$TMP_DIR/grafel" "$BIN_DIR/grafel"
    chmod +x "$BIN_DIR/grafel"
  }

  configure_path

  info ""
  if "$BIN_DIR/grafel" doctor >/dev/null 2>&1; then
    "$BIN_DIR/grafel" doctor || true
  else
    "$BIN_DIR/grafel" --version 2>/dev/null || true
  fi

  # If a daemon is already registered, restart it so it picks up the new
  # binary, and CONFIRM the running daemon actually reports the
  # just-installed version before calling it done (#5850) — a daemon that
  # merely answers the socket is not proof it is the new binary. Best-effort:
  # restart_daemon never aborts the installer.
  daemon_restarted=$(restart_daemon "$os" "$version_no_v" || true)

  info ""
  case "$daemon_restarted" in
    restarted)
      info "grafel updated and daemon restarted (confirmed running $version_no_v)."
      ;;
    stale)
      info "grafel installed, but the daemon still running the old version — run 'grafel install' to finish."
      ;;
    *)
      info "grafel installed. Run \"grafel wizard\" to set up your first group."
      ;;
  esac
  info "(restart your shell or 'source' your rc file so PATH picks up $BIN_DIR)"
}

# Only auto-run when executed directly. Sourcing with GRAFEL_INSTALL_SH_LIB=1
# (e.g. from installsh_restart_verify_test.go) exposes the helper functions for
# unit testing WITHOUT running the full installer.
if [ "${GRAFEL_INSTALL_SH_LIB:-0}" != "1" ]; then
  main "$@"
fi
