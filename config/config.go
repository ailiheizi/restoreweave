// Package config defines RestoreWeave's persisted operator configuration.
//
// The configuration is deliberately small and versioned.  It describes where
// the operational projections live and which replaceable storage, embedding,
// description, and recovery profiles are selected; it is not a plugin
// manifest and must not contain credentials.
package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

const (
	// SchemaV1 is the only schema accepted by this package.
	SchemaV1 = "restoreweave.config.v1"

	// These are the only storage profiles implemented by the current daemon.
	// They are deliberately named as development profiles so a generated
	// configuration cannot imply qualified compression that is not present.
	RepositoryProfileDirectoryCASDev = "directory-cas-dev-v1"
	// RepositoryProfileLocalZstdV1 is the single-process local compression
	// candidate. It is accepted by configuration but is not the release
	// default until its repository driver is qualified.
	RepositoryProfileLocalZstdV1   = "local-zstd-v1"
	CompressionProfileIdentity     = "identity-v1"
	CompressionProfileZstdV1       = "zstd-v1"
	NeuralCodecDisabled            = "disabled"
	PublicationSigningLocalEd25519 = "local-ed25519-v1"
	DefaultPublicationDomain       = "workspace:default"

	ConfigEnv          = "RESTOREWEAVE_CONFIG"
	CatalogEnv         = "RESTOREWEAVE_CATALOG"
	RepositoryEnv      = "RESTOREWEAVE_REPOSITORY"
	VectorsEnv         = "RESTOREWEAVE_VECTORS"
	RecoveryRecordsEnv = "RESTOREWEAVE_RECOVERY_RECORDS"
)

// Paths identifies the independent operational stores.  Repository may be a
// local filesystem path or a backend URI (for example, an explicitly selected
// object-store profile).  The other paths are local filesystem paths.
type Paths struct {
	Catalog         string `yaml:"catalog" json:"catalog"`
	Repository      string `yaml:"repository" json:"repository"`
	Vectors         string `yaml:"vectors" json:"vectors"`
	RecoveryRecords string `yaml:"recovery_records" json:"recovery_records"`
}

// StorageConfig controls exact protection and repository representations.
type StorageConfig struct {
	RepositoryProfile            string `yaml:"repository_profile" json:"repository_profile"`
	DefaultProtection            string `yaml:"default_protection" json:"default_protection"`
	AllowLinkOnly                bool   `yaml:"allow_link_only" json:"allow_link_only"`
	LinkOnlyRequiresConfirmation bool   `yaml:"link_only_requires_confirmation" json:"link_only_requires_confirmation"`
	CompressionProfile           string `yaml:"compression_profile" json:"compression_profile"`
	NeuralCodec                  string `yaml:"neural_codec" json:"neural_codec"`
}

// SemanticConfig selects the embedding and vector-generation providers.
// OnlineCredentialRef is a reference into a host secret store, never a
// secret itself.
type SemanticConfig struct {
	EmbeddingMode                  string `yaml:"embedding_mode" json:"embedding_mode"`
	LocalProfile                   string `yaml:"local_profile" json:"local_profile"`
	OnlineProfile                  string `yaml:"online_profile" json:"online_profile"`
	OnlineCredentialRef            string `yaml:"online_credential_ref" json:"online_credential_ref"`
	VectorBackend                  string `yaml:"vector_backend" json:"vector_backend"`
	SendContentWithoutConfirmation bool   `yaml:"send_content_without_confirmation" json:"send_content_without_confirmation"`
}

// DescriptionConfig controls durable long-form descriptions.  Description
// generation and embedding are separate providers by design.
type DescriptionConfig struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	Generate        string `yaml:"generate" json:"generate"`
	ProviderProfile string `yaml:"provider_profile" json:"provider_profile"`
	CredentialRef   string `yaml:"credential_ref" json:"credential_ref"`
	RetainFullText  bool   `yaml:"retain_full_text" json:"retain_full_text"`
}

// Descriptions is retained as a convenient field/type name for callers that
// use the wording from the configuration document.
type Descriptions = DescriptionConfig

// RecoveryConfig controls fallback and external reacquisition policy.
type RecoveryConfig struct {
	RequireExactFallback       bool   `yaml:"require_exact_fallback" json:"require_exact_fallback"`
	AllowExternalReacquisition bool   `yaml:"allow_external_reacquisition" json:"allow_external_reacquisition"`
	PublicationSigning         string `yaml:"publication_signing" json:"publication_signing"`
	PublicationDomain          string `yaml:"publication_domain" json:"publication_domain"`
}

// Recovery is a short alias matching the top-level YAML section name.
type Recovery = RecoveryConfig

