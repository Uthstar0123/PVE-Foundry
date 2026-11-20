package runner

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
)

const allowedProviderSource = "bpg/proxmox"
const allowedProviderVersion = "0.123.0"

func ValidateConfiguration(directory string) error {
	parser := hclparse.NewParser()
	providerFound := false
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".tf") && !strings.HasSuffix(entry.Name(), ".tf.json")) {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("configuration contains a non-regular file: %s", entry.Name())
		}
		var file *hcl.File
		var diagnostics hcl.Diagnostics
		if strings.HasSuffix(entry.Name(), ".tf.json") {
			file, diagnostics = parser.ParseJSONFile(path)
		} else {
			file, diagnostics = parser.ParseHCLFile(path)
		}
		if diagnostics.HasErrors() {
			return fmt.Errorf("parse %s: %s", entry.Name(), diagnostics.Error())
		}
		found, err := validateBody(file.Body)
		providerFound = providerFound || found
		return err
	})
	if err != nil {
		return err
	}
	if !providerFound {
		return fmt.Errorf("required provider %s version %s is not declared", allowedProviderSource, allowedProviderVersion)
	}
	return nil
}

func validateBody(body hcl.Body) (bool, error) {
	content, _, diagnostics := body.PartialContent(&hcl.BodySchema{Blocks: []hcl.BlockHeaderSchema{
		{Type: "terraform"}, {Type: "resource", LabelNames: []string{"type", "name"}},
		{Type: "data", LabelNames: []string{"type", "name"}}, {Type: "module", LabelNames: []string{"name"}},
	}})
	if diagnostics.HasErrors() {
		return false, diagnostics
	}
	providerFound := false
	for _, block := range content.Blocks {
		switch block.Type {
		case "module":
			return false, fmt.Errorf("module blocks are forbidden: artifact must be self-contained")
		case "resource", "data":
			inner, _, diags := block.Body.PartialContent(&hcl.BodySchema{Blocks: []hcl.BlockHeaderSchema{{Type: "provisioner", LabelNames: []string{"type"}}}})
			if diags.HasErrors() {
				return false, diags
			}
			if len(inner.Blocks) != 0 {
				return false, fmt.Errorf("provisioners are forbidden")
			}
		case "terraform":
			found, err := validateTerraformBlock(block.Body)
			providerFound = providerFound || found
			if err != nil {
				return false, err
			}
		}
	}
	return providerFound, nil
}

func validateTerraformBlock(body hcl.Body) (bool, error) {
	content, _, diagnostics := body.PartialContent(&hcl.BodySchema{Blocks: []hcl.BlockHeaderSchema{
		{Type: "required_providers"}, {Type: "backend", LabelNames: []string{"type"}},
	}})
	if diagnostics.HasErrors() {
		return false, diagnostics
	}
	providerFound := false
	for _, block := range content.Blocks {
		if block.Type == "backend" {
			if len(block.Labels) != 1 || block.Labels[0] != "local" {
				return false, fmt.Errorf("only the local backend is allowed")
			}
			continue
		}
		attributes, diags := block.Body.JustAttributes()
		if diags.HasErrors() {
			return false, diags
		}
		for name, attribute := range attributes {
			var provider struct {
				Source  string `hcl:"source" cty:"source"`
				Version string `hcl:"version" cty:"version"`
			}
			if diags := gohcl.DecodeExpression(attribute.Expr, nil, &provider); diags.HasErrors() {
				return false, fmt.Errorf("provider %s must contain literal source and version: %s", name, diags.Error())
			}
			versionText := strings.TrimSpace(provider.Version)
			if provider.Source != allowedProviderSource || (versionText != allowedProviderVersion && versionText != "= "+allowedProviderVersion) {
				return false, fmt.Errorf("provider %s must use %s version %s", name, allowedProviderSource, allowedProviderVersion)
			}
			providerFound = true
		}
	}
	return providerFound, nil
}
