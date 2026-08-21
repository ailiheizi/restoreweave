// Package decode implements host-owned DECODE_REPRESENTATION for exact/raw
// representations. Decoders receive only an EncodedRangeSource; they cannot
// open repositories, paths, or networks. The host independently digests
// decoded bytes and never treats decoder claims as identity proof.
package decode

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/ailiheizi/restoreweave/server/internal/readsvc"
)

const (
	IdentityDecoderID = "decode:identity.v1"
	IdentityCodec     = "identity/sha256-v1"
)

var (
	ErrUnsupportedCodec = errors.New("representation codec is not identity")
	ErrBudgetExceeded   = errors.New("decode exceeded host budget")
)

// IdentityDecoder copies one RepositoryDriver logical range to the output.
// Backend-private compression is already decoded by the driver, so current
// exact representations retain a 1:1 mapping here.
type IdentityDecoder struct{}

func (IdentityDecoder) ID() string { return IdentityDecoderID }

func (IdentityDecoder) Capabilities() readsvc.DecoderCapabilities {
	return readsvc.DecoderCapabilities{
		AccessMode:          readsvc.AccessRandomNative,
		MinimumReadableUnit: 1,
		MaximumConcurrency:  1,
	}
}

func (IdentityDecoder) DecodeRange(
	ctx context.Context,
	request readsvc.DecodeRequest,
	source readsvc.EncodedRangeSource,
	out io.Writer,
) (readsvc.DecodeReceipt, error) {
	var receipt readsvc.DecodeReceipt
	receipt.DecoderID = IdentityDecoderID
	if err := ctx.Err(); err != nil {
		return receipt, err
	}
	if source == nil || out == nil {
		return receipt, errors.New("decode requires a host source and output")
	}
	codec := request.Representation.CodecProfileRef
	if codec != "" && codec != IdentityCodec {
		return receipt, fmt.Errorf("%w: %s", ErrUnsupportedCodec, codec)
	}
	size := source.Size()
	if err := request.OutputRange.Validate(size); err != nil {
		return receipt, err
	}
	if request.Budget.MaxOutputBytes > 0 && request.OutputRange.Length > request.Budget.MaxOutputBytes {
		return receipt, ErrBudgetExceeded
	}
	if request.Budget.MaxSourceBytes > 0 && request.OutputRange.Length > request.Budget.MaxSourceBytes {
		return receipt, ErrBudgetExceeded
	}
	encoded, err := source.ReadRange(ctx, request.OutputRange, out)
	if err != nil {
		return receipt, err
	}
	receipt.BytesWritten = encoded.BytesWritten
	receipt.EncodedBytesRead = encoded.SourceBytesRead
	receipt.Claims = encoded.Claims
	return receipt, nil
}
