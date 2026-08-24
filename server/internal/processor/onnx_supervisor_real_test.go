//go:build cgo && purego && arm64 && supervised_integration && (darwin || linux)

package processor

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/processor/sandbox"
	"github.com/ailiheizi/restoreweave/server/internal/search"
)

// TestSupervisedONNXWorkerRealEmbedding proves the production process boundary
// with the pinned local assets. It is opt-in because the bundle is a
// platform-owned input, never a test fixture; Linux additionally exercises
// bubblewrap while Darwin exercises the authenticated process-only path.
func TestSupervisedONNXWorkerRealEmbedding(t *testing.T) {
	if strings.TrimSpace(os.Getenv("RESTOREWEAVE_RUN_SUPERVISED_ONNX")) != "1" {
		t.Skip("RESTOREWEAVE_RUN_SUPERVISED_ONNX=1 is required for supervised qualification")
	}
	runtimeSource := strings.TrimSpace(os.Getenv("RESTOREWEAVE_BGE_ONNX_RUNTIME"))
	modelSource := strings.TrimSpace(os.Getenv("RESTOREWEAVE_BGE_ONNX_MODEL"))
	tokenizerSource := strings.TrimSpace(os.Getenv("RESTOREWEAVE_BGE_TOKENIZER"))
	if runtimeSource == "" || modelSource == "" || tokenizerSource == "" {
		t.Skip("real BGE asset paths are required for supervised qualification")
	}
	if runtime.GOOS == "linux" && !sandbox.Supported() {
		t.Skip("Linux bubblewrap is unavailable for supervised qualification")
	}

	bundleRoot, admission := realONNXAdmission(t, runtimeSource, modelSource, tokenizerSource)
	daemon := buildRestoreweavedForWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	worker, closeWorker, err := StartONNXWorker(ctx, ONNXWorkerSupervisorOptions{
		Command: daemon, BundleRoot: bundleRoot, ConfigDigest: admission.Manifest.ConfigDigest,
		GenerationID: "supervised-generation-1", FenceToken: 7,
		SandboxPolicyDigest: sandbox.PolicyDigest(),
		FenceValidator:      func(context.Context) error { return nil },
		HandshakeTimeout:    25 * time.Second,
	})
	if err != nil {
		t.Fatalf("start supervised ONNX worker: %v", err)
	}
	defer closeWorker()
	if worker.Admission.Capability.State != ONNXWorkerStateReady {
		t.Fatalf("worker state = %s, want READY", worker.Admission.Capability.State)
	}
	if worker.Admission.Capability.CAPI != search.SemanticBundleONNXRuntimeCAPI {
		t.Fatalf("worker C API = %d, want %d", worker.Admission.Capability.CAPI, search.SemanticBundleONNXRuntimeCAPI)
	}
	provider, err := NewONNXSemanticEmbeddingProvider(worker)
	if err != nil {
		t.Fatalf("construct semantic provider: %v", err)
	}

	const text = "测试中文语义恢复描述"
	document, err := provider.Embed(ctx, search.SemanticEmbeddingRequest{
		Purpose: search.SemanticEmbeddingDocument, GenerationID: "supervised-generation-1", Manifest: admission.Manifest,
		Inputs: []search.SemanticTextInput{{SubjectID: "subject-real", SegmentID: "segment-real", DescriptionDocumentID: "description-real", Ordinal: 0, Language: "zh", Text: text}},
	})
	if err != nil {
		t.Fatalf("supervised document embedding: %v", err)
	}
	assertSupervisedVector(t, document, "subject-real", "segment-real", admission.Manifest.Dimension)
	query, err := provider.Embed(ctx, search.SemanticEmbeddingRequest{
		Purpose: search.SemanticEmbeddingQuery, GenerationID: "supervised-generation-1", Manifest: admission.Manifest,
		Inputs: []search.SemanticTextInput{{SegmentID: "query-real", Text: text}},
	})
	if err != nil {
		t.Fatalf("supervised query embedding: %v", err)
	}
	assertSupervisedVector(t, query, "", "query-real", admission.Manifest.Dimension)

	var cosine float64
	for i, value := range document[0].Vector {
		cosine += float64(value) * float64(query[0].Vector[i])
	}
	if cosine <= 0 || cosine > 1.01 {
		t.Fatalf("document/query cosine = %v, want a finite positive cosine", cosine)
	}
}

func assertSupervisedVector(t *testing.T, vectors []search.SemanticVector, subject, segment string, dimension int) {
	t.Helper()
	if len(vectors) != 1 || vectors[0].SubjectID != subject || vectors[0].SegmentID != segment || len(vectors[0].Vector) != dimension {
		t.Fatalf("supervised vectors = %+v", vectors)
	}
	var norm float64
	for _, value := range vectors[0].Vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			t.Fatal("supervised vector contains a non-finite value")
		}
		norm += float64(value) * float64(value)
	}
	if math.Abs(math.Sqrt(norm)-1) > EmbedTextL2NormTolerance {
		t.Fatalf("supervised vector norm = %v", math.Sqrt(norm))
	}
}

func buildRestoreweavedForWorker(t *testing.T) string {
	t.Helper()
	if binary := strings.TrimSpace(os.Getenv("RESTOREWEAVE_WORKER_BINARY")); binary != "" {
		info, err := os.Stat(binary)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("RESTOREWEAVE_WORKER_BINARY is not executable: %q (%v)", binary, err)
		}
		return binary
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate processor source root")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", ".."))
	output := filepath.Join(t.TempDir(), "restoreweaved")
	cmd := exec.Command("go", "build", "-tags", "purego", "-o", output, "./server/cmd/restoreweaved")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build restoreweaved worker binary: %v\n%s", err, out)
	}
	return output
}
