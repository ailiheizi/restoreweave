//go:build cgo && purego && linux && arm64 && supervised_integration

package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/processor/sandbox"
	"github.com/ailiheizi/restoreweave/server/internal/search"
)

// TestRealBGEEmbeddingBuildsAndQueriesNativeZvec is an opt-in component
// proof. It deliberately exercises the package-local native measurement seam;
// only the supervised worker may establish production readiness.
func TestRealBGEEmbeddingBuildsAndQueriesNativeZvec(t *testing.T) {
	runtimeSource := strings.TrimSpace(os.Getenv("RESTOREWEAVE_BGE_ONNX_RUNTIME"))
	modelSource := strings.TrimSpace(os.Getenv("RESTOREWEAVE_BGE_ONNX_MODEL"))
	tokenizerSource := strings.TrimSpace(os.Getenv("RESTOREWEAVE_BGE_TOKENIZER"))
	zvecSource := strings.TrimSpace(os.Getenv("RESTOREWEAVE_ZVEC_LIBRARY"))
	if runtimeSource == "" || modelSource == "" || tokenizerSource == "" || zvecSource == "" {
		t.Skip("real BGE and zvec asset paths are required for the opt-in integration test")
	}

	bundleRoot, admission := realONNXAdmission(t, runtimeSource, modelSource, tokenizerSource)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	workerBinary := buildRestoreweavedForWorker(t)
	worker, closeWorker, err := StartONNXWorker(ctx, ONNXWorkerSupervisorOptions{
		Command: workerBinary, BundleRoot: bundleRoot, ConfigDigest: admission.Manifest.ConfigDigest,
		GenerationID: "zvec-supervised-generation", FenceToken: 11,
		SandboxPolicyDigest: sandbox.PolicyDigest(),
		FenceValidator: func(context.Context) error { return nil }, HandshakeTimeout: 25 * time.Second,
	})
	if err != nil {
		t.Fatalf("start supervised worker: %v", err)
	}
	defer closeWorker()
	provider, err := NewONNXSemanticEmbeddingProvider(worker)
	if err != nil {
		t.Fatalf("construct supervised provider: %v", err)
	}

	text := []byte("测试中文语义恢复描述")
	documentVectors, err := provider.Embed(ctx, search.SemanticEmbeddingRequest{
		Purpose: search.SemanticEmbeddingDocument, GenerationID: "zvec-supervised-generation", Manifest: admission.Manifest,
		Inputs: []search.SemanticTextInput{{SubjectID: "subject-bge", SegmentID: "segment-bge", DescriptionDocumentID: "description-bge", Ordinal: 0, Language: "zh", Text: string(text)}},
	})
	if err != nil {
		t.Fatalf("supervised document embedding: %v", err)
	}
	if len(documentVectors) != 1 || len(documentVectors[0].Vector) != admission.Manifest.Dimension {
		t.Fatalf("supervised document vectors = %+v", documentVectors)
	}

	queryVectors, err := provider.Embed(ctx, search.SemanticEmbeddingRequest{
		Purpose: search.SemanticEmbeddingQuery, GenerationID: "zvec-supervised-generation", Manifest: admission.Manifest,
		Inputs: []search.SemanticTextInput{{SegmentID: "query-bge", Text: string(text)}},
	})
	if err != nil {
		t.Fatalf("supervised query embedding: %v", err)
	}
	if len(queryVectors) != 1 || len(queryVectors[0].Vector) != admission.Manifest.Dimension {
		t.Fatalf("supervised query vectors = %+v", queryVectors)
	}

	library, err := os.ReadFile(zvecSource)
	if err != nil {
		t.Fatalf("read native zvec library: %v", err)
	}
	digest := sha256.Sum256(library)
	manifest := admission.Manifest
	spec := search.ZvecGenerationSpec{
		Path:          filepath.Join(t.TempDir(), "real-bge-generation"),
		LibraryPath:   zvecSource,
		LibraryDigest: "sha256:" + hex.EncodeToString(digest[:]),
		Manifest:      manifest,
		ProfileDigest: manifest.CanonicalDigest(),
	}
	driver := search.NewZvecGenerationDriver(zvecSource)
	segments := []search.ZvecSegment{{SubjectID: "subject-bge", SegmentID: "segment-bge", Vector: documentVectors[0].Vector}}
	if _, err := driver.Build(ctx, spec, segments); err != nil {
		t.Fatalf("build native zvec generation from BGE vector: %v", err)
	}
	opened, err := driver.Open(ctx, spec)
	if err != nil {
		t.Fatalf("open native zvec generation: %v", err)
	}
	defer opened.Close()
	hits, err := opened.Query(ctx, queryVectors[0].Vector, 1)
	if err != nil {
		t.Fatalf("query native zvec generation: %v", err)
	}
	if len(hits) != 1 || hits[0].SubjectID != "subject-bge" || hits[0].SegmentID != "segment-bge" {
		t.Fatalf("native zvec hits = %+v", hits)
	}

}
