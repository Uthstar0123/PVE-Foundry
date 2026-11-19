#!/usr/bin/env bash
# Install the official bpg/proxmox 0.123.0 binary into a user filesystem mirror.
# Run as orchestrator on the Runner VM. No compilation and no sudo required.

set -Eeuo pipefail

readonly VERSION="0.123.0"
readonly ARCHIVE="terraform-provider-proxmox_${VERSION}_linux_amd64.zip"
readonly SUMS="terraform-provider-proxmox_${VERSION}_SHA256SUMS"
readonly RELEASE_URL="https://github.com/bpg/terraform-provider-proxmox/releases/download/v${VERSION}"
readonly MIRROR_ROOT="/home/orchestrator/.local/share/opentofu/providers"
readonly PLUGIN_DIR="${MIRROR_ROOT}/registry.opentofu.org/bpg/proxmox/${VERSION}/linux_amd64"
readonly BINARY="terraform-provider-proxmox_v${VERSION}"

fail() {
    printf '\nERROR: %s\n' "$*" >&2
    exit 1
}

for command_name in curl sha256sum python3; do
    command -v "$command_name" >/dev/null 2>&1 || fail "Required command not found: $command_name"
done

WORK_DIR="$(mktemp -d /tmp/proxmox-provider.XXXXXX)"
trap 'rm -rf -- "$WORK_DIR"' EXIT

printf 'Downloading official bpg/proxmox %s release\n' "$VERSION"
curl -fsSL "$RELEASE_URL/$ARCHIVE" -o "$WORK_DIR/$ARCHIVE"
curl -fsSL "$RELEASE_URL/$SUMS" -o "$WORK_DIR/$SUMS"

(
    cd "$WORK_DIR"
    grep -F " $ARCHIVE" "$SUMS" | sha256sum --check --status
) || fail "Provider archive checksum verification failed."

mkdir -p -- "$PLUGIN_DIR"
python3 - "$WORK_DIR/$ARCHIVE" "$PLUGIN_DIR" "$BINARY" <<'PY'
import sys
import zipfile

archive, destination, binary = sys.argv[1:]
with zipfile.ZipFile(archive) as package:
    if binary not in package.namelist():
        raise SystemExit(f"Expected binary {binary} not found in {archive}")
    package.extract(binary, destination)
PY

chmod 0755 "$PLUGIN_DIR/$BINARY"
printf 'Installed provider: %s\n' "$PLUGIN_DIR/$BINARY"
sha256sum "$PLUGIN_DIR/$BINARY"
