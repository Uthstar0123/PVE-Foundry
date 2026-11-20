package runner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type tfFile struct {
	name, body string
	typeflag   byte
	linkname   string
}

func buildTarGz(t *testing.T, files []tfFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, f := range files {
		flag := f.typeflag
		if flag == 0 {
			flag = tar.TypeReg
		}
		data := []byte(f.body)
		hdr := &tar.Header{Name: f.name, Mode: 0644, Size: int64(len(data)), Typeflag: flag, Linkname: f.linkname}
		if flag == 'x' {
			hdr.Mode = 0755
			hdr.Typeflag = tar.TypeReg
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader failed: %v", err)
		}
		if flag != tar.TypeSymlink {
			if _, err := tw.Write(data); err != nil {
				t.Fatalf("Write failed: %v", err)
			}
		}
	}
	_ = tw.Close()
	_ = gw.Close()
	return buf.Bytes()
}

func TestArchiveAndValidation(t *testing.T) {
	validHCL := `
terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = "0.123.0"
    }
  }
}
`
	validJSON := `{
  "terraform": {
    "required_providers": {
      "proxmox": {
        "source": "bpg/proxmox",
        "version": "0.123.0"
      }
    }
  }
}`

	t.Run("valid main.tf extracts and validates", func(t *testing.T) {
		dir := t.TempDir()
		data := buildTarGz(t, []tfFile{{name: "main.tf", body: validHCL}})
		if err := SecureExtractTarGz(data, dir); err != nil {
			t.Fatalf("unexpected extract error: %v", err)
		}
		if err := ValidateConfiguration(dir); err != nil {
			t.Fatalf("unexpected validate error: %v", err)
		}
	})

	t.Run("valid .tf.json accepted", func(t *testing.T) {
		dir := t.TempDir()
		data := buildTarGz(t, []tfFile{{name: "main.tf.json", body: validJSON}})
		if err := SecureExtractTarGz(data, dir); err != nil {
			t.Fatalf("unexpected extract error: %v", err)
		}
		if err := ValidateConfiguration(dir); err != nil {
			t.Fatalf("unexpected validate error: %v", err)
		}
	})

	t.Run("path traversal rejected and outside file absent", func(t *testing.T) {
		base := t.TempDir()
		dest := filepath.Join(base, "sub")
		_ = os.Mkdir(dest, 0755)
		data := buildTarGz(t, []tfFile{{name: "../escape.tf", body: "resource \"x\" \"y\" {}"}})
		if err := SecureExtractTarGz(data, dest); err == nil {
			t.Fatal("expected path traversal to be rejected")
		}
		if _, err := os.Stat(filepath.Join(base, "escape.tf")); !os.IsNotExist(err) {
			t.Fatal("outside file should not exist")
		}
	})

	t.Run("symlink rejected", func(t *testing.T) {
		dir := t.TempDir()
		data := buildTarGz(t, []tfFile{{name: "link.tf", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"}})
		if err := SecureExtractTarGz(data, dir); err == nil {
			t.Fatal("expected symlink extraction error")
		}
	})

	t.Run("duplicate path rejected", func(t *testing.T) {
		dir := t.TempDir()
		data := buildTarGz(t, []tfFile{
			{name: "main.tf", body: validHCL},
			{name: "main.tf", body: validHCL},
		})
		if err := SecureExtractTarGz(data, dir); err == nil {
			t.Fatal("expected duplicate file error")
		}
	})

	t.Run("disallowed executable rejected", func(t *testing.T) {
		dir := t.TempDir()
		data := buildTarGz(t, []tfFile{{name: "run.sh", body: "#!/bin/sh\nexit 0", typeflag: 'x'}})
		if err := SecureExtractTarGz(data, dir); err == nil {
			t.Fatal("expected executable permission rejection")
		}
	})

	tests := []struct {
		name    string
		content string
	}{
		{"provider source hashicorp/aws rejected", `
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "0.123.0"
    }
  }
}`},
		{"provider constraint >= rejected", `
terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.123.0"
    }
  }
}`},
		{"local-exec provisioner rejected", fmt.Sprintf(`%s
resource "null_resource" "test" {
  provisioner "local-exec" {
    command = "echo hello"
  }
}`, validHCL)},
		{"remote backend rejected", fmt.Sprintf(`%s
terraform {
  backend "s3" {
    bucket = "mybucket"
  }
}`, validHCL)},
		{"module block rejected", fmt.Sprintf(`%s
module "nested" {
  source = "./child"
}`, validHCL)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(tc.content), 0644); err != nil {
				t.Fatalf("failed to setup test file: %v", err)
			}
			if err := ValidateConfiguration(dir); err == nil {
				t.Fatalf("expected validation failure for: %s", tc.name)
			}
		})
	}
}
