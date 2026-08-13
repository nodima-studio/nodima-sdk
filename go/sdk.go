// Package runnersdk adapts the Nodima runner protocol on stdin/stdout to a
// runner implementation. It is shared by Go WASI reference guests.
package runnersdk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	runnerv1 "github.com/nodima-studio/nodima-sdk/runner/v1"
)

type Input interface {
	Next(context.Context) (runnerv1.Message, error)
}

type Output interface {
	Emit(context.Context, runnerv1.Message) error
}

type Runner interface {
	Run(context.Context, Input, Output) error
}

type RunnerWithCapabilities interface {
	RunWithCapabilities(context.Context, Input, Output, Capabilities) error
}

type Capabilities interface {
	HTTP(context.Context, runnerv1.HTTPRequest) (runnerv1.HTTPResponse, error)
}
type FileCapabilities interface {
	FileRead(context.Context, runnerv1.FileReadRequest) (runnerv1.FileReadResponse, error)
	FileWrite(context.Context, runnerv1.FileWriteRequest) (runnerv1.FileWriteResponse, error)
}
type ScratchCapabilities interface {
	Scratch(context.Context, runnerv1.ScratchRequest) (runnerv1.ScratchResponse, error)
}
type SecretCapabilities interface {
	Secret(context.Context, runnerv1.SecretRequest) (runnerv1.SecretResponse, error)
}

type CapabilityError struct {
	Code    string
	Message string
}

