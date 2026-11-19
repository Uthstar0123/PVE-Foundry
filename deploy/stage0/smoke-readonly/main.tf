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
  endpoint = "https://192.0.2.10:8006/"

  # Temporary Stage 0 exception for the current self-signed certificate.
  # Replace with trusted TLS validation before production use.
  insecure = true

  # No SSH block: this test uses only the Proxmox API.
}

data "proxmox_virtual_environment_version" "current" {}

data "proxmox_virtual_environment_nodes" "all" {}

data "proxmox_virtual_environment_datastores" "pve" {
  node_name = "pve"
}

output "proxmox_version" {
  value = data.proxmox_virtual_environment_version.current
}

output "nodes" {
  value = data.proxmox_virtual_environment_nodes.all
}

output "datastores" {
  value = data.proxmox_virtual_environment_datastores.pve
}
