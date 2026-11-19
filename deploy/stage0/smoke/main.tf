terraform {
  required_version = ">= 1.10.0"

  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = "0.123.0"
    }
  }
}

provider "proxmox" {
  endpoint = "https://192.168.80.149:8006/"

  # Temporary Stage 0 exception for the current self-signed certificate.
  # Replace with trusted TLS validation before production use.
  insecure = true

  # No SSH block: this test uses only the Proxmox API.
}

variable "ssh_public_key" {
  description = "Public SSH key installed in the two disposable smoke-test guests"
  type        = string

  validation {
    condition     = can(regex("^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp256) ", var.ssh_public_key))
    error_message = "Provide a supported SSH public key, not a private key."
  }
}

locals {
  node_name          = "pve"
  pool_id            = "orchestrator-test"
  datastore_id       = "raid_1_2tb"
  bridge             = "vmbr0"
  guest_network_cidr = "192.168.80.0/24"
}

resource "proxmox_virtual_environment_vm" "smoke_qemu" {
  name        = "orchestrator-smoke-qemu"
  description = "Disposable Stage 0 API smoke test; safe to destroy"
  tags        = ["orchestrator", "smoke", "stage0"]

  node_name = local.node_name
  vm_id     = 9201
  pool_id   = local.pool_id

  clone {
    node_name    = local.node_name
    vm_id        = 9001
    datastore_id = local.datastore_id
    full         = true
    retries      = 3
  }

  agent {
    enabled = true
    timeout = "10m"

    wait_for_ip {
      ipv4      = true
      cidrs     = [local.guest_network_cidr]
      interfaces = ["eth0"]
    }
  }

  cpu {
    cores = 3
    type  = "x86-64-v2-AES"
  }

  memory {
    dedicated = 3072
  }

  disk {
    datastore_id = local.datastore_id
    interface    = "scsi0"
    file_format  = "qcow2"
    size         = 8
  }

  initialization {
    datastore_id = local.datastore_id
    interface    = "ide2"

    ip_config {
      ipv4 {
        address = "dhcp"
      }
    }

    user_account {
      username = "orchestrator"
      keys     = [trimspace(var.ssh_public_key)]
    }
  }

  network_device {
    bridge = local.bridge
    model  = "virtio"
  }

  started         = true
  on_boot         = false
  stop_on_destroy = true
}

resource "proxmox_virtual_environment_container" "smoke_lxc" {
  description = "Disposable Stage 0 API smoke test; safe to destroy"
  tags        = ["orchestrator", "smoke", "stage0"]

  node_name    = local.node_name
  vm_id        = 9202
  pool_id      = local.pool_id
  unprivileged = true

  cpu {
    cores = 3
  }

  memory {
    dedicated = 2048
    swap      = 512
  }

  disk {
    datastore_id = local.datastore_id
    size         = 8
  }

  initialization {
    hostname = "orchestrator-smoke-lxc"

    ip_config {
      ipv4 {
        address = "dhcp"
      }
    }

    user_account {
      keys = [trimspace(var.ssh_public_key)]
    }
  }

  network_interface {
    name   = "eth0"
    bridge = local.bridge
  }

  operating_system {
    template_file_id = "NVME_1TB_sec:vztmpl/debian-13-standard_13.6-1_amd64.tar.zst"
    type             = "debian"
  }

  wait_for_ip {
    ipv4       = true
    cidrs      = [local.guest_network_cidr]
    interfaces = ["eth0"]
    timeout    = "5m"
  }

  started       = true
  start_on_boot = false
}

output "qemu_ipv4_addresses" {
  value = proxmox_virtual_environment_vm.smoke_qemu.ipv4_addresses
}

output "lxc_ipv4_addresses" {
  value = proxmox_virtual_environment_container.smoke_lxc.ipv4
}