// Config is the persisted schema-v1 profile.
type Config struct {
	Schema       string            `yaml:"schema" json:"schema"`
	Paths        Paths             `yaml:"paths" json:"paths"`
	Storage      StorageConfig     `yaml:"storage" json:"storage"`
	Semantic     SemanticConfig    `yaml:"semantic" json:"semantic"`
	Descriptions DescriptionConfig `yaml:"descriptions" json:"descriptions"`
	Recovery     RecoveryConfig    `yaml:"recovery" json:"recovery"`

	// YAML's bool type has no "unset" state.  These markers are populated by
	// Decode so a subsequent Resolve call does not turn an explicit false back
	// into a default true.  They are intentionally excluded from persistence.
	explicitStorageAllow        bool `yaml:"-" json:"-"`
	explicitStorageConfirmation bool `yaml:"-" json:"-"`
	explicitDescriptionsEnabled bool `yaml:"-" json:"-"`
	explicitDescriptionsRetain  bool `yaml:"-" json:"-"`
	explicitRecoveryFallback    bool `yaml:"-" json:"-"`
}

// PathOverrides are one-shot path choices.  Empty fields mean "no override".
// They are intentionally separate from Config so a command invocation does
// not silently rewrite the persisted profile.
type PathOverrides struct {
	Catalog         string
	Repository      string
	Vectors         string
	RecoveryRecords string
}

// ResolveOptions controls effective configuration resolution.
type ResolveOptions struct {
	// BaseDir is the base for relative paths.  LoadEffective sets it to the
	// persisted config file's directory; callers resolving an in-memory Config
	// may leave it empty to use the current working directory.
	BaseDir   string
	Overrides PathOverrides
	// Environ permits deterministic tests and embedding in a daemon.  A nil
	// map reads the process environment.
	Environ map[string]string
}

// LoadOptions controls LoadEffective.
type LoadOptions struct {
	// Path is the persisted config path.  Empty selects RESTOREWEAVE_CONFIG or
	// the platform default.
	Path string
	// AllowMissing returns the platform default profile when Path does not
	// exist.  It is useful for first-run commands; normal daemon startup should
	// leave this false so an accidentally missing profile is visible.
	AllowMissing bool
	ResolveOptions
}

// ResolvedConfig is an effective, absolute-path configuration snapshot.  Its
// digest is over Config only, so moving the config file itself does not alter
// the meaning of the profile.
type ResolvedConfig struct {
	Config     Config `json:"-"`
	ConfigPath string `json:"config_path"`
	Digest     string `json:"config_digest"`
	// ConfigDigest is a descriptive alias for Digest.  Both values are kept in
	// sync by Resolve/LoadEffective; callers should prefer it when binding a
	// plan or generation to make the meaning explicit.
	ConfigDigest string `json:"-"`
}

// DefaultConfigPath returns the persisted config location.  RESTOREWEAVE_CONFIG
// has precedence over the platform XDG location.
func DefaultConfigPath() string {
	if value := strings.TrimSpace(os.Getenv(ConfigEnv)); value != "" {
		return value
	}
	return platformConfigPath()
}

// PlatformConfigPath returns the config location without consulting the
// RESTOREWEAVE_CONFIG override.
func PlatformConfigPath() string {
	return platformConfigPath()
}

func platformConfigPath() string {
	return platformConfigPathForEnv(processEnvironment())
}

func platformConfigPathForEnv(env map[string]string) string {
	if value := strings.TrimSpace(env["XDG_CONFIG_HOME"]); value != "" {
		return filepath.Join(value, "restoreweave", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(os.TempDir(), "restoreweave", "config.yaml")
	}
	return filepath.Join(home, ".config", "restoreweave", "config.yaml")
}

// Default returns the current runnable development storage profile and the
// intended personal-use semantic selection. The daemon reports the semantic
// capability unavailable until the pinned BGE/zvec bundle is actually wired;
// it never silently substitutes an embedding-free default.
func Default() Config {
	return Config{
		Schema: SchemaV1,
		Paths: Paths{
			Catalog:         platformDataPath("catalog.sqlite"),
			Repository:      platformDataPath("repository"),
			Vectors:         platformDataPath("vectors"),
			RecoveryRecords: platformDataPath("recovery"),
		},
		Storage: StorageConfig{
			RepositoryProfile:            RepositoryProfileDirectoryCASDev,
			DefaultProtection:            "STORE_EXACT",
			AllowLinkOnly:                true,
			LinkOnlyRequiresConfirmation: true,
			CompressionProfile:           CompressionProfileIdentity,
			NeuralCodec:                  NeuralCodecDisabled,
		},
		Semantic: SemanticConfig{
			EmbeddingMode: "local",
			LocalProfile:  "bge-small-zh-v1.5",
			VectorBackend: "zvec",
		},
		Descriptions: DescriptionConfig{
			Enabled:         true,
			Generate:        "on_demand",
			ProviderProfile: "local-or-configured-online",
			RetainFullText:  true,
		},
		Recovery: RecoveryConfig{
			RequireExactFallback:       true,
			AllowExternalReacquisition: false,
			PublicationSigning:         PublicationSigningLocalEd25519,
			PublicationDomain:          DefaultPublicationDomain,
		},
	}
}

// DefaultConfig is an explicit alias for callers that prefer a noun.
func DefaultConfig() Config { return Default() }

func platformDataPath(name string) string {
	if value := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); value != "" {
		return filepath.Join(value, "restoreweave", name)
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(os.TempDir(), "restoreweave", name)
	}
	return filepath.Join(home, ".local", "share", "restoreweave", name)
}

