package runner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	maxCompressedBytes   = 1 * 1024 * 1024 // 1 MiB
	maxUncompressedBytes = 4 * 1024 * 1024 // 4 MiB
	maxFileBytes         = 1 * 1024 * 1024 // 1 MiB
	maxFileCount         = 64
)

// SecureExtractTarGz safely extracts an untrusted gzip-compressed tar archive containing
// OpenTofu configuration files into the specified destination directory.
func SecureExtractTarGz(data []byte, destination string) error {
	if len(data) > maxCompressedBytes {
		return fmt.Errorf("compressed archive size %d exceeds limit of %d bytes", len(data), maxCompressedBytes)
	}

	if destination == "" {
		return errors.New("destination directory path cannot be empty")
	}

	destClean := filepath.Clean(destination)
	destAbs, err := filepath.Abs(destClean)
	if err != nil {
		return fmt.Errorf("resolving absolute path for destination %q: %w", destination, err)
	}

	// Verify destination itself is not a symlink if it already exists.
	if fi, err := os.Lstat(destAbs); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("destination %q is a symbolic link", destAbs)
		}
		if !fi.IsDir() {
			return fmt.Errorf("destination %q exists and is not a directory", destAbs)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(destAbs, 0700); err != nil {
			return fmt.Errorf("creating destination directory %q: %w", destAbs, err)
		}
	} else {
		return fmt.Errorf("checking destination directory %q: %w", destAbs, err)
	}

	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)

	seenPaths := make(map[string]struct{})
	var totalUncompressed int64
	var fileCount int

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		fileCount++
		if fileCount > maxFileCount {
			return fmt.Errorf("archive contains more than %d entries", maxFileCount)
		}

		// Reject any backslashes explicitly.
		if strings.ContainsRune(header.Name, '\\') {
			return fmt.Errorf("entry path %q contains invalid backslash", header.Name)
		}

		// Clean using forward slashes as tar specifications require Unix path separators.
		cleanedTarPath := path.Clean(header.Name)

		// Reject absolute paths, parent directory traversal, and current directory references.
		if path.IsAbs(cleanedTarPath) || strings.HasPrefix(cleanedTarPath, "/") {
			return fmt.Errorf("entry path %q is absolute", header.Name)
		}
		if cleanedTarPath == "." || cleanedTarPath == ".." || strings.HasPrefix(cleanedTarPath, "../") {
			return fmt.Errorf("entry path %q attempts path traversal or references root", header.Name)
		}

		// Reject non-clean paths (e.g. redundant slashes, internal dots/traversals).
		rawTrimmed := strings.TrimSuffix(header.Name, "/")
		if rawTrimmed != cleanedTarPath {
			return fmt.Errorf("entry path %q is not clean (normalized: %q)", header.Name, cleanedTarPath)
		}

		// Ensure no duplicate cleaned paths are extracted.
		if _, exists := seenPaths[cleanedTarPath]; exists {
			return fmt.Errorf("duplicate entry path encountered: %q", cleanedTarPath)
		}
		seenPaths[cleanedTarPath] = struct{}{}

		// Enforce allowed entry types: regular files and directories only.
		switch header.Typeflag {
		case tar.TypeDir:
			// Allowed directory
		case tar.TypeReg, tar.TypeRegA:
			// Allowed regular file; validate file extension/basename rule.
			base := path.Base(cleanedTarPath)
			if !isPermittedFile(base) {
				return fmt.Errorf("entry %q with basename %q is not a permitted OpenTofu file", header.Name, base)
			}
		default:
			return fmt.Errorf("unsupported or disallowed tar entry type %q for entry %q", header.Typeflag, header.Name)
		}

		// Map to target path in OS-specific representation.
		targetPath := filepath.Join(destAbs, filepath.FromSlash(cleanedTarPath))

		// Ensure target path stays strictly inside destAbs.
		relPath, err := filepath.Rel(destAbs, targetPath)
		if err != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
			return fmt.Errorf("entry path %q escapes destination directory", header.Name)
		}

		// Ensure no segment in the path traverses through an existing symlink.
		if err := verifyNoSymlinks(destAbs, filepath.FromSlash(cleanedTarPath)); err != nil {
			return fmt.Errorf("path validation failed for entry %q: %w", header.Name, err)
		}

		if header.Typeflag == tar.TypeDir {
			if fi, err := os.Lstat(targetPath); err == nil {
				if fi.Mode()&os.ModeSymlink != 0 {
					return fmt.Errorf("directory target %q is a symbolic link", targetPath)
				}
				if !fi.IsDir() {
					return fmt.Errorf("directory target %q exists and is not a directory", targetPath)
				}
				if err := os.Chmod(targetPath, 0700); err != nil {
					return fmt.Errorf("setting permissions on directory %q: %w", targetPath, err)
				}
			} else if errors.Is(err, os.ErrNotExist) {
				if err := os.MkdirAll(targetPath, 0700); err != nil {
					return fmt.Errorf("creating directory %q: %w", targetPath, err)
				}
			} else {
				return fmt.Errorf("checking directory target %q: %w", targetPath, err)
			}
			continue
		}

		// Ensure parent directories exist with 0700 permissions.
		parentDir := filepath.Dir(targetPath)
		if err := os.MkdirAll(parentDir, 0700); err != nil {
			return fmt.Errorf("creating parent directory %q for %q: %w", parentDir, header.Name, err)
		}

		// Extract regular file. Check if target exists and verify it is not a symlink.
		if fi, err := os.Lstat(targetPath); err == nil {
			if fi.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("file target %q is a symbolic link", targetPath)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("checking file target %q: %w", targetPath, err)
		}

		outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return fmt.Errorf("creating output file %q: %w", targetPath, err)
		}

		var fileBytes int64
		buf := make([]byte, 32*1024)
		for {
			nr, readErr := tarReader.Read(buf)
			if nr > 0 {
				fileBytes += int64(nr)
				if fileBytes > maxFileBytes {
					outFile.Close()
					return fmt.Errorf("file %q exceeds single-file size limit of %d bytes", header.Name, maxFileBytes)
				}

				totalUncompressed += int64(nr)
				if totalUncompressed > maxUncompressedBytes {
					outFile.Close()
					return fmt.Errorf("total uncompressed archive size exceeds limit of %d bytes", maxUncompressedBytes)
				}

				if _, writeErr := outFile.Write(buf[:nr]); writeErr != nil {
					outFile.Close()
					return fmt.Errorf("writing to output file %q: %w", targetPath, writeErr)
				}
			}

			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					break
				}
				outFile.Close()
				return fmt.Errorf("reading file contents for %q: %w", header.Name, readErr)
			}
		}

		if err := outFile.Chmod(0600); err != nil {
			outFile.Close()
			return fmt.Errorf("setting permissions on output file %q: %w", targetPath, err)
		}

		if err := outFile.Close(); err != nil {
			return fmt.Errorf("closing output file %q: %w", targetPath, err)
		}
	}

	return nil
}

func isPermittedFile(name string) bool {
	if name == ".terraform.lock.hcl" {
		return true
	}
	if strings.HasSuffix(name, ".tf") {
		return true
	}
	if strings.HasSuffix(name, ".tf.json") {
		return true
	}
	if strings.HasSuffix(name, ".tfvars") {
		return true
	}
	return false
}

func verifyNoSymlinks(baseDir, relativePath string) error {
	parts := strings.Split(relativePath, string(filepath.Separator))
	curr := baseDir
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		curr = filepath.Join(curr, part)
		fi, err := os.Lstat(curr)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// Path does not exist yet; safe to proceed.
				return nil
			}
			return fmt.Errorf("stat %q: %w", curr, err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path component %q is a symbolic link", curr)
		}
	}
	return nil
}
