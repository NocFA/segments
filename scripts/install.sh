#!/bin/bash
set -euo pipefail

REPO="NocFA/segments"
GITEA="https://git.nocfa.net"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

C='\033[0;36m'
G='\033[0;32m'
R='\033[0m'
B='\033[1m'
E='\033[0;31m'

info()  { printf "${C}%s${R}\n" "$1"; }
ok()    { printf "${G}%s${R}\n" "$1"; }
err()   { printf "${E}%s${R}\n" "$1" >&2; exit 1; }

detect_platform() {
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) err "Unsupported architecture: $ARCH" ;;
  esac
  case "$OS" in
    linux|darwin) ;;
    *) err "Unsupported OS: $OS" ;;
  esac
  echo "${OS}-${ARCH}"
}

latest_release() {
  curl -fsSL "${GITEA}/api/v1/repos/${REPO}/releases/latest" 2>/dev/null \
    | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['tag_name'])" 2>/dev/null
}

install_from_release() {
  local platform="$1"
  local version
  version=$(latest_release)
  if [ -z "$version" ]; then
    info "No release found, building from source..."
    return 1
  fi

  local url="${GITEA}/${REPO}/releases/download/${version}/segments-${platform}"
  info "Downloading segments ${version} for ${platform}..."
  if ! curl -fsSL "$url" -o /tmp/segments; then
    info "No binary for ${platform} in ${version}, building from source..."
    return 1
  fi
  chmod +x /tmp/segments
  mkdir -p "$INSTALL_DIR"
  mv /tmp/segments "${INSTALL_DIR}/segments"
  return 0
}

install_from_source() {
  command -v go >/dev/null 2>&1 || err "No release found for your platform and Go is not installed. See https://go.dev/dl"
  command -v git >/dev/null 2>&1 || err "git is required to build from source"

  info "Building from source..."
  local tmp
  tmp=$(mktemp -d)
  git clone --depth=1 "${GITEA}/${REPO}.git" "$tmp/segments" >/dev/null 2>&1
  cd "$tmp/segments"
  CGO_ENABLED=1 go build -o segments ./cmd/segments/
  mkdir -p "$INSTALL_DIR"
  mv segments "${INSTALL_DIR}/segments"
  cd - >/dev/null
  rm -rf "$tmp"
}

add_to_path() {
  if echo "$PATH" | grep -q "$INSTALL_DIR"; then
    return
  fi
  local rc="$HOME/.bashrc"
  [[ "${SHELL:-}" == */zsh ]] && rc="$HOME/.zshrc"
  echo "export PATH=\"\$HOME/.local/bin:\$PATH\"" >> "$rc"
  info "Added $INSTALL_DIR to PATH in $rc -- restart your shell or run: source $rc"
}

main() {
  local platform
  platform=$(detect_platform)

  if ! install_from_release "$platform"; then
    install_from_source
  fi

  local bin="${INSTALL_DIR}/segments"

  # macOS: clear quarantine attributes
  if [ "$(uname -s)" = "Darwin" ]; then
    xattr -c "$bin" 2>/dev/null || true
    codesign -fs - "$bin" 2>/dev/null || true
  fi

  ln -sf "$bin" "${INSTALL_DIR}/sg"
  add_to_path

  "$bin" init >/dev/null 2>&1 || true

  printf "\n${B}Segments installed.${R}\n\n"
  printf "  ${G}segments serve${R}   -- start the server\n"
  printf "  ${G}segments setup${R}   -- configure integrations\n"
  printf "  ${G}sg list${R}          -- list your projects\n\n"
}

main
