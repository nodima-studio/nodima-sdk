package v1_test

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/fxamacker/cbor/v2"
	runnerv1 "github.com/nodima-studio/nodima-sdk/runner/v1"
)

func TestTransportControlRoundTrip(t *testing.T) {
	t.Parallel()

	message := runnerv1.NewMessage(runnerv1.MessageInitialize)
	message.ExecutionID = "run-1"
	message.NodeID = "node-1"
	message.Config = map[string]string{"columns": "country,name"}

	var wire bytes.Buffer
	encoder, err := runnerv1.NewTransportEncoder(&wire, runnerv1.DefaultMaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.EncodeControl(message); err != nil {
		t.Fatal(err)
	}

	decoder, err := runnerv1.NewTransportDecoder(&wire, runnerv1.DefaultMaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := decoder.Decode()
	if err != nil {
		t.Fatal(err)
	}
	defer frame.Release()

	if frame.Kind != runnerv1.FrameKindControl || frame.Control == nil || frame.Batch != nil {
		t.Fatalf("decoded frame = %#v, want a control frame", frame)
	}
	if !reflect.DeepEqual(*frame.Control, message) {
		t.Fatalf("decoded control = %#v, want %#v", *frame.Control, message)
	}
}

func TestTransportArrowMessageRoundTrip(t *testing.T) {
	t.Parallel()

	message := runnerv1.NewMessage(runnerv1.MessageInputBatch)
	message.ExecutionID = "run-1"
	message.NodeID = "node-1"
	message.PortID = "input"
	message.Batch = portableBatch()

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

func TestTransportDirectArrowFrameRoundTrip(t *testing.T) {
	t.Parallel()

	expected := portableBatch()
	record, err := runnerv1.BatchToArrow(*expected)
	if err != nil {
		t.Fatal(err)
	}
	defer record.Release()

	var wire bytes.Buffer
	encoder, err := runnerv1.NewTransportEncoder(&wire, runnerv1.DefaultMaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	metadata := runnerv1.BatchMetadata{
		ABI:         runnerv1.ABIVersion,
		Type:        runnerv1.MessageOutputBatch,
		ExecutionID: "run-1",
		NodeID:      "node-1",
		PortID:      "output",
		EdgeID:      "edge-1",
		Sequence:    42,
	}
	if err := encoder.EncodeBatch(metadata, record); err != nil {
		t.Fatal(err)
	}

	decoder, err := runnerv1.NewTransportDecoder(&wire, runnerv1.DefaultMaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := decoder.Decode()
	if err != nil {
		t.Fatal(err)
	}
	defer frame.Release()
	if frame.Kind != runnerv1.FrameKindArrowBatch || frame.Batch == nil || frame.Control != nil {
		t.Fatalf("decoded frame = %#v, want an Arrow batch frame", frame)
	}
	if !reflect.DeepEqual(frame.Batch.Metadata, metadata) {
		t.Fatalf("metadata = %#v, want %#v", frame.Batch.Metadata, metadata)
	}

	actual, err := runnerv1.ArrowToBatch(frame.Batch.Record)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, *expected) {
		t.Fatalf("decoded batch = %#v, want %#v", actual, *expected)
	}
}

func TestTransportRejectsOversizedFrameBeforePayloadRead(t *testing.T) {
	t.Parallel()

	var wire bytes.Buffer
	wire.Write([]byte{'D', 'B', 'M', 1, byte(runnerv1.FrameKindControl)})
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], 1_024)
	wire.Write(length[:])

	decoder, err := runnerv1.NewTransportDecoder(&wire, 128)
	if err != nil {
		t.Fatal(err)
	}
	_, err = decoder.Decode()
	if err == nil || !strings.Contains(err.Error(), "limit is 128") {
		t.Fatalf("Decode() error = %v, want frame-limit error", err)
	}
}

func TestTransportRejectsInvalidMagicAndKindBeforePayloadRead(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		header []byte
		match  string
	}{
		{
			name:   "magic",
			header: []byte{'N', 'O', 'P', 'E', byte(runnerv1.FrameKindControl), 0, 0, 0, 1},
			match:  "invalid runner transport magic",
		},
		{
			name:   "kind",
			header: []byte{'D', 'B', 'M', 1, 255, 0, 0, 0, 1},
			match:  "unknown runner transport frame kind",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			decoder, err := runnerv1.NewTransportDecoder(
				bytes.NewReader(testCase.header),
				runnerv1.DefaultMaxFrameBytes,
			)
			if err != nil {
				t.Fatal(err)
			}
			_, err = decoder.Decode()
			if err == nil || !strings.Contains(err.Error(), testCase.match) {
				t.Fatalf("Decode() error = %v, want %q", err, testCase.match)
			}
		})
	}
}

