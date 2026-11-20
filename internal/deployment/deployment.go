package deployment

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"

	"proxmox-orchestrator/internal/proxmox"
)

const (
	maxFiles       = 64
	maxFileSize    = 1 << 20
	maxDecodedSize = 4 << 20
)

type Defaults struct {
	NodeName    string             `json:"nodeName"`
	PoolID      string             `json:"poolId"`
	DatastoreID string             `json:"datastoreId"`
	Bridge      string             `json:"bridge"`
	NextVMID    int                `json:"nextVmid"`
	Templates   []proxmox.Template `json:"templates"`
}

func DefaultsFromInventory(inventory proxmox.Inventory) Defaults {
	defaults := Defaults{NextVMID: inventory.NextVMID, Templates: inventory.Templates}
	for _, node := range inventory.Nodes {
		if node.Status == "online" || defaults.NodeName == "" {
			defaults.NodeName = node.Node
			if node.Status == "online" {
				break
			}
		}
	}
	if len(inventory.Pools) > 0 {
		defaults.PoolID = inventory.Pools[0].PoolID
	}
	for _, storage := range inventory.Storage {
		if defaults.NodeName != "" && storage.Node != defaults.NodeName || storage.Active == 0 || storage.Enabled == 0 {
			continue
		}
		if strings.Contains(storage.Content, "rootdir") || strings.Contains(storage.Content, "images") {
			defaults.DatastoreID = storage.Storage
			break
		}
	}
	for _, network := range inventory.Networks {
		if defaults.NodeName != "" && network.Node != defaults.NodeName || network.Active == 0 {
			continue
		}
		if network.Type == "bridge" {
			defaults.Bridge = network.Iface
			break
		}
	}
	return defaults
}

type InputFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type File struct {
	Name    string
	Content []byte
}

func DecodeFiles(inputs []InputFile) ([]File, error) {
	if len(inputs) == 0 || len(inputs) > maxFiles {
		return nil, errors.New("выберите от 1 до 64 файлов")
	}
	seen := map[string]bool{}
	total := 0
	files := make([]File, 0, len(inputs))
	for _, input := range inputs {
		name, err := validFilename(input.Name)
		if err != nil {
			return nil, err
		}
		if seen[name] {
			return nil, fmt.Errorf("файл %s указан повторно", name)
		}
		seen[name] = true
		content, err := base64.StdEncoding.DecodeString(input.Content)
		if err != nil || len(content) == 0 || len(content) > maxFileSize {
			return nil, fmt.Errorf("файл %s пуст, повреждён или превышает 1 МиБ", name)
		}
		total += len(content)
		if total > maxDecodedSize {
			return nil, errors.New("общий размер файлов превышает 4 МиБ")
		}
		files = append(files, File{Name: name, Content: content})
	}
	return files, nil
}

func validFilename(name string) (string, error) {
	if name == "" || strings.Contains(name, "\\") || path.Clean(name) != name || path.IsAbs(name) || name == "." || strings.HasPrefix(name, "../") {
		return "", errors.New("некорректный путь к файлу конфигурации")
	}
	base := path.Base(name)
	if !strings.HasSuffix(base, ".tf") && !strings.HasSuffix(base, ".tf.json") && !strings.HasSuffix(base, ".tfvars") && base != ".terraform.lock.hcl" {
		return "", fmt.Errorf("файл %s не является конфигурацией OpenTofu", name)
	}
	return name, nil
}

type Variable struct {
	Name      string `json:"name"`
	Required  bool   `json:"required"`
	Sensitive bool   `json:"sensitive"`
	Source    string `json:"source"`
	Value     any    `json:"value,omitempty"`
}

