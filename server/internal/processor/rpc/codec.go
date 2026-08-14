package rpc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const maxControlFrame = 64 << 10

var errFrameTooLarge = errors.New("processor control frame exceeds 64KiB")

func writeFrame(w io.Writer, payload []byte) error {
	if len(payload) > maxControlFrame {
		return errFrameTooLarge
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func readFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(header[:])
	if n > maxControlFrame {
		return nil, fmt.Errorf("%w: %d", errFrameTooLarge, n)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func appendVarint(buf []byte, v uint64) []byte {
	for v >= 0x80 {
		buf = append(buf, byte(v)|0x80)
		v >>= 7
	}
	return append(buf, byte(v))
}

func consumeVarint(buf []byte) (uint64, int) {
	var n uint64
	var shift uint
	for i, b := range buf {
		if shift >= 64 {
			return 0, 0
		}
		n |= uint64(b&0x7f) << shift
		if b < 0x80 {
			return n, i + 1
		}
		shift += 7
	}
	return 0, 0
}

func appendString(buf []byte, field int, value string) []byte {
	if value == "" {
		return buf
	}
	buf = appendVarint(buf, uint64(field<<3|2))
	buf = appendVarint(buf, uint64(len(value)))
	return append(buf, value...)
}

func appendInt(buf []byte, field int, value int64) []byte {
	if value == 0 {
		return buf
	}
	buf = appendVarint(buf, uint64(field<<3))
	return appendVarint(buf, uint64(value))
}

func appendBool(buf []byte, field int, value bool) []byte {
	if !value {
		return buf
	}
	buf = appendVarint(buf, uint64(field<<3))
	return append(buf, 1)
}

func marshalRequest(req Request) []byte {
	buf := appendString(nil, 1, req.AttemptID)
	buf = appendInt(buf, 2, req.FenceToken)
	buf = appendString(buf, 3, req.CapabilityID)
	buf = appendString(buf, 4, req.Stage)
	buf = appendInt(buf, 5, req.MaxOutputBytes)
	buf = appendInt(buf, 6, int64(req.SourceFDIndex))
	buf = appendInt(buf, 7, int64(req.StagingFDIndex))
	return buf
}

func marshalResponse(res Response) []byte {
	buf := appendString(nil, 1, res.Status)
	buf = appendString(buf, 2, res.DeterminismClass)
	buf = appendString(buf, 3, res.SchemaRef)
	buf = appendString(buf, 4, res.MediaType)
	buf = appendBool(buf, 5, res.Sealed)
	buf = appendString(buf, 6, res.Reason)
	return buf
}

func unmarshalRequest(buf []byte) (Request, error) {
	var req Request
	if err := walk(buf, func(field int, wire int, value []byte, n uint64) error {
		switch field {
		case 1:
			req.AttemptID = string(value)
		case 2:
			req.FenceToken = int64(n)
		case 3:
			req.CapabilityID = string(value)
		case 4:
			req.Stage = string(value)
		case 5:
			req.MaxOutputBytes = int64(n)
		case 6:
			req.SourceFDIndex = int(n)
		case 7:
			req.StagingFDIndex = int(n)
		}
		return nil
	}); err != nil {
		return Request{}, err
	}
	return req, nil
}

func unmarshalResponse(buf []byte) (Response, error) {
	var res Response
	if err := walk(buf, func(field int, wire int, value []byte, n uint64) error {
		switch field {
		case 1:
			res.Status = string(value)
		case 2:
			res.DeterminismClass = string(value)
		case 3:
			res.SchemaRef = string(value)
		case 4:
			res.MediaType = string(value)
		case 5:
			res.Sealed = n != 0
		case 6:
			res.Reason = string(value)
		}
		return nil
	}); err != nil {
		return Response{}, err
	}
	return res, nil
}

func walk(buf []byte, fn func(field, wire int, value []byte, n uint64) error) error {
	for len(buf) > 0 {
		tag, n := consumeVarint(buf)
		if n == 0 {
			return errors.New("invalid protobuf tag")
		}
		buf = buf[n:]
		field := int(tag >> 3)
		wire := int(tag & 7)
		switch wire {
		case 0:
			v, m := consumeVarint(buf)
			if m == 0 {
				return errors.New("invalid protobuf varint")
			}
			buf = buf[m:]
			if err := fn(field, wire, nil, v); err != nil {
				return err
			}
		case 2:
			size, m := consumeVarint(buf)
			if m == 0 {
				return errors.New("invalid protobuf length")
			}
			buf = buf[m:]
			if uint64(len(buf)) < size {
				return errors.New("truncated protobuf string")
			}
			value := buf[:size]
			buf = buf[size:]
			if err := fn(field, wire, value, 0); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported protobuf wire type %d", wire)
		}
	}
	return nil
}
