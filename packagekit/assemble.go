package runnerpackage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	runnerv1 "github.com/nodima-studio/nodima-sdk/runner/v1"
)

// Assemble creates a verified package directory from an already-built Wasm or
// JavaScript entrypoint and the assets beside a manifest template.
func Assemble(ctx context.Context, manifestTemplate, entrypoint, output string) (*Package, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if manifestTemplate == "" || entrypoint == "" || output == "" {
		return nil, errors.New("manifest, entrypoint, and output are required")
	}
	templatePath, err := filepath.Abs(manifestTemplate)
	if err != nil {
		return nil, err
	}
	manifest, err := loadManifestTemplate(templatePath)
	if err != nil {
		return nil, err
	}
	entrypointPath, err := filepath.Abs(entrypoint)
	if err != nil {
		return nil, err
	}
	entrypointInfo, err := os.Lstat(entrypointPath)
	if err != nil {
		return nil, fmt.Errorf("inspect package entrypoint: %w", err)
	}
	if !entrypointInfo.Mode().IsRegular() || entrypointInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("package entrypoint must be a regular file, not a symlink")
	}
	outputPath, err := filepath.Abs(output)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return nil, fmt.Errorf("package output %q already exists", outputPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return nil, err
	}
	temporary, err := os.MkdirTemp(filepath.Dir(outputPath), ".nodima-runner-assemble-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(temporary)

	paths := []string{manifest.ConfigSchema}
	for _, optional := range []string{manifest.Icon, manifest.UI, manifest.Readme} {
		if optional != "" {
			paths = append(paths, optional)
		}
	}
	entrypointData, err := os.ReadFile(entrypointPath)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{manifest.Entrypoint: entrypointData}
	templateRoot := filepath.Dir(templatePath)
	for _, name := range paths {
		data, err := os.ReadFile(filepath.Join(templateRoot, filepath.FromSlash(name)))
		if err != nil {
			return nil, fmt.Errorf("read package source asset %q: %w", name, err)
		}
		files[name] = data
	}
	manifest.Files = make(map[string]runnerv1.FileIntegrity, len(files))
	for name, data := range files {
		destination, err := prepareOutputPath(temporary, name)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(destination, data, 0o644); err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		manifest.Files[name] = runnerv1.FileIntegrity{SHA256: hex.EncodeToString(sum[:])}
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(filepath.Join(temporary, ManifestFilename), encoded, 0o644); err != nil {
		return nil, err
	}
	if _, err := LoadDirectory(temporary); err != nil {
		return nil, fmt.Errorf("validate assembled runner package: %w", err)
	}
	if err := os.Rename(temporary, outputPath); err != nil {
		return nil, err
	}
	return LoadDirectory(outputPath)
}