// LoadFile decodes one strict YAML document and validates its schema.  It does
// not resolve relative paths; use LoadEffective or Resolve for that.
func LoadFile(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		return Config{}, errors.New("config path is empty")
	}
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config %q: %w", path, err)
	}
	defer file.Close()
	return Decode(file)
}

// Decode decodes one strict YAML document from r.
func Decode(r io.Reader) (Config, error) {
	if r == nil {
		return Config{}, errors.New("config reader is nil")
	}
	payload, err := io.ReadAll(r)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(payload))
	decoder.KnownFields(true)
	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return Config{}, errors.New("config document is empty")
		}
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, errors.New("config contains more than one YAML document")
		}
		return Config{}, fmt.Errorf("read config document: %w", err)
	}
	if strings.TrimSpace(cfg.Schema) == "" {
		return Config{}, fmt.Errorf("schema is required (want %q)", SchemaV1)
	}
	// Preserve explicit false values while still filling omitted fields from
	// the profile defaults.  Go bools cannot encode the distinction on their
	// own, so inspect the top-level YAML nodes after strict decoding.
	original := cfg
	cfg = withDefaults(cfg)
	cfg = restoreExplicitBooleans(cfg, original, payload)
	if err := validate(cfg, false); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Parse is a shorthand for Decode.
func Parse(r io.Reader) (Config, error) { return Decode(r) }

// DecodeJSON decodes the same schema from strict JSON.  JSON is accepted as a
// convenience for generated profiles and diagnostics; YAML remains the
// persisted human-facing format.
func DecodeJSON(r io.Reader) (Config, error) {
	if r == nil {
		return Config{}, errors.New("config reader is nil")
	}
	payload, err := io.ReadAll(r)
	if err != nil {
		return Config{}, fmt.Errorf("read config JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return Config{}, errors.New("config document is empty")
		}
		return Config{}, fmt.Errorf("decode config JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, errors.New("config contains more than one JSON document")
		}
		return Config{}, fmt.Errorf("read config JSON: %w", err)
	}
	if strings.TrimSpace(cfg.Schema) == "" {
		return Config{}, fmt.Errorf("schema is required (want %q)", SchemaV1)
	}
	original := cfg
	cfg = withDefaults(cfg)
	cfg = restoreExplicitBooleansJSON(cfg, original, payload)
	if err := validate(cfg, false); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func restoreExplicitBooleansJSON(cfg, original Config, payload []byte) Config {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(payload, &root); err != nil {
		return cfg
	}
	has := func(section, field string) bool {
		var fields map[string]json.RawMessage
		value, ok := root[section]
		if !ok || json.Unmarshal(value, &fields) != nil {
			return false
		}
		_, ok = fields[field]
		return ok
	}
	if has("storage", "link_only_requires_confirmation") {
		cfg.Storage.LinkOnlyRequiresConfirmation = original.Storage.LinkOnlyRequiresConfirmation
		cfg.explicitStorageConfirmation = true
	}
	if has("storage", "allow_link_only") {
		cfg.Storage.AllowLinkOnly = original.Storage.AllowLinkOnly
		cfg.explicitStorageAllow = true
	}
	if has("descriptions", "enabled") {
		cfg.Descriptions.Enabled = original.Descriptions.Enabled
		cfg.explicitDescriptionsEnabled = true
	}
	if has("descriptions", "retain_full_text") {
		cfg.Descriptions.RetainFullText = original.Descriptions.RetainFullText
		cfg.explicitDescriptionsRetain = true
	}
	if has("recovery", "require_exact_fallback") {
		cfg.Recovery.RequireExactFallback = original.Recovery.RequireExactFallback
		cfg.explicitRecoveryFallback = true
	}
	if has("descriptions", "enabled") && !cfg.Descriptions.Enabled && !has("descriptions", "generate") {
		cfg.Descriptions.Generate = "disabled"
	}
	return cfg
}

// LoadEffective reads and resolves the persisted profile.  Explicit path
// overrides win over environment values, which win over persisted values and
// platform defaults.
func LoadEffective(options LoadOptions) (ResolvedConfig, error) {
	env := options.Environ
	if env == nil {
		env = processEnvironment()
	}
	configPath := strings.TrimSpace(options.Path)
	if configPath == "" {
		configPath = strings.TrimSpace(env[ConfigEnv])
	}
	if configPath == "" {
		configPath = platformConfigPathForEnv(env)
	}
	absoluteConfig, err := filepath.Abs(configPath)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := LoadFile(absoluteConfig)
	if err != nil {
		if options.AllowMissing && errors.Is(err, os.ErrNotExist) {
			// Leave paths empty so Resolve can derive platform defaults from the
			// caller-supplied environment map (important for tests and embedded
			// daemons that do not use the process environment directly).
			cfg = Config{Schema: SchemaV1}
		} else {
			return ResolvedConfig{}, err
		}
	}
	baseDir := options.BaseDir
	if strings.TrimSpace(baseDir) == "" {
		baseDir = filepath.Dir(absoluteConfig)
	}
	resolved, err := Resolve(cfg, ResolveOptions{
		BaseDir:   baseDir,
		Overrides: options.Overrides,
		Environ:   env,
	})
	if err != nil {
		return ResolvedConfig{}, err
	}
	resolved.ConfigPath = absoluteConfig
	return resolved, nil
}

// Load is a convenience wrapper for an explicitly named persisted config.
func Load(path string) (ResolvedConfig, error) {
	return LoadEffective(LoadOptions{Path: path})
}

// Resolve applies defaults, environment paths, one-shot overrides, and path
// normalization to an in-memory profile.
func Resolve(cfg Config, options ResolveOptions) (ResolvedConfig, error) {
	env := options.Environ
	if env == nil {
		env = processEnvironment()
	}
	cfg = withDefaultsFrom(cfg, defaultForEnvironment(env))
	applyEnvironmentPaths(&cfg.Paths, env)
	applyPathOverrides(&cfg.Paths, options.Overrides)
	baseDir := options.BaseDir
	if strings.TrimSpace(baseDir) == "" {
		var err error
		baseDir, err = os.Getwd()
		if err != nil {
			return ResolvedConfig{}, fmt.Errorf("get config base directory: %w", err)
		}
	}
	baseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("resolve config base directory: %w", err)
	}
	cfg.Paths.Catalog, err = resolveFilesystemPath(cfg.Paths.Catalog, baseDir)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("resolve paths.catalog: %w", err)
	}
	cfg.Paths.Repository, err = resolveLocation(cfg.Paths.Repository, baseDir)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("resolve paths.repository: %w", err)
	}
	cfg.Paths.Vectors, err = resolveFilesystemPath(cfg.Paths.Vectors, baseDir)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("resolve paths.vectors: %w", err)
	}
	cfg.Paths.RecoveryRecords, err = resolveFilesystemPath(cfg.Paths.RecoveryRecords, baseDir)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("resolve paths.recovery_records: %w", err)
	}
	if err := validate(cfg, true); err != nil {
		return ResolvedConfig{}, err
	}
	digest, err := digestConfig(cfg)
	if err != nil {
		return ResolvedConfig{}, err
	}
	return ResolvedConfig{Config: cfg, Digest: digest, ConfigDigest: digest}, nil
}

