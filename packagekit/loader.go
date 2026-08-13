// Package runnerpackage loads and verifies distributable runner packages.
package runnerpackage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/dop251/goja"
	runnerv1 "github.com/nodima-studio/nodima-sdk/runner/v1"
)

const (
	ManifestFilename     = "manifest.json"
	MaxManifestBytes     = 256 << 10
	MaxModuleBytes       = 64 << 20
	MaxJavaScriptBytes   = 64 << 10
	MaxConfigSchemaBytes = 1 << 20
	MaxUIBytes           = 1 << 20
	MaxReadmeBytes       = 256 << 10
	MaxAssetBytes        = 4 << 20
	MaxPackageBytes      = 96 << 20
)

var wasmHeader = []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

type Package struct {
	Root         string
	Manifest     runnerv1.Manifest
	Module       []byte
	Entrypoint   []byte
	ConfigSchema json.RawMessage
	UI           *runnerv1.UIManifest
	Readme       string
	Files        map[string][]byte
}

func LoadDirectory(directory string) (*Package, error) {
	absoluteRoot, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("resolve runner package directory: %w", err)
	}
	rootInfo, err := os.Lstat(absoluteRoot)
	if err != nil {
		return nil, fmt.Errorf("inspect runner package directory: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, errors.New("runner package root must be a real directory, not a symlink")
	}
	root, err := os.OpenRoot(absoluteRoot)
	if err != nil {
		return nil, fmt.Errorf("open runner package root: %w", err)
	}
	defer root.Close()

	manifestBytes, err := readRegularFile(
		root,
		ManifestFilename,
		MaxManifestBytes,
	)
	if err != nil {
		return nil, fmt.Errorf("read runner package manifest: %w", err)
	}
	manifest, err := decodeManifest(manifestBytes)
	if err != nil {
		return nil, err
	}
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("validate runner package manifest: %w", err)
	}

	loaded := make(map[string][]byte, len(manifest.Files))
	totalBytes := uint64(len(manifestBytes))
	for filePath, integrity := range manifest.Files {
		if totalBytes >= MaxPackageBytes {
			return nil, fmt.Errorf(
				"runner package content exceeds %d-byte limit",
				MaxPackageBytes,
			)
		}
		limit := uint64(MaxAssetBytes)
		switch filePath {
		case manifest.Entrypoint:
			if manifest.Implementation == runnerv1.ImplementationJavaScript {
				limit = MaxJavaScriptBytes
			} else {
				limit = MaxModuleBytes
			}
		case manifest.ConfigSchema:
			limit = MaxConfigSchemaBytes
		case manifest.UI:
			limit = MaxUIBytes
		case manifest.Readme:
			limit = MaxReadmeBytes
		}
		limit = min(limit, uint64(MaxPackageBytes)-totalBytes)
		data, err := readRegularFile(root, filePath, limit)
		if err != nil {
			return nil, fmt.Errorf("read runner package file %q: %w", filePath, err)
		}
		totalBytes += uint64(len(data))
		if totalBytes > MaxPackageBytes {
			return nil, fmt.Errorf(
				"runner package content exceeds %d-byte limit",
				MaxPackageBytes,
			)
		}
		actualHash := sha256.Sum256(data)
		if hex.EncodeToString(actualHash[:]) != integrity.SHA256 {
			return nil, fmt.Errorf("runner package file %q failed SHA-256 verification", filePath)
		}
		loaded[filePath] = data
	}

	entrypoint := loaded[manifest.Entrypoint]
	var module []byte
	if manifest.Implementation == runnerv1.ImplementationWasm {
		if !bytes.HasPrefix(entrypoint, wasmHeader) {
			return nil, errors.New("runner package entrypoint is not a WebAssembly 1.0 module")
		}
		module = entrypoint
	} else {
		if !utf8.Valid(entrypoint) {
			return nil, errors.New("runner package JavaScript entrypoint must be valid UTF-8")
		}
		if _, err := goja.Compile(manifest.Entrypoint, string(entrypoint), false); err != nil {
			return nil, fmt.Errorf("compile runner package JavaScript entrypoint: %w", err)
		}
	}
	configSchema := loaded[manifest.ConfigSchema]
	if err := validateConfigSchema(configSchema); err != nil {
		return nil, err
	}
	var ui *runnerv1.UIManifest
	if manifest.UI != "" {
		decoded, err := decodeUIManifest(loaded[manifest.UI], configSchema)
		if err != nil {
			return nil, err
		}
		ui = &decoded
	}
	readme := loaded[manifest.Readme]
	if len(readme) > 0 && !utf8.Valid(readme) {
		return nil, errors.New("runner package README must be valid UTF-8")
	}

	return &Package{
		Root:         absoluteRoot,
		Manifest:     manifest,
		Module:       module,
		Entrypoint:   append([]byte(nil), entrypoint...),
		ConfigSchema: append(json.RawMessage(nil), configSchema...),
		UI:           ui,
		Readme:       string(readme),
		Files:        loaded,
	}, nil
}

