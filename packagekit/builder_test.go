package runnerpackage_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	runnerpackage "github.com/nodima-studio/nodima-sdk/packagekit"
)

func TestBuildGoCreatesReproducibleVerifiedPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test compiles a Go WASI guest")
	}

	repositoryRoot := repositoryRoot(t)
	templatePath := writeBuildTemplate(t)
	outputParent := t.TempDir()

	first, err := runnerpackage.BuildGo(context.Background(), runnerpackage.GoBuildOptions{
		ManifestTemplate: templatePath,
		Source:           "./packagekit/testdata/guest",
		OutputDirectory:  filepath.Join(outputParent, "first"),
		WorkingDirectory: repositoryRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := runnerpackage.BuildGo(context.Background(), runnerpackage.GoBuildOptions{
		ManifestTemplate: templatePath,
		Source:           "./packagekit/testdata/guest",
		OutputDirectory:  filepath.Join(outputParent, "second"),
		WorkingDirectory: repositoryRoot,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first.Module, second.Module) {
		t.Fatal("two equivalent builds produced different Wasm modules")
	}
	firstManifest, err := os.ReadFile(filepath.Join(first.Root, runnerpackage.ManifestFilename))
	if err != nil {
		t.Fatal(err)
	}
	secondManifest, err := os.ReadFile(filepath.Join(second.Root, runnerpackage.ManifestFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstManifest, secondManifest) {
		t.Fatal("two equivalent builds produced different manifests")
	}
	if got := first.Manifest.Files[first.Manifest.Entrypoint].SHA256; got != hash(first.Module) {
		t.Fatalf("module checksum = %q, want %q", got, hash(first.Module))
	}
}

func TestBuildGoDoesNotOverwriteDestination(t *testing.T) {
	output := filepath.Join(t.TempDir(), "existing")
	if err := os.Mkdir(output, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(output, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := runnerpackage.BuildGo(context.Background(), runnerpackage.GoBuildOptions{
		ManifestTemplate: writeBuildTemplate(t),
		Source:           "./packagekit/testdata/guest",
		OutputDirectory:  output,
		WorkingDirectory: repositoryRoot(t),
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("BuildGo() error = %v, want destination-exists error", err)
	}
	data, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(data) != "keep" {
		t.Fatalf("existing destination was modified: data %q, error %v", data, readErr)
	}
}

func TestBuildGoCleansFailedTemporaryPackage(t *testing.T) {
	parent := t.TempDir()
	output := filepath.Join(parent, "failed")
	_, err := runnerpackage.BuildGo(context.Background(), runnerpackage.GoBuildOptions{
		ManifestTemplate: writeBuildTemplate(t),
		Source:           "./packagekit/testdata/does-not-exist",
		OutputDirectory:  output,
		WorkingDirectory: repositoryRoot(t),
	})
	if err == nil || !strings.Contains(err.Error(), "compile Go WASI runner") {
		t.Fatalf("BuildGo() error = %v, want compilation error", err)
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Fatalf("failed build left output directory: %v", statErr)
	}
	temporary, globErr := filepath.Glob(filepath.Join(parent, ".nodima-runner-build-*"))
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(temporary) != 0 {
		t.Fatalf("failed build left temporary directories: %v", temporary)
	}
}

func TestBuildGoRejectsChecksumsInTemplate(t *testing.T) {
	templatePath := writeBuildTemplate(t)
	schema := []byte(`{"type":"object"}`)
	manifest := testManifest(wasmModule(), schema)
	writeJSON(t, templatePath, manifest)

	_, err := runnerpackage.BuildGo(context.Background(), runnerpackage.GoBuildOptions{
		ManifestTemplate: templatePath,
		Source:           "./packagekit/testdata/guest",
		OutputDirectory:  filepath.Join(t.TempDir(), "output"),
		WorkingDirectory: repositoryRoot(t),
	})
	if err == nil || !strings.Contains(err.Error(), "files must be omitted") {
		t.Fatalf("BuildGo() error = %v, want generated-checksum error", err)
	}
}

func writeBuildTemplate(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	schema := []byte(`{"type":"object","additionalProperties":false}`)
	if err := os.WriteFile(filepath.Join(root, "config.schema.json"), schema, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := testManifest(wasmModule(), schema)
	manifest.Files = nil
	templatePath := filepath.Join(root, "package.template.json")
	writeJSON(t, templatePath, manifest)
	return templatePath
}

func repositoryRoot(t *testing.T) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), ".."))
}