func processEnvironment() map[string]string {
	values := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}

func applyEnvironmentPaths(paths *Paths, env map[string]string) {
	if value := strings.TrimSpace(env[CatalogEnv]); value != "" {
		paths.Catalog = value
	}
	if value := strings.TrimSpace(env[RepositoryEnv]); value != "" {
		paths.Repository = value
	}
	if value := strings.TrimSpace(env[VectorsEnv]); value != "" {
		paths.Vectors = value
	}
	if value := strings.TrimSpace(env[RecoveryRecordsEnv]); value != "" {
		paths.RecoveryRecords = value
	}
}

func applyPathOverrides(paths *Paths, overrides PathOverrides) {
	if value := strings.TrimSpace(overrides.Catalog); value != "" {
		paths.Catalog = value
	}
	if value := strings.TrimSpace(overrides.Repository); value != "" {
		paths.Repository = value
	}
	if value := strings.TrimSpace(overrides.Vectors); value != "" {
		paths.Vectors = value
	}
	if value := strings.TrimSpace(overrides.RecoveryRecords); value != "" {
		paths.RecoveryRecords = value
	}
}

// Validate checks schema and policy invariants without resolving relative
// paths.  Resolve performs the same checks after making paths absolute and
// additionally checks for overlapping stores.
func Validate(cfg Config) error {
	return validate(cfg, false)
}

