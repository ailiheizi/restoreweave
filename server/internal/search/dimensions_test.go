package search

import (
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

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
