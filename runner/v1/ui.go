package v1

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const UIFormatVersion = "dbminer.runner.ui.v1alpha1"

var uiFieldKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,127}$`)

// UIManifest contains presentation metadata only. Execution continues to use
// the package manifest and configuration schema as its source of truth.
type UIManifest struct {
	FormatVersion string          `json:"formatVersion"`
	Name          string          `json:"name"`
	Group         string          `json:"group"`
	Order         int             `json:"order,omitempty"`
	Glyph         string          `json:"glyph,omitempty"`
	Fields        []UIConfigField `json:"fields"`
}

type UIConfigField struct {
	Key         string              `json:"key"`
	Label       string              `json:"label"`
	Kind        string              `json:"kind"`
	Placeholder string              `json:"placeholder,omitempty"`
	Help        []string            `json:"help,omitempty"`
	Options     []UIConfigOption    `json:"options,omitempty"`
	VisibleWhen []UIVisibleWhenRule `json:"visibleWhen,omitempty"`
	Editor      *UIConfigEditor     `json:"editor,omitempty"`
}

// UIConfigEditor describes an optional structured editor for an underlying
// string configuration value. The runner continues to receive the serialized
// string, so editor metadata has no effect on execution contracts.
type UIConfigEditor struct {
	Kind          string   `json:"kind"`
	Types         []string `json:"types,omitempty"`
	DefaultType   string   `json:"defaultType,omitempty"`
	AllowBareName bool     `json:"allowBareName,omitempty"`
}

type UIConfigOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type UIVisibleWhenRule struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

func (m UIManifest) Validate() error {
	if m.FormatVersion != UIFormatVersion {
		return fmt.Errorf("unsupported runner UI format %q", m.FormatVersion)
	}
	if strings.TrimSpace(m.Name) == "" || len(m.Name) > 200 {
		return errors.New("runner UI name must contain 1 to 200 characters")
	}
	if strings.TrimSpace(m.Group) == "" || len(m.Group) > 100 {
		return errors.New("runner UI group must contain 1 to 100 characters")
	}
	if strings.TrimSpace(m.Glyph) == "" || len([]rune(m.Glyph)) > 4 {
		return errors.New("runner UI glyph must contain 1 to 4 characters")
	}
	if m.Order < 0 || m.Order > 1_000_000 {
		return errors.New("runner UI order must be between 0 and 1000000")
	}
	if len(m.Fields) > 1_000 {
		return errors.New("runner UI has more than 1000 fields")
	}
	seen := make(map[string]struct{}, len(m.Fields))
	for _, field := range m.Fields {
		if !uiFieldKeyPattern.MatchString(field.Key) {
			return fmt.Errorf("runner UI has invalid field key %q", field.Key)
		}
		if _, exists := seen[field.Key]; exists {
			return fmt.Errorf("runner UI repeats field %q", field.Key)
		}
		seen[field.Key] = struct{}{}
		if strings.TrimSpace(field.Label) == "" || len(field.Label) > 200 {
			return fmt.Errorf("runner UI field %q has an invalid label", field.Key)
		}
		if len(field.Placeholder) > 1_000 || len(field.Help) > 16 || len(field.Options) > 1_000 || len(field.VisibleWhen) > 32 {
			return fmt.Errorf("runner UI field %q exceeds presentation metadata limits", field.Key)
		}
		for _, help := range field.Help {
			if strings.TrimSpace(help) == "" || len(help) > 2_000 {
				return fmt.Errorf("runner UI field %q has invalid help text", field.Key)
			}
		}
		switch field.Kind {
		case "text", "url", "select", "textarea":
		default:
			return fmt.Errorf("runner UI field %q has invalid kind %q", field.Key, field.Kind)
		}
		options := make(map[string]struct{}, len(field.Options))
		for _, option := range field.Options {
			if option.Value == "" || len(option.Value) > 1_000 || strings.TrimSpace(option.Label) == "" || len(option.Label) > 200 {
				return fmt.Errorf("runner UI field %q has an invalid option", field.Key)
			}
			if _, exists := options[option.Value]; exists {
				return fmt.Errorf("runner UI field %q repeats option %q", field.Key, option.Value)
			}
			options[option.Value] = struct{}{}
		}
		if field.Kind == "select" && len(field.Options) == 0 {
			return fmt.Errorf("runner UI select field %q requires options", field.Key)
		}
		if field.Editor != nil {
			if field.Kind != "text" {
				return fmt.Errorf("runner UI field %q structured editor requires text kind", field.Key)
			}
			if field.Editor.Kind != "column-schema" {
				return fmt.Errorf("runner UI field %q has invalid editor kind %q", field.Key, field.Editor.Kind)
			}
			if len(field.Editor.Types) == 0 || len(field.Editor.Types) > 100 {
				return fmt.Errorf("runner UI field %q column-schema editor requires 1 to 100 types", field.Key)
			}
			types := make(map[string]struct{}, len(field.Editor.Types))
			for _, typeName := range field.Editor.Types {
				if strings.TrimSpace(typeName) == "" || len(typeName) > 100 {
					return fmt.Errorf("runner UI field %q has an invalid editor type", field.Key)
				}
				if _, exists := types[typeName]; exists {
					return fmt.Errorf("runner UI field %q repeats editor type %q", field.Key, typeName)
				}
				types[typeName] = struct{}{}
			}
			if _, exists := types[field.Editor.DefaultType]; !exists {
				return fmt.Errorf("runner UI field %q editor default type %q is not listed", field.Key, field.Editor.DefaultType)
			}
		}
	}
	for _, field := range m.Fields {
		controllers := make(map[string]struct{}, len(field.VisibleWhen))
		for _, rule := range field.VisibleWhen {
			if _, exists := seen[rule.Key]; !exists || len(rule.Values) == 0 || len(rule.Values) > 1_000 {
				return fmt.Errorf("runner UI field %q has invalid visibility controller %q", field.Key, rule.Key)
			}
			if _, exists := controllers[rule.Key]; exists {
				return fmt.Errorf("runner UI field %q repeats visibility controller %q", field.Key, rule.Key)
			}
			controllers[rule.Key] = struct{}{}
			values := make(map[string]struct{}, len(rule.Values))
			for _, value := range rule.Values {
				if value == "" || len(value) > 1_000 {
					return fmt.Errorf("runner UI field %q has an invalid visibility value", field.Key)
				}
				if _, exists := values[value]; exists {
					return fmt.Errorf("runner UI field %q repeats visibility value %q", field.Key, value)
				}
				values[value] = struct{}{}
			}
		}
	}
	return nil
}