func restoreExplicitBooleans(cfg, original Config, payload []byte) Config {
	var root yaml.Node
	if err := yaml.Unmarshal(payload, &root); err != nil || len(root.Content) == 0 {
		return cfg
	}
	mapNode := root.Content[0]
	if mapNode.Kind != yaml.MappingNode {
		return cfg
	}
	has := func(section, field string) bool {
		sectionNode := mappingValue(mapNode, section)
		if sectionNode == nil || sectionNode.Kind != yaml.MappingNode {
			return false
		}
		return mappingValue(sectionNode, field) != nil
	}
	if has("storage", "allow_link_only") {
		cfg.Storage.AllowLinkOnly = original.Storage.AllowLinkOnly
		cfg.explicitStorageAllow = true
	}
	if has("storage", "link_only_requires_confirmation") {
		cfg.Storage.LinkOnlyRequiresConfirmation = original.Storage.LinkOnlyRequiresConfirmation
		cfg.explicitStorageConfirmation = true
	}
	if has("semantic", "send_content_without_confirmation") {
		cfg.Semantic.SendContentWithoutConfirmation = original.Semantic.SendContentWithoutConfirmation
	}
	if has("descriptions", "enabled") {
		cfg.Descriptions.Enabled = original.Descriptions.Enabled
		cfg.explicitDescriptionsEnabled = true
	}
	if has("descriptions", "retain_full_text") {
		cfg.Descriptions.RetainFullText = original.Descriptions.RetainFullText
		cfg.explicitDescriptionsRetain = true
	}
	if has("recovery", "require_exact_fallback") {
		cfg.Recovery.RequireExactFallback = original.Recovery.RequireExactFallback
		cfg.explicitRecoveryFallback = true
	}
	if has("descriptions", "enabled") && !cfg.Descriptions.Enabled && !has("descriptions", "generate") {
		cfg.Descriptions.Generate = "disabled"
	}
	return cfg
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func validate(cfg Config, resolvedPaths bool) error {
	if cfg.Schema != SchemaV1 {
		if strings.TrimSpace(cfg.Schema) == "" {
			return fmt.Errorf("schema is required (want %q)", SchemaV1)
		}
		return fmt.Errorf("unsupported config schema %q (want %q)", cfg.Schema, SchemaV1)
	}
	if err := validateLocation("paths.catalog", cfg.Paths.Catalog, resolvedPaths, false); err != nil {
		return err
	}
	if err := validateLocation("paths.repository", cfg.Paths.Repository, resolvedPaths, true); err != nil {
		return err
	}
	if err := validateLocation("paths.vectors", cfg.Paths.Vectors, resolvedPaths, false); err != nil {
		return err
	}
	if err := validateLocation("paths.recovery_records", cfg.Paths.RecoveryRecords, resolvedPaths, false); err != nil {
		return err
	}
	if cfg.Storage.RepositoryProfile == "" {
		return errors.New("storage.repository_profile is required")
	}
	if err := validateProfileName("storage.repository_profile", cfg.Storage.RepositoryProfile); err != nil {
		return err
	}
	switch cfg.Storage.DefaultProtection {
	case "STORE_EXACT", "STORE_EXACT_WITH_EXTERNAL_FALLBACK", "LINK_ONLY", "METADATA_ONLY":
	default:
		return fmt.Errorf("storage.default_protection %q is invalid", cfg.Storage.DefaultProtection)
	}
	if cfg.Storage.CompressionProfile == "" {
		return errors.New("storage.compression_profile is required")
	}
	if err := validateProfileName("storage.compression_profile", cfg.Storage.CompressionProfile); err != nil {
		return err
	}
	switch {
	case cfg.Storage.RepositoryProfile == RepositoryProfileDirectoryCASDev && cfg.Storage.CompressionProfile == CompressionProfileIdentity:
	case cfg.Storage.RepositoryProfile == RepositoryProfileLocalZstdV1 && cfg.Storage.CompressionProfile == CompressionProfileZstdV1:
	default:
		return fmt.Errorf("storage repository/compression profile tuple %q + %q is unsupported", cfg.Storage.RepositoryProfile, cfg.Storage.CompressionProfile)
	}
	if cfg.Storage.NeuralCodec == "" {
		return errors.New("storage.neural_codec is required (use disabled to opt out)")
	}
	if err := validateProfileName("storage.neural_codec", cfg.Storage.NeuralCodec); err != nil {
		return err
	}
	if cfg.Storage.AllowLinkOnly && !cfg.Storage.LinkOnlyRequiresConfirmation {
		return errors.New("storage.allow_link_only requires storage.link_only_requires_confirmation=true")
	}
	if cfg.Storage.DefaultProtection == "LINK_ONLY" && !cfg.Storage.AllowLinkOnly {
		return errors.New("storage.default_protection LINK_ONLY requires storage.allow_link_only=true")
	}

	switch cfg.Semantic.EmbeddingMode {
	case "local":
		if strings.TrimSpace(cfg.Semantic.LocalProfile) == "" {
			return errors.New("semantic.local_profile is required for local embeddings")
		}
		if err := validateProfileName("semantic.local_profile", cfg.Semantic.LocalProfile); err != nil {
			return err
		}
	case "online":
		if strings.TrimSpace(cfg.Semantic.OnlineProfile) == "" {
			return errors.New("semantic.online_profile is required for online embeddings")
		}
		if err := validateProfileName("semantic.online_profile", cfg.Semantic.OnlineProfile); err != nil {
			return err
		}
	case "hybrid":
		if strings.TrimSpace(cfg.Semantic.LocalProfile) == "" || strings.TrimSpace(cfg.Semantic.OnlineProfile) == "" {
			return errors.New("semantic.local_profile and semantic.online_profile are required for hybrid embeddings")
		}
		if err := validateProfileName("semantic.local_profile", cfg.Semantic.LocalProfile); err != nil {
			return err
		}
		if err := validateProfileName("semantic.online_profile", cfg.Semantic.OnlineProfile); err != nil {
			return err
		}
	default:
		return fmt.Errorf("semantic.embedding_mode %q is invalid (want local, online, or hybrid)", cfg.Semantic.EmbeddingMode)
	}
	if strings.TrimSpace(cfg.Semantic.VectorBackend) == "" {
		return errors.New("semantic.vector_backend is required")
	}
	if err := validateProfileName("semantic.vector_backend", cfg.Semantic.VectorBackend); err != nil {
		return err
	}
	if cfg.Descriptions.Enabled {
		switch cfg.Descriptions.Generate {
		case "on_ingest", "on_demand", "disabled":
		default:
			return fmt.Errorf("descriptions.generate %q is invalid", cfg.Descriptions.Generate)
		}
		if cfg.Descriptions.Generate != "disabled" && strings.TrimSpace(cfg.Descriptions.ProviderProfile) == "" {
			return errors.New("descriptions.provider_profile is required when description generation is enabled")
		}
		if cfg.Descriptions.Generate != "disabled" {
			if err := validateProfileName("descriptions.provider_profile", cfg.Descriptions.ProviderProfile); err != nil {
				return err
			}
		}
		if cfg.Descriptions.Generate != "disabled" && !cfg.Descriptions.RetainFullText {
			return errors.New("descriptions.retain_full_text must be true for generated descriptions")
		}
	} else if cfg.Descriptions.Generate != "disabled" {
		return errors.New("descriptions.generate must be disabled when descriptions.enabled is false")
	}
	if cfg.Descriptions.CredentialRef != "" && containsSecretValue(cfg.Descriptions.CredentialRef) {
		return errors.New("descriptions.credential_ref appears to contain a secret; use a host secret reference")
	}
	if cfg.Semantic.OnlineCredentialRef != "" && containsSecretValue(cfg.Semantic.OnlineCredentialRef) {
		return errors.New("semantic.online_credential_ref appears to contain a secret; use a host secret reference")
	}
	if cfg.Recovery.PublicationSigning != PublicationSigningLocalEd25519 {
		return fmt.Errorf("recovery.publication_signing %q is invalid (want %s)", cfg.Recovery.PublicationSigning, PublicationSigningLocalEd25519)
	}
	if err := validatePublicationDomain(cfg.Recovery.PublicationDomain); err != nil {
		return err
	}
	if resolvedPaths {
		if err := validatePathSeparation(cfg.Paths); err != nil {
			return err
		}
	}
	return nil
}

func validateProfileName(name, value string) error {
	if strings.TrimSpace(value) != value || strings.ContainsFunc(value, unicode.IsSpace) || strings.ContainsFunc(value, unicode.IsControl) {
		return fmt.Errorf("%s must be a non-whitespace profile identifier", name)
	}
	return nil
}

func validatePublicationDomain(value string) error {
	if strings.TrimSpace(value) != value || value == "" || strings.ContainsFunc(value, unicode.IsControl) || strings.ContainsFunc(value, unicode.IsSpace) {
		return errors.New("recovery.publication_domain must be a non-whitespace stable identifier")
	}
	return nil
}

func containsSecretValue(value string) bool {
	// Credential references are deliberately opaque names such as
	// "keychain://restoreweave/openai".  Reject common accidental secret forms
	// while allowing ordinary references.
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "sk-") || strings.Contains(lower, "api_key=") || strings.Contains(lower, "token=") || strings.Contains(lower, "password=") || strings.Contains(lower, "secret=")
}