func (e *CapabilityError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

type protocolSession struct {
	decoder       *runnerv1.TransportDecoder
	encoder       *runnerv1.TransportEncoder
	decoderMu     sync.Mutex
	encoderMu     sync.Mutex
	buffered      []runnerv1.Message
	executionID   string
	nodeID        string
	nextRequestID uint64
}

type protocolInput struct {
	session *protocolSession
}

func (i *protocolInput) Next(ctx context.Context) (runnerv1.Message, error) {
	if err := ctx.Err(); err != nil {
		return runnerv1.Message{}, err
	}
	return i.session.nextInput()
}

type protocolOutput struct {
	session *protocolSession
}

func (o *protocolOutput) Emit(ctx context.Context, message runnerv1.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return o.session.send(message)
}

func (s *protocolSession) nextInput() (runnerv1.Message, error) {
	s.decoderMu.Lock()
	defer s.decoderMu.Unlock()

	var message runnerv1.Message
	if len(s.buffered) > 0 {
		message = s.buffered[0]
		s.buffered = s.buffered[1:]
	} else {
		decoded, err := s.decoder.DecodeMessage()
		if err != nil {
			return runnerv1.Message{}, err
		}
		message = decoded
	}
	if message.Type == runnerv1.MessageCapabilityResponse {
		return runnerv1.Message{}, errors.New("received an unsolicited capability response")
	}
	// A terminal input_end marker with an empty port ID signals that the host has
	// exhausted all input. The host keeps stdin open afterwards for capability
	// responses, so a runner that reads until io.EOF would otherwise block here.
	if message.Type == runnerv1.MessageInputEnd && message.PortID == "" {
		return runnerv1.Message{}, io.EOF
	}
	if message.Type == runnerv1.MessageInitialize {
		s.executionID = message.ExecutionID
		s.nodeID = message.NodeID
	}
	return message, nil
}

func (s *protocolSession) send(message runnerv1.Message) error {
	s.encoderMu.Lock()
	defer s.encoderMu.Unlock()
	return s.encoder.EncodeMessage(message)
}

func (s *protocolSession) HTTP(
	ctx context.Context,
	request runnerv1.HTTPRequest,
) (runnerv1.HTTPResponse, error) {
	response, err := s.capability(ctx, runnerv1.CapabilityRequest{Kind: runnerv1.CapabilityHTTP, HTTP: &request})
	if err != nil {
		return runnerv1.HTTPResponse{}, err
	}
	return *response.HTTP, nil
}

func (s *protocolSession) FileRead(ctx context.Context, request runnerv1.FileReadRequest) (runnerv1.FileReadResponse, error) {
	response, err := s.capability(ctx, runnerv1.CapabilityRequest{Kind: runnerv1.CapabilityFileRead, FileRead: &request})
	if err != nil {
		return runnerv1.FileReadResponse{}, err
	}
	return *response.FileRead, nil
}
func (s *protocolSession) FileWrite(ctx context.Context, request runnerv1.FileWriteRequest) (runnerv1.FileWriteResponse, error) {
	response, err := s.capability(ctx, runnerv1.CapabilityRequest{Kind: runnerv1.CapabilityFileWrite, FileWrite: &request})
	if err != nil {
		return runnerv1.FileWriteResponse{}, err
	}
	return *response.FileWrite, nil
}
func (s *protocolSession) Scratch(ctx context.Context, request runnerv1.ScratchRequest) (runnerv1.ScratchResponse, error) {
	response, err := s.capability(ctx, runnerv1.CapabilityRequest{Kind: runnerv1.CapabilityScratch, Scratch: &request})
	if err != nil {
		return runnerv1.ScratchResponse{}, err
	}
	return *response.Scratch, nil
}
func (s *protocolSession) Secret(ctx context.Context, request runnerv1.SecretRequest) (runnerv1.SecretResponse, error) {
	response, err := s.capability(ctx, runnerv1.CapabilityRequest{Kind: runnerv1.CapabilitySecret, Secret: &request})
	if err != nil {
		return runnerv1.SecretResponse{}, err
	}
	return *response.Secret, nil
}

func (s *protocolSession) capability(ctx context.Context, request runnerv1.CapabilityRequest) (*runnerv1.CapabilityResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.decoderMu.Lock()
	defer s.decoderMu.Unlock()

	if s.executionID == "" || s.nodeID == "" {
		return nil, errors.New(
			"capability request requires an initialize message first",
		)
	}
	s.nextRequestID++
	requestID := fmt.Sprintf("cap-%d", s.nextRequestID)
	request.ID = requestID
	message := runnerv1.NewMessage(runnerv1.MessageCapabilityRequest)
	message.ExecutionID = s.executionID
	message.NodeID = s.nodeID
	message.CapabilityRequest = &request
	if err := s.send(message); err != nil {
		return nil, err
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		incoming, err := s.decoder.DecodeMessage()
		if err != nil {
			return nil, err
		}
		if incoming.Type != runnerv1.MessageCapabilityResponse {
			s.buffered = append(s.buffered, incoming)
			continue
		}
		response := incoming.CapabilityResponse
		if response.ID != requestID {
			return nil, fmt.Errorf(
				"received capability response %q while waiting for %q",
				response.ID,
				requestID,
			)
		}
		if response.Error != nil {
			return nil, &CapabilityError{
				Code:    response.Error.Code,
				Message: response.Error.Message,
			}
		}
		return response, nil
	}
}

// Session carries the runner protocol over a reader and a writer. Guests use
// Main, which binds it to stdio; the Nodima agent uses it directly so it can
// read a session header off the same stream before dispatching to a runner.
type Session struct {
	session *protocolSession
}

// NewSession frames the protocol over reader and writer. Nothing is read or
// written until the returned Session is used.
func NewSession(
	reader io.Reader,
	writer io.Writer,
	maxFrameBytes uint32,
) (*Session, error) {
	encoder, err := runnerv1.NewTransportEncoder(writer, maxFrameBytes)
	if err != nil {
		return nil, err
	}
	decoder, err := runnerv1.NewTransportDecoder(reader, maxFrameBytes)
	if err != nil {
		return nil, err
	}
	return &Session{session: &protocolSession{decoder: decoder, encoder: encoder}}, nil
}

func (s *Session) Input() Input { return &protocolInput{session: s.session} }

func (s *Session) Output() Output { return &protocolOutput{session: s.session} }

// Capabilities returns the host-brokered capability surface. Requests travel
// back over the same stream, so a remote agent never holds a grant of its own.
func (s *Session) Capabilities() Capabilities { return s.session }

// Decode reads the next message without the input-stream interpretation Input
// applies. The agent uses it for the session header that precedes a node's
// pipeline messages.
func (s *Session) Decode() (runnerv1.Message, error) {
	s.session.decoderMu.Lock()
	defer s.session.decoderMu.Unlock()
	return s.session.decoder.DecodeMessage()
}

// Send writes one message. It is safe to call concurrently with Output.Emit.
func (s *Session) Send(message runnerv1.Message) error {
	return s.session.send(message)
}

// Identify records the execution and node a session belongs to. Input does this
// from the initialize message; the agent calls it when it has learned the
// identity from a session header instead, so capability requests can be
// correlated before the first pipeline message arrives.
func (s *Session) Identify(executionID, nodeID string) {
	s.session.decoderMu.Lock()
	defer s.session.decoderMu.Unlock()
	s.session.executionID = executionID
	s.session.nodeID = nodeID
}

// Run dispatches to the implementation, choosing the capability-aware entry
// point when it has one.
func (s *Session) Run(ctx context.Context, implementation any) error {
	switch capable := implementation.(type) {
	case RunnerWithCapabilities:
		return capable.RunWithCapabilities(ctx, s.Input(), s.Output(), s.session)
	case Runner:
		return capable.Run(ctx, s.Input(), s.Output())
	default:
		return errors.New("runner SDK implementation has no supported Run method")
	}
}

// Fail reports a runner failure to the host on the session's own stream.
func (s *Session) Fail(code string, err error) error {
	failed := runnerv1.NewMessage(runnerv1.MessageFailed)
	failed.ExecutionID = s.session.executionID
	failed.NodeID = s.session.nodeID
	failed.Error = &runnerv1.Failure{Code: code, Message: err.Error()}
	return s.session.send(failed)
}

func Main(implementation any) {
	if implementation == nil {
		fmt.Fprintln(os.Stderr, errors.New("runner SDK requires an implementation"))
		os.Exit(1)
	}
	session, err := NewSession(os.Stdin, os.Stdout, runnerv1.DefaultMaxFrameBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if runErr := session.Run(context.Background(), implementation); runErr != nil {
		if encodeErr := session.Fail("runner_failed", runErr); encodeErr != nil {
			fmt.Fprintln(os.Stderr, encodeErr)
		}
		os.Exit(1)
	}
}
