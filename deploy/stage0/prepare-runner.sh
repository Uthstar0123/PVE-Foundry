#!/usr/bin/env bash
# One-time creation of the isolated Runner VM from template 9000.
# Run as root from the Proxmox web shell.

set -Eeuo pipefail

readonly TEMPLATE_VMID="9000"
readonly RUNNER_VMID="9100"
readonly RUNNER_NAME="orchestrator-runner"
readonly POOL="orchestrator-system"
readonly STORAGE="raid_1_2tb"
readonly RUNNER_USER="orchestrator"

log() {
    printf '\n[%s] %s\n' "$(date +'%H:%M:%S')" "$*"
}

fail() {
    printf '\nERROR: %s\n' "$*" >&2
    exit 1
}

[[ "$(id -u)" -eq 0 ]] || fail "Run this script as root from the Proxmox web shell."
command -v qm >/dev/null 2>&1 || fail "qm is unavailable."
command -v pvesh >/dev/null 2>&1 || fail "pvesh is unavailable."
command -v perl >/dev/null 2>&1 || fail "perl is unavailable."

qm status "$TEMPLATE_VMID" >/dev/null 2>&1 || fail "Template $TEMPLATE_VMID does not exist."
if qm status "$RUNNER_VMID" >/dev/null 2>&1; then
    fail "VMID $RUNNER_VMID already exists. It was not changed."
fi
pvesh get "/pools/$POOL" >/dev/null 2>&1 || fail "Create pool $POOL before running this script."

printf 'Paste the SSH public key for Runner access (ssh-ed25519 ...):\n'
read -r -p '> ' SSH_PUBLIC_KEY
case "$SSH_PUBLIC_KEY" in
    ssh-ed25519\ *|ssh-rsa\ *|ecdsa-sha2-nistp256\ *) ;;
    *) fail "The value does not look like a supported SSH public key." ;;
esac

WORK_DIR="$(mktemp -d /var/tmp/orchestrator-runner.XXXXXX)"
trap 'rm -rf -- "$WORK_DIR"' EXIT
printf '%s\n' "$SSH_PUBLIC_KEY" >"$WORK_DIR/authorized_key.pub"
chmod 0600 "$WORK_DIR/authorized_key.pub"

log "Creating full clone $RUNNER_VMID from template $TEMPLATE_VMID"
qm clone "$TEMPLATE_VMID" "$RUNNER_VMID" \
    --name "$RUNNER_NAME" \
    --full \
    --storage "$STORAGE" \
    --pool "$POOL"

log "Configuring Runner resources and Cloud-Init"
qm set "$RUNNER_VMID" \
    --cores 4 \
    --memory 4096 \
    --ciuser "$RUNNER_USER" \
    --sshkeys "$WORK_DIR/authorized_key.pub" \
    --ipconfig0 ip=dhcp \
    --onboot 1
qm resize "$RUNNER_VMID" scsi0 32G

log "Starting Runner VM"
qm start "$RUNNER_VMID"

log "Waiting for DHCP and QEMU Guest Agent"
RUNNER_IP=""
for attempt in $(seq 1 60); do
    if AGENT_JSON="$(qm guest cmd "$RUNNER_VMID" network-get-interfaces 2>/dev/null)"; then
        RUNNER_IP="$(printf '%s' "$AGENT_JSON" | perl -MJSON::PP -0777 -ne '
            my $data = decode_json($_);
            my $interfaces = ref($data) eq q{ARRAY} ? $data : ($data->{result} // []);
            for my $iface (@{$interfaces}) {
                next if ($iface->{name} // q{}) eq q{lo};
                for my $address (@{$iface->{q{ip-addresses}} // []}) {
                    if (($address->{q{ip-address-type}} // q{}) eq q{ipv4}) {
                        print $address->{q{ip-address}};
                        exit;
                    }
                }
            }
        ')"
    fi
    [[ -n "$RUNNER_IP" ]] && break
    sleep 5
done

if [[ -n "$RUNNER_IP" ]]; then
    printf '\nRunner VM created successfully.\n'
    printf 'VMID: %s\nIP: %s\nSSH: ssh %s@%s\n' \
        "$RUNNER_VMID" "$RUNNER_IP" "$RUNNER_USER" "$RUNNER_IP"
else
    printf '\nRunner VM %s was created and started, but no DHCP address was detected yet.\n' "$RUNNER_VMID"
    printf 'Check the VM summary in Proxmox and verify DHCP on vmbr0.\n'
fi

qm status "$RUNNER_VMID"
