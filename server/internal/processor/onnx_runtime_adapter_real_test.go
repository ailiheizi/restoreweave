//go:build cgo && (darwin || linux)

package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/search"
)

func TestONNXRuntimeAdapterExecutesPinnedBGEAssets(t *testing.T) {
	runtimeSource := os.Getenv("RESTOREWEAVE_BGE_ONNX_RUNTIME")
	modelSource := os.Getenv("RESTOREWEAVE_BGE_ONNX_MODEL")
	tokenizerSource := os.Getenv("RESTOREWEAVE_BGE_TOKENIZER")
	if runtimeSource == "" || modelSource == "" || tokenizerSource == "" {
		t.Skip("real ONNX/BGE asset paths are not set")
	}
	root, admission := realONNXAdmission(t, runtimeSource, modelSource, tokenizerSource)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	adapter, err := NewONNXRuntimeAdapter(ctx, admission, ONNXRuntimeAdapterOptions{})
	if err != nil {
		t.Fatalf("create real adapter: %v", err)
	}
	defer func() {
		if adapter != nil {
			if err := adapter.Close(); err != nil {
				t.Errorf("close real adapter: %v", err)
			}
		}
	}()

	probe, probeErr := adapter.Probe(ctx, admission)
	if !errors.Is(probeErr, ErrONNXRuntimeUnavailable) || probe.ModelLoaded || probe.TokenizerLoaded || probe.IsolationClass == ONNXWorkerIsolationProcess {
		t.Fatalf("in-process session crossed readiness boundary: probe=%+v err=%v", probe, probeErr)
	}

	text := []byte("测试中文语义")
	document := realONNXRequest(admission, EmbedTextPurposeDocument, text)
	publicBatch, publicErr := adapter.EmbedTextWithText(ctx, document, []ONNXWorkerTextInput{{SegmentID: document.Segments[0].ID, Text: text}})
	if !errors.Is(publicErr, ErrONNXRuntimeUnavailable) {
		t.Fatalf("public in-process execution error = %v, want unavailable", publicErr)
	}
	if err := ValidateEmbedTextResult(document, publicBatch); err != nil || publicBatch.Results[0].Status == EmbedTextAccepted {
		t.Fatalf("public in-process path crossed readiness gate: batch=%+v err=%v", publicBatch, err)
	}
	documentBatch, err := adapter.measureEmbedTextWithText(ctx, document, []ONNXWorkerTextInput{{SegmentID: document.Segments[0].ID, Text: text}})
	if err != nil {
		t.Fatalf("document embedding: %v", err)
	}
	assertRealONNXBatch(t, document, documentBatch)
	repeatedDocument, err := adapter.measureEmbedTextWithText(ctx, document, []ONNXWorkerTextInput{{SegmentID: document.Segments[0].ID, Text: text}})
	if err != nil {
		t.Fatalf("repeat document embedding: %v", err)
	}
	if !reflect.DeepEqual(documentBatch.Results[0].Vector, repeatedDocument.Results[0].Vector) {
		t.Fatal("same-session CPU inference changed the measured document vector")
	}

	query := realONNXRequest(admission, EmbedTextPurposeQuery, text)
	queryBatch, err := adapter.measureEmbedTextWithText(ctx, query, []ONNXWorkerTextInput{{SegmentID: query.Segments[0].ID, Text: text}})
	if err != nil {
		t.Fatalf("query embedding: %v", err)
	}
	assertRealONNXBatch(t, query, queryBatch)
	if reflect.DeepEqual(documentBatch.Results[0].Vector, queryBatch.Results[0].Vector) {
		t.Fatal("query prefix did not change the measured embedding")
	}
	var cosine float64
	for i, value := range documentBatch.Results[0].Vector {
		cosine += float64(value) * float64(queryBatch.Results[0].Vector[i])
	}
	if cosine <= 0 || cosine > 1+EmbedTextL2NormTolerance {
		t.Fatalf("same-text document/query cosine = %v", cosine)
	}

	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	adapter = nil
	// Replace, rather than modify, the hard-linked tokenizer so the external
	// opt-in asset remains untouched.
	tokenizerPath := filepath.Join(root, "tokenizer.json")
	if err := os.Rename(tokenizerPath, tokenizerPath+".admitted"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenizerPath, []byte(`{"tampered":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := NewONNXRuntimeAdapter(ctx, admission, ONNXRuntimeAdapterOptions{}); err == nil || got != nil {
		if got != nil {
			_ = got.Close()
		}
		t.Fatal("tampered tokenizer reopened a native adapter")
	}
}

func realONNXAdmission(t *testing.T, runtimeSource, modelSource, tokenizerSource string) (string, ONNXWorkerAdmission) {
	return realONNXAdmissionWithZvec(t, runtimeSource, modelSource, tokenizerSource, "")
}

func realONNXAdmissionWithZvec(t *testing.T, runtimeSource, modelSource, tokenizerSource, zvecSource string) (string, ONNXWorkerAdmission) {
	t.Helper()
	root := t.TempDir()
	realAssets := map[string]string{"runtime.bin": runtimeSource, "model.onnx": modelSource, "tokenizer.json": tokenizerSource}
	if strings.TrimSpace(zvecSource) != "" {
		realAssets["zvec.bin"] = zvecSource
	}
	for name, source := range realAssets {
		linkOrCopyTestAsset(t, source, filepath.Join(root, name))
	}
	fixtures := map[string][]byte{
		"onnx-binding": []byte(onnxRuntimeGoModulePath + "@" + onnxRuntimeGoModuleVersion + "\n" + onnxRuntimeGoBindingCommit),
		"onnx-c-api.h": []byte("#define ORT_API_VERSION 29\n"), "profile.json": []byte(`{"profile":"bge-small-zh-v1.5"}`),
		"LICENSE": []byte("MIT AND Apache-2.0\n"), "NOTICE": []byte("opt-in execution test"),
		"sbom.json": []byte(`{"test_only":true}`),
	}
	if strings.TrimSpace(zvecSource) == "" {
		fixtures["zvec.bin"] = []byte("zvec opt-in fixture identity")
	}
	fixtures["zvec-go.txt"] = []byte("zvec-go opt-in fixture identity")
	for name, payload := range fixtures {
		if err := os.WriteFile(filepath.Join(root, name), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	asset := func(name string) search.SemanticBundleAsset {
		file, err := os.Open(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.New()
		if _, err := io.Copy(h, file); err != nil {
			t.Fatal(err)
		}
		return search.SemanticBundleAsset{Path: name, SHA256: hex.EncodeToString(h.Sum(nil)), Size: uint64(info.Size())}
	}
	descriptor := search.SemanticBundleDescriptor{
		Schema: search.SemanticBundleSchemaV1, ProfileID: search.SemanticBundleBGEProfileID,
		PlatformOS: CurrentONNXWorkerPlatform().OS, PlatformArch: CurrentONNXWorkerPlatform().Arch,
		ONNXRuntimeVersion: "1.29.0", ONNXRuntimeBuild: "official-cpu-opt-in", ONNXRuntimeCAPI: search.SemanticBundleONNXRuntimeCAPI,
		ONNXGoBindingCommit: onnxRuntimeGoBindingCommit, ONNXGoBindingCAPI: onnxRuntimeGoBindingCAPI,
		ONNXGoBindingDigest: asset("onnx-binding").SHA256,
		ModelID:             "BAAI/bge-small-zh-v1.5", ModelRevision: "opt-in-pinned-asset", ModelExport: "onnx-last-hidden-state",
		ONNXOpset: 17, ModelLicenseID: "BAAI/bge-small-zh-v1.5:MIT",
		TokenizerVersion: "huggingface-tokenizers", TokenizerRevision: "opt-in-pinned-asset",
		ZvecVersion: "0.6.0", ZvecBuild: "not-loaded-by-worker-test", ZvecGoVersion: "0.6.0", ZvecGoCommit: "not-loaded-by-worker-test",
		LicenseExpression:   search.SemanticBundleLicenseExpression,
		PreprocessingDigest: "sha256:" + strings.Repeat("b", 64), QueryPrefix: search.SemanticBundleBGEQueryPrefix,
		DocumentPrefix: search.SemanticBundleBGEDocumentPrefix, MaxTokens: search.SemanticBundleBGEMaxTokens,
		Pooling: search.SemanticBundleBGEPooling, Normalization: search.SemanticBundleBGENormalization,
		ElementType: search.SemanticBundleBGEElementType, Dimension: search.SemanticBundleBGEDimension,
		VectorSchema: search.SemanticBundleBGEVectorSchema, SemanticSpace: search.SemanticBundleBGESemanticSpace,
		Distance: search.SemanticBundleBGEDistance, IndexConfig: search.ZvecIndexConfigV1, QueryConfig: search.ZvecQueryConfigV1,
		Runtime: asset("runtime.bin"), ONNXBinding: asset("onnx-binding"), ONNXCAPI: asset("onnx-c-api.h"),
		Model: asset("model.onnx"), Tokenizer: asset("tokenizer.json"), Profile: asset("profile.json"),
		Zvec: asset("zvec.bin"), ZvecGo: asset("zvec-go.txt"), License: asset("LICENSE"), Notice: asset("NOTICE"), SBOM: asset("sbom.json"),
	}
	payload, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, search.SemanticBundleManifestName), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	bundle, err := search.LoadSemanticBundle(root)
	if err != nil {
		t.Fatalf("load real test bundle: %v", err)
	}
	admission, err := LoadONNXWorkerAdmission(root, bundle.ProfileDigest, "sha256:"+strings.Repeat("c", 64))
	if err != nil {
		t.Fatalf("admit real test bundle: %v", err)
	}
	return root, admission
}

func realONNXRequest(admission ONNXWorkerAdmission, purpose EmbedTextPurpose, text []byte) EmbedTextRequest {
	req := testONNXRequest(admission)
	req.Binding.Purpose = purpose
	req.Profile.DeterminismClass = EmbedTextDeterminismSemantic
	req.MaxInputBytes = 4096
	req.MaxInputTokens = 512
	req.MaxResourceBytes = 8 << 20
	req.MaxOutputBytes = 4096
	sum := sha256.Sum256(text)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	segment := req.Segments[0]
	segment.TextDigest = digest
	segment.TextBytes = int64(len(text))
	if purpose == EmbedTextPurposeQuery {
		req.Binding.AppliedPreprocessingDigest = admission.QueryPreprocessingDigest
		segment.ID = "query-1"
		segment.Source = EmbedTextSource{Kind: EmbedTextSourceQuerySegment, Ref: segment.ID, Revision: digest}
		segment.DescriptionDocumentID = ""
		segment.SubjectRef = ""
		segment.Ordinal = 0
	} else {
		req.Binding.AppliedPreprocessingDigest = admission.DocumentPreprocessingDigest
	}
	req.Segments = []EmbedTextSegment{segment}
	return req
}

func assertRealONNXBatch(t *testing.T, req EmbedTextRequest, batch EmbedTextResultBatch) {
	t.Helper()
	if err := ValidateEmbedTextResult(req, batch); err != nil {
		t.Fatalf("validate measured batch: %v", err)
	}
	if len(batch.Results) != 1 || batch.Results[0].Status != EmbedTextAccepted || len(batch.Results[0].Vector) != search.SemanticBundleBGEDimension {
		t.Fatalf("measured result = %+v", batch.Results)
	}
	var normSquared float64
	for _, value := range batch.Results[0].Vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			t.Fatal("measured vector contains a non-finite value")
		}
		normSquared += float64(value) * float64(value)
	}
	if math.Abs(math.Sqrt(normSquared)-1) > EmbedTextL2NormTolerance {
		t.Fatalf("measured vector norm = %v", math.Sqrt(normSquared))
	}
}

func linkOrCopyTestAsset(t *testing.T, source, destination string) {
	t.Helper()
	if err := os.Link(source, destination); err == nil {
		return
	}
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
