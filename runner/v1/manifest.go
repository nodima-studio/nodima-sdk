package v1

import (
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

const PackageFormatVersion = "dbminer.runner.package.v1alpha1"

type ImplementationKind string

const (
	ImplementationWasm       ImplementationKind = "wasm"
	ImplementationJavaScript ImplementationKind = "javascript"
	ImplementationNative     ImplementationKind = "native"
)

type ExecutionBehavior string

const (
	BehaviorStreaming ExecutionBehavior = "streaming"
	BehaviorBlocking  ExecutionBehavior = "blocking"
	BehaviorSpilling  ExecutionBehavior = "spilling"
)

type DistributionMode string

const (
	DistributionSingleton     DistributionMode = "singleton"
	DistributionBatchParallel DistributionMode = "batch-parallel"
)

type Distribution struct {
	Mode DistributionMode `json:"mode"`
}

type PortDirection string

const (
	PortInput  PortDirection = "input"
	PortOutput PortDirection = "output"
)

type Capability string

const (
	CapabilityHTTP      Capability = "http"
	CapabilityFileRead  Capability = "file-read"
	CapabilityFileWrite Capability = "file-write"
	CapabilityScratch   Capability = "scratch"
	CapabilitySecret    Capability = "secret"
)

type Manifest struct {
	FormatVersion  string                   `json:"formatVersion"`
	ID             string                   `json:"id"`
	Version        string                   `json:"version"`
	ABI            string                   `json:"abi"`
	Implementation ImplementationKind       `json:"implementation"`
	Entrypoint     string                   `json:"entrypoint"`
	ConfigSchema   string                   `json:"configSchema"`
	Icon           string                   `json:"icon,omitempty"`
	UI             string                   `json:"ui,omitempty"`
	Readme         string                   `json:"readme,omitempty"`
	Behavior       ExecutionBehavior        `json:"behavior"`
	Distribution   *Distribution            `json:"distribution,omitempty"`
	Ports          []Port                   `json:"ports"`
	Capabilities   []Capability             `json:"capabilities"`
	Limits         PackageLimits            `json:"limits"`
	Files          map[string]FileIntegrity `json:"files"`
}

type Port struct {
	ID        string        `json:"id"`
	Direction PortDirection `json:"direction"`
	Required  bool          `json:"required"`
}

type PackageLimits struct {
	MemoryBytes       uint64 `json:"memoryBytes"`
	WallTimeMillis    uint64 `json:"wallTimeMillis"`
	MaxOutputBytes    uint64 `json:"maxOutputBytes"`
	MaxOutputMessages uint64 `json:"maxOutputMessages"`
	StderrBytes       uint64 `json:"stderrBytes"`
}

type FileIntegrity struct {
	SHA256 string `json:"sha256"`
}

var (
	packageIDPattern = regexp.MustCompile(
		`^[a-z](?:[a-z0-9-]*[a-z0-9])?` +
			`(?:\.[a-z](?:[a-z0-9-]*[a-z0-9])?)+$`,
	)
	semanticVersionPattern = regexp.MustCompile(
		`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
			`(?:-(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)` +
			`(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?` +
			`(?:\+(?:[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`,
	)
	portIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
)

func ValidatePackageID(value string) error {
	if !packageIDPattern.MatchString(value) {
		return fmt.Errorf("invalid runner package ID %q", value)
	}
	return nil
}

func ValidateSemanticVersion(value string) error {
	if !semanticVersionPattern.MatchString(value) {
		return fmt.Errorf("invalid semantic version %q", value)
	}
	return nil
}

func (m Manifest) Validate() error {
	if m.FormatVersion != PackageFormatVersion {
		return fmt.Errorf("unsupported runner package format %q", m.FormatVersion)
	}
	if err := ValidatePackageID(m.ID); err != nil {
		return err
	}
	if err := ValidateSemanticVersion(m.Version); err != nil {
		return err
	}
	if m.ABI != ABIVersion {
		return fmt.Errorf("unsupported runner ABI %q", m.ABI)
	}
	if m.Implementation != ImplementationWasm && m.Implementation != ImplementationJavaScript {
		return fmt.Errorf(
			"loadable runner package implementation must be %q or %q, got %q",
			ImplementationWasm, ImplementationJavaScript,
			m.Implementation,
		)
	}
	if err := ValidatePackagePath(m.Entrypoint); err != nil {
		return fmt.Errorf("invalid entrypoint: %w", err)
	}
	if err := ValidatePackagePath(m.ConfigSchema); err != nil {
		return fmt.Errorf("invalid config schema path: %w", err)
	}
	if m.Entrypoint == m.ConfigSchema {
		return errors.New("entrypoint and config schema must be different files")
	}
	if m.Icon != "" {
		if err := ValidatePackagePath(m.Icon); err != nil {
			return fmt.Errorf("invalid icon path: %w", err)
		}
	}
	for label, assetPath := range map[string]string{
		"UI metadata": m.UI,
		"README":      m.Readme,
	} {
		if assetPath == "" {
			continue
		}
		if err := ValidatePackagePath(assetPath); err != nil {
			return fmt.Errorf("invalid %s path: %w", label, err)
		}
	}
	switch m.Behavior {
	case BehaviorStreaming, BehaviorBlocking, BehaviorSpilling:
	default:
		return fmt.Errorf("invalid runner execution behavior %q", m.Behavior)
	}
	if len(m.Ports) == 0 {
		return errors.New("runner package requires at least one port")
	}
	if m.Implementation == ImplementationJavaScript {
		if m.Behavior != BehaviorStreaming {
			return errors.New("JavaScript runner packages require streaming behavior")
		}
		if len(m.Capabilities) != 0 {
			return errors.New("JavaScript runner packages cannot declare capabilities")
		}
		if m.Distribution != nil && m.Distribution.Mode != DistributionSingleton {
			return errors.New("JavaScript runner packages require singleton distribution")
		}
		ports := make(map[string]Port, len(m.Ports))
		for _, port := range m.Ports {
			ports[port.ID] = port
		}
		if len(m.Ports) != 2 || ports["input"] != (Port{ID: "input", Direction: PortInput, Required: true}) ||
			ports["output"] != (Port{ID: "output", Direction: PortOutput, Required: true}) {
			return errors.New("JavaScript runner packages require one required input port and one required output port")
		}
	}
	if m.Distribution != nil {
		switch m.Distribution.Mode {
		case DistributionSingleton:
		case DistributionBatchParallel:
			if m.Behavior != BehaviorStreaming {
				return errors.New("batch-parallel distribution requires streaming behavior")
			}
			if len(m.Capabilities) != 0 {
				return errors.New("batch-parallel distribution does not support capabilities")
			}
			inputs := 0
			requiredInputs := 0
			for _, port := range m.Ports {
				if port.Direction == PortInput {
					inputs++
					if port.Required {
						requiredInputs++
					}
				}
			}
			if inputs != 1 || requiredInputs != 1 {
				return errors.New("batch-parallel distribution requires exactly one required input")
			}
		default:
			return fmt.Errorf("invalid runner distribution mode %q", m.Distribution.Mode)
		}
	}
	portIDs := make(map[string]struct{}, len(m.Ports))
	for index, port := range m.Ports {
		if !portIDPattern.MatchString(port.ID) {
			return fmt.Errorf("port %d has invalid ID %q", index, port.ID)
		}
		if _, exists := portIDs[port.ID]; exists {
			return fmt.Errorf("duplicate port ID %q", port.ID)
		}
		portIDs[port.ID] = struct{}{}
		if port.Direction != PortInput && port.Direction != PortOutput {
			return fmt.Errorf("port %q has invalid direction %q", port.ID, port.Direction)
		}
	}
	if err := validateCapabilities(m.Capabilities); err != nil {
		return err
	}
	if err := m.Limits.Validate(); err != nil {
		return fmt.Errorf("invalid package limits: %w", err)
	}
	if len(m.Files) < 2 {
		return errors.New("runner package must checksum its entrypoint and config schema")
	}
	if len(m.Files) > 32 {
		return fmt.Errorf("runner package declares %d files, limit is 32", len(m.Files))
	}
	for filePath, integrity := range m.Files {
		if filePath == "manifest.json" {
			return errors.New("manifest.json cannot checksum itself")
		}
		if err := ValidatePackagePath(filePath); err != nil {
			return fmt.Errorf("invalid checksummed file path %q: %w", filePath, err)
		}
		if err := integrity.Validate(); err != nil {
			return fmt.Errorf("invalid integrity for %q: %w", filePath, err)
		}
	}
	for label, requiredPath := range map[string]string{
		"entrypoint":    m.Entrypoint,
		"config schema": m.ConfigSchema,
	} {
		if _, exists := m.Files[requiredPath]; !exists {
			return fmt.Errorf("%s %q has no checksum", label, requiredPath)
		}
	}
	if m.Icon != "" {
		if _, exists := m.Files[m.Icon]; !exists {
			return fmt.Errorf("icon %q has no checksum", m.Icon)
		}
	}
	for label, optionalPath := range map[string]string{
		"UI metadata": m.UI,
		"README":      m.Readme,
	} {
		if optionalPath == "" {
			continue
		}
		if _, exists := m.Files[optionalPath]; !exists {
			return fmt.Errorf("%s %q has no checksum", label, optionalPath)
		}
	}
	return nil
}

func (l PackageLimits) Validate() error {
	if l.MemoryBytes == 0 || l.MemoryBytes%(64<<10) != 0 {
		return errors.New("memoryBytes must be a positive multiple of 65536")
	}
	if l.WallTimeMillis == 0 {
		return errors.New("wallTimeMillis must be positive")
	}
	if l.MaxOutputBytes == 0 {
		return errors.New("maxOutputBytes must be positive")
	}
	if l.MaxOutputMessages == 0 {
		return errors.New("maxOutputMessages must be positive")
	}
	return nil
}

func (i FileIntegrity) Validate() error {
	if len(i.SHA256) != 64 || strings.ToLower(i.SHA256) != i.SHA256 {
		return errors.New("sha256 must be 64 lowercase hexadecimal characters")
	}
	if _, err := hex.DecodeString(i.SHA256); err != nil {
		return errors.New("sha256 must be 64 lowercase hexadecimal characters")
	}
	return nil
}

func ValidatePackagePath(value string) error {
	if value == "" {
		return errors.New("path cannot be empty")
	}
	if strings.Contains(value, `\`) || strings.Contains(value, ":") {
		return errors.New("path must use portable forward-slash syntax")
	}
	if path.IsAbs(value) {
		return errors.New("path must be relative")
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned != value || strings.HasPrefix(cleaned, "../") {
		return errors.New("path must be normalized and remain inside the package")
	}
	return nil
}

func (c Capability) Validate() error {
	switch c {
	case CapabilityHTTP,
		CapabilityFileRead,
		CapabilityFileWrite,
		CapabilityScratch,
		CapabilitySecret:
		return nil
	default:
		return fmt.Errorf("unknown runner capability %q", c)
	}
}

func validateCapabilities(capabilities []Capability) error {
	seen := make(map[Capability]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if err := capability.Validate(); err != nil {
			return err
		}
		if _, exists := seen[capability]; exists {
			return fmt.Errorf("duplicate runner capability %q", capability)
		}
		seen[capability] = struct{}{}
	}
	return nil
}
