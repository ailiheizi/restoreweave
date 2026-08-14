//go:build unix

package rpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"

	"github.com/ailiheizi/restoreweave/server/internal/processor"
)

// CallSpec is a host-owned RUN_STAGE invocation. Source and staging files
// stay on the host; only their descriptors are sent.
type CallSpec struct {
	Socket         string
	Source         *os.File
	Staging        *os.File
	AttemptID      string
	FenceToken     int64
	CapabilityID   string
	Stage          string
	MaxOutputBytes int64
}

// Result is the worker's control outcome plus the host's independent digest
// of the staging file.
type Result struct {
	Response
	Digest      string
	ByteLength  int64
	Request     []byte
	ResponseRaw []byte
}

// RunStage sends two FDs then one protobuf request. It never puts source or
// staging bytes into the control frame. The host hashes staging itself.
func RunStage(ctx context.Context, spec CallSpec) (Result, error) {
	var out Result
	if spec.Source == nil || spec.Staging == nil {
		return out, fmt.Errorf("source and staging files are required")
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", spec.Socket)
	if err != nil {
		return out, err
	}
	defer conn.Close()
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return out, fmt.Errorf("connection is not a unix socket")
	}
	if err := sendFDs(unixConn, int(spec.Source.Fd()), int(spec.Staging.Fd())); err != nil {
		return out, fmt.Errorf("send fds: %w", err)
	}
	runtime.KeepAlive(spec.Source)
	runtime.KeepAlive(spec.Staging)
	req := Request{
		AttemptID:      spec.AttemptID,
		FenceToken:     spec.FenceToken,
		CapabilityID:   spec.CapabilityID,
		Stage:          spec.Stage,
		MaxOutputBytes: spec.MaxOutputBytes,
		SourceFDIndex:  0,
		StagingFDIndex: 1,
	}
	out.Request = marshalRequest(req)
	if err := writeFrame(conn, out.Request); err != nil {
		return out, err
	}
	raw, err := readFrame(conn)
	if err != nil {
		return out, err
	}
	out.ResponseRaw = raw
	res, err := unmarshalResponse(raw)
	if err != nil {
		return out, err
	}
	out.Response = res
	if _, err := spec.Staging.Seek(0, io.SeekStart); err != nil {
		return out, err
	}
	sum := sha256.New()
	n, err := io.Copy(sum, spec.Staging)
	if err != nil {
		return out, err
	}
	out.ByteLength = n
	out.Digest = "sha256:" + hex.EncodeToString(sum.Sum(nil))
	runtime.KeepAlive(spec.Source)
	runtime.KeepAlive(spec.Staging)
	if res.Status == string(processor.StatusSucceeded) && !res.Sealed {
		return out, fmt.Errorf("worker reported success without sealing staging")
	}
	return out, nil
}
