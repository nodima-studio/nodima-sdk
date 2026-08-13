// Package v1 defines the experimental Nodima Studio runner wire protocol.
//
// The protocol is intentionally small and versioned so native and WebAssembly
// implementations can share conformance tests from the beginning.
package v1

import (
	"errors"
	"fmt"
)

const ABIVersion = "dbminer.runner.v1alpha1"

type MessageType string

const (
	MessageInitialize         MessageType = "initialize"
	MessageReady              MessageType = "ready"
	MessageInputBatch         MessageType = "input_batch"
	MessageOutputBatch        MessageType = "output_batch"
	MessageInputEnd           MessageType = "input_end"
	MessageCompleted          MessageType = "completed"
	MessageFailed             MessageType = "failed"
	MessageLog                MessageType = "log"
	MessageProgress           MessageType = "progress"
	MessageBatchComplete      MessageType = "batch_complete"
	MessageCapabilityRequest  MessageType = "capability_request"
	MessageCapabilityResponse MessageType = "capability_response"
)

type DataType string

const (
	DataTypeBoolean DataType = "boolean"
	DataTypeInt64   DataType = "int64"
	DataTypeFloat64 DataType = "float64"
	DataTypeString  DataType = "string"
	DataTypeBytes   DataType = "bytes"
)

// Message is the common control and data envelope used across a runner
// instance. Fields not relevant to a message type are omitted.
type Message struct {
	ABI         string      `cbor:"abi"`
	Type        MessageType `cbor:"type"`
	ExecutionID string      `cbor:"execution_id,omitempty"`
	NodeID      string      `cbor:"node_id,omitempty"`
	PortID      string      `cbor:"port_id,omitempty"`
	EdgeID      string      `cbor:"edge_id,omitempty"`
	// Sequence correlates a partition-safe input batch with every output it
	// produces. Zero means the ordinary singleton stream.
	Sequence           uint64              `cbor:"sequence,omitempty"`
	Config             map[string]string   `cbor:"config,omitempty"`
	Batch              *Batch              `cbor:"batch,omitempty"`
	Error              *Failure            `cbor:"error,omitempty"`
	Log                *Log                `cbor:"log,omitempty"`
	Progress           *Progress           `cbor:"progress,omitempty"`
	CapabilityRequest  *CapabilityRequest  `cbor:"capability_request,omitempty"`
	CapabilityResponse *CapabilityResponse `cbor:"capability_response,omitempty"`
	SessionStart       *SessionStart       `cbor:"session_start,omitempty"`
	SessionReady       *SessionReady       `cbor:"session_ready,omitempty"`
	SessionConnect     *SessionConnect     `cbor:"session_connect,omitempty"`
	SessionCompleted   *SessionCompleted   `cbor:"session_completed,omitempty"`
	Credit             *Credit             `cbor:"credit,omitempty"`
	EdgeStats          *EdgeStats          `cbor:"edge_stats,omitempty"`
}

type Failure struct {
	Code    string `cbor:"code"`
	Message string `cbor:"message"`
}

type Log struct {
	Level   string `cbor:"level"`
	Message string `cbor:"message"`
}

type Progress struct {
	RowsProcessed uint64 `cbor:"rows_processed"`
	BytesRead     uint64 `cbor:"bytes_read"`
	BytesWritten  uint64 `cbor:"bytes_written"`
}

type CapabilityRequest struct {
	ID        string            `cbor:"id"`
	Kind      Capability        `cbor:"kind"`
	HTTP      *HTTPRequest      `cbor:"http,omitempty"`
	FileRead  *FileReadRequest  `cbor:"file_read,omitempty"`
	FileWrite *FileWriteRequest `cbor:"file_write,omitempty"`
	Scratch   *ScratchRequest   `cbor:"scratch,omitempty"`
	Secret    *SecretRequest    `cbor:"secret,omitempty"`
}

type CapabilityResponse struct {
	ID        string             `cbor:"id"`
	Kind      Capability         `cbor:"kind"`
	HTTP      *HTTPResponse      `cbor:"http,omitempty"`
	FileRead  *FileReadResponse  `cbor:"file_read,omitempty"`
	FileWrite *FileWriteResponse `cbor:"file_write,omitempty"`
	Scratch   *ScratchResponse   `cbor:"scratch,omitempty"`
	Secret    *SecretResponse    `cbor:"secret,omitempty"`
	Error     *Failure           `cbor:"error,omitempty"`
}

