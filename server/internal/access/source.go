package access

import (
	"context"
	"io"

	"github.com/ailiheizi/restoreweave/server/internal/readsvc"
)

// encodedCAS is a host-owned EncodedRangeSource over already-read exact
// bytes. Decoders never receive a repository path or credential.
type encodedCAS struct {
	body []byte
}

func (s encodedCAS) Size() uint64 { return uint64(len(s.body)) }

func (s encodedCAS) ReadRange(ctx context.Context, r readsvc.ByteRange, w io.Writer) (readsvc.EncodedRangeReceipt, error) {
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

var _ readsvc.EncodedRangeSource = encodedCAS{}