func validateLocation(name, value string, resolved, uriAllowed bool) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("%s contains a NUL byte", name)
	}
	if strings.ContainsFunc(value, unicode.IsControl) {
		return fmt.Errorf("%s contains a control character", name)
	}
	if isURI(value) {
		if !uriAllowed {
			return fmt.Errorf("%s must be a filesystem path", name)
		}
		return nil
	}
	if resolved && !filepath.IsAbs(value) {
		return fmt.Errorf("%s must resolve to an absolute path", name)
	}
	if filepath.Clean(value) == string(filepath.Separator) {
		return fmt.Errorf("%s must not be the filesystem root", name)
	}
	return nil
}

func isURI(value string) bool {
	colon := strings.IndexByte(value, ':')
	if colon <= 0 {
		return false
	}
	for i, r := range value[:colon] {
		if i == 0 {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
				return false
			}
			continue
		}
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '+' || r == '-' || r == '.') {
			return false
		}
	}
	return strings.HasPrefix(value[colon:], "://")
}

func resolveFilesystemPath(value, baseDir string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("path is empty")
	}
	if isURI(value) {
		return "", errors.New("URI is not allowed for this path")
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(baseDir, value)
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func resolveLocation(value, baseDir string) (string, error) {
	value = strings.TrimSpace(value)
	if isURI(value) {
		return value, nil
	}
	return resolveFilesystemPath(value, baseDir)
}

func validatePathSeparation(paths Paths) error {
	locations := []struct {
		name  string
		value string
	}{
		{"catalog", paths.Catalog},
		{"repository", paths.Repository},
		{"vectors", paths.Vectors},
		{"recovery_records", paths.RecoveryRecords},
	}
	for i := range locations {
		if isURI(locations[i].value) {
			continue
		}
		for j := i + 1; j < len(locations); j++ {
			if isURI(locations[j].value) {
				continue
			}
			if pathsOverlap(locations[i].value, locations[j].value) {
				return fmt.Errorf("paths.%s and paths.%s overlap", locations[i].name, locations[j].name)
			}
		}
	}
	return nil
}

func pathsOverlap(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if left == right {
		return true
	}
	if relativeWithin(left, right) || relativeWithin(right, left) {
		return true
	}
	return false
}

func relativeWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil || rel == "." {
		return err == nil
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func withDefaults(cfg Config) Config {
	return withDefaultsFrom(cfg, Default())
}

func defaultForEnvironment(env map[string]string) Config {
	defaults := Default()
	if value := strings.TrimSpace(env["XDG_DATA_HOME"]); value != "" {
		defaults.Paths.Catalog = filepath.Join(value, "restoreweave", "catalog.sqlite")
		defaults.Paths.Repository = filepath.Join(value, "restoreweave", "repository")
		defaults.Paths.Vectors = filepath.Join(value, "restoreweave", "vectors")
		defaults.Paths.RecoveryRecords = filepath.Join(value, "restoreweave", "recovery")
	}
	return defaults
}

func withDefaultsFrom(cfg Config, defaults Config) Config {
	if cfg.Schema == "" {
		// Decode callers receive the same defaults as `rw config init`; an
		// explicitly unknown non-empty schema is still rejected.
		cfg.Schema = defaults.Schema
	}
	if cfg.Paths.Catalog == "" {
		cfg.Paths.Catalog = defaults.Paths.Catalog
	}
	if cfg.Paths.Repository == "" {
		cfg.Paths.Repository = defaults.Paths.Repository
	}
	if cfg.Paths.Vectors == "" {
		cfg.Paths.Vectors = defaults.Paths.Vectors
	}
	if cfg.Paths.RecoveryRecords == "" {
		cfg.Paths.RecoveryRecords = defaults.Paths.RecoveryRecords
	}
	if cfg.Storage.RepositoryProfile == "" {
		cfg.Storage.RepositoryProfile = defaults.Storage.RepositoryProfile
	}
	if cfg.Storage.DefaultProtection == "" {
		cfg.Storage.DefaultProtection = defaults.Storage.DefaultProtection
	}
	if cfg.Storage.CompressionProfile == "" {
		cfg.Storage.CompressionProfile = defaults.Storage.CompressionProfile
	}
	if cfg.Storage.NeuralCodec == "" {
		cfg.Storage.NeuralCodec = defaults.Storage.NeuralCodec
	}
	if !cfg.Storage.AllowLinkOnly && !cfg.explicitStorageAllow {
		cfg.Storage.AllowLinkOnly = defaults.Storage.AllowLinkOnly
	}
	if cfg.Semantic.EmbeddingMode == "" {
		cfg.Semantic.EmbeddingMode = defaults.Semantic.EmbeddingMode
	}
	if cfg.Semantic.LocalProfile == "" {
		cfg.Semantic.LocalProfile = defaults.Semantic.LocalProfile
	}
	if cfg.Semantic.VectorBackend == "" {
		cfg.Semantic.VectorBackend = defaults.Semantic.VectorBackend
	}
	if cfg.Descriptions.Generate == "" {
		cfg.Descriptions.Generate = defaults.Descriptions.Generate
	}
	if cfg.Descriptions.ProviderProfile == "" {
		cfg.Descriptions.ProviderProfile = defaults.Descriptions.ProviderProfile
	}
	// Booleans cannot distinguish omitted from explicitly false in a plain
	// struct.  Defaults therefore describe the generated profile; a caller can
	// still explicitly disable the feature after loading and validate it.
	if !cfg.Descriptions.Enabled && cfg.Descriptions.Generate == defaults.Descriptions.Generate && !cfg.explicitDescriptionsEnabled {
		// Keep a zero-value Config useful for programmatic callers while Decode
		// remains strict about contradictory explicit sections.
		cfg.Descriptions.Enabled = defaults.Descriptions.Enabled
	}
	if !cfg.Descriptions.RetainFullText && cfg.Descriptions.Enabled && !cfg.explicitDescriptionsRetain {
		cfg.Descriptions.RetainFullText = defaults.Descriptions.RetainFullText
	}
	if !cfg.Storage.LinkOnlyRequiresConfirmation && cfg.Storage.AllowLinkOnly && !cfg.explicitStorageConfirmation {
		cfg.Storage.LinkOnlyRequiresConfirmation = defaults.Storage.LinkOnlyRequiresConfirmation
	}
	if !cfg.Recovery.RequireExactFallback && !cfg.explicitRecoveryFallback {
		cfg.Recovery.RequireExactFallback = defaults.Recovery.RequireExactFallback
	}
	if cfg.Recovery.PublicationSigning == "" {
		cfg.Recovery.PublicationSigning = defaults.Recovery.PublicationSigning
	}
	if cfg.Recovery.PublicationDomain == "" {
		cfg.Recovery.PublicationDomain = defaults.Recovery.PublicationDomain
	}
	return cfg
}

func digestConfig(cfg Config) (string, error) {
	encoded, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("canonicalize config: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// CanonicalJSON returns deterministic JSON for a validated profile.  It is
// suitable as the input to config_digest and for machine comparison.
func (cfg Config) CanonicalJSON() ([]byte, error) {
	if err := validate(cfg, false); err != nil {
		return nil, err
	}
	return json.Marshal(cfg)
}

// Digest returns the digest of the canonical (unresolved) profile.  Effective
// daemon bindings should use ResolvedConfig.Digest instead.
func (cfg Config) Digest() (string, error) {
	if err := validate(cfg, false); err != nil {
		return "", err
	}
	return digestConfig(cfg)
}

// RedactedJSON returns the canonical profile view.  Schema-v1 contains only
// credential references, never secret material, so no values need masking at
// this layer.
func (cfg Config) RedactedJSON() ([]byte, error) { return cfg.CanonicalJSON() }

// EffectiveJSON returns a redacted, deterministic JSON view including the
// resolved config path and digest.  Credential references are safe identifiers
// and are retained; no secret-bearing fields are part of schema v1.
func (resolved ResolvedConfig) EffectiveJSON() ([]byte, error) {
	if err := validate(resolved.Config, true); err != nil {
		return nil, err
	}
	digest := resolved.Digest
	if digest == "" {
		digest = resolved.ConfigDigest
	}
	if digest == "" {
		var err error
		digest, err = digestConfig(resolved.Config)
		if err != nil {
			return nil, err
		}
	}
	view := struct {
		Config
		ConfigPath string `json:"config_path"`
		Digest     string `json:"config_digest"`
	}{
		Config:     resolved.Config,
		ConfigPath: resolved.ConfigPath,
		Digest:     digest,
	}
	return json.Marshal(view)
}

// RedactedJSON is an alias for EffectiveJSON used by command surfaces.
func (resolved ResolvedConfig) RedactedJSON() ([]byte, error) {
	return resolved.EffectiveJSON()
}

// EffectiveConfigJSON is a descriptive alias for EffectiveJSON.
func (resolved ResolvedConfig) EffectiveConfigJSON() ([]byte, error) {
	return resolved.EffectiveJSON()
}

// CanonicalJSON returns the deterministic JSON representation of a profile.
// It is the package-level form of Config.CanonicalJSON for callers that keep
// profiles behind an interface.
func CanonicalJSON(cfg Config) ([]byte, error) { return cfg.CanonicalJSON() }

// RedactedJSON returns the effective redacted view for a resolved profile.
func RedactedJSON(resolved ResolvedConfig) ([]byte, error) {
	return resolved.RedactedJSON()
}

// MarshalYAML validates and serializes a persisted profile with defaults
// filled.  Relative paths are preserved so a config remains portable between
// machines; resolution happens at load time.
func MarshalYAML(cfg Config) ([]byte, error) {
	if err := validate(cfg, false); err != nil {
		return nil, err
	}
	return yaml.Marshal(cfg)
}

// Save writes a validated profile atomically with owner-only permissions.
func Save(path string, cfg Config) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("config path is empty")
	}
	payload, err := MarshalYAML(cfg)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create config temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set config permissions: %w", err)
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	if err := os.Rename(tmpName, abs); err != nil {
		return fmt.Errorf("install config: %w", err)
	}
	return nil
}

// Write is an alias for Save.
func Write(path string, cfg Config) error { return Save(path, cfg) }

// Init creates a default profile and refuses to overwrite an existing file.
func Init(path string) (ResolvedConfig, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultConfigPath()
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("resolve config path: %w", err)
	}
	if _, err := os.Stat(abs); err == nil {
		return ResolvedConfig{}, fmt.Errorf("config already exists: %s", abs)
	} else if !errors.Is(err, os.ErrNotExist) {
		return ResolvedConfig{}, fmt.Errorf("inspect config path: %w", err)
	}
	cfg := Default()
	if err := Save(abs, cfg); err != nil {
		return ResolvedConfig{}, err
	}
	resolved, err := Resolve(cfg, ResolveOptions{BaseDir: filepath.Dir(abs)})
	if err != nil {
		return ResolvedConfig{}, err
	}
	resolved.ConfigPath = abs
	return resolved, nil
}