type HTTPRequest struct {
	Method  string              `cbor:"method"`
	URL     string              `cbor:"url"`
	Headers map[string][]string `cbor:"headers,omitempty"`
	Body    []byte              `cbor:"body,omitempty"`
}

type HTTPResponse struct {
	StatusCode int                 `cbor:"status_code"`
	Headers    map[string][]string `cbor:"headers,omitempty"`
	Body       []byte              `cbor:"body,omitempty"`
	FinalURL   string              `cbor:"final_url"`
}

type FileReadRequest struct {
	Scope string `cbor:"scope"`
	Path  string `cbor:"path"`
	List  bool   `cbor:"list,omitempty"`
}
type FileEntry struct {
	Name      string `cbor:"name"`
	Size      uint64 `cbor:"size"`
	Directory bool   `cbor:"directory,omitempty"`
}
type FileReadResponse struct {
	Data    []byte      `cbor:"data,omitempty"`
	Entries []FileEntry `cbor:"entries,omitempty"`
}
type FileWriteRequest struct {
	Scope         string `cbor:"scope"`
	Operation     string `cbor:"operation"`
	TransactionID string `cbor:"transaction_id,omitempty"`
	Path          string `cbor:"path,omitempty"`
	Data          []byte `cbor:"data,omitempty"`
	// HostPath is agent-supplied commit metadata. Ordinary runner requests
	// leave it empty; a service-side artifact relay records it after the remote
	// destination has committed.
	HostPath string `cbor:"host_path,omitempty"`
}
type FileWriteResponse struct {
	TransactionID string `cbor:"transaction_id,omitempty"`
	HostPath      string `cbor:"host_path,omitempty"`
	ArtifactName  string `cbor:"artifact_name,omitempty"`
}
type ScratchRequest struct {
	Operation string `cbor:"operation"`
	Path      string `cbor:"path,omitempty"`
	Data      []byte `cbor:"data,omitempty"`
	Offset    uint64 `cbor:"offset,omitempty"`
	Limit     uint64 `cbor:"limit,omitempty"`
}
type ScratchUsage struct {
	CurrentBytes uint64 `cbor:"current_bytes"`
	PeakBytes    uint64 `cbor:"peak_bytes"`
	CurrentFiles uint32 `cbor:"current_files"`
	PeakFiles    uint32 `cbor:"peak_files"`
	MaxBytes     uint64 `cbor:"max_bytes"`
	MaxFiles     uint32 `cbor:"max_files"`
}
type ScratchResponse struct {
	Data    []byte        `cbor:"data,omitempty"`
	Entries []FileEntry   `cbor:"entries,omitempty"`
	Size    uint64        `cbor:"size,omitempty"`
	EOF     bool          `cbor:"eof,omitempty"`
	Usage   *ScratchUsage `cbor:"usage,omitempty"`
}
type SecretRequest struct {
	Name string `cbor:"name"`
}
type SecretResponse struct {
	Value string `cbor:"value"`
}

func (r CapabilityRequest) Validate() error {
	if r.ID == "" {
		return errors.New("capability request requires an ID")
	}
	payloads := 0
	for _, present := range []bool{r.HTTP != nil, r.FileRead != nil, r.FileWrite != nil, r.Scratch != nil, r.Secret != nil} {
		if present {
			payloads++
		}
	}
	if payloads != 1 {
		return errors.New("capability request requires exactly one payload")
	}
	switch r.Kind {
	case CapabilityHTTP:
		if r.HTTP == nil || r.HTTP.Method == "" || r.HTTP.URL == "" {
			return errors.New("HTTP capability request requires a method and URL")
		}
	case CapabilityFileRead:
		if r.FileRead == nil || r.FileRead.Scope == "" {
			return errors.New("file-read capability request requires a scope")
		}
	case CapabilityFileWrite:
		if r.FileWrite == nil || r.FileWrite.Scope == "" || r.FileWrite.Operation == "" {
			return errors.New("file-write capability request requires a scope and operation")
		}
	case CapabilityScratch:
		if r.Scratch == nil || r.Scratch.Operation == "" {
			return errors.New("scratch capability request requires an operation")
		}
		switch r.Scratch.Operation {
		case "write", "append", "read", "list", "delete":
			if r.Scratch.Path == "" {
				return errors.New("scratch capability request requires a path")
			}
		case "usage":
		default:
			return fmt.Errorf("unsupported scratch operation %q", r.Scratch.Operation)
		}
	case CapabilitySecret:
		if r.Secret == nil || r.Secret.Name == "" {
			return errors.New("secret capability request requires a name")
		}
	default:
		return fmt.Errorf("unsupported capability request kind %q", r.Kind)
	}
	return nil
}

