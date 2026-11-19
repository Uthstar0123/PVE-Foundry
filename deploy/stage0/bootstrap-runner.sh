#!/usr/bin/env bash
# Install pinned OpenTofu and Ansible on the isolated Debian 13 Runner VM.
# Run as the orchestrator user; sudo is required.

set -Eeuo pipefail

readonly TOFU_VERSION="1.13.9"
readonly TOFU_ARCHIVE="tofu_${TOFU_VERSION}_linux_amd64.tar.gz"
readonly TOFU_SUMS="tofu_${TOFU_VERSION}_SHA256SUMS"
readonly RELEASE_URL="https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}"

log() {
    printf '\n[%s] %s\n' "$(date +'%H:%M:%S')" "$*"
}

fail() {
    printf '\nERROR: %s\n' "$*" >&2
    exit 1
}

command -v sudo >/dev/null 2>&1 || fail "sudo is unavailable."
sudo -n true || fail "Passwordless sudo is required for this setup."

log "Installing Ansible and support tools"
sudo -n apt-get update
sudo -n DEBIAN_FRONTEND=noninteractive apt-get install -y \
    ansible \
    ca-certificates \
    curl \
    git \
    jq \
    tar

log "Downloading OpenTofu $TOFU_VERSION and its official SHA-256 list"
WORK_DIR="$(mktemp -d /tmp/orchestrator-tofu.XXXXXX)"
trap 'rm -rf -- "$WORK_DIR"' EXIT

curl -fsSL "$RELEASE_URL/$TOFU_ARCHIVE" -o "$WORK_DIR/$TOFU_ARCHIVE"
curl -fsSL "$RELEASE_URL/$TOFU_SUMS" -o "$WORK_DIR/$TOFU_SUMS"

(
    cd "$WORK_DIR"
    grep -F " $TOFU_ARCHIVE" "$TOFU_SUMS" | sha256sum --check --status
) || fail "OpenTofu archive checksum verification failed."

log "Installing OpenTofu into /usr/local/bin"
tar -xzf "$WORK_DIR/$TOFU_ARCHIVE" -C "$WORK_DIR" tofu
sudo -n install -m 0755 "$WORK_DIR/tofu" /usr/local/bin/tofu

log "Installed versions"
tofu version
ansible --version
git --version

log "Runner tool installation completed"
