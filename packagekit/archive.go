package runnerpackage

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	runnerv1 "github.com/nodima-studio/nodima-sdk/runner/v1"
)

const (
	RunnerArchiveMediaType       = "application/vnd.nodima.runner+zip"
	LegacyRunnerArchiveMediaType = "application/vnd.dbminer.runner+zip"
)

var deterministicArchiveTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// ArchiveDirectory writes a deterministic, single-root runner archive from a
// verified package directory. Existing output is never overwritten.
func ArchiveDirectory(ctx context.Context, directory, output string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	pkg, err := LoadDirectory(directory)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(output); err == nil {
		return fmt.Errorf("runner archive output %q already exists", output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(output), ".nodima-runner-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()

	zw := zip.NewWriter(temporary)
	paths := make([]string, 0, len(pkg.Manifest.Files)+1)
	paths = append(paths, ManifestFilename)
	for name := range pkg.Manifest.Files {
		paths = append(paths, name)
	}
	sort.Strings(paths[1:])
	for _, name := range paths {
		if err := ctx.Err(); err != nil {
			_ = zw.Close()
			_ = temporary.Close()
			return err
		}
		data, err := os.ReadFile(filepath.Join(pkg.Root, filepath.FromSlash(name)))
		if err != nil {
			_ = zw.Close()
			_ = temporary.Close()
			return err
		}
		header := &zip.FileHeader{Name: name, Method: zip.Store, Modified: deterministicArchiveTime}
		header.SetMode(0o600)
		entry, err := zw.CreateHeader(header)
		if err != nil {
			_ = zw.Close()
			_ = temporary.Close()
			return err
		}
		if _, err := entry.Write(data); err != nil {
			_ = zw.Close()
			_ = temporary.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, output); err != nil {
		return err
	}
	return nil
}

// LoadArchive validates a runner ZIP with the same checks as a directory.
func LoadArchive(archivePath string) (*Package, error) {
	info, err := os.Stat(archivePath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxPackageBytes {
		return nil, fmt.Errorf("runner archive exceeds %d-byte limit", MaxPackageBytes)
	}
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open runner archive: %w", err)
	}
	defer archive.Close()
	if len(archive.File) < 3 || len(archive.File) > 33 {
		return nil, errors.New("runner archive has an invalid entry count")
	}
	root, err := os.MkdirTemp("", "nodima-runner-archive-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	seen := make(map[string]struct{}, len(archive.File))
	var total uint64
	for _, entry := range archive.File {
		if entry.Name != ManifestFilename {
			if err := runnerv1.ValidatePackagePath(entry.Name); err != nil {
				return nil, fmt.Errorf("invalid runner archive entry %q: %w", entry.Name, err)
			}
		}
		if entry.FileInfo().IsDir() || entry.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("runner archive entry %q must be a regular file", entry.Name)
		}
		if _, exists := seen[entry.Name]; exists {
			return nil, fmt.Errorf("runner archive repeats entry %q", entry.Name)
		}
		seen[entry.Name] = struct{}{}
		if entry.UncompressedSize64 > MaxPackageBytes-total {
			return nil, fmt.Errorf("runner archive content exceeds %d-byte limit", MaxPackageBytes)
		}
		total += entry.UncompressedSize64
		reader, err := entry.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, int64(entry.UncompressedSize64)+1))
		closeErr := reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if uint64(len(data)) != entry.UncompressedSize64 {
			return nil, fmt.Errorf("runner archive entry %q changed size while reading", entry.Name)
		}
		destination := filepath.Join(root, filepath.FromSlash(entry.Name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(destination, data, 0o600); err != nil {
			return nil, err
		}
	}
	pkg, err := LoadDirectory(root)
	if err != nil {
		return nil, err
	}
	if len(seen) != len(pkg.Manifest.Files)+1 {
		return nil, errors.New("runner archive contains undeclared files")
	}
	for name := range seen {
		if name == ManifestFilename {
			continue
		}
		if _, declared := pkg.Manifest.Files[name]; !declared {
			return nil, fmt.Errorf("runner archive file %q is not declared by the manifest", name)
		}
	}
	pkg.Root = ""
	return pkg, nil
}
