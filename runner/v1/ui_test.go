package v1

import (
	"strings"
	"testing"
)

func TestUIManifestAcceptsColumnSchemaEditor(t *testing.T) {
	manifest := validUIManifest()
	manifest.Fields[0].Editor = &UIConfigEditor{
		Kind: "column-schema", Types: []string{"string", "int64"},
		DefaultType: "string", AllowBareName: true,
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestUIManifestRejectsInvalidColumnSchemaEditor(t *testing.T) {
	for name, editor := range map[string]*UIConfigEditor{
		"unknown kind": {Kind: "unknown", Types: []string{"string"}, DefaultType: "string"},
		"no types":     {Kind: "column-schema", DefaultType: "string"},
		"bad default":  {Kind: "column-schema", Types: []string{"int64"}, DefaultType: "string"},
		"duplicate":    {Kind: "column-schema", Types: []string{"string", "string"}, DefaultType: "string"},
	} {
		t.Run(name, func(t *testing.T) {
			manifest := validUIManifest()
			manifest.Fields[0].Editor = editor
			if err := manifest.Validate(); err == nil || !strings.Contains(err.Error(), "editor") {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestUIManifestRejectsStructuredEditorOnNonTextField(t *testing.T) {
	manifest := validUIManifest()
	manifest.Fields[0].Kind = "textarea"
	manifest.Fields[0].Editor = &UIConfigEditor{
		Kind: "column-schema", Types: []string{"string"}, DefaultType: "string",
	}
	if err := manifest.Validate(); err == nil || !strings.Contains(err.Error(), "requires text kind") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func validUIManifest() UIManifest {
	return UIManifest{
		FormatVersion: UIFormatVersion,
		Name:          "Example",
		Group:         "Transformations",
		Glyph:         "E",
		Fields:        []UIConfigField{{Key: "columns", Label: "Columns", Kind: "text"}},
	}
}
