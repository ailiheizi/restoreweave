// Package config exposes the shared persisted configuration to server-side
// components.  The implementation lives in the module-level config package so
// clients and the daemon use exactly the same schema and digest rules.
package config

import (
	"io"

	shared "github.com/ailiheizi/restoreweave/config"
)

const (
	SchemaV1           = shared.SchemaV1
	ConfigEnv          = shared.ConfigEnv
	CatalogEnv         = shared.CatalogEnv
	RepositoryEnv      = shared.RepositoryEnv
	VectorsEnv         = shared.VectorsEnv
	ModelsEnv          = shared.ModelsEnv
	RecoveryRecordsEnv = shared.RecoveryRecordsEnv
)

type Paths = shared.Paths
type StorageConfig = shared.StorageConfig
type SemanticConfig = shared.SemanticConfig
type DescriptionConfig = shared.DescriptionConfig
type Descriptions = shared.Descriptions
type RecoveryConfig = shared.RecoveryConfig
type Recovery = shared.Recovery
type APIConfig = shared.APIConfig
type Config = shared.Config
type PathOverrides = shared.PathOverrides
type ResolveOptions = shared.ResolveOptions
type LoadOptions = shared.LoadOptions
type ResolvedConfig = shared.ResolvedConfig

func DefaultConfigPath() string                           { return shared.DefaultConfigPath() }
func PlatformConfigPath() string                          { return shared.PlatformConfigPath() }
func Default() Config                                     { return shared.Default() }
func DefaultConfig() Config                               { return shared.DefaultConfig() }
func LoadFile(path string) (Config, error)                { return shared.LoadFile(path) }
func Decode(r io.Reader) (Config, error)                  { return shared.Decode(r) }
func DecodeTOML(r io.Reader) (Config, error)              { return shared.DecodeTOML(r) }
func Parse(r io.Reader) (Config, error)                   { return shared.Parse(r) }
func DecodeJSON(r io.Reader) (Config, error)              { return shared.DecodeJSON(r) }
func LoadEffective(o LoadOptions) (ResolvedConfig, error) { return shared.LoadEffective(o) }
func Load(path string) (ResolvedConfig, error)            { return shared.Load(path) }
func Resolve(c Config, o ResolveOptions) (ResolvedConfig, error) {
	return shared.Resolve(c, o)
}
func Validate(c Config) error                { return shared.Validate(c) }
func MarshalYAML(c Config) ([]byte, error)   { return shared.MarshalYAML(c) }
func MarshalTOML(c Config) ([]byte, error)   { return shared.MarshalTOML(c) }
func CanonicalJSON(c Config) ([]byte, error) { return shared.CanonicalJSON(c) }
func RedactedJSON(c ResolvedConfig) ([]byte, error) {
	return shared.RedactedJSON(c)
}
func Save(path string, c Config) error         { return shared.Save(path, c) }
func Write(path string, c Config) error        { return shared.Write(path, c) }
func Init(path string) (ResolvedConfig, error) { return shared.Init(path) }
