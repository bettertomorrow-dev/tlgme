#!/usr/bin/env bash

set -euo pipefail

repo="bettertomorrow-dev/tlgme"
api_url="https://api.github.com/repos/$repo/releases/latest"
preview=false
scenario=""
platform=""
arch=""
release_version=""

usage() {
  printf 'Usage: bash install.sh [--preview] [--scenario first|update] [--platform darwin|linux] [--arch amd64|arm64] [--version vX.Y.Z]\n'
}

while (($#)); do
  case "$1" in
    --preview) preview=true ;;
    --scenario)
      scenario="${2:-}"
      shift
      ;;
    --platform)
      platform="${2:-}"
      shift
      ;;
    --arch)
      arch="${2:-}"
      shift
      ;;
    --version)
      release_version="${2:-}"
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 1
      ;;
  esac
  shift
done

if ! "$preview" && [[ -n "$scenario$platform$arch$release_version" ]]; then
  printf '%s\n' 'Preview-only options require --preview.' >&2
  usage >&2
  exit 1
fi
if [[ -n "$release_version" && ! "$release_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  printf '%s\n' 'Preview version must use the vX.Y.Z format.' >&2
  exit 1
fi

if [[ -t 1 && "${TERM:-}" != "dumb" ]]; then
  cyan=$'\033[36m'
  white=$'\033[97m'
  gray=$'\033[90m'
  red=$'\033[31m'
  reset=$'\033[0m'
else
  cyan=""
  white=""
  gray=""
  red=""
  reset=""
fi

info() { printf '%s%s%s\n' "$gray" "$1" "$reset"; }
success() { printf '%s%s%s\n' "$white" "$1" "$reset"; }
failure() { printf '%s%s%s\n' "$red" "$1" "$reset" >&2; }

has_tty() {
  (: </dev/tty) 2>/dev/null
}

prompt() {
  local answer=""
  if has_tty && IFS= read -r answer </dev/tty; then
    :
  fi
  printf '%s' "$answer"
}

detect_platform() {
  if [[ -z "$platform" ]]; then
    case "$(uname -s)" in
      Darwin) platform="darwin" ;;
      Linux) platform="linux" ;;
      *) failure "Unsupported operating system: $(uname -s)"; exit 1 ;;
    esac
  fi
  case "$platform" in
    darwin|linux) ;;
    *) failure "Unsupported platform: $platform"; exit 1 ;;
  esac

  if [[ -z "$arch" ]]; then
    case "$(uname -m)" in
      x86_64|amd64) arch="amd64" ;;
      arm64|aarch64) arch="arm64" ;;
      *) failure "Unsupported architecture: $(uname -m)"; exit 1 ;;
    esac
  fi
  case "$arch" in
    amd64|arm64) ;;
    *) failure "Unsupported architecture: $arch"; exit 1 ;;
  esac
}

display_platform() {
  case "$platform" in
    darwin) printf 'macOS' ;;
    linux) printf 'Linux' ;;
  esac
}

print_title() {
  printf '%s>%s %sTlgMe%s %sinstaller for %s, %s%s\n\n' \
    "$cyan" "$reset" "$white" "$reset" "$gray" "$(display_platform)" "$arch" "$reset"
}

print_global_prompt() {
  printf '%sInstall globally for all users?%s %sOptional%s\n' "$white" "$reset" "$gray" "$reset"
  info 'Installs `tlgme` in /usr/local/bin, so every user and automation on this computer can run it.'
  info 'Useful on shared machines and headless servers where agents run under different accounts.'
  info 'Requires sudo permission.'
  printf '%s[y/N]:%s ' "$white" "$reset"
}

print_setup_prompt() {
  printf '%sRun TlgMe first-time setup now?%s %s[Y/n]:%s ' "$white" "$reset" "$gray" "$reset"
}

latest_version() {
  local metadata tag
  metadata="$(curl -fsSL "$api_url")"
  tag="$(printf '%s\n' "$metadata" | sed -n 's/^[[:space:]]*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
  if [[ ! "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    failure 'Could not determine the latest stable TlgMe release.'
    exit 1
  fi
  printf '%s' "$tag"
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    failure 'TlgMe installer needs sha256sum or shasum to verify the download.'
    exit 1
  fi
}

install_path_entry() {
  local profile marker path_line
  [[ ":$PATH:" == *":$HOME/.local/bin:"* ]] && return
  case "${SHELL:-}" in
    */zsh) profile="$HOME/.zshrc" ;;
    *) profile="$HOME/.bashrc" ;;
  esac
  marker='# Added by TlgMe installer'
  path_line='export PATH="$HOME/.local/bin:$PATH"'
  if [[ -f "$profile" ]] && grep -Fqx "$marker" "$profile"; then
    return
  fi
  printf '\n%s\n%s\n' "$marker" "$path_line" >>"$profile"
  export PATH="$HOME/.local/bin:$PATH"
  info "Adding $HOME/.local/bin to your PATH..."
}

