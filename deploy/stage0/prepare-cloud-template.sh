#!/usr/bin/env bash
# One-time preparation of a Debian 13 Cloud-Init template on Proxmox VE 9.2.
# Run as root from the Proxmox web shell. Does not modify existing guests.

set -Eeuo pipefail

readonly VMID="1000"
readonly VM_NAME="foundry-debian13-cloud"
readonly NODE="pve"
readonly POOL="foundry-test"
readonly STORAGE="storage-disks"
readonly BRIDGE="vmbr0"
readonly IMAGE_NAME="debian-13-generic-amd64.qcow2"
readonly IMAGE_BASE_URL="https://cloud.debian.org/images/cloud/trixie/latest"
readonly SNIPPET_NAME="orchestrator-cloud-template-${VMID}.yaml"

log() {
    printf '\n[%s] %s\n' "$(date +'%H:%M:%S')" "$*"
}

fail() {
    printf '\nERROR: %s\n' "$*" >&2
    exit 1
}

[[ "$(id -u)" -eq 0 ]] || fail "Run this script as root from the Proxmox web shell."

for command_name in qm pvesh pvesm curl sha512sum awk perl; do
    command -v "$command_name" >/dev/null 2>&1 || fail "Required command not found: $command_name"
done

if qm status "$VMID" >/dev/null 2>&1; then
    fail "VMID $VMID already exists. It was not changed. Inspect it manually before continuing."
fi

pvesh get "/pools/$POOL" >/dev/null 2>&1 || fail "Pool $POOL does not exist."
pvesh get "/nodes/$NODE/status" >/dev/null 2>&1 || fail "Node $NODE is unavailable."

STORAGE_PATH="$(pvesh get "/storage/$STORAGE" --output-format json | perl -MJSON::PP -0777 -ne 'print decode_json($_)->{path} // q{}')"
[[ -n "$STORAGE_PATH" ]] || fail "Cannot determine the filesystem path for storage $STORAGE."

WORK_DIR="$(mktemp -d /var/tmp/orchestrator-cloud-template.XXXXXX)"
SNIPPET_DIR="$STORAGE_PATH/snippets"
SNIPPET_PATH="$SNIPPET_DIR/$SNIPPET_NAME"
VM_STARTED=0

cleanup() {
    rm -rf -- "$WORK_DIR"
}
trap cleanup EXIT

log "Downloading and verifying the official Debian 13 cloud image"
curl -fsSL "$IMAGE_BASE_URL/SHA512SUMS" -o "$WORK_DIR/SHA512SUMS"
curl -fsSL "$IMAGE_BASE_URL/$IMAGE_NAME" -o "$WORK_DIR/$IMAGE_NAME"
(
    cd "$WORK_DIR"
    grep -F " $IMAGE_NAME" SHA512SUMS | sha512sum --check --status
) || fail "SHA-512 verification failed. No VM has been created yet."

log "Preparing temporary Cloud-Init configuration"
mkdir -p -- "$SNIPPET_DIR"
[[ -f "$SNIPPET_PATH" ]] && fail "Snippet already exists: $SNIPPET_PATH"

cat >"$SNIPPET_PATH" <<'EOF'
#cloud-config
package_update: true
package_reboot_if_required: false
packages:
  - qemu-guest-agent
runcmd:
  - systemctl enable qemu-guest-agent
  - systemctl start qemu-guest-agent
  - poweroff
EOF
chmod 0644 "$SNIPPET_PATH"

log "Creating temporary VM $VMID in pool $POOL"
qm create "$VMID" \
    --name "$VM_NAME" \
    --pool "$POOL" \
    --memory 2048 \
    --cores 2 \
    --sockets 1 \
    --cpu x86-64-v2-AES \
    --net0 "virtio,bridge=$BRIDGE" \
    --ostype l26 \
    --scsihw virtio-scsi-pci \
    --agent enabled=1 \
    --ipconfig0 ip=dhcp \
    --serial0 socket \
    --vga serial0

log "Importing the cloud disk into $STORAGE"
qm disk import "$VMID" "$WORK_DIR/$IMAGE_NAME" "$STORAGE" --format qcow2

DISK_VOLUME="$(qm config "$VMID" | awk -F': ' '$1 == "unused0" { print $2; exit }')"
[[ -n "$DISK_VOLUME" ]] || fail "The imported disk was not found in the VM configuration."

qm set "$VMID" --scsi0 "$DISK_VOLUME"
qm set "$VMID" --ide2 "$STORAGE:cloudinit"
qm set "$VMID" --boot order=scsi0
qm set "$VMID" --cicustom "user=$STORAGE:snippets/$SNIPPET_NAME"

log "Starting VM $VMID to install QEMU Guest Agent"
qm start "$VMID"
VM_STARTED=1

log "Waiting for Cloud-Init to install the agent and power off the VM"
for attempt in $(seq 1 90); do
    STATUS="$(qm status "$VMID" | awk '{print $2}')"
    if [[ "$STATUS" == "stopped" ]]; then
        VM_STARTED=0
        break
    fi
    sleep 10
done

if [[ "$VM_STARTED" -eq 1 ]]; then
    fail "VM $VMID did not stop within 15 minutes. It was left intact for diagnostics. Check its console and Cloud-Init logs."
fi

log "Removing temporary Cloud-Init customization"
qm set "$VMID" --delete cicustom
rm -f -- "$SNIPPET_PATH"

log "Converting VM $VMID into a template"
qm template "$VMID"

log "Template created successfully"
qm config "$VMID"
printf '\nTemplate VMID: %s\nTemplate name: %s\nStorage: %s\nPool: %s\n' \
    "$VMID" "$VM_NAME" "$STORAGE" "$POOL"
