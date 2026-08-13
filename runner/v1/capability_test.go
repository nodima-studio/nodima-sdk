package v1_test

import (
	"bytes"
	"reflect"
	"testing"

	runnerv1 "github.com/nodima-studio/nodima-sdk/runner/v1"
)

func TestCapabilityControlMessageTransportRoundTrip(t *testing.T) {
	t.Parallel()

	message := runnerv1.NewMessage(runnerv1.MessageCapabilityRequest)
	message.ExecutionID = "run-1"
	message.NodeID = "http-1"
	message.CapabilityRequest = &runnerv1.CapabilityRequest{
		ID:   "request-1",
		Kind: runnerv1.CapabilityHTTP,
		HTTP: &runnerv1.HTTPRequest{
			Method: "POST",
			URL:    "https://example.com/items",
			Headers: map[string][]string{
				"Content-Type": {"application/json"},
			},
			Body: []byte(`{"name":"Ada"}`),
		},
	}

	var wire bytes.Buffer
	encoder, err := runnerv1.NewTransportEncoder(&wire, runnerv1.DefaultMaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.EncodeMessage(message); err != nil {
		t.Fatal(err)
	}
	decoder, err := runnerv1.NewTransportDecoder(&wire, runnerv1.DefaultMaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := decoder.DecodeMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, message) {
		t.Fatalf("decoded message = %#v, want %#v", actual, message)
	}
}

func TestCapabilityResponseRequiresExactlyOneOutcome(t *testing.T) {
	t.Parallel()

	response := runnerv1.CapabilityResponse{
		ID:   "request-1",
		Kind: runnerv1.CapabilityHTTP,
	}
	if err := response.Validate(); err == nil {
		t.Fatal("Validate() accepted a response without an outcome")
	}
	response.HTTP = &runnerv1.HTTPResponse{
		StatusCode: 200,
		FinalURL:   "https://example.com/",
	}
	response.Error = &runnerv1.Failure{Code: "failed", Message: "failure"}
	if err := response.Validate(); err == nil {
		t.Fatal("Validate() accepted both a result and an error")
	}
}

func TestEveryBrokerCapabilityRequestAndResponseValidates(t *testing.T) {
	tests := []struct {
		kind     runnerv1.Capability
		request  runnerv1.CapabilityRequest
		response runnerv1.CapabilityResponse
	}{
		{runnerv1.CapabilityFileRead, runnerv1.CapabilityRequest{FileRead: &runnerv1.FileReadRequest{Scope: "input", Path: "a"}}, runnerv1.CapabilityResponse{FileRead: &runnerv1.FileReadResponse{Data: []byte("a")}}},
		{runnerv1.CapabilityFileWrite, runnerv1.CapabilityRequest{FileWrite: &runnerv1.FileWriteRequest{Scope: "output", Operation: "begin", Path: "a"}}, runnerv1.CapabilityResponse{FileWrite: &runnerv1.FileWriteResponse{TransactionID: "write-1"}}},
		{runnerv1.CapabilityScratch, runnerv1.CapabilityRequest{Scratch: &runnerv1.ScratchRequest{Operation: "read", Path: "a"}}, runnerv1.CapabilityResponse{Scratch: &runnerv1.ScratchResponse{Data: []byte("a")}}},
		{runnerv1.CapabilitySecret, runnerv1.CapabilityRequest{Secret: &runnerv1.SecretRequest{Name: "TOKEN"}}, runnerv1.CapabilityResponse{Secret: &runnerv1.SecretResponse{Value: "secret"}}},
	}
	for _, test := range tests {
		test.request.ID = "cap-1"
		test.request.Kind = test.kind
		if err := test.request.Validate(); err != nil {
			t.Errorf("%s request: %v", test.kind, err)
		}
		test.response.ID = "cap-1"
		test.response.Kind = test.kind
		if err := test.response.Validate(); err != nil {
			t.Errorf("%s response: %v", test.kind, err)
		}
	}
}
