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

# restart_daemon restarts an already-registered grafel daemon so it runs the
# freshly-installed binary. It is best-effort: a missing tool or a failed
# restart prints a hint and returns non-zero, but never aborts the installer.
# Echoes "restarted" on stdout when a registered daemon was restarted.
restart_daemon() {
  os=$1
  hint="re-run 'grafel install' or restart the daemon to finish the update"

  case "$os" in
    macos)
      command -v launchctl >/dev/null 2>&1 || return 1
      uid=$(id -u)
      target="gui/${uid}/com.grafel.daemon"
      # Detect a registered launchd agent (print, falling back to list).
      if launchctl print "$target" >/dev/null 2>&1 ||
         launchctl list 2>/dev/null | grep -q "com.grafel.daemon"; then
        if launchctl kickstart -k "$target" >/dev/null 2>&1; then
          echo "restarted"
          return 0
        fi
        info "warning: failed to restart the grafel daemon; $hint" >&2
        return 1
      fi
      return 1
      ;;
    linux)
      command -v systemctl >/dev/null 2>&1 || return 1
      # Match the user unit grafel install registers (grafel-daemon.service).
      unit=$(systemctl --user list-unit-files 2>/dev/null \
        | awk '/grafel/ {print $1; exit}')
      [ -n "$unit" ] || return 1
      if systemctl --user restart "$unit" >/dev/null 2>&1; then
        echo "restarted"
        return 0
      fi
      info "warning: failed to restart the grafel daemon; $hint" >&2
      return 1
      ;;
    *)
      return 1
      ;;
  esac
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
  # binary. Best-effort: restart_daemon never aborts the installer.
  daemon_restarted=$(restart_daemon "$os" || true)

  info ""
  if [ "$daemon_restarted" = "restarted" ]; then
    info "grafel updated and daemon restarted."
  else
    info "grafel installed. Run \"grafel wizard\" to set up your first group."
  fi
  info "(restart your shell or 'source' your rc file so PATH picks up $BIN_DIR)"
}

main "$@"