func Preflight(files []File, defaults Defaults) ([]Variable, error) {
	values := map[string]any{}
	for _, file := range files {
		if !strings.HasSuffix(file.Name, ".tfvars") {
			continue
		}
		parsed, diags := hclparse.NewParser().ParseHCL(file.Content, file.Name)
		if diags.HasErrors() {
			return nil, fmt.Errorf("разбор %s: %s", file.Name, diags.Error())
		}
		attrs, diags := parsed.Body.JustAttributes()
		if diags.HasErrors() {
			return nil, fmt.Errorf("разбор %s: %s", file.Name, diags.Error())
		}
		for name, attr := range attrs {
			value, diags := attr.Expr.Value(nil)
			if diags.HasErrors() {
				return nil, fmt.Errorf("переменная %s должна иметь литеральное значение", name)
			}
			decoded, err := fromCty(value)
			if err != nil {
				return nil, err
			}
			values[name] = decoded
		}
	}

	found := map[string]Variable{}
	for _, file := range files {
		if !strings.HasSuffix(file.Name, ".tf") && !strings.HasSuffix(file.Name, ".tf.json") {
			continue
		}
		parser := hclparse.NewParser()
		var parsed *hcl.File
		var diags hcl.Diagnostics
		if strings.HasSuffix(file.Name, ".tf.json") {
			parsed, diags = parser.ParseJSON(file.Content, file.Name)
		} else {
			parsed, diags = parser.ParseHCL(file.Content, file.Name)
		}
		if diags.HasErrors() {
			return nil, fmt.Errorf("разбор %s: %s", file.Name, diags.Error())
		}
		content, _, diags := parsed.Body.PartialContent(&hcl.BodySchema{Blocks: []hcl.BlockHeaderSchema{{Type: "variable", LabelNames: []string{"name"}}}})
		if diags.HasErrors() {
			return nil, diags
		}
		for _, block := range content.Blocks {
			name := block.Labels[0]
			variable := Variable{Name: name, Required: true, Source: "required"}
			attrs, diags := block.Body.JustAttributes()
			if diags.HasErrors() {
				return nil, diags
			}
			if attr, ok := attrs["sensitive"]; ok {
				if sensitive, diags := attr.Expr.Value(nil); !diags.HasErrors() && sensitive.True() {
					variable.Sensitive = true
				}
			}
			if attr, ok := attrs["default"]; ok {
				if value, diags := attr.Expr.Value(nil); !diags.HasErrors() {
					decoded, err := fromCty(value)
					if err != nil {
						return nil, err
					}
					variable.Value, variable.Required, variable.Source = decoded, false, "default"
				}
			}
			if value, ok := values[name]; ok {
				variable.Value, variable.Required, variable.Source = value, false, "file"
			}
			if variable.Required && !variable.Sensitive && !secretName(name) {
				if value, ok := automaticValue(name, defaults); ok {
					variable.Value, variable.Required, variable.Source = value, false, "auto"
				}
			}
			found[name] = variable
		}
	}
	result := make([]Variable, 0, len(found))
	for _, variable := range found {
		result = append(result, variable)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func secretName(name string) bool {
	name = strings.ToLower(name)
	for _, part := range []string{"token", "password", "secret", "private_key", "private-key"} {
		if strings.Contains(name, part) {
			return true
		}
	}
	return false
}

func automaticValue(name string, defaults Defaults) (any, bool) {
	switch strings.ToLower(name) {
	case "node", "node_name", "nodename":
		return defaults.NodeName, defaults.NodeName != ""
	case "pool", "pool_id", "poolid":
		return defaults.PoolID, defaults.PoolID != ""
	case "vm_id", "vmid":
		return defaults.NextVMID, defaults.NextVMID >= 100
	case "datastore", "datastore_id", "datastoreid", "storage":
		return defaults.DatastoreID, defaults.DatastoreID != ""
	case "bridge", "network_bridge":
		return defaults.Bridge, defaults.Bridge != ""
	case "hostname", "host_name":
		return fmt.Sprintf("foundry-%d", defaults.NextVMID), defaults.NextVMID >= 100
	case "template_file_id", "template":
		for _, template := range defaults.Templates {
			if template.Type == "lxc" {
				return template.ID, true
			}
		}
	case "template_vmid", "template_vm_id", "clone_vmid":
		for _, template := range defaults.Templates {
			if template.Type == "qemu" {
				return template.VMID, true
			}
		}
	}
	return nil, false
}

func fromCty(value cty.Value) (any, error) {
	if !value.IsWhollyKnown() || value.IsNull() {
		return nil, nil
	}
	encoded, err := ctyjson.Marshal(value, value.Type())
	if err != nil {
		return nil, errors.New("неподдерживаемое значение переменной")
	}
	var result any
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func toCty(value any) (cty.Value, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return cty.NilVal, err
	}
	typeName, err := ctyjson.ImpliedType(encoded)
	if err != nil {
		return cty.NilVal, err
	}
	return ctyjson.Unmarshal(encoded, typeName)
}

func Archive(files []File, variables map[string]any) ([]byte, error) {
	if len(variables) > 0 {
		body := hclwrite.NewEmptyFile().Body()
		names := make([]string, 0, len(variables))
		for name := range variables {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			value, err := toCty(variables[name])
			if err != nil {
				return nil, fmt.Errorf("переменная %s: %w", name, err)
			}
			body.SetAttributeValue(name, value)
		}
		files = append(append([]File{}, files...), File{Name: "foundry.auto.tfvars", Content: hclwrite.Format(body.BuildTokens(nil).Bytes())})
	}
	var buffer bytes.Buffer
	gzipWriter, _ := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, file := range files {
		header := &tar.Header{Name: file.Name, Mode: 0o600, Size: int64(len(file.Content)), ModTime: time.Unix(0, 0), Typeflag: tar.TypeReg}
		if err := tarWriter.WriteHeader(header); err != nil {
			return nil, err
		}
		if _, err := tarWriter.Write(file.Content); err != nil {
			return nil, err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return nil, err
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}
	if buffer.Len() > maxFileSize {
		return nil, errors.New("сжатая конфигурация превышает 1 МиБ")
	}
	return buffer.Bytes(), nil
}
