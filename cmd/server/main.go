package main

import "proxmox-orchestrator/internal/app"

func main() {
	app.Run("management", "MANAGEMENT", ":8080")
}
