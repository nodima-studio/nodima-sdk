package runnerpackage_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runnerpackage "github.com/nodima-studio/nodima-sdk/packagekit"
	runnerv1 "github.com/nodima-studio/nodima-sdk/runner/v1"
)

func TestLoadDirectoryVerifiesPackage(t *testing.T) {
	root := writeValidPackage(t)

	loaded, err := runnerpackage.LoadDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Manifest.ID != "com.dbminer.pick-columns" {
		t.Fatalf("package ID = %q", loaded.Manifest.ID)
	}
	if len(loaded.Module) != 8 {
		t.Fatalf("module length = %d, want 8", len(loaded.Module))
	}
	if string(loaded.ConfigSchema) != `{"type":"object","additionalProperties":false}` {
		t.Fatalf("config schema = %s", loaded.ConfigSchema)
	}
}

func TestLoadDirectoryRejectsTamperedFile(t *testing.T) {
	root := writeValidPackage(t)
	if err := os.WriteFile(filepath.Join(root, "runner.wasm"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := runnerpackage.LoadDirectory(root)
	if err == nil || !strings.Contains(err.Error(), "SHA-256 verification") {
		t.Fatalf("LoadDirectory() error = %v, want integrity error", err)
	}
}

func TestLoadDirectoryRejectsUnknownManifestField(t *testing.T) {
	root := writeValidPackage(t)
	manifestPath := filepath.Join(root, runnerpackage.ManifestFilename)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["surprise"] = true
	writeJSON(t, manifestPath, document)

	_, err = runnerpackage.LoadDirectory(root)
	if err == nil || !strings.Contains(err.Error(), `unknown field "surprise"`) {
		t.Fatalf("LoadDirectory() error = %v, want unknown-field error", err)
	}
}

func TestLoadDirectoryRejectsInvalidWasmHeader(t *testing.T) {
	root := writeValidPackageWithModule(t, []byte("not wasm"))

	_, err := runnerpackage.LoadDirectory(root)
	if err == nil || !strings.Contains(err.Error(), "not a WebAssembly 1.0 module") {
		t.Fatalf("LoadDirectory() error = %v, want Wasm-header error", err)
	}
}

func TestLoadDirectoryAcceptsJavaScriptAndRejectsInvalidSource(t *testing.T) {
	root := t.TempDir()
	script := []byte(`function process(row, config) { row.name = config.prefix + row.name; return row; }`)
	schema := []byte(`{"type":"object","properties":{"prefix":{"type":"string"}},"additionalProperties":false}`)
	if err := os.WriteFile(filepath.Join(root, "runner.js"), script, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.schema.json"), schema, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := testManifest(script, schema)
	manifest.ID = "studio.nodima.javascript-test"
	manifest.Implementation = runnerv1.ImplementationJavaScript
	manifest.Entrypoint = "runner.js"
	manifest.Files = map[string]runnerv1.FileIntegrity{"runner.js": {SHA256: hash(script)}, "config.schema.json": {SHA256: hash(schema)}}
	writeJSON(t, filepath.Join(root, runnerpackage.ManifestFilename), manifest)
	loaded, err := runnerpackage.LoadDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded.Entrypoint) != string(script) || len(loaded.Module) != 0 {
		t.Fatalf("unexpected JavaScript package payload")
	}

	invalid := []byte(`function process(`)
	if err := os.WriteFile(filepath.Join(root, "runner.js"), invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	rewriteIntegrity(t, root, "runner.js", invalid)
	if _, err := runnerpackage.LoadDirectory(root); err == nil || !strings.Contains(err.Error(), "compile runner package JavaScript") {
		t.Fatalf("LoadDirectory() error = %v", err)
	}
}

func TestLoadDirectoryRejectsInvalidConfigSchema(t *testing.T) {
	root := writeValidPackage(t)
	schemaPath := filepath.Join(root, "config.schema.json")
	invalid := []byte("[]")
	if err := os.WriteFile(schemaPath, invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	rewriteIntegrity(t, root, "config.schema.json", invalid)

	_, err := runnerpackage.LoadDirectory(root)
	if err == nil || !strings.Contains(err.Error(), "must be a JSON object") {
		t.Fatalf("LoadDirectory() error = %v, want schema-object error", err)
	}
}

func TestLoadDirectoryRejectsSymlinkedPackageFile(t *testing.T) {
	root := writeValidPackage(t)
	modulePath := filepath.Join(root, "runner.wasm")
	targetPath := filepath.Join(t.TempDir(), "external.wasm")
	module := wasmModule()
	if err := os.WriteFile(targetPath, module, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(modulePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetPath, modulePath); err != nil {
		t.Skipf("cannot create symlink on this platform: %v", err)
	}

	_, err := runnerpackage.LoadDirectory(root)
	if err == nil || !strings.Contains(err.Error(), "symbolic links") {
		t.Fatalf("LoadDirectory() error = %v, want symlink error", err)
	}
}

func TestLoadDirectoryRejectsOversizedManifest(t *testing.T) {
	root := t.TempDir()
	oversized := strings.Repeat(" ", runnerpackage.MaxManifestBytes+1)
	if err := os.WriteFile(
		filepath.Join(root, runnerpackage.ManifestFilename),
		[]byte(oversized),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	_, err := runnerpackage.LoadDirectory(root)
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("LoadDirectory() error = %v, want size-limit error", err)
	}
}

func writeValidPackage(t *testing.T) string {
	t.Helper()
	return writeValidPackageWithModule(t, wasmModule())
}

func writeValidPackageWithModule(t *testing.T, module []byte) string {
	t.Helper()

	root := t.TempDir()
	schema := []byte(`{"type":"object","additionalProperties":false}`)
	if err := os.WriteFile(filepath.Join(root, "runner.wasm"), module, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.schema.json"), schema, 0o600); err != nil {
		t.Fatal(err)
	}

	manifest := testManifest(module, schema)
	writeJSON(t, filepath.Join(root, runnerpackage.ManifestFilename), manifest)
	return root
}

func testManifest(module, schema []byte) runnerv1.Manifest {
	return runnerv1.Manifest{
		FormatVersion:  runnerv1.PackageFormatVersion,
		ID:             "com.dbminer.pick-columns",
		Version:        "0.1.0",
		ABI:            runnerv1.ABIVersion,
		Implementation: runnerv1.ImplementationWasm,
		Entrypoint:     "runner.wasm",
		ConfigSchema:   "config.schema.json",
		Behavior:       runnerv1.BehaviorStreaming,
		Ports: []runnerv1.Port{
			{ID: "input", Direction: runnerv1.PortInput, Required: true},
			{ID: "output", Direction: runnerv1.PortOutput, Required: true},
		},
		Capabilities: []runnerv1.Capability{},
		Limits: runnerv1.PackageLimits{
			MemoryBytes:       256 << 20,
			WallTimeMillis:    300_000,
			MaxOutputBytes:    256 << 20,
			MaxOutputMessages: 100_000,
			StderrBytes:       64 << 10,
		},
		Files: map[string]runnerv1.FileIntegrity{
			"runner.wasm":        {SHA256: hash(module)},
			"config.schema.json": {SHA256: hash(schema)},
		},
	}
}

func rewriteIntegrity(t *testing.T, root, filePath string, data []byte) {
	t.Helper()

	manifestPath := filepath.Join(root, runnerpackage.ManifestFilename)
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest runnerv1.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Files[filePath] = runnerv1.FileIntegrity{SHA256: hash(data)}
	writeJSON(t, manifestPath, manifest)
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func wasmModule() []byte {
	return []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
}
