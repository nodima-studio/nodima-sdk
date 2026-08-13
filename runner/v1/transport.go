package v1

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/fxamacker/cbor/v2"
)

const (
	transportHeaderBytes  = 9
	maxBatchMetadataBytes = 64 << 10
)

var transportMagic = [4]byte{'D', 'B', 'M', 1}

type FrameKind byte

const (
	FrameKindControl    FrameKind = 1
	FrameKindArrowBatch FrameKind = 2
)

type BatchMetadata struct {
	ABI         string      `cbor:"abi"`
	Type        MessageType `cbor:"type"`
	ExecutionID string      `cbor:"execution_id,omitempty"`
	NodeID      string      `cbor:"node_id,omitempty"`
	PortID      string      `cbor:"port_id,omitempty"`
	// EdgeID identifies the graph link a batch belongs to. Nothing reads it
	// today: an input_batch already says which node and port it is for, and an
	// output_batch which it is from, which is unambiguous while a session runs
	// one node. It stops being unambiguous once a session runs several, so the
	// identity is carried now to keep that addition from needing a wire change.
	EdgeID   string `cbor:"edge_id,omitempty"`
	Sequence uint64 `cbor:"sequence,omitempty"`
}

func (m BatchMetadata) Validate() error {
	if m.ABI != ABIVersion {
		return fmt.Errorf("unsupported runner ABI %q", m.ABI)
	}
	if m.Type != MessageInputBatch && m.Type != MessageOutputBatch {
		return fmt.Errorf("Arrow batch frame has invalid message type %q", m.Type)
	}
	return nil
}

type ArrowBatchFrame struct {
	Metadata BatchMetadata
	Record   arrow.RecordBatch
}

type TransportFrame struct {
	Kind    FrameKind
	Control *Message
	Batch   *ArrowBatchFrame
}

func (f *TransportFrame) Release() {
	if f != nil && f.Batch != nil && f.Batch.Record != nil {
		f.Batch.Record.Release()
		f.Batch.Record = nil
	}
}

type TransportEncoder struct {
	writer        io.Writer
	maxFrameBytes uint32
	encoding      cbor.EncMode
}

type TransportDecoder struct {
	reader        io.Reader
	maxFrameBytes uint32
	decoding      cbor.DecMode
}

func NewTransportEncoder(writer io.Writer, maxFrameBytes uint32) (*TransportEncoder, error) {
	if writer == nil {
		return nil, errors.New("runner transport encoder requires a writer")
	}
	if maxFrameBytes == 0 {
		return nil, errors.New("runner transport encoder requires a positive frame limit")
	}
	encoding, err := cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		return nil, fmt.Errorf("create transport CBOR encoding mode: %w", err)
	}
	return &TransportEncoder{
		writer:        writer,
		maxFrameBytes: maxFrameBytes,
		encoding:      encoding,
	}, nil
}

func NewTransportDecoder(reader io.Reader, maxFrameBytes uint32) (*TransportDecoder, error) {
	if reader == nil {
		return nil, errors.New("runner transport decoder requires a reader")
	}
	if maxFrameBytes == 0 {
		return nil, errors.New("runner transport decoder requires a positive frame limit")
	}
	decoding, err := cbor.DecOptions{
		MaxNestedLevels:  16,
		MaxArrayElements: 1_000_000,
		MaxMapPairs:      4_096,
	}.DecMode()
	if err != nil {
		return nil, fmt.Errorf("create transport CBOR decoding mode: %w", err)
	}
	return &TransportDecoder{
		reader:        reader,
		maxFrameBytes: maxFrameBytes,
		decoding:      decoding,
	}, nil
}

func (e *TransportEncoder) EncodeControl(message Message) error {
	if message.Type == MessageInputBatch || message.Type == MessageOutputBatch {
		return errors.New("batch messages must use Arrow transport frames")
	}
	if err := message.Validate(); err != nil {
		return fmt.Errorf("validate runner control message: %w", err)
	}
	payload, err := e.encoding.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode runner control message: %w", err)
	}
	return e.writeFrame(FrameKindControl, payload)
}

