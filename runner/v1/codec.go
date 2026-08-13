package v1

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/fxamacker/cbor/v2"
)

// DefaultMaxFrameBytes is the default maximum payload size for either codec.
const DefaultMaxFrameBytes uint32 = 8 << 20

// Encoder implements the original all-CBOR data-plane candidate. Encoder and
// Decoder remain available for the reproducible comparison benchmark. Runtime
// runner traffic uses TransportEncoder and TransportDecoder.
type Encoder struct {
	writer        io.Writer
	maxFrameBytes uint32
	encoding      cbor.EncMode
}

type Decoder struct {
	reader        io.Reader
	maxFrameBytes uint32
	decoding      cbor.DecMode
}

func NewEncoder(writer io.Writer, maxFrameBytes uint32) (*Encoder, error) {
	if writer == nil {
		return nil, errors.New("runner protocol encoder requires a writer")
	}
	if maxFrameBytes == 0 {
		return nil, errors.New("runner protocol encoder requires a positive frame limit")
	}

	encoding, err := cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		return nil, fmt.Errorf("create CBOR encoding mode: %w", err)
	}

	return &Encoder{
		writer:        writer,
		maxFrameBytes: maxFrameBytes,
		encoding:      encoding,
	}, nil
}

func NewDecoder(reader io.Reader, maxFrameBytes uint32) (*Decoder, error) {
	if reader == nil {
		return nil, errors.New("runner protocol decoder requires a reader")
	}
	if maxFrameBytes == 0 {
		return nil, errors.New("runner protocol decoder requires a positive frame limit")
	}

	decoding, err := cbor.DecOptions{
		MaxNestedLevels:  16,
		MaxArrayElements: 1_000_000,
		MaxMapPairs:      4_096,
	}.DecMode()
	if err != nil {
		return nil, fmt.Errorf("create CBOR decoding mode: %w", err)
	}

	return &Decoder{
		reader:        reader,
		maxFrameBytes: maxFrameBytes,
		decoding:      decoding,
	}, nil
}

func (e *Encoder) Encode(message Message) error {
	if err := message.Validate(); err != nil {
		return fmt.Errorf("validate runner message: %w", err)
	}

	data, err := e.encoding.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode runner message: %w", err)
	}
	if uint64(len(data)) > uint64(e.maxFrameBytes) {
		return fmt.Errorf("runner frame is %d bytes, limit is %d", len(data), e.maxFrameBytes)
	}

	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	if err := writeAll(e.writer, header[:]); err != nil {
		return fmt.Errorf("write runner frame header: %w", err)
	}
	if err := writeAll(e.writer, data); err != nil {
		return fmt.Errorf("write runner frame payload: %w", err)
	}
	return nil
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(data) {
			return io.ErrNoProgress
		}
		data = data[written:]
	}
	return nil
}

func (d *Decoder) Decode() (Message, error) {
	var header [4]byte
	if _, err := io.ReadFull(d.reader, header[:]); err != nil {
		return Message{}, err
	}

	length := binary.BigEndian.Uint32(header[:])
	if length == 0 {
		return Message{}, errors.New("runner frame cannot be empty")
	}
	if length > d.maxFrameBytes {
		return Message{}, fmt.Errorf("runner frame declares %d bytes, limit is %d", length, d.maxFrameBytes)
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(d.reader, data); err != nil {
		return Message{}, fmt.Errorf("read runner frame payload: %w", err)
	}

	var message Message
	if err := d.decoding.Unmarshal(data, &message); err != nil {
		return Message{}, fmt.Errorf("decode runner frame: %w", err)
	}
	if err := message.Validate(); err != nil {
		return Message{}, fmt.Errorf("validate runner frame: %w", err)
	}
	return message, nil
}
