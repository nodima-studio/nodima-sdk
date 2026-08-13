package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunRequiresBuildGoCommand(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(context.Background(), nil, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("run() exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "usage: nodima-package build-go") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}

func TestRunReportsMissingRequiredFlags(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(context.Background(), []string{"build-go"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("run() exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "manifest template path is required") {
		t.Fatalf("stderr = %q, want missing-manifest error", stderr.String())
	}
}