func (e *TransportEncoder) EncodeBatch(
	metadata BatchMetadata,
	record arrow.RecordBatch,
) error {
	if err := metadata.Validate(); err != nil {
		return fmt.Errorf("validate Arrow batch metadata: %w", err)
	}
	if record == nil {
		return errors.New("Arrow batch frame requires a record batch")
	}
	if err := ValidateArrowRecord(record); err != nil {
		return fmt.Errorf("validate Arrow record batch: %w", err)
	}

	metadataBytes, err := e.encoding.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode Arrow batch metadata: %w", err)
	}
	if len(metadataBytes) > maxBatchMetadataBytes {
		return fmt.Errorf(
			"Arrow batch metadata is %d bytes, limit is %d",
			len(metadataBytes),
			maxBatchMetadataBytes,
		)
	}

	var payload bytes.Buffer
	var metadataHeader [4]byte
	binary.BigEndian.PutUint32(metadataHeader[:], uint32(len(metadataBytes)))
	payload.Write(metadataHeader[:])
	payload.Write(metadataBytes)

	if uint64(payload.Len()) >= uint64(e.maxFrameBytes) {
		return fmt.Errorf(
			"Arrow batch metadata requires %d bytes, frame limit is %d",
			payload.Len(),
			e.maxFrameBytes,
		)
	}
	remaining := uint64(e.maxFrameBytes) - uint64(payload.Len())
	limited := &limitedWriter{writer: &payload, remaining: remaining}
	arrowWriter := ipc.NewWriter(limited, ipc.WithSchema(record.Schema()))
	writeErr := arrowWriter.Write(record)
	closeErr := arrowWriter.Close()
	if writeErr != nil {
		return fmt.Errorf("encode Arrow record batch: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close Arrow record batch stream: %w", closeErr)
	}
	return e.writeFrame(FrameKindArrowBatch, payload.Bytes())
}

func (e *TransportEncoder) EncodeMessage(message Message) error {
	if message.Type != MessageInputBatch && message.Type != MessageOutputBatch {
		return e.EncodeControl(message)
	}
	if err := message.Validate(); err != nil {
		return fmt.Errorf("validate runner batch message: %w", err)
	}

	record, err := BatchToArrow(*message.Batch)
	if err != nil {
		return fmt.Errorf("convert runner batch to Arrow: %w", err)
	}
	defer record.Release()

	return e.EncodeBatch(BatchMetadata{
		ABI:         message.ABI,
		Type:        message.Type,
		ExecutionID: message.ExecutionID,
		NodeID:      message.NodeID,
		PortID:      message.PortID,
		EdgeID:      message.EdgeID,
		Sequence:    message.Sequence,
	}, record)
}

func (e *TransportEncoder) writeFrame(kind FrameKind, payload []byte) error {
	if len(payload) == 0 {
		return errors.New("runner transport frame cannot be empty")
	}
	if uint64(len(payload)) > uint64(e.maxFrameBytes) {
		return fmt.Errorf(
			"runner transport frame is %d bytes, limit is %d",
			len(payload),
			e.maxFrameBytes,
		)
	}

	var header [transportHeaderBytes]byte
	copy(header[:4], transportMagic[:])
	header[4] = byte(kind)
	binary.BigEndian.PutUint32(header[5:], uint32(len(payload)))
	if err := writeAll(e.writer, header[:]); err != nil {
		return fmt.Errorf("write runner transport header: %w", err)
	}
	if err := writeAll(e.writer, payload); err != nil {
		return fmt.Errorf("write runner transport payload: %w", err)
	}
	return nil
}