func decodeUIManifest(data, schemaData []byte) (runnerv1.UIManifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var ui runnerv1.UIManifest
	if err := decoder.Decode(&ui); err != nil {
		return runnerv1.UIManifest{}, fmt.Errorf("decode runner UI metadata: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return runnerv1.UIManifest{}, errors.New("runner UI metadata contains multiple JSON values")
		}
		return runnerv1.UIManifest{}, fmt.Errorf("decode runner UI metadata trailer: %w", err)
	}
	if err := ui.Validate(); err != nil {
		return runnerv1.UIManifest{}, fmt.Errorf("validate runner UI metadata: %w", err)
	}
	var schema struct {
		Properties map[string]struct {
			Type string   `json:"type"`
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		return runnerv1.UIManifest{}, fmt.Errorf("decode runner UI schema fields: %w", err)
	}
	for _, field := range ui.Fields {
		property, exists := schema.Properties[field.Key]
		if !exists {
			return runnerv1.UIManifest{}, fmt.Errorf("runner UI field %q is not present in the configuration schema", field.Key)
		}
		if property.Type != "string" {
			return runnerv1.UIManifest{}, fmt.Errorf("runner UI field %q does not reference a string schema field", field.Key)
		}
		allowed := make(map[string]struct{}, len(property.Enum))
		for _, value := range property.Enum {
			allowed[value] = struct{}{}
		}
		for _, option := range field.Options {
			if _, valid := allowed[option.Value]; len(allowed) == 0 || !valid {
				return runnerv1.UIManifest{}, fmt.Errorf("runner UI field %q option %q is not allowed by the configuration schema", field.Key, option.Value)
			}
		}
		if field.Kind == "select" && len(field.Options) != len(property.Enum) {
			return runnerv1.UIManifest{}, fmt.Errorf("runner UI select field %q must present every schema enum value", field.Key)
		}
		for _, rule := range field.VisibleWhen {
			controller, exists := schema.Properties[rule.Key]
			if !exists {
				return runnerv1.UIManifest{}, fmt.Errorf("runner UI visibility controller %q is not present in the configuration schema", rule.Key)
			}
			allowedValues := make(map[string]struct{}, len(controller.Enum))
			for _, value := range controller.Enum {
				allowedValues[value] = struct{}{}
			}
			for _, value := range rule.Values {
				if _, valid := allowedValues[value]; !valid {
					return runnerv1.UIManifest{}, fmt.Errorf("runner UI visibility value %q is not allowed by controller %q", value, rule.Key)
				}
			}
		}
	}
	if len(ui.Fields) != len(schema.Properties) {
		return runnerv1.UIManifest{}, errors.New("runner UI metadata must present every configuration schema field")
	}
	return ui, nil
}

func decodeManifest(data []byte) (runnerv1.Manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var manifest runnerv1.Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return runnerv1.Manifest{}, fmt.Errorf("decode runner package manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return runnerv1.Manifest{}, errors.New("runner package manifest contains multiple JSON values")
		}
		return runnerv1.Manifest{}, fmt.Errorf("decode runner package manifest trailer: %w", err)
	}
	return manifest, nil
}

func validateConfigSchema(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))

	var document any
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("decode runner config schema: %w", err)
	}
	if _, ok := document.(map[string]any); !ok {
		return errors.New("runner config schema must be a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("runner config schema contains multiple JSON values")
		}
		return fmt.Errorf("decode runner config schema trailer: %w", err)
	}
	return nil
}

func readRegularFile(root *os.Root, packagePath string, maximumBytes uint64) ([]byte, error) {
	if err := runnerv1.ValidatePackagePath(packagePath); err != nil {
		return nil, err
	}

	info, err := root.Lstat(packagePath)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("package files cannot be symbolic links")
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("package file must be a regular file")
	}
	if uint64(info.Size()) > maximumBytes {
		return nil, fmt.Errorf(
			"file is %d bytes, limit is %d",
			info.Size(),
			maximumBytes,
		)
	}

	file, err := root.Open(packagePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !openedInfo.Mode().IsRegular() {
		return nil, errors.New("opened package file must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maximumBytes)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(data)) > maximumBytes {
		return nil, fmt.Errorf("file exceeds %d-byte limit", maximumBytes)
	}
	return data, nil
}
