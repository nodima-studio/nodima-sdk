package v1_test

import (
	"strings"
	"testing"

	runnerv1 "github.com/nodima-studio/nodima-sdk/runner/v1"
)

func TestStopInputRequiresPortAndRejectsPreviousABI(t *testing.T) {
	message := runnerv1.NewMessage(runnerv1.MessageStopInput)
	if err := message.Validate(); err == nil || !strings.Contains(err.Error(), "input port") {
		t.Fatalf("portless stop_input validation = %v", err)
	}
	message.PortID = "input"
	if err := message.Validate(); err != nil {
		t.Fatalf("stop_input validation = %v", err)
	}
	message.ABI = "dbminer.runner.v1alpha1"
	if err := message.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported runner ABI") {
		t.Fatalf("old ABI validation = %v", err)
	}
}