func (d *TransportDecoder) Decode() (TransportFrame, error) {
	var header [transportHeaderBytes]byte
	if _, err := io.ReadFull(d.reader, header[:]); err != nil {
		return TransportFrame{}, err
	}
	if !bytes.Equal(header[:4], transportMagic[:]) {
		return TransportFrame{}, fmt.Errorf("invalid runner transport magic %x", header[:4])
	}

	kind := FrameKind(header[4])
	if kind != FrameKindControl && kind != FrameKindArrowBatch {
		return TransportFrame{}, fmt.Errorf("unknown runner transport frame kind %d", kind)
	}

	length := binary.BigEndian.Uint32(header[5:])
	if length == 0 {
		return TransportFrame{}, errors.New("runner transport frame cannot be empty")
	}
	if length > d.maxFrameBytes {
		return TransportFrame{}, fmt.Errorf(
			"runner transport frame declares %d bytes, limit is %d",
			length,
			d.maxFrameBytes,
		)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(d.reader, payload); err != nil {
		return TransportFrame{}, fmt.Errorf("read runner transport payload: %w", err)
	}

	switch kind {
	case FrameKindControl:
		return d.decodeControl(payload)
	case FrameKindArrowBatch:
		return d.decodeBatch(payload)
	default:
		panic("validated transport frame kind became invalid")
	}
}

func (d *TransportDecoder) DecodeMessage() (Message, error) {
	frame, err := d.Decode()
	if err != nil {
		return Message{}, err
	}
	defer frame.Release()

	if frame.Control != nil {
		return *frame.Control, nil
	}
	batch, err := ArrowToBatch(frame.Batch.Record)
	if err != nil {
		return Message{}, fmt.Errorf("convert Arrow transport batch: %w", err)
	}
	message := Message{
		ABI:         frame.Batch.Metadata.ABI,
		Type:        frame.Batch.Metadata.Type,
		ExecutionID: frame.Batch.Metadata.ExecutionID,
		NodeID:      frame.Batch.Metadata.NodeID,
		PortID:      frame.Batch.Metadata.PortID,
		EdgeID:      frame.Batch.Metadata.EdgeID,
		Sequence:    frame.Batch.Metadata.Sequence,
		Batch:       &batch,
	}
	if err := message.Validate(); err != nil {
		return Message{}, fmt.Errorf("validate decoded Arrow batch message: %w", err)
	}
	return message, nil
}

func (d *TransportDecoder) decodeControl(payload []byte) (TransportFrame, error) {
	var message Message
	if err := d.decoding.Unmarshal(payload, &message); err != nil {
		return TransportFrame{}, fmt.Errorf("decode runner control message: %w", err)
	}
	if message.Type == MessageInputBatch || message.Type == MessageOutputBatch {
		return TransportFrame{}, errors.New("CBOR control frame cannot contain a batch message")
	}
	if err := message.Validate(); err != nil {
		return TransportFrame{}, fmt.Errorf("validate runner control message: %w", err)
	}
	return TransportFrame{Kind: FrameKindControl, Control: &message}, nil
}

func (d *TransportDecoder) decodeBatch(payload []byte) (TransportFrame, error) {
	if len(payload) < 4 {
		return TransportFrame{}, errors.New("Arrow batch frame has no metadata length")
	}
	metadataLength := binary.BigEndian.Uint32(payload[:4])
	if metadataLength == 0 || metadataLength > maxBatchMetadataBytes {
		return TransportFrame{}, fmt.Errorf(
			"Arrow batch metadata declares %d bytes, limit is %d",
			metadataLength,
			maxBatchMetadataBytes,
		)
	}
	arrowOffset := 4 + uint64(metadataLength)
	if arrowOffset >= uint64(len(payload)) {
		return TransportFrame{}, errors.New("Arrow batch frame has truncated metadata or no IPC payload")
	}

	var metadata BatchMetadata
	if err := d.decoding.Unmarshal(payload[4:arrowOffset], &metadata); err != nil {
		return TransportFrame{}, fmt.Errorf("decode Arrow batch metadata: %w", err)
	}
	if err := metadata.Validate(); err != nil {
		return TransportFrame{}, fmt.Errorf("validate Arrow batch metadata: %w", err)
	}

	reader, err := ipc.NewReader(bytes.NewReader(payload[arrowOffset:]))
	if err != nil {
		return TransportFrame{}, fmt.Errorf("create Arrow batch reader: %w", err)
	}
	defer reader.Release()
	if !reader.Next() {
		if err := reader.Err(); err != nil {
			return TransportFrame{}, fmt.Errorf("read Arrow record batch: %w", err)
		}
		return TransportFrame{}, errors.New("Arrow batch frame contains no record batch")
	}
	record := reader.RecordBatch()
	record.Retain()
	if reader.Next() {
		record.Release()
		return TransportFrame{}, errors.New("Arrow batch frame contains more than one record batch")
	}
	if err := reader.Err(); err != nil {
		record.Release()
		return TransportFrame{}, fmt.Errorf("finish Arrow record batch: %w", err)
	}
	if err := ValidateArrowRecord(record); err != nil {
		record.Release()
		return TransportFrame{}, fmt.Errorf("validate Arrow record batch: %w", err)
	}

	return TransportFrame{
		Kind: FrameKindArrowBatch,
		Batch: &ArrowBatchFrame{
			Metadata: metadata,
			Record:   record,
		},
	}, nil
}

type limitedWriter struct {
	writer    io.Writer
	remaining uint64
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	if uint64(len(data)) > w.remaining {
		return 0, fmt.Errorf("Arrow IPC payload exceeds runner frame limit")
	}
	n, err := w.writer.Write(data)
	w.remaining -= uint64(n)
	return n, err
}