func (r CapabilityResponse) Validate() error {
	if r.ID == "" {
		return errors.New("capability response requires an ID")
	}
	if err := r.Kind.Validate(); err != nil {
		return err
	}
	results := 0
	for _, present := range []bool{r.HTTP != nil, r.FileRead != nil, r.FileWrite != nil, r.Scratch != nil, r.Secret != nil, r.Error != nil} {
		if present {
			results++
		}
	}
	if results != 1 {
		return errors.New("capability response requires exactly one result or error")
	}
	if r.Error != nil && (r.Error.Code == "" || r.Error.Message == "") {
		return errors.New("capability response error requires a code and message")
	}
	if r.HTTP != nil {
		if r.HTTP.StatusCode < 100 || r.HTTP.StatusCode > 999 || r.HTTP.FinalURL == "" {
			return errors.New("HTTP capability response has invalid status or final URL")
		}
	}
	if r.Error == nil {
		matches := (r.Kind == CapabilityHTTP && r.HTTP != nil) ||
			(r.Kind == CapabilityFileRead && r.FileRead != nil) ||
			(r.Kind == CapabilityFileWrite && r.FileWrite != nil) ||
			(r.Kind == CapabilityScratch && r.Scratch != nil) ||
			(r.Kind == CapabilitySecret && r.Secret != nil)
		if !matches {
			return errors.New("capability response result does not match its kind")
		}
	}
	return nil
}

// Batch is the transitional in-process columnar model used by the reference
// runners. TransportEncoder converts it to Arrow IPC on the wire.
type Batch struct {
	RowCount uint32   `cbor:"row_count"`
	Columns  []Column `cbor:"columns"`
}

type Column struct {
	Name    string    `cbor:"name"`
	Type    DataType  `cbor:"type"`
	Valid   []bool    `cbor:"valid,omitempty"`
	Boolean []bool    `cbor:"boolean,omitempty"`
	Int64   []int64   `cbor:"int64,omitempty"`
	Float64 []float64 `cbor:"float64,omitempty"`
	String  []string  `cbor:"string,omitempty"`
	Bytes   [][]byte  `cbor:"bytes,omitempty"`
}

func NewMessage(messageType MessageType) Message {
	return Message{ABI: ABIVersion, Type: messageType}
}

