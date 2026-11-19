package main

import "proxmox-orchestrator/internal/app"

func main() {
	app.Run("runner", "RUNNER", ":8081")
}
