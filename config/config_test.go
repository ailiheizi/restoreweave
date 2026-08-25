package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeRejectsUnknownFields(t *testing.T) {
	_, err := Decode(strings.NewReader(`schema: restoreweave.config.v1
paths:
  catalog: catalog.sqlite
  repository: repository
  vectors: vectors
  recovery_records: recovery
unknown: true
`))
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("Decode error = %v, want unknown-field error", err)
	}
}

func TestDefaultEnablesLocalSemanticProfile(t *testing.T) {
	cfg := Default()
	if cfg.Semantic.EmbeddingMode != "local" || cfg.Semantic.LocalProfile == "" || cfg.Semantic.VectorBackend != "zvec" {
		t.Fatalf("default semantic profile = %+v", cfg.Semantic)
	}
	if cfg.Storage.DefaultProtection != "STORE_EXACT" || !cfg.Storage.AllowLinkOnly || !cfg.Storage.LinkOnlyRequiresConfirmation {
		t.Fatalf("default storage profile = %+v", cfg.Storage)
	}
	if cfg.Storage.RepositoryProfile != RepositoryProfileDirectoryCASDev ||
		cfg.Storage.CompressionProfile != CompressionProfileIdentity ||
		cfg.Storage.NeuralCodec != NeuralCodecDisabled {
		t.Fatalf("default implementation profiles = %+v", cfg.Storage)
	}
	if cfg.Recovery.PublicationSigning != PublicationSigningLocalEd25519 || cfg.Recovery.PublicationDomain != DefaultPublicationDomain {
		t.Fatalf("default recovery signing = %+v", cfg.Recovery)
	}
	if cfg.Descriptions.ProviderProfile != "" || cfg.Descriptions.Generate != "on_demand" {
		t.Fatalf("default description profile = %+v", cfg.Descriptions)
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("default config should validate with an unselected on-demand provider: %v", err)
	}
}

func TestTOMLIsCanonicalAndPreservesExplicitFalse(t *testing.T) {
	cfg := Default()
	cfg.Storage.AllowLinkOnly = false
	cfg.Storage.LinkOnlyRequiresConfirmation = false
	cfg.Descriptions.Enabled = false
	cfg.Descriptions.Generate = "disabled"
	payload, err := MarshalTOML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeTOML(strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Storage.AllowLinkOnly || decoded.Storage.LinkOnlyRequiresConfirmation || decoded.Descriptions.Enabled {
		t.Fatalf("explicit false values were defaulted: %+v", decoded)
	}
	if !strings.Contains(string(payload), "[paths]") || strings.Contains(string(payload), "yaml") {
		t.Fatalf("unexpected TOML payload: %s", payload)
	}
}

func TestDefaultConfigPathUsesTOML(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got := DefaultConfigPath(); filepath.Ext(got) != ".toml" {
		t.Fatalf("default config path = %q, want TOML", got)
	}
}

func TestDescriptionProviderIsRequiredOnlyForOnIngest(t *testing.T) {
	onDemand := Default()
	onDemand.Descriptions.ProviderProfile = ""
	if err := Validate(onDemand); err != nil {
		t.Fatalf("empty on-demand provider rejected: %v", err)
	}
	onIngest := onDemand
	onIngest.Descriptions.Generate = "on_ingest"
	if err := Validate(onIngest); err == nil || !strings.Contains(err.Error(), "on_ingest") {
		t.Fatalf("empty on-ingest provider error = %v", err)
	}
}

func TestAPIListenRequiresCurrentLocalLoopbackProfile(t *testing.T) {
	for _, address := range []string{"127.0.0.1:4534", "localhost:4534", "[::1]:4534"} {
		cfg := Default()
		cfg.API = APIConfig{Enabled: true, Listen: address}
		if err := Validate(cfg); err != nil {
			t.Fatalf("loopback address %q rejected: %v", address, err)
		}
	}
	for _, address := range []string{"", ":4534", "0.0.0.0:4534", "192.0.2.10:4534", "example.com:4534", "127.0.0.1:0", "127.0.0.1:http"} {
		cfg := Default()
		cfg.API = APIConfig{Enabled: true, Listen: address}
		if err := Validate(cfg); err == nil {
			t.Fatalf("non-local or invalid address %q unexpectedly validated", address)
		}
	}
}

func TestStorageProfileTuples(t *testing.T) {
	tests := []struct {
		name        string
		repository  string
		compression string
		wantValid   bool
	}{
		{
			name:        "directory cas identity",
			repository:  RepositoryProfileDirectoryCASDev,
			compression: CompressionProfileIdentity,
			wantValid:   true,
		},
		{
			name:        "local zstd",
			repository:  RepositoryProfileLocalZstdV1,
			compression: CompressionProfileZstdV1,
			wantValid:   true,
		},
		{
			name:        "directory cas zstd mismatch",
			repository:  RepositoryProfileDirectoryCASDev,
			compression: CompressionProfileZstdV1,
		},
		{
			name:        "local zstd identity mismatch",
			repository:  RepositoryProfileLocalZstdV1,
			compression: CompressionProfileIdentity,
		},
		{
			name:        "unknown tuple",
			repository:  "future-repository-v1",
			compression: "future-compression-v1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			cfg.Storage.RepositoryProfile = tt.repository
			cfg.Storage.CompressionProfile = tt.compression
			err := Validate(cfg)
			if (err == nil) != tt.wantValid {
				t.Fatalf("Validate() error = %v, want valid = %t", err, tt.wantValid)
			}
		})
	}
}

func TestResolvePrecedenceAndDigest(t *testing.T) {
	cfg := Default()
	cfg.Paths.Catalog = "persisted/catalog.sqlite"
	cfg.Paths.Repository = "persisted/repository"
	base := t.TempDir()
	resolved, err := Resolve(cfg, ResolveOptions{
		BaseDir: base,
		Environ: map[string]string{CatalogEnv: "env/catalog.sqlite"},
		Overrides: PathOverrides{
			Catalog:    "cli/catalog.sqlite",
			Repository: "cli/repository",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantCatalog := filepath.Join(base, "cli", "catalog.sqlite")
	wantRepo := filepath.Join(base, "cli", "repository")
	if resolved.Config.Paths.Catalog != wantCatalog || resolved.Config.Paths.Repository != wantRepo {
		t.Fatalf("resolved paths = %q, %q; want %q, %q", resolved.Config.Paths.Catalog, resolved.Config.Paths.Repository, wantCatalog, wantRepo)
	}
	if !strings.HasPrefix(resolved.Digest, "sha256:") {
		t.Fatalf("digest = %q", resolved.Digest)
	}
	other, err := Resolve(cfg, ResolveOptions{BaseDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Digest == other.Digest {
		t.Fatalf("digest should bind resolved paths")
	}
}

func TestInitRefusesOverwriteAndUsesOwnerPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	if _, err := Init(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions = %o, want 600", info.Mode().Perm())
	}
	if _, err := Init(path); err == nil {
		t.Fatal("second Init unexpectedly overwrote config")
	}
}

func TestEffectiveJSONKeepsCredentialReferenceWithoutSecret(t *testing.T) {
	cfg := Default()
	cfg.Semantic.OnlineCredentialRef = "keychain://restoreweave/provider"
	cfg.Semantic.EmbeddingMode = "online"
	cfg.Semantic.OnlineProfile = "example"
	resolved, err := Resolve(cfg, ResolveOptions{BaseDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := resolved.EffectiveJSON()
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if !strings.Contains(text, "keychain://restoreweave/provider") || strings.Contains(text, "api_key") {
		t.Fatalf("effective JSON = %s", text)
	}
}
