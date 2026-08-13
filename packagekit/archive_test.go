package runnerpackage_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runnerpackage "github.com/nodima-studio/nodima-sdk/packagekit"
)

func TestArchiveDirectoryIsDeterministicAndRoundTripsUIHelp(t *testing.T) {
	root := writeInstallablePackage(t)
	first := filepath.Join(t.TempDir(), "first.nodima-runner.zip")
	second := filepath.Join(t.TempDir(), "second.nodima-runner.zip")
	if err := runnerpackage.ArchiveDirectory(context.Background(), root, first); err != nil {
		t.Fatal(err)
	}
	if err := runnerpackage.ArchiveDirectory(context.Background(), root, second); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(first)
	b, _ := os.ReadFile(second)
	if !bytes.Equal(a, b) {
		t.Fatal("equivalent runner archives differ")
	}
	loaded, err := runnerpackage.LoadArchive(first)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UI == nil || loaded.UI.Name != "Pick Columns" || loaded.Readme != "# Pick Columns\n" {
		t.Fatalf("loaded assets = %#v, %q", loaded.UI, loaded.Readme)
	}
}

func TestLoadArchiveRejectsTraversalAndDuplicateEntries(t *testing.T) {
	for _, entries := range [][]string{{"../escape"}, {"package.json", "package.json"}} {
		t.Run(strings.Join(entries, "-"), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "attack.zip")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			writer := zip.NewWriter(file)
			for _, name := range entries {
				entry, err := writer.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = entry.Write([]byte("{}"))
			}
			_, _ = writer.Create("runner.wasm")
			_, _ = writer.Create("config.schema.json")
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := runnerpackage.LoadArchive(path); err == nil {
				t.Fatal("LoadArchive accepted hostile entries")
			}
		})
	}
}

func TestLoadDirectoryRejectsInvalidUTF8HelpAndUnknownUIField(t *testing.T) {
	root := writeInstallablePackage(t)
	readme := []byte{0xff, 0xfe}
	if err := os.WriteFile(filepath.Join(root, "README.md"), readme, 0o600); err != nil {
		t.Fatal(err)
	}
	rewriteIntegrity(t, root, "README.md", readme)
	if _, err := runnerpackage.LoadDirectory(root); err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("invalid help error = %v", err)
	}

	root = writeInstallablePackage(t)
	ui := []byte(`{"formatVersion":"dbminer.runner.ui.v1alpha1","name":"Pick Columns","group":"Transform","glyph":"P","fields":[{"key":"missing","label":"Missing","kind":"text"}]}`)
	if err := os.WriteFile(filepath.Join(root, "ui.json"), ui, 0o600); err != nil {
		t.Fatal(err)
	}
	rewriteIntegrity(t, root, "ui.json", ui)
	if _, err := runnerpackage.LoadDirectory(root); err == nil || !strings.Contains(err.Error(), "not present") {
		t.Fatalf("invalid UI error = %v", err)
	}
}

func writeInstallablePackage(t *testing.T) string {
	t.Helper()
	root := writeValidPackage(t)
	readme := []byte("# Pick Columns\n")
	ui := []byte(`{"formatVersion":"dbminer.runner.ui.v1alpha1","name":"Pick Columns","group":"Transform","glyph":"☷","fields":[]}`)
	if err := os.WriteFile(filepath.Join(root, "README.md"), readme, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ui.json"), ui, 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, runnerpackage.ManifestFilename)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["ui"] = "ui.json"
	manifest["readme"] = "README.md"
	files := manifest["files"].(map[string]any)
	files["ui.json"] = map[string]any{"sha256": hash(ui)}
	files["README.md"] = map[string]any{"sha256": hash(readme)}
	writeJSON(t, manifestPath, manifest)
	return root
}