func (m Message) Validate() error {
	if m.ABI != ABIVersion {
		return fmt.Errorf("unsupported runner ABI %q", m.ABI)
	}
	if m.Type != MessageInputBatch && m.Type != MessageOutputBatch && m.Batch != nil {
		return fmt.Errorf("%s message cannot contain a batch", m.Type)
	}
	if m.Type != MessageCapabilityRequest && m.CapabilityRequest != nil {
		return fmt.Errorf("%s message cannot contain a capability request", m.Type)
	}
	if m.Type != MessageCapabilityResponse && m.CapabilityResponse != nil {
		return fmt.Errorf("%s message cannot contain a capability response", m.Type)
	}
	if m.Type != MessageSessionStart && m.SessionStart != nil {
		return fmt.Errorf("%s message cannot contain a session start", m.Type)
	}
	if m.Type != MessageSessionReady && m.SessionReady != nil {
		return fmt.Errorf("%s message cannot contain a session ready", m.Type)
	}
	if m.Type != MessageSessionConnect && m.SessionConnect != nil {
		return fmt.Errorf("%s message cannot contain session connect", m.Type)
	}
	if m.Type != MessageSessionCompleted && m.SessionCompleted != nil {
		return fmt.Errorf("%s message cannot contain session completion", m.Type)
	}
	if m.Type != MessageCredit && m.Credit != nil {
		return fmt.Errorf("%s message cannot contain credit", m.Type)
	}
	if m.Type != MessageEdgeStats && m.EdgeStats != nil {
		return fmt.Errorf("%s message cannot contain edge stats", m.Type)
	}
	if m.Sequence != 0 && m.Type != MessageInputBatch && m.Type != MessageOutputBatch && m.Type != MessageBatchComplete {
		return fmt.Errorf("%s message cannot contain a batch sequence", m.Type)
	}

	switch m.Type {
	case MessageInitialize:
	case MessageReady, MessageInputEnd, MessageCompleted:
	case MessageBatchComplete:
		if m.Sequence == 0 {
			return errors.New("batch_complete message requires a sequence")
		}
	case MessageCancel:
	case MessageSessionStart:
		if m.SessionStart == nil {
			return errors.New("session_start message requires session content")
		}
		if err := m.SessionStart.Validate(); err != nil {
			return err
		}
	case MessageSessionReady:
		if m.SessionReady == nil {
			return errors.New("session_ready message requires session content")
		}
		if err := m.SessionReady.Validate(); err != nil {
			return err
		}
	case MessageSessionConnect:
		if m.SessionConnect == nil {
			return errors.New("session_connect message requires topology")
		}
		if err := m.SessionConnect.Validate(); err != nil {
			return err
		}
	case MessageSessionConnected, MessageSessionRun:
	case MessageSessionCompleted:
		if m.SessionCompleted != nil && m.SessionCompleted.Preview != nil {
			if err := m.SessionCompleted.Preview.Validate(); err != nil {
				return err
			}
		}
	case MessageEdgeStats:
		if m.EdgeStats == nil || m.EdgeStats.EdgeID == "" {
			return errors.New("edge_stats message requires an edge ID")
		}
	case MessageCredit:
		if m.Credit == nil {
			return errors.New("credit message requires credit content")
		}
		if err := m.Credit.Validate(); err != nil {
			return err
		}
	case MessageInputBatch, MessageOutputBatch:
		if m.Batch == nil {
			return fmt.Errorf("%s message requires a batch", m.Type)
		}
		if err := m.Batch.Validate(); err != nil {
			return fmt.Errorf("invalid %s: %w", m.Type, err)
		}
	case MessageFailed:
		if m.Error == nil || m.Error.Code == "" || m.Error.Message == "" {
			return errors.New("failed message requires an error code and message")
		}
	case MessageLog:
		if m.Log == nil || m.Log.Message == "" {
			return errors.New("log message requires log content")
		}
	case MessageProgress:
		if m.Progress == nil {
			return errors.New("progress message requires progress content")
		}
	case MessageCapabilityRequest:
		if m.CapabilityRequest == nil {
			return errors.New("capability_request message requires request content")
		}
		if err := m.CapabilityRequest.Validate(); err != nil {
			return err
		}
	case MessageCapabilityResponse:
		if m.CapabilityResponse == nil {
			return errors.New("capability_response message requires response content")
		}
		if err := m.CapabilityResponse.Validate(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown runner message type %q", m.Type)
	}

	return nil
}

func (b Batch) Validate() error {
	if len(b.Columns) == 0 && b.RowCount > 0 {
		return errors.New("non-empty batch requires at least one column")
	}

	names := make(map[string]struct{}, len(b.Columns))
	for index, column := range b.Columns {
		if column.Name == "" {
			return fmt.Errorf("column %d has no name", index)
		}
		if _, exists := names[column.Name]; exists {
			return fmt.Errorf("duplicate column name %q", column.Name)
		}
		names[column.Name] = struct{}{}

		if len(column.Valid) != 0 && len(column.Valid) != int(b.RowCount) {
			return fmt.Errorf("column %q validity length is %d, want %d", column.Name, len(column.Valid), b.RowCount)
		}

		length, err := column.valueLength()
		if err != nil {
			return fmt.Errorf("column %q: %w", column.Name, err)
		}
		if length != int(b.RowCount) {
			return fmt.Errorf("column %q value length is %d, want %d", column.Name, length, b.RowCount)
		}
	}

	return nil
}

func (c Column) valueLength() (int, error) {
	switch c.Type {
	case DataTypeBoolean:
		return len(c.Boolean), nil
	case DataTypeInt64:
		return len(c.Int64), nil
	case DataTypeFloat64:
		return len(c.Float64), nil
	case DataTypeString:
		return len(c.String), nil
	case DataTypeBytes:
		return len(c.Bytes), nil
	default:
		return 0, fmt.Errorf("unsupported data type %q", c.Type)
	}
}
