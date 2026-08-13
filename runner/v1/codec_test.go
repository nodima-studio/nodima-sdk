package v1_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	runnerv1 "github.com/nodima-studio/nodima-sdk/runner/v1"
)

func TestCodecRoundTrip(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer
	encoder, err := runnerv1.NewEncoder(&buffer, runnerv1.DefaultMaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := runnerv1.NewDecoder(&buffer, runnerv1.DefaultMaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}

	message := runnerv1.NewMessage(runnerv1.MessageInputBatch)
	message.PortID = "input"
	message.Batch = &runnerv1.Batch{
		RowCount: 2,
		Columns: []runnerv1.Column{{
			Name:   "name",
			Type:   runnerv1.DataTypeString,
			String: []string{"Ada", "Grace"},
		}},
	}

	if err := encoder.Encode(message); err != nil {
		t.Fatal(err)
	}
	actual, err := decoder.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if actual.Type != message.Type || actual.PortID != message.PortID {
		t.Fatalf("decoded message = %#v, want %#v", actual, message)
	}
	if got := actual.Batch.Columns[0].String[1]; got != "Grace" {
		t.Fatalf("decoded value = %q, want Grace", got)
	}
}

func TestDecoderRejectsOversizedFrameBeforeAllocation(t *testing.T) {
	t.Parallel()

	var frame bytes.Buffer
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], 1_024)
	frame.Write(header[:])

	decoder, err := runnerv1.NewDecoder(&frame, 128)
	if err != nil {
		t.Fatal(err)
	}
	_, err = decoder.Decode()
	if err == nil || !strings.Contains(err.Error(), "limit is 128") {
		t.Fatalf("Decode() error = %v, want frame-limit error", err)
	}
}

func TestBatchRejectsMismatchedColumnLength(t *testing.T) {
	t.Parallel()

	batch := runnerv1.Batch{
		RowCount: 2,
		Columns: []runnerv1.Column{{
			Name:   "name",
			Type:   runnerv1.DataTypeString,
			String: []string{"only one"},
		}},
	}

	if err := batch.Validate(); err == nil {
		t.Fatal("Validate() succeeded for mismatched column length")
	}
}
