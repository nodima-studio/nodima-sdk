package runnerpackage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	runnerv1 "github.com/nodima-studio/nodima-sdk/runner/v1"
)

type GoBuildOptions struct {
	ManifestTemplate string
	Source           string
	OutputDirectory  string
	WorkingDirectory string
	GoBinary         string
}

func BuildGo(ctx context.Context, options GoBuildOptions) (*Package, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.ManifestTemplate == "" {
		return nil, errors.New("manifest template path is required")
	}
	if options.Source == "" {
		return nil, errors.New("Go package source is required")
	}
	if options.OutputDirectory == "" {
		return nil, errors.New("output directory is required")
	}
	if options.GoBinary == "" {
		options.GoBinary = "go"
	}
	if options.WorkingDirectory == "" {
		options.WorkingDirectory = "."
	}

	templatePath, err := filepath.Abs(options.ManifestTemplate)
	if err != nil {
		return nil, fmt.Errorf("resolve manifest template: %w", err)
	}
	manifest, err := loadManifestTemplate(templatePath)
	if err != nil {
		return nil, err
	}

	outputPath, err := filepath.Abs(options.OutputDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve package output directory: %w", err)
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return nil, fmt.Errorf("package output %q already exists", outputPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect package output: %w", err)
	}

	outputParent := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputParent, 0o755); err != nil {
		return nil, fmt.Errorf("create package output parent: %w", err)
	}
	temporaryRoot, err := os.MkdirTemp(outputParent, ".nodima-runner-build-*")
	if err != nil {
		return nil, fmt.Errorf("create temporary package directory: %w", err)
	}
	defer func() {
		if temporaryRoot != "" {
			_ = os.RemoveAll(temporaryRoot)
		}
	}()

	entrypointPath, err := prepareOutputPath(temporaryRoot, manifest.Entrypoint)
	if err != nil {
		return nil, fmt.Errorf("prepare package entrypoint: %w", err)
	}
	command := exec.CommandContext(
		ctx,
		options.GoBinary,
		"build",
		"-mod=readonly",
		"-trimpath",
		"-buildvcs=false",
		"-ldflags=-buildid=",
		"-o",
		entrypointPath,
		options.Source,
	)
	command.Dir = options.WorkingDirectory
	command.Env = append(
		os.Environ(),
		"GOOS=wasip1",
		"GOARCH=wasm",
		"CGO_ENABLED=0",
	)
	buildOutput, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf(
			"compile Go WASI runner: %w%s",
			err,
			formatCommandOutput(buildOutput),
		)
	}
	if err := os.Chmod(entrypointPath, 0o644); err != nil {
		return nil, fmt.Errorf("normalize runner module permissions: %w", err)
	}

	templateRootPath := filepath.Dir(templatePath)
	templateRoot, err := os.OpenRoot(templateRootPath)
	if err != nil {
		return nil, fmt.Errorf("open manifest template root: %w", err)
	}
	defer templateRoot.Close()

	assetPaths := []string{manifest.ConfigSchema}
	if manifest.Icon != "" {
		assetPaths = append(assetPaths, manifest.Icon)
	}
	if manifest.UI != "" {
		assetPaths = append(assetPaths, manifest.UI)
	}
	if manifest.Readme != "" {
		assetPaths = append(assetPaths, manifest.Readme)
	}
	for _, assetPath := range assetPaths {
		limit := uint64(MaxAssetBytes)
		if assetPath == manifest.ConfigSchema {
			limit = MaxConfigSchemaBytes
		} else if assetPath == manifest.UI {
			limit = MaxUIBytes
		} else if assetPath == manifest.Readme {
			limit = MaxReadmeBytes
		}
		data, err := readRegularFile(templateRoot, assetPath, limit)
		if err != nil {
			return nil, fmt.Errorf("read package source asset %q: %w", assetPath, err)
		}
		destination, err := prepareOutputPath(temporaryRoot, assetPath)
		if err != nil {
			return nil, fmt.Errorf("prepare package asset %q: %w", assetPath, err)
		}
		if err := os.WriteFile(destination, data, 0o644); err != nil {
			return nil, fmt.Errorf("write package asset %q: %w", assetPath, err)
		}
	}

	manifest.Files = make(map[string]runnerv1.FileIntegrity, 1+len(assetPaths))
	for _, filePath := range append([]string{manifest.Entrypoint}, assetPaths...) {
		data, err := os.ReadFile(filepath.Join(temporaryRoot, filepath.FromSlash(filePath)))
		if err != nil {
			return nil, fmt.Errorf("read built package file %q: %w", filePath, err)
		}
		sum := sha256.Sum256(data)
		manifest.Files[filePath] = runnerv1.FileIntegrity{
			SHA256: hex.EncodeToString(sum[:]),
		}
	}
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("validate completed runner manifest: %w", err)
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode completed runner manifest: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := os.WriteFile(
		filepath.Join(temporaryRoot, ManifestFilename),
		manifestBytes,
		0o644,
	); err != nil {
		return nil, fmt.Errorf("write completed runner manifest: %w", err)
	}

	if _, err := LoadDirectory(temporaryRoot); err != nil {
		return nil, fmt.Errorf("validate completed runner package: %w", err)
	}
	if err := os.Rename(temporaryRoot, outputPath); err != nil {
		return nil, fmt.Errorf("publish completed runner package: %w", err)
	}
	temporaryRoot = ""

	return LoadDirectory(outputPath)
}

