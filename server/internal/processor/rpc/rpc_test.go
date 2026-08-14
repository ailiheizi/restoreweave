//go:build unix

package rpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/processor"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestRunStagePassesBytesOnFDsNotInControlFrames(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	payload := bytes.Repeat([]byte("unique-rpc-extract-token\n"), 4096)
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "note.txt")
	stagingPath := filepath.Join(dir, "out.stage")
	if err := os.WriteFile(sourcePath, payload, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	staging, err := os.OpenFile(stagingPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("create staging: %v", err)
	}
	defer staging.Close()
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	defer source.Close()

	socket := testutil.TempSocketPath(t)
	lis, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lis.Close()
	server := &Server{Processors: map[string]processor.Processor{
		processor.CapabilityTextExtract: processor.TextExtract{},
	}}
	go func() { _ = server.Serve(ctx, lis) }()

	result, err := RunStage(ctx, CallSpec{
		Socket:         socket,
		Source:         source,
		Staging:        staging,
		AttemptID:      "attempt-1",
		FenceToken:     1,
		CapabilityID:   processor.CapabilityTextExtract,
		Stage:          string(processor.StageExtract),
		MaxOutputBytes: int64(len(payload) + 16),
	})
	if err != nil {
		t.Fatalf("run stage: %v", err)
	}
	if result.Status != string(processor.StatusSucceeded) || !result.Sealed {
		t.Fatalf("result = %+v", result.Response)
	}
	if result.SchemaRef != processor.SchemaRefExtractedText() || result.MediaType != processor.MediaTypeUTF8Text {
		t.Fatalf("schema/media = %s %s", result.SchemaRef, result.MediaType)
	}
	if bytes.Contains(result.Request, payload) || bytes.Contains(result.ResponseRaw, payload) {
		t.Fatal("control frames contained source payload")
	}
	if len(result.Request) > 256 || len(result.ResponseRaw) > 512 {
		t.Fatalf("control frames too large: req=%d res=%d", len(result.Request), len(result.ResponseRaw))
	}
	sum := sha256.Sum256(payload)
	want := "sha256:" + hex.EncodeToString(sum[:])
	if result.Digest != want || result.ByteLength != int64(len(payload)) {
		t.Fatalf("host digest = %s len=%d, want %s %d", result.Digest, result.ByteLength, want, len(payload))
	}
	got, err := os.ReadFile(stagingPath)
	if err != nil {
		t.Fatalf("read staging: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("staging bytes mismatch")
	}
}

func TestRunStageUnknownCapabilityFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	source, err := os.Create(filepath.Join(dir, "src"))
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	defer source.Close()
	staging, err := os.Create(filepath.Join(dir, "stg"))
	if err != nil {
		t.Fatalf("staging: %v", err)
	}
	defer staging.Close()
	socket := testutil.TempSocketPath(t)
	lis, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lis.Close()
	server := &Server{Processors: map[string]processor.Processor{}}
	go func() { _ = server.Serve(ctx, lis) }()
	result, err := RunStage(ctx, CallSpec{
		Socket:         socket,
		Source:         source,
		Staging:        staging,
		CapabilityID:   "extract.missing.v1",
		MaxOutputBytes: 32,
	})
	if err != nil {
		t.Fatalf("run stage: %v", err)
	}
	if result.Status != string(processor.StatusFailed) || !strings.Contains(result.Reason, "unknown capability") {
		t.Fatalf("result = %+v", result.Response)
	}
}

func TestCodecRoundTripOmitsEmptyFields(t *testing.T) {
	req := Request{AttemptID: "a1", CapabilityID: "extract.text.v1", SourceFDIndex: 0, StagingFDIndex: 1, FenceToken: 3}
	got, err := unmarshalRequest(marshalRequest(req))
	if err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if got != req {
		t.Fatalf("request = %+v, want %+v", got, req)
	}
	res := Response{Status: "SUCCEEDED", Sealed: true, SchemaRef: "sha256:abc"}
	decoded, err := unmarshalResponse(marshalResponse(res))
	if err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if decoded != res {
		t.Fatalf("response = %+v, want %+v", decoded, res)
	}
}
