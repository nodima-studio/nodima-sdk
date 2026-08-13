package v1_test

import (
	"strings"
	"testing"

	runnerv1 "github.com/nodima-studio/nodima-sdk/runner/v1"
)

func TestManifestValidation(t *testing.T) {
	t.Parallel()

	if err := validManifest().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestManifestRejectsInvalidContracts(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		mutate func(*runnerv1.Manifest)
		match  string
	}{
		{
			name:   "format",
			mutate: func(manifest *runnerv1.Manifest) { manifest.FormatVersion = "future" },
			match:  "unsupported runner package format",
		},
		{
			name:   "package ID",
			mutate: func(manifest *runnerv1.Manifest) { manifest.ID = "Not Portable" },
			match:  "invalid runner package ID",
		},
		{
			name:   "semantic version",
			mutate: func(manifest *runnerv1.Manifest) { manifest.Version = "1.0" },
			match:  "invalid semantic version",
		},
		{
			name:   "numeric prerelease leading zero",
			mutate: func(manifest *runnerv1.Manifest) { manifest.Version = "1.0.0-01" },
			match:  "invalid semantic version",
		},
		{
			name:   "ABI",
			mutate: func(manifest *runnerv1.Manifest) { manifest.ABI = "dbminer.runner.v2" },
			match:  "unsupported runner ABI",
		},
		{
			name:   "implementation",
			mutate: func(manifest *runnerv1.Manifest) { manifest.Implementation = runnerv1.ImplementationNative },
			match:  "must be",
		},
		{
			name:   "unsafe entrypoint",
			mutate: func(manifest *runnerv1.Manifest) { manifest.Entrypoint = "../runner.wasm" },
			match:  "invalid entrypoint",
		},
		{
			name: "duplicate port",
			mutate: func(manifest *runnerv1.Manifest) {
				manifest.Ports[1].ID = manifest.Ports[0].ID
			},
			match: "duplicate port ID",
		},
		{
			name: "duplicate capability",
			mutate: func(manifest *runnerv1.Manifest) {
				manifest.Capabilities = []runnerv1.Capability{
					runnerv1.CapabilityHTTP,
					runnerv1.CapabilityHTTP,
				}
			},
			match: "duplicate runner capability",
		},
		{
			name: "unaligned memory",
			mutate: func(manifest *runnerv1.Manifest) {
				manifest.Limits.MemoryBytes = 1
			},
			match: "memoryBytes",
		},
		{
			name: "missing entrypoint checksum",
			mutate: func(manifest *runnerv1.Manifest) {
				delete(manifest.Files, manifest.Entrypoint)
			},
			match: "entrypoint",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			manifest := validManifest()
			testCase.mutate(&manifest)
			err := manifest.Validate()
			if err == nil || !strings.Contains(err.Error(), testCase.match) {
				t.Fatalf("Validate() error = %v, want %q", err, testCase.match)
			}
		})
	}
}

func TestPackagePathValidationIsPortable(t *testing.T) {
	t.Parallel()

	valid := []string{
		"runner.wasm",
		"schemas/config.schema.json",
		"assets/icon.svg",
	}
	for _, value := range valid {
		if err := runnerv1.ValidatePackagePath(value); err != nil {
			t.Fatalf("ValidatePackagePath(%q) error = %v", value, err)
		}
	}

	invalid := []string{
		"",
		".",
		"./runner.wasm",
		"../runner.wasm",
		"assets/../runner.wasm",
		"/runner.wasm",
		`assets\icon.svg`,
		"C:/runner.wasm",
	}
	for _, value := range invalid {
		if err := runnerv1.ValidatePackagePath(value); err == nil {
			t.Fatalf("ValidatePackagePath(%q) succeeded", value)
		}
	}
}

func validManifest() runnerv1.Manifest {
	hash := strings.Repeat("a", 64)
	return runnerv1.Manifest{
		FormatVersion:  runnerv1.PackageFormatVersion,
		ID:             "com.dbminer.pick-columns",
		Version:        "0.1.0-alpha.1+test",
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
			"runner.wasm":        {SHA256: hash},
			"config.schema.json": {SHA256: hash},
		},
	}
}