func TestTransportRejectsBatchAsControl(t *testing.T) {
	t.Parallel()

	message := runnerv1.NewMessage(runnerv1.MessageReady)
	message.Batch = portableBatch()
	encoder, err := runnerv1.NewTransportEncoder(
		&bytes.Buffer{},
		runnerv1.DefaultMaxFrameBytes,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.EncodeControl(message); err == nil {
		t.Fatal("EncodeControl() accepted a batch message")
	}
}

func TestTransportEnforcesArrowFrameLimit(t *testing.T) {
	t.Parallel()

	record, err := runnerv1.BatchToArrow(*portableBatch())
	if err != nil {
		t.Fatal(err)
	}
	defer record.Release()

	var wire bytes.Buffer
	encoder, err := runnerv1.NewTransportEncoder(&wire, 128)
	if err != nil {
		t.Fatal(err)
	}
	err = encoder.EncodeBatch(runnerv1.BatchMetadata{
		ABI:  runnerv1.ABIVersion,
		Type: runnerv1.MessageInputBatch,
	}, record)
	if err == nil || !strings.Contains(err.Error(), "frame limit") {
		t.Fatalf("EncodeBatch() error = %v, want frame-limit error", err)
	}
	if wire.Len() != 0 {
		t.Fatalf("EncodeBatch() wrote %d bytes before rejecting the frame", wire.Len())
	}
}

func TestTransportRejectsMoreThanOneArrowRecordBatch(t *testing.T) {
	t.Parallel()

	record, err := runnerv1.BatchToArrow(*portableBatch())
	if err != nil {
		t.Fatal(err)
	}
	defer record.Release()

	metadata, err := cbor.Marshal(runnerv1.BatchMetadata{
		ABI:  runnerv1.ABIVersion,
		Type: runnerv1.MessageInputBatch,
	})
	if err != nil {
		t.Fatal(err)
	}

	var arrowPayload bytes.Buffer
	arrowWriter := ipc.NewWriter(&arrowPayload, ipc.WithSchema(record.Schema()))
	if err := arrowWriter.Write(record); err != nil {
		t.Fatal(err)
	}
	if err := arrowWriter.Write(record); err != nil {
		t.Fatal(err)
	}
	if err := arrowWriter.Close(); err != nil {
		t.Fatal(err)
	}

	var payload bytes.Buffer
	var metadataLength [4]byte
	binary.BigEndian.PutUint32(metadataLength[:], uint32(len(metadata)))
	payload.Write(metadataLength[:])
	payload.Write(metadata)
	payload.Write(arrowPayload.Bytes())

	var wire bytes.Buffer
	wire.Write([]byte{'D', 'B', 'M', 1, byte(runnerv1.FrameKindArrowBatch)})
	var payloadLength [4]byte
	binary.BigEndian.PutUint32(payloadLength[:], uint32(payload.Len()))
	wire.Write(payloadLength[:])
	wire.Write(payload.Bytes())

	decoder, err := runnerv1.NewTransportDecoder(&wire, runnerv1.DefaultMaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := decoder.Decode()
	frame.Release()
	if err == nil || !strings.Contains(err.Error(), "more than one record batch") {
		t.Fatalf("Decode() error = %v, want multiple-record error", err)
	}
}

func TestArrowValidationRejectsUnsupportedType(t *testing.T) {
	t.Parallel()

	builder := array.NewRecordBuilder(
		memory.DefaultAllocator,
		arrow.NewSchema([]arrow.Field{{
			Name: "small",
			Type: arrow.PrimitiveTypes.Int32,
		}}, nil),
	)
	defer builder.Release()
	builder.Field(0).(*array.Int32Builder).Append(1)
	record := builder.NewRecordBatch()
	defer record.Release()

	if err := runnerv1.ValidateArrowRecord(record); err == nil {
		t.Fatal("ValidateArrowRecord() accepted an unsupported type")
	}
}

func portableBatch() *runnerv1.Batch {
	return &runnerv1.Batch{
		RowCount: 3,
		Columns: []runnerv1.Column{
			{
				Name:    "enabled",
				Type:    runnerv1.DataTypeBoolean,
				Boolean: []bool{true, false, true},
			},
			{
				Name:  "id",
				Type:  runnerv1.DataTypeInt64,
				Int64: []int64{1, 2, 3},
			},
			{
				Name:    "score",
				Type:    runnerv1.DataTypeFloat64,
				Float64: []float64{1.5, 2.5, 3.5},
			},
			{
				Name:   "name",
				Type:   runnerv1.DataTypeString,
				Valid:  []bool{true, false, true},
				String: []string{"Ada", "", "Grace"},
			},
			{
				Name:  "hash",
				Type:  runnerv1.DataTypeBytes,
				Bytes: [][]byte{{1, 2}, {3, 4}, {5, 6}},
			},
		},
	}
}
