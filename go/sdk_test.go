package runnersdk

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	runnerv1 "github.com/nodima-studio/nodima-sdk/runner/v1"
)

// TestTerminalInputEndSignalsEOF verifies that an input_end marker with an empty
// port ID, which the Wasm host sends once it has exhausted all input, is
// reported to the runner as io.EOF rather than a readable message. The host
// keeps stdin open afterwards for capability responses, so without this a runner
// that reads until io.EOF would block forever.
func TestTerminalInputEndSignalsEOF(t *testing.T) {
	t.Parallel()

	var inbound bytes.Buffer
	hostEncoder, err := runnerv1.NewTransportEncoder(
		&inbound,
		runnerv1.DefaultMaxFrameBytes,
	)
	if err != nil {
		t.Fatal(err)
	}
	terminal := runnerv1.NewMessage(runnerv1.MessageInputEnd)
	if err := hostEncoder.EncodeMessage(terminal); err != nil {
		t.Fatal(err)
	}

	decoder, err := runnerv1.NewTransportDecoder(&inbound, runnerv1.DefaultMaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	session := &protocolSession{decoder: decoder}
	input := &protocolInput{session: session}

	if _, err := input.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("input.Next() error = %v, want io.EOF", err)
	}
}

func TestCapabilityClientCorrelatesResponseAndBuffersInput(t *testing.T) {
	t.Parallel()

	var inbound bytes.Buffer
	hostEncoder, err := runnerv1.NewTransportEncoder(
		&inbound,
		runnerv1.DefaultMaxFrameBytes,
	)
	if err != nil {
		t.Fatal(err)
	}
	initialize := runnerv1.NewMessage(runnerv1.MessageInitialize)
	initialize.ExecutionID = "run-1"
	initialize.NodeID = "http-1"
	if err := hostEncoder.EncodeMessage(initialize); err != nil {
		t.Fatal(err)
	}
	end := runnerv1.NewMessage(runnerv1.MessageInputEnd)
	end.ExecutionID = initialize.ExecutionID
	end.NodeID = initialize.NodeID
	end.PortID = "input"
	if err := hostEncoder.EncodeMessage(end); err != nil {
		t.Fatal(err)
	}
	response := runnerv1.NewMessage(runnerv1.MessageCapabilityResponse)
	response.ExecutionID = initialize.ExecutionID
	response.NodeID = initialize.NodeID
	response.CapabilityResponse = &runnerv1.CapabilityResponse{
		ID:   "cap-1",
		Kind: runnerv1.CapabilityHTTP,
		HTTP: &runnerv1.HTTPResponse{
			StatusCode: 200,
			Body:       []byte("response"),
			FinalURL:   "https://example.com/",
		},
	}
	if err := hostEncoder.EncodeMessage(response); err != nil {
		t.Fatal(err)
	}

	decoder, err := runnerv1.NewTransportDecoder(
		&inbound,
		runnerv1.DefaultMaxFrameBytes,
	)
	if err != nil {
		t.Fatal(err)
	}
	var outbound bytes.Buffer
	encoder, err := runnerv1.NewTransportEncoder(
		&outbound,
		runnerv1.DefaultMaxFrameBytes,
	)
	if err != nil {
		t.Fatal(err)
	}
	session := &protocolSession{decoder: decoder, encoder: encoder}
	input := &protocolInput{session: session}

	actualInitialize, err := input.Next(context.Background())
	if err != nil || actualInitialize.Type != runnerv1.MessageInitialize {
		t.Fatalf("initialize = %#v, error %v", actualInitialize, err)
	}
	httpResponse, err := session.HTTP(context.Background(), runnerv1.HTTPRequest{
		Method: "GET",
		URL:    "https://example.com/",
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(httpResponse.Body) != "response" {
		t.Fatalf("HTTP response = %#v", httpResponse)
	}
	actualEnd, err := input.Next(context.Background())
	if err != nil || actualEnd.Type != runnerv1.MessageInputEnd {
		t.Fatalf("buffered input = %#v, error %v", actualEnd, err)
	}

	hostDecoder, err := runnerv1.NewTransportDecoder(
		&outbound,
		runnerv1.DefaultMaxFrameBytes,
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := hostDecoder.DecodeMessage()
	if err != nil {
		t.Fatal(err)
	}
	if request.Type != runnerv1.MessageCapabilityRequest ||
		request.ExecutionID != initialize.ExecutionID ||
		request.NodeID != initialize.NodeID ||
		request.CapabilityRequest.ID != "cap-1" {
		t.Fatalf("capability request = %#v", request)
	}
}
