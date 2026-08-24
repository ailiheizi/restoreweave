package search

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type readinessSemanticProvider struct{}

func (readinessSemanticProvider) Embed(context.Context, SemanticEmbeddingRequest) ([]SemanticVector, error) {
	return nil, nil
}

func (readinessSemanticProvider) SemanticReady() bool { return true }

type readinessZvecDriver struct{}

func (readinessZvecDriver) ZvecReady(string, string, EmbeddingGenerationManifest) bool { return true }

func (readinessZvecDriver) Build(context.Context, ZvecGenerationSpec, []ZvecSegment) (ZvecGenerationReceipt, error) {
	return ZvecGenerationReceipt{}, nil
}

func (readinessZvecDriver) Open(context.Context, ZvecGenerationSpec) (ZvecGeneration, error) {
	return nil, ErrZvecUnavailable
}

func TestDeclaredDimensionsStayHonest(t *testing.T) {
	ready := DeclaredDimensions(ProviderReadiness{Lexical: true, Acoustic: true, Semantic: true, Multimodal: true, Graph: true})
	unready := DeclaredDimensions(ProviderReadiness{})
	if len(ready) != 5 || len(unready) != 5 {
		t.Fatalf("dimension count ready=%d unready=%d", len(ready), len(unready))
	}
	if ready[0].ID != DimensionLexical || ready[0].State != "AVAILABLE" {
		t.Fatalf("lexical ready = %+v", ready[0])
	}
	if unready[0].ID != DimensionLexical || unready[0].State != "UNAVAILABLE" {
		t.Fatalf("lexical unready = %+v", unready[0])
	}
	if ready[1].ID != DimensionAcoustic || ready[1].State != "AVAILABLE" || ready[1].Provider != ProviderAcousticFix {
		t.Fatalf("acoustic ready = %+v", ready[1])
	}
	if unready[1].ID != DimensionAcoustic || unready[1].State != "UNAVAILABLE" {
		t.Fatalf("acoustic unready = %+v", unready[1])
	}
	if ready[2].ID != DimensionSemantic || ready[2].State != "AVAILABLE" {
		t.Fatalf("semantic ready = %+v", ready[2])
	}
	if ready[3].ID != DimensionMultimodal || ready[3].State != "AVAILABLE" {
		t.Fatalf("multimodal ready = %+v", ready[3])
	}
	if ready[4].ID != DimensionGraph || ready[4].State != "AVAILABLE" || ready[4].Provider != ProviderGraphCatalog {
		t.Fatalf("graph ready = %+v", ready[4])
	}
	if unready[4].ID != DimensionGraph || unready[4].State != "UNAVAILABLE" {
		t.Fatalf("graph unready = %+v", unready[4])
	}
	if _, ok := LookupDimension("not-a-dimension", ProviderReadiness{Lexical: true}); ok {
		t.Fatal("unknown dimension looked up")
	}
	lexical, ok := LookupDimension("", ProviderReadiness{Lexical: true})
	if !ok || lexical.ID != DimensionLexical {
		t.Fatalf("default dimension = %+v ok=%v", lexical, ok)
	}
}

func TestIndexerReadinessDoesNotAdvertiseFixtureDimensionsByDefault(t *testing.T) {
	idx := &Indexer{Store: &sqlite.Store{}, Engine: &Engine{}}
	ready := IndexerReadiness(idx)
	if !ready.Lexical || !ready.Graph {
		t.Fatalf("core readiness = %+v, want lexical and graph available", ready)
	}
	if ready.Acoustic || ready.Semantic || ready.Multimodal {
		t.Fatalf("fixture readiness = %+v, want unavailable without explicit opt-in", ready)
	}
	semantic, ok := LookupDimension(DimensionSemantic, ready)
	if !ok || semantic.State != "UNAVAILABLE" || !strings.Contains(semantic.Notes, SemanticIndexUnavailableReason) {
		t.Fatalf("semantic capability = %+v ok=%v", semantic, ok)
	}
}

