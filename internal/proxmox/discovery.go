package proxmox

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

type Template struct {
	ID      string `json:"id"`
	VMID    int    `json:"vmid,omitempty"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Node    string `json:"node,omitempty"`
	Storage string `json:"storage,omitempty"`
}

func nextAvailableVMID(resources []Resource) int {
	next := 100
	used := make(map[int]bool, len(resources))
	for _, resource := range resources {
		used[resource.VMID] = true
		if resource.VMID >= next {
			next = resource.VMID + 1
		}
	}
	for used[next] {
		next++
	}
	return next
}

// discover fills optional deployment hints. Base inventory remains usable when
// the read-only token cannot list template content or query cluster/nextid.
func (c *Client) discover(ctx context.Context, inventory *Inventory) {
	inventory.NextVMID = nextAvailableVMID(inventory.Resources)
	var nextID string
	if err := c.get(ctx, "cluster/nextid", &nextID); err == nil {
		if parsed, parseErr := strconv.Atoi(nextID); parseErr == nil && parsed >= 100 {
			inventory.NextVMID = parsed
		}
	}

	seen := map[string]bool{}
	for _, resource := range inventory.Resources {
		if resource.Template == 1 && resource.Type == "qemu" {
			inventory.Templates = append(inventory.Templates, Template{ID: strconv.Itoa(resource.VMID), VMID: resource.VMID, Name: resource.Name, Type: "qemu", Node: resource.Node})
			seen["qemu/"+strconv.Itoa(resource.VMID)] = true
		}
	}
	for _, storage := range inventory.Storage {
		if !strings.Contains(storage.Content, "vztmpl") {
			continue
		}
		var content []struct {
			VolumeID string `json:"volid"`
			Name     string `json:"name"`
			Size     int64  `json:"size"`
		}
		path := "nodes/" + url.PathEscape(storage.Node) + "/storage/" + url.PathEscape(storage.Storage) + "/content?content=vztmpl"
		if err := c.get(ctx, path, &content); err != nil {
			continue
		}
		for _, item := range content {
			key := "lxc/" + item.VolumeID
			if item.VolumeID == "" || seen[key] {
				continue
			}
			name := item.Name
			if name == "" {
				name = item.VolumeID
			}
			inventory.Templates = append(inventory.Templates, Template{ID: item.VolumeID, Name: name, Type: "lxc", Node: storage.Node, Storage: storage.Storage})
			seen[key] = true
		}
	}
}
