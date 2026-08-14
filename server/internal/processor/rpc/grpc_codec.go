package rpc

import (
	"fmt"

	"google.golang.org/grpc/encoding"
)

// controlCodec serializes the same RUN_STAGE protobuf messages used by the
// length-prefixed Unix frames. It is a private content subtype, not a public
// ABI, and never encodes source or staging bytes.
type controlCodec struct{}

func (controlCodec) Name() string { return "rw-processor" }

func (controlCodec) Marshal(v any) ([]byte, error) {
	switch m := v.(type) {
	case *Request:
		return marshalRequest(*m), nil
	case Request:
		return marshalRequest(m), nil
	case *Response:
		return marshalResponse(*m), nil
	case Response:
		return marshalResponse(m), nil
	default:
		return nil, fmt.Errorf("unsupported processor control type %T", v)
	}
}

func (controlCodec) Unmarshal(data []byte, v any) error {
	switch m := v.(type) {
	case *Request:
		got, err := unmarshalRequest(data)
		if err != nil {
			return err
		}
		*m = got
		return nil
	case *Response:
		got, err := unmarshalResponse(data)
		if err != nil {
			return err
		}
		*m = got
		return nil
	default:
		return fmt.Errorf("unsupported processor control type %T", v)
	}
}

var _ encoding.Codec = controlCodec{}