func TestIndexerReadinessRequiresZvecEvidence(t *testing.T) {
	manifest := testZvecManifest()
	idx := &Indexer{
		Store: &sqlite.Store{}, Engine: &Engine{}, SemanticProvider: readinessSemanticProvider{},
		SemanticZvec: readinessZvecDriver{}, SemanticManifest: manifest,
		SemanticLibraryPath:   "/private/explicit/libzvec_c_api.dylib",
		SemanticLibraryDigest: "sha256:" + strings.Repeat("0", 64),
	}
	if got := IndexerReadiness(idx); got.SemanticReal {
		t.Fatalf("semantic readiness = %+v, want unavailable before zvec evidence", got)
	}
	idx.semanticIndexReady.Store(true)
	if got := IndexerReadiness(idx); !got.SemanticReal {
		t.Fatalf("semantic readiness = %+v, want available after zvec evidence", got)
	} else if dimension, ok := LookupDimension(DimensionSemantic, got); !ok || dimension.Provider != ProviderSemanticONNX || dimension.ScoreSemantics != ScoreSemanticCosine {
		t.Fatalf("real semantic dimension = %+v ok=%v", dimension, ok)
	}
}

func TestEmbeddingGenerationManifestDigestBindsEveryField(t *testing.T) {
	base := EmbeddingGenerationManifest{
		RuntimeDigest: "runtime-a", ModelDigest: "model-a", TokenizerDigest: "tokenizer-a",
		PreprocessingDigest: "preprocess-a", Pooling: "mean", Normalization: "l2",
		ElementType: "float32", Dimension: 512, VectorSchema: "float32:512",
		SemanticSpace: "space-a", Distance: "cosine", IndexConfig: "index-a",
		QueryConfig: "query-a", ProviderDigest: "provider-a", ConfigDigest: "config-a",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("complete manifest rejected: %v", err)
	}
	if base.CanonicalDigest() == "" {
		t.Fatal("manifest digest is empty")
	}
	mutations := []struct {
		name   string
		mutate func(*EmbeddingGenerationManifest)
	}{
		{"runtime", func(m *EmbeddingGenerationManifest) { m.RuntimeDigest = "runtime-b" }},
		{"model", func(m *EmbeddingGenerationManifest) { m.ModelDigest = "model-b" }},
		{"tokenizer", func(m *EmbeddingGenerationManifest) { m.TokenizerDigest = "tokenizer-b" }},
		{"preprocessing", func(m *EmbeddingGenerationManifest) { m.PreprocessingDigest = "preprocess-b" }},
		{"pooling", func(m *EmbeddingGenerationManifest) { m.Pooling = "cls" }},
		{"normalization", func(m *EmbeddingGenerationManifest) { m.Normalization = "none" }},
		{"element type", func(m *EmbeddingGenerationManifest) { m.ElementType = "float16" }},
		{"dimension", func(m *EmbeddingGenerationManifest) { m.Dimension = 768 }},
		{"vector schema", func(m *EmbeddingGenerationManifest) { m.VectorSchema = "float16:768" }},
		{"space", func(m *EmbeddingGenerationManifest) { m.SemanticSpace = "space-b" }},
		{"distance", func(m *EmbeddingGenerationManifest) { m.Distance = "dot" }},
		{"index config", func(m *EmbeddingGenerationManifest) { m.IndexConfig = "index-b" }},
		{"query config", func(m *EmbeddingGenerationManifest) { m.QueryConfig = "query-b" }},
		{"provider", func(m *EmbeddingGenerationManifest) { m.ProviderDigest = "provider-b" }},
		{"config", func(m *EmbeddingGenerationManifest) { m.ConfigDigest = "config-b" }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			candidate := base
			mutation.mutate(&candidate)
			if candidate.CanonicalDigest() == base.CanonicalDigest() {
				t.Fatalf("digest did not change for %s", mutation.name)
			}
		})
	}
}