verify_archive() {
  local archive="$1" checksums="$2" archive_name="$3" expected actual
  expected="$(awk -v name="$archive_name" '$2 == name || $2 == "*" name { print $1; exit }' "$checksums")"
  actual="$(sha256 "$archive")"
  if [[ -z "$expected" || "$expected" != "$actual" ]]; then
    failure 'Verification failed. The downloaded archive does not match the published SHA-256 checksum.'
    failure 'TlgMe was not installed or changed.'
    exit 1
  fi
}

run_preview() {
  local installed_version="$release_version" latest="$release_version"
  [[ -n "$scenario" ]] || scenario="first"
  case "$scenario" in
    first|update) ;;
    *) failure 'Preview scenario must be first or update.'; exit 1 ;;
  esac
  if [[ -z "$latest" ]]; then
    if [[ "$scenario" == "first" ]]; then latest="v0.1.3"; else latest="v0.1.4"; fi
  fi
  if [[ "$scenario" == "first" ]]; then
    print_global_prompt
    local global_answer
    global_answer="$(prompt)"
    printf '\n'
    if [[ "$global_answer" =~ ^[Yy]$ ]]; then
      success 'TlgMe will be installed to /usr/local/bin.'
      info 'Administrator permission is required.'
      printf '\n'
    fi
    info "Downloading TlgMe $latest..."
    info 'Verifying download...'
    info 'Installing TlgMe...'
    info "TlgMe $latest installed successfully."
    printf '\n'
    print_setup_prompt
    local setup_answer
    setup_answer="$(prompt)"
    printf '\n'
    if [[ ! "$setup_answer" =~ ^[Nn]$ ]]; then
      success 'Preview would now launch TlgMe setup.'
    else
      success 'Run `tlgme` whenever you are ready to finish setup.'
    fi
  else
    local current="v0.1.3"
    success "Existing installation found at $HOME/.local/bin/tlgme."
    success 'This will replace it with the latest release.'
    printf '\n'
    info "Downloading TlgMe $latest..."
    info 'Verifying download...'
    info 'Updating TlgMe...'
    success "TlgMe updated successfully: $current → $latest."
  fi
  printf '\n'
  info 'Preview complete. Nothing was downloaded or changed.'
}

main() {
  clear 2>/dev/null || true
  printf '\n\n'
  detect_platform
  print_title

  if "$preview"; then
    run_preview
    return
  fi

  local global_answer="" install_dir archive_name archive_url checksums_url temp_dir existing=false current_version="" latest
  print_global_prompt
  global_answer="$(prompt)"
  printf '\n'
  if [[ "$global_answer" =~ ^[Yy]$ ]]; then
    install_dir='/usr/local/bin'
    success 'TlgMe will be installed to /usr/local/bin.'
    info 'Administrator permission is required.'
    printf '\n'
  else
    install_dir="$HOME/.local/bin"
  fi
  [[ -e "$install_dir/tlgme" ]] && existing=true
  if "$existing"; then
    current_version="$($install_dir/tlgme --version 2>/dev/null | awk '{print $NF}' || true)"
    if [[ "$current_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      current_version="v$current_version"
    fi
    success "Existing installation found at $install_dir/tlgme."
    success 'This will replace it with the latest release.'
    printf '\n'
  fi

  latest="$(latest_version)"
  release_version="$latest"
  archive_name="tlgme_${release_version#v}_${platform}_${arch}.tar.gz"
  archive_url="https://github.com/$repo/releases/download/$release_version/$archive_name"
  checksums_url="https://github.com/$repo/releases/download/$release_version/checksums.txt"
  temp_dir="$(mktemp -d)"
  trap 'rm -rf "$temp_dir"' EXIT

  info "Downloading TlgMe $release_version..."
  curl -fsSL "$archive_url" -o "$temp_dir/$archive_name"
  curl -fsSL "$checksums_url" -o "$temp_dir/checksums.txt"
  info 'Verifying download...'
  verify_archive "$temp_dir/$archive_name" "$temp_dir/checksums.txt" "$archive_name"
  tar -xzf "$temp_dir/$archive_name" -C "$temp_dir"
  if [[ ! -f "$temp_dir/tlgme" ]]; then
    failure 'The release archive does not contain tlgme.'
    exit 1
  fi

  if "$existing"; then info 'Updating TlgMe...'; else info 'Installing TlgMe...'; fi
  if [[ "$install_dir" == '/usr/local/bin' ]]; then
    sudo mkdir -p "$install_dir"
    sudo install -m 755 "$temp_dir/tlgme" "$install_dir/tlgme"
  else
    mkdir -p "$install_dir"
    install -m 755 "$temp_dir/tlgme" "$install_dir/tlgme"
    install_path_entry
  fi

  if "$existing"; then
    success "TlgMe updated successfully: ${current_version:-the installed version} → $release_version."
    return
  fi
  info "TlgMe $release_version installed successfully."
  printf '\n'
  print_setup_prompt
  local setup_answer
  setup_answer="$(prompt)"
  printf '\n'
  if [[ ! "$setup_answer" =~ ^[Nn]$ ]]; then
    exec "$install_dir/tlgme"
  fi
  success 'Run `tlgme` whenever you are ready to finish setup.'
}

main
