#!/usr/bin/env bash
# archigraph one-line installer for macOS and Linux.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/cajasmota/archigraph/main/install.sh | bash
#
# Environment variables:
#   ARCHIGRAPH_VERSION   Release tag to install (default: latest, e.g. v0.1.0)
#   ARCHIGRAPH_FORCE     If set to 1, overwrite an existing install without warning.
#   ARCHIGRAPH_PREFIX    Install prefix (default: $HOME/.archigraph)

set -eu

REPO="cajasmota/archigraph"
PREFIX="${ARCHIGRAPH_PREFIX:-$HOME/.archigraph}"
BIN_DIR="$PREFIX/bin"
TMP_DIR="${TMPDIR:-/tmp}/archigraph-install.$$"

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
  v="${ARCHIGRAPH_VERSION:-latest}"
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
  marker_start="# archigraph PATH (auto-installed)"
  marker_end="# end archigraph"

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
  marker_start="# archigraph PATH (auto-installed)"
  marker_end="# end archigraph"

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

  archive_name="archigraph_${version_no_v}_${os}_${arch}.tar.gz"
  archive_url="https://github.com/${REPO}/releases/download/${version}/${archive_name}"
  checksums_url="https://github.com/${REPO}/releases/download/${version}/checksums.txt"

  info "archigraph installer"
  info "  version: $version"
  info "  target:  ${os}/${arch}"
  info "  prefix:  $PREFIX"

  if [ -x "$BIN_DIR/archigraph" ] && [ "${ARCHIGRAPH_FORCE:-0}" != "1" ]; then
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

  if [ ! -f "$TMP_DIR/archigraph" ]; then
    err "archive did not contain an 'archigraph' binary"
  fi

  install -m 0755 "$TMP_DIR/archigraph" "$BIN_DIR/archigraph" 2>/dev/null || {
    cp "$TMP_DIR/archigraph" "$BIN_DIR/archigraph"
    chmod +x "$BIN_DIR/archigraph"
  }

  configure_path

  info ""
  if "$BIN_DIR/archigraph" doctor >/dev/null 2>&1; then
    "$BIN_DIR/archigraph" doctor || true
  else
    "$BIN_DIR/archigraph" --version 2>/dev/null || true
  fi

  info ""
  info "archigraph installed. Run \"archigraph wizard\" to set up your first group."
  info "(restart your shell or 'source' your rc file so PATH picks up $BIN_DIR)"
}

main "$@"
