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

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const processorRunStageMethod = "/restoreweave.processor.v1.Processor/RunStage"

type runStageServer interface {
	RunStageRPC(ctx context.Context, req *Request) (*Response, error)
}

var processorServiceDesc = grpc.ServiceDesc{
	ServiceName: "restoreweave.processor.v1.Processor",
	HandlerType: (*runStageServer)(nil),
	Methods: []grpc.MethodDesc{{
		MethodName: "RunStage",
		Handler:    runStageHandler,
	}},
}

func runStageHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(Request)
	if err := dec(in); err != nil {
		return nil, err
	}
	impl := srv.(runStageServer)
	if interceptor == nil {
		return impl.RunStageRPC(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: processorRunStageMethod}
	handler := func(ctx context.Context, req any) (any, error) {
		return impl.RunStageRPC(ctx, req.(*Request))
	}
	return interceptor(ctx, in, info, handler)
}

// ServeGRPC accepts Unix connections, receives source/staging FDs during the
// transport handshake, then serves RUN_STAGE over grpc-go. It is private
// plumbing; default ingest stays in-process.
func (s *Server) ServeGRPC(ctx context.Context, lis net.Listener) error {
	gs := grpc.NewServer(
		grpc.Creds(&fdCreds{}),
		grpc.ForceServerCodec(controlCodec{}),
	)
	gs.RegisterService(&processorServiceDesc, s)
	errCh := make(chan error, 1)
	go func() { errCh <- gs.Serve(lis) }()
	select {
	case <-ctx.Done():
		gs.GracefulStop()
		<-errCh
		return nil
	case err := <-errCh:
		return err
	}
}

// RunStageRPC is the grpc-go RUN_STAGE handler. Processor failures are
// returned in the control Response; they are not rewritten as transport
// success.
func (s *Server) RunStageRPC(ctx context.Context, req *Request) (*Response, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Internal, "missing peer")
	}
	auth, ok := p.AuthInfo.(fdAuth)
	if !ok {
		return nil, status.Error(codes.Internal, "missing unix file descriptors")
	}
	sourceFile := os.NewFile(uintptr(auth.source), "source")
	stagingFile := os.NewFile(uintptr(auth.staging), "staging")
	defer sourceFile.Close()
	defer stagingFile.Close()
	res := s.run(ctx, *req, sourceFile, stagingFile)
	return &res, nil
}

// RunStageGRPC sends two FDs during the Unix handshake, then one grpc-go
// RUN_STAGE RPC using the same protobuf messages. Source and staging bytes
// stay out of gRPC messages. The host hashes staging itself.
func RunStageGRPC(ctx context.Context, spec CallSpec) (Result, error) {
	var out Result
	if spec.Source == nil || spec.Staging == nil {
		return out, fmt.Errorf("source and staging files are required")
	}
	cc, err := grpc.NewClient("unix://"+spec.Socket,
		grpc.WithTransportCredentials(&fdCreds{source: spec.Source, staging: spec.Staging}),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(controlCodec{})),
	)
	if err != nil {
		return out, err
	}
	defer cc.Close()
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
	var res Response
	if err := cc.Invoke(ctx, processorRunStageMethod, &req, &res); err != nil {
		return out, err
	}
	out.Response = res
	out.ResponseRaw = marshalResponse(res)
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
	if res.Status == "SUCCEEDED" && !res.Sealed {
		return out, fmt.Errorf("worker reported success without sealing staging")
	}
	return out, nil
}