func loadManifestTemplate(templatePath string) (runnerv1.Manifest, error) {
	info, err := os.Lstat(templatePath)
	if err != nil {
		return runnerv1.Manifest{}, fmt.Errorf("inspect manifest template: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return runnerv1.Manifest{}, errors.New("manifest template must be a regular file, not a symlink")
	}
	if info.Size() > MaxManifestBytes {
		return runnerv1.Manifest{}, fmt.Errorf(
			"manifest template is %d bytes, limit is %d",
			info.Size(),
			MaxManifestBytes,
		)
	}
	data, err := os.ReadFile(templatePath)
	if err != nil {
		return runnerv1.Manifest{}, fmt.Errorf("read manifest template: %w", err)
	}
	manifest, err := decodeManifest(data)
	if err != nil {
		return runnerv1.Manifest{}, fmt.Errorf("decode manifest template: %w", err)
	}
	if len(manifest.Files) != 0 {
		return runnerv1.Manifest{}, errors.New(
			"manifest template files must be omitted; the builder generates checksums",
		)
	}

	placeholder := strings.Repeat("0", 64)
	manifest.Files = map[string]runnerv1.FileIntegrity{
		manifest.Entrypoint:   {SHA256: placeholder},
		manifest.ConfigSchema: {SHA256: placeholder},
	}
	if manifest.Icon != "" {
		manifest.Files[manifest.Icon] = runnerv1.FileIntegrity{SHA256: placeholder}
	}
	if manifest.UI != "" {
		manifest.Files[manifest.UI] = runnerv1.FileIntegrity{SHA256: placeholder}
	}
	if manifest.Readme != "" {
		manifest.Files[manifest.Readme] = runnerv1.FileIntegrity{SHA256: placeholder}
	}
	if err := manifest.Validate(); err != nil {
		return runnerv1.Manifest{}, fmt.Errorf("validate manifest template: %w", err)
	}
	manifest.Files = nil
	return manifest, nil
}

func prepareOutputPath(root, packagePath string) (string, error) {
	if err := runnerv1.ValidatePackagePath(packagePath); err != nil {
		return "", err
	}
	destination := filepath.Join(root, filepath.FromSlash(packagePath))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", err
	}
	return destination, nil
}

func formatCommandOutput(output []byte) string {
	const maximum = 16 << 10
	if len(output) == 0 {
		return ""
	}
	if len(output) > maximum {
		output = output[len(output)-maximum:]
		return "\n... compiler output truncated ...\n" + string(output)
	}
	return "\n" + string(output)
}
