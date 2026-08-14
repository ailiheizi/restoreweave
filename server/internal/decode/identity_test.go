package decode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/readsvc"
)

type memSource struct {
	body []byte
}

func (s memSource) Size() uint64 { return uint64(len(s.body)) }

func (s memSource) ReadRange(ctx context.Context, r readsvc.ByteRange, w io.Writer) (readsvc.EncodedRangeReceipt, error) {
	if err := ctx.Err(); err != nil {
		return readsvc.EncodedRangeReceipt{}, err
	}
	if err := r.Validate(s.Size()); err != nil {
		return readsvc.EncodedRangeReceipt{}, err
	}
	n, err := w.Write(s.body[r.Offset : r.Offset+r.Length])
	return readsvc.EncodedRangeReceipt{
		BytesWritten:    uint64(n),
		SourceBytesRead: uint64(n),
	}, err
}

func TestIdentityDecodeMatchesEncodedBytes(t *testing.T) {
	ctx := context.Background()
	payload := []byte("exact-identity-bytes")
	var out bytes.Buffer
	receipt, err := IdentityDecoder{}.DecodeRange(ctx, readsvc.DecodeRequest{
		Representation: readsvc.RepresentationRef{
			ContentID:       digestOf(payload),
			LogicalSize:     uint64(len(payload)),
			CodecProfileRef: IdentityCodec,
			AccessMode:      readsvc.AccessRandomNative,
		},
		OutputRange: readsvc.ByteRange{Offset: 0, Length: uint64(len(payload))},
	}, memSource{body: payload}, &out)
	if err != nil {
		t.Fatalf("DecodeRange: %v", err)
	}
	if !bytes.Equal(out.Bytes(), payload) {
		t.Fatalf("decoded = %q, want %q", out.Bytes(), payload)
	}
	if receipt.DecoderID != IdentityDecoderID || receipt.BytesWritten != uint64(len(payload)) {
		t.Fatalf("receipt = %+v", receipt)
	}
	if digestOf(out.Bytes()) != digestOf(payload) {
		t.Fatal("host digest of decoded bytes diverged from source")
	}
}

func TestIdentityDecodeRejectsUnknownCodec(t *testing.T) {
	_, err := IdentityDecoder{}.DecodeRange(context.Background(), readsvc.DecodeRequest{
		Representation: readsvc.RepresentationRef{CodecProfileRef: "lossy/preview-v1"},
		OutputRange:    readsvc.ByteRange{Length: 1},
	}, memSource{body: []byte("x")}, io.Discard)
	if !errors.Is(err, ErrUnsupportedCodec) {
		t.Fatalf("error = %v, want ErrUnsupportedCodec", err)
	}
}

func TestIdentityDecodeRespectsRangeAndBudget(t *testing.T) {
	payload := []byte("abcdefghij")
	var out bytes.Buffer
	_, err := IdentityDecoder{}.DecodeRange(context.Background(), readsvc.DecodeRequest{
		Representation: readsvc.RepresentationRef{CodecProfileRef: IdentityCodec},
		OutputRange:    readsvc.ByteRange{Offset: 3, Length: 4},
		Budget:         readsvc.ReadBudget{MaxOutputBytes: 4},
	}, memSource{body: payload}, &out)
	if err != nil {
		t.Fatalf("range decode: %v", err)
	}
	if !bytes.Equal(out.Bytes(), []byte("defg")) {
		t.Fatalf("range = %q", out.Bytes())
	}
	_, err = IdentityDecoder{}.DecodeRange(context.Background(), readsvc.DecodeRequest{
		Representation: readsvc.RepresentationRef{CodecProfileRef: IdentityCodec},
		OutputRange:    readsvc.ByteRange{Length: 4},
		Budget:         readsvc.ReadBudget{MaxOutputBytes: 2},
	}, memSource{body: payload}, io.Discard)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("budget error = %v", err)
	}
}

func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}