func TestEmbeddingGenerationManifestRejectsIncompleteBindings(t *testing.T) {
	base := EmbeddingGenerationManifest{
		RuntimeDigest: "runtime-a", ModelDigest: "model-a", TokenizerDigest: "tokenizer-a",
		PreprocessingDigest: "preprocess-a", Pooling: "mean", Normalization: "l2",
		ElementType: "float32", Dimension: 512, VectorSchema: "float32:512",
		SemanticSpace: "space-a", Distance: "cosine", IndexConfig: "index-a",
		QueryConfig: "query-a", ProviderDigest: "provider-a", ConfigDigest: "config-a",
	}
	fields := []struct {
		name   string
		mutate func(*EmbeddingGenerationManifest)
	}{
		{"runtime", func(m *EmbeddingGenerationManifest) { m.RuntimeDigest = "" }},
		{"model", func(m *EmbeddingGenerationManifest) { m.ModelDigest = "" }},
		{"tokenizer", func(m *EmbeddingGenerationManifest) { m.TokenizerDigest = "" }},
		{"preprocessing", func(m *EmbeddingGenerationManifest) { m.PreprocessingDigest = "" }},
		{"pooling", func(m *EmbeddingGenerationManifest) { m.Pooling = "" }},
		{"normalization", func(m *EmbeddingGenerationManifest) { m.Normalization = "" }},
		{"element type", func(m *EmbeddingGenerationManifest) { m.ElementType = "" }},
		{"dimension", func(m *EmbeddingGenerationManifest) { m.Dimension = 0 }},
		{"vector schema", func(m *EmbeddingGenerationManifest) { m.VectorSchema = "" }},
		{"semantic space", func(m *EmbeddingGenerationManifest) { m.SemanticSpace = "" }},
		{"distance", func(m *EmbeddingGenerationManifest) { m.Distance = "" }},
		{"index config", func(m *EmbeddingGenerationManifest) { m.IndexConfig = "" }},
		{"query config", func(m *EmbeddingGenerationManifest) { m.QueryConfig = "" }},
		{"provider", func(m *EmbeddingGenerationManifest) { m.ProviderDigest = "" }},
		{"config", func(m *EmbeddingGenerationManifest) { m.ConfigDigest = "" }},
	}
	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			candidate := base
			field.mutate(&candidate)
			if err := candidate.Validate(); !errors.Is(err, ErrInvalidEmbeddingGenerationManifest) {
				t.Fatalf("validation error = %v, want ErrInvalidEmbeddingGenerationManifest", err)
			}
			if got := candidate.CanonicalDigest(); got != "" {
				t.Fatalf("incomplete manifest digest = %q, want empty", got)
			}
		})
	}
}

func TestEmbeddingGenerationManifestValidationOrderIsStable(t *testing.T) {
	err := (EmbeddingGenerationManifest{}).Validate()
	want := "missing runtime_digest, model_digest, tokenizer_digest, preprocessing_digest, pooling, normalization, element_type, vector_schema, semantic_space, distance, index_config, query_config, provider_digest, config_digest, dimension"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("validation error = %q, want ordered fields %q", err, want)
	}
}

func TestNormalizeConstructAxes(t *testing.T) {
	all, err := NormalizeConstructAxes(nil)
	if err != nil || len(all) != len(LexicalConstructAxes) {
		t.Fatalf("nil axes = %v %v", all, err)
	}
	tags, err := NormalizeConstructAxes([]string{"TAGS", "tags", " name "})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(tags) != 2 || tags[0] != AxisTags || tags[1] != AxisName {
		t.Fatalf("normalized = %v", tags)
	}
	if _, err := NormalizeConstructAxes([]string{"lyrics"}); err == nil {
		t.Fatal("expected unknown axis error")
	}
}

func TestNormalizeFuse(t *testing.T) {
	got, err := NormalizeFuse([]string{DimensionLexical, DimensionSemantic, DimensionLexical})
	if err != nil || len(got) != 2 {
		t.Fatalf("fuse = %v %v", got, err)
	}
	if _, err := NormalizeFuse([]string{DimensionLexical}); err == nil {
		t.Fatal("expected too-few fuse error")
	}
	if _, err := NormalizeFuse([]string{DimensionLexical, "not-a-dimension"}); err == nil {
		t.Fatal("expected unknown fuse dimension")
	}
}
