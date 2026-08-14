//go:build unix

package rpc

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/ailiheizi/restoreweave/server/internal/processor"
)

// Server accepts one Unix connection per RUN_STAGE, receives source and
// staging FDs, and runs a registered in-process Processor. Control frames
// never carry payload bytes.
type Server struct {
	Processors map[string]processor.Processor
}

func (s *Server) Serve(ctx context.Context, lis net.Listener) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		conn, err := lis.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		go s.handle(ctx, conn)
	}
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return
	}
	fds, err := recvFDs(unixConn, 2)
	if err != nil {
		_ = writeFrame(conn, marshalResponse(Response{Status: string(processor.StatusFailed), Reason: err.Error()}))
		return
	}
	sourceFile := os.NewFile(uintptr(fds[0]), "source")
	stagingFile := os.NewFile(uintptr(fds[1]), "staging")
	defer sourceFile.Close()
	defer stagingFile.Close()

	raw, err := readFrame(conn)
	if err != nil {
		return
	}
	req, err := unmarshalRequest(raw)
	if err != nil {
		_ = writeFrame(conn, marshalResponse(Response{Status: string(processor.StatusFailed), Reason: err.Error()}))
		return
	}
	res := s.run(ctx, req, sourceFile, stagingFile)
	_ = writeFrame(conn, marshalResponse(res))
}

func (s *Server) run(ctx context.Context, req Request, sourceFile, stagingFile *os.File) Response {
	if req.SourceFDIndex != 0 || req.StagingFDIndex != 1 {
		return Response{Status: string(processor.StatusFailed), Reason: "fd indexes must be 0=source, 1=staging"}
	}
	proc := s.Processors[req.CapabilityID]
	if proc == nil {
		return Response{Status: string(processor.StatusFailed), Reason: fmt.Sprintf("unknown capability %q", req.CapabilityID)}
	}
	source, err := processor.SourceFromFile(ctx, sourceFile, req.MaxOutputBytes)
	if err != nil {
		return Response{Status: string(processor.StatusFailed), Reason: err.Error()}
	}
	defer source.Close()
	staging, err := processor.StagingFromFile(stagingFile, req.MaxOutputBytes)
	if err != nil {
		return Response{Status: string(processor.StatusFailed), Reason: err.Error()}
	}
	defer staging.Close()
	result, runErr := proc.RunStage(ctx, processor.Invocation{
		AttemptID:      req.AttemptID,
		FenceToken:     req.FenceToken,
		Node:           processor.RouteNode{Stage: processor.Stage(req.Stage), CapabilityID: req.CapabilityID},
		Source:         source,
		Staging:        staging,
		MaxOutputBytes: req.MaxOutputBytes,
	})
	if runErr != nil && result.Status == "" {
		result.Status = processor.StatusFailed
		result.Reason = runErr.Error()
	}
	return Response{
		Status:           string(result.Status),
		DeterminismClass: result.DeterminismClass,
		SchemaRef:        result.SchemaRef,
		MediaType:        result.MediaType,
		Sealed:           result.Sealed,
		Reason:           result.Reason,
	}
}
