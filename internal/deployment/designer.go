package deployment

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

type DesignerNode struct {
	ID     string         `json:"id"`
	Kind   string         `json:"kind"`
	Label  string         `json:"label"`
	X      float64        `json:"x"`
	Y      float64        `json:"y"`
	Config map[string]any `json:"config"`
}

type DesignerEdge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
}

type DesignerValue struct {
	Name  string         `json:"name"`
	Nodes []DesignerNode `json:"nodes"`
	Edges []DesignerEdge `json:"edges"`
}

var deploymentName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9/_-]{0,127}$`)
var guestName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{0,62}$`)

func GenerateDesigner(value DesignerValue) ([]File, error) {
	if !deploymentName.MatchString(value.Name) || len(value.Nodes) == 0 || len(value.Nodes) > 32 {
		return nil, errors.New("укажите имя deployment и добавьте от 1 до 32 блоков")
	}
	nodes := map[string]DesignerNode{}
	attachments := map[string]map[string][]DesignerNode{}
	usedVMID := map[int]bool{}
	usedName := map[string]bool{}
	guests := 0
	for _, node := range value.Nodes {
		if node.ID == "" || nodes[node.ID].ID != "" {
			return nil, errors.New("идентификаторы блоков должны быть уникальны")
		}
		switch node.Kind {
		case "qemu", "lxc", "network", "storage", "template":
		default:
			return nil, fmt.Errorf("неподдерживаемый блок %s", node.Kind)
		}
		nodes[node.ID] = node
		if node.Kind != "qemu" && node.Kind != "lxc" {
			continue
		}
		guests++
		vmid, err := integer(node.Config, "vmid", 100, 999999999)
		if err != nil || usedVMID[vmid] {
			return nil, errors.New("VMID должен быть уникальным числом от 100 до 999999999")
		}
		usedVMID[vmid] = true
		if !guestName.MatchString(node.Label) || usedName[node.Label] {
			return nil, errors.New("имена ВМ и контейнеров должны быть уникальными DNS-именами")
		}
		usedName[node.Label] = true
		for key, bounds := range map[string][2]int{"cores": {1, 256}, "memory": {128, 4194304}, "disk": {1, 65536}} {
			if _, err := integer(node.Config, key, bounds[0], bounds[1]); err != nil {
				return nil, fmt.Errorf("%s: %w", node.Label, err)
			}
		}
	}
	if guests == 0 {
		return nil, errors.New("добавьте хотя бы одну ВМ QEMU или контейнер LXC")
	}
	for _, edge := range value.Edges {
		source, sourceOK := nodes[edge.Source]
		target, targetOK := nodes[edge.Target]
		if !sourceOK || !targetOK || edge.Source == edge.Target || (target.Kind != "qemu" && target.Kind != "lxc") {
			return nil, errors.New("связь должна вести от сети, хранилища или шаблона к ВМ/контейнеру")
		}
		if source.Kind != "network" && source.Kind != "storage" && source.Kind != "template" {
			return nil, errors.New("источником связи может быть только сеть, хранилище или шаблон")
		}
		if attachments[target.ID] == nil {
			attachments[target.ID] = map[string][]DesignerNode{}
		}
		attachments[target.ID][source.Kind] = append(attachments[target.ID][source.Kind], source)
	}

	file := hclwrite.NewEmptyFile()
	terraform := file.Body().AppendNewBlock("terraform", nil).Body()
	providers := terraform.AppendNewBlock("required_providers", nil).Body()
	providers.SetAttributeValue("proxmox", mustCty(map[string]any{"source": "bpg/proxmox", "version": "0.123.0"}))
	ordered := append([]DesignerNode{}, value.Nodes...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	for index, node := range ordered {
		if node.Kind != "qemu" && node.Kind != "lxc" {
			continue
		}
		attached := attachments[node.ID]
		if len(attached["network"]) != 1 || len(attached["storage"]) != 1 || len(attached["template"]) != 1 {
			return nil, fmt.Errorf("%s: соедините блок ровно с одной сетью, хранилищем и шаблоном", node.Label)
		}
		resourceType := "proxmox_virtual_environment_vm"
		if node.Kind == "lxc" {
			resourceType = "proxmox_virtual_environment_container"
		}
		nodeName := stringConfig(node.Config, "nodeName")
		datastoreID := stringConfig(attached["storage"][0].Config, "datastoreId")
		bridge := stringConfig(attached["network"][0].Config, "bridge")
		templateID := stringConfig(attached["template"][0].Config, "templateId")
		if node.Kind == "lxc" && templateID != "" && !strings.Contains(templateID, ":") {
			return nil, fmt.Errorf("%s: для контейнера нужен LXC-шаблон (vztmpl, например local:vztmpl/debian.tar.zst), а выбран QEMU-шаблон %q", node.Label, templateID)
		}
		if node.Kind == "qemu" && strings.Contains(templateID, ":") {
			return nil, fmt.Errorf("%s: для ВМ нужен QEMU-шаблон (VMID), а выбран LXC-шаблон %q", node.Label, templateID)
		}
		if nodeName == "" || datastoreID == "" || bridge == "" || templateID == "" || node.Kind == "qemu" && templateVMID(attached["template"][0]) < 100 {
			return nil, fmt.Errorf("%s: заполните ноду, хранилище, сеть и корректный шаблон", node.Label)
		}
		body := file.Body().AppendNewBlock("resource", []string{resourceType, "guest_" + strconv.Itoa(index+1)}).Body()
		body.SetAttributeValue("node_name", mustCty(nodeName))
		body.SetAttributeValue("vm_id", mustCty(numberConfig(node.Config, "vmid")))
		if poolID := stringConfig(node.Config, "poolId"); poolID != "" {
			body.SetAttributeValue("pool_id", mustCty(poolID))
		}
		body.SetAttributeValue("started", mustCty(false))
		if node.Kind == "qemu" {
			body.SetAttributeValue("name", mustCty(node.Label))
			clone := body.AppendNewBlock("clone", nil).Body()
			clone.SetAttributeValue("vm_id", mustCty(templateVMID(attached["template"][0])))
			clone.SetAttributeValue("full", mustCty(true))
		} else {
			body.SetAttributeValue("unprivileged", mustCty(true))
			osBlock := body.AppendNewBlock("operating_system", nil).Body()
			osBlock.SetAttributeValue("template_file_id", mustCty(stringConfig(attached["template"][0].Config, "templateId")))
			osBlock.SetAttributeValue("type", mustCty("debian"))
			initialization := body.AppendNewBlock("initialization", nil).Body()
			initialization.SetAttributeValue("hostname", mustCty(node.Label))
			ip := initialization.AppendNewBlock("ip_config", nil).Body().AppendNewBlock("ipv4", nil).Body()
			ip.SetAttributeValue("address", mustCty("dhcp"))
		}
		cpu := body.AppendNewBlock("cpu", nil).Body()
		cpu.SetAttributeValue("cores", mustCty(numberConfig(node.Config, "cores")))
		memory := body.AppendNewBlock("memory", nil).Body()
		memory.SetAttributeValue("dedicated", mustCty(numberConfig(node.Config, "memory")))
		if node.Kind == "lxc" {
			memory.SetAttributeValue("swap", mustCty(512))
		}
		disk := body.AppendNewBlock("disk", nil).Body()
		disk.SetAttributeValue("datastore_id", mustCty(stringConfig(attached["storage"][0].Config, "datastoreId")))
		disk.SetAttributeValue("size", mustCty(numberConfig(node.Config, "disk")))
		if node.Kind == "qemu" {
			disk.SetAttributeValue("interface", mustCty("scsi0"))
			network := body.AppendNewBlock("network_device", nil).Body()
			network.SetAttributeValue("bridge", mustCty(stringConfig(attached["network"][0].Config, "bridge")))
			network.SetAttributeValue("model", mustCty("virtio"))
		} else {
			network := body.AppendNewBlock("network_interface", nil).Body()
			network.SetAttributeValue("name", mustCty("eth0"))
			network.SetAttributeValue("bridge", mustCty(stringConfig(attached["network"][0].Config, "bridge")))
		}
		file.Body().AppendNewline()
	}
	return []File{{Name: "main.tf", Content: hclwrite.Format(file.Bytes())}}, nil
}

func stringConfig(config map[string]any, key string) string {
	value, _ := config[key].(string)
	return strings.TrimSpace(value)
}

func numberConfig(config map[string]any, key string) float64 {
	switch value := config[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case string:
		parsed, _ := strconv.ParseFloat(value, 64)
		return parsed
	default:
		return 0
	}
}

func integer(config map[string]any, key string, min, max int) (int, error) {
	value := numberConfig(config, key)
	if value != float64(int(value)) || int(value) < min || int(value) > max {
		return 0, fmt.Errorf("параметр %s должен быть целым числом от %d до %d", key, min, max)
	}
	return int(value), nil
}

func templateVMID(node DesignerNode) int {
	value := stringConfig(node.Config, "templateId")
	parsed, _ := strconv.Atoi(value)
	if parsed == 0 {
		parsed = int(numberConfig(node.Config, "vmid"))
	}
	return parsed
}

func mustCty(value any) cty.Value {
	result, err := toCty(value)
	if err != nil {
		panic(err)
	}
	return result
}
