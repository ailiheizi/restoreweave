// Deprecated: this package is the legacy extension prototype described in
// docs/technical/nas-vertical-slice-implementation-plan.md. It is retained
// only as reference and has no current callers. The active seams are
// CaptureDriver, Processor, RepositoryDriver, IndexProvider, and
// QueryProvider; its builtin suffix/magic rules were ported to
// server/internal/identify.
package plugin

import (
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
)

const ManifestSchemaV1Alpha1 = "restoreweave.plugin/v1alpha1"

// Digest is an algorithm-qualified lowercase hexadecimal content digest, for
// example sha256:0123.... It deliberately does not accept mutable references.
type Digest string

func (d Digest) Validate() error {
	algorithm, encoded, ok := strings.Cut(string(d), ":")
	if !ok || algorithm == "" || encoded == "" {
		return errors.New("digest must use algorithm:lowercase-hex form")
	}
	wantBytes := 0
	switch algorithm {
	case "sha256", "blake3":
		wantBytes = 32
	case "sha512":
		wantBytes = 64
	default:
		return fmt.Errorf("unsupported digest algorithm %q", algorithm)
	}
	if encoded != strings.ToLower(encoded) {
		return errors.New("digest hexadecimal must be lowercase")
	}
	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("invalid digest hexadecimal: %w", err)
	}
	if len(decoded) != wantBytes {
		return fmt.Errorf("%s digest has %d bytes, want %d", algorithm, len(decoded), wantBytes)
	}
	return nil
}

type Category string

const (
	CategoryDetector                         Category = "DETECTOR"
	CategoryParser                           Category = "PARSER"
	CategoryCollectionResolver               Category = "COLLECTION_RESOLVER"
	CategoryCaptureProvider                  Category = "CAPTURE_PROVIDER"
	CategoryApplicationCaptureAdapter        Category = "APPLICATION_CAPTURE_ADAPTER"
	CategoryExtractor                        Category = "EXTRACTOR"
	CategoryFingerprinter                    Category = "FINGERPRINTER"
	CategoryEmbedder                         Category = "EMBEDDER"
	CategoryTransformer                      Category = "TRANSFORMER"
	CategoryValidator                        Category = "VALIDATOR"
	CategoryRetriever                        Category = "RETRIEVER"
	CategoryMagnetRetriever                  Category = "MAGNET_RETRIEVER"
	CategoryPolicyAdvisor                    Category = "POLICY_ADVISOR"
	CategoryRouter                           Category = "ROUTER"
	CategoryStorageAdapter                   Category = "STORAGE_ADAPTER"
	CategorySwarmStorageAdapter              Category = "SWARM_STORAGE_ADAPTER"
	CategoryRecoveryArtifactTransportAdapter Category = "RECOVERY_ARTIFACT_TRANSPORT_ADAPTER"
	CategoryBackupEngineAdapter              Category = "BACKUP_ENGINE_ADAPTER"
	CategoryRepositoryReadAdapter            Category = "REPOSITORY_READ_ADAPTER"
	CategoryStorageRangeReader               Category = "STORAGE_RANGE_READER"
	CategoryRepresentationDecoder            Category = "REPRESENTATION_DECODER"
	CategoryPackIndexReader                  Category = "PACK_INDEX_READER"
	CategoryNamespaceIndexAdapter            Category = "NAMESPACE_INDEX_ADAPTER"
	CategoryNamespaceGatewayAdapter          Category = "NAMESPACE_GATEWAY_ADAPTER"
	CategorySearchIndexer                    Category = "SEARCH_INDEXER"
	CategoryRestorer                         Category = "RESTORER"
)

var categories = map[Category]struct{}{
	CategoryDetector: {}, CategoryParser: {}, CategoryCollectionResolver: {},
	CategoryCaptureProvider: {}, CategoryApplicationCaptureAdapter: {},
	CategoryExtractor: {}, CategoryFingerprinter: {}, CategoryEmbedder: {},
	CategoryTransformer: {}, CategoryValidator: {}, CategoryRetriever: {},
	CategoryMagnetRetriever: {}, CategoryPolicyAdvisor: {}, CategoryRouter: {},
	CategoryStorageAdapter: {}, CategorySwarmStorageAdapter: {},
	CategoryRecoveryArtifactTransportAdapter: {}, CategoryBackupEngineAdapter: {},
	CategoryRepositoryReadAdapter: {}, CategoryStorageRangeReader: {},
	CategoryRepresentationDecoder: {}, CategoryPackIndexReader: {},
	CategoryNamespaceIndexAdapter: {}, CategoryNamespaceGatewayAdapter: {},
	CategorySearchIndexer: {},
	CategoryRestorer:      {},
}

type TransformationClass string

const (
	TransformationNotApplicable          TransformationClass = "NOT_APPLICABLE"
	TransformationIdentityRaw            TransformationClass = "IDENTITY_RAW"
	TransformationByteLosslessCodec      TransformationClass = "BYTE_LOSSLESS_CODEC"
	TransformationReversibleFormat       TransformationClass = "REVERSIBLE_FORMAT_TRANSFORM"
	TransformationContentLossless        TransformationClass = "CONTENT_LOSSLESS_TRANSCODE"
	TransformationLossy                  TransformationClass = "LOSSY_TRANSCODE"
	TransformationGenerative             TransformationClass = "GENERATIVE_RECONSTRUCTION"
	TransformationSemanticRepresentation TransformationClass = "SEMANTIC_REPRESENTATION"
	TransformationRehydratableReference  TransformationClass = "REHYDRATABLE_REFERENCE"
)

var transformationClasses = map[TransformationClass]struct{}{
	TransformationNotApplicable: {}, TransformationIdentityRaw: {},
	TransformationByteLosslessCodec: {}, TransformationReversibleFormat: {},
	TransformationContentLossless: {}, TransformationLossy: {},
	TransformationGenerative: {}, TransformationSemanticRepresentation: {},
	TransformationRehydratableReference: {},
}

// ExecutionClass describes output reproducibility, not process placement.
type ExecutionClass string

const (
	ExecutionByteDeterministic      ExecutionClass = "BYTE_DETERMINISTIC"
	ExecutionSeededStochastic       ExecutionClass = "SEEDED_STOCHASTIC"
	ExecutionOpaqueNondeterministic ExecutionClass = "OPAQUE_NONDETERMINISTIC"
)

func (c ExecutionClass) valid() bool {
	switch c {
	case ExecutionByteDeterministic, ExecutionSeededStochastic, ExecutionOpaqueNondeterministic:
		return true
	default:
		return false
	}
}

type PortType string

const (
	PortCaptureRequest              PortType = "CaptureRequest"
	PortCaptureSetRecord            PortType = "CaptureSetRecord"
	PortCaptureSetLifecycleEvent    PortType = "CaptureSetLifecycleEvent"
	PortDetectionRequest            PortType = "DetectionRequest"
	PortDetectionEvidence           PortType = "DetectionEvidence"
	PortParseRequest                PortType = "ParseRequest"
	PortParseTree                   PortType = "ParseTree"
	PortComponentInventory          PortType = "ComponentInventory"
	PortExtractionResult            PortType = "ExtractionResult"
	PortFingerprintRecord           PortType = "FingerprintRecord"
	PortEmbeddingRecord             PortType = "EmbeddingRecord"
	PortDiscoveryCandidate          PortType = "DiscoveryCandidate"
	PortRepresentationRef           PortType = "RepresentationRef"
	PortValidationEvidence          PortType = "ValidationEvidence"
	PortPolicyProposal              PortType = "PolicyProposal"
	PortStorageReceipt              PortType = "StorageReceipt"
	PortPlacementReceipt            PortType = "PlacementReceipt"
	PortSnapshotTree                PortType = "SnapshotTree"
	PortFileAccess                  PortType = "FileAccess"
	PortRepositoryReadRequest       PortType = "RepositoryReadRequest"
	PortStorageRangeRequest         PortType = "StorageRangeRequest"
	PortRepresentationDecodeRequest PortType = "RepresentationDecodeRequest"
	PortExactReadHandle             PortType = "ExactReadHandle"
	PortImmutableRangeRead          PortType = "ImmutableRangeRead"
	PortDecodedReadHandle           PortType = "DecodedReadHandle"
	PortReadEvidence                PortType = "ReadEvidence"
	PortDecodeEvidence              PortType = "DecodeEvidence"
	PortPackIndexRequest            PortType = "PackIndexRequest"
	PortPackSliceCandidates         PortType = "PackSliceCandidates"
	PortPackIndexEvidence           PortType = "PackIndexEvidence"
	PortGatewayRequest              PortType = "GatewayRequest"
	PortGatewaySessionReceipt       PortType = "GatewaySessionReceipt"
	PortNamespaceMutationBatch      PortType = "NamespaceMutationBatch"
	PortNamespaceIndexReceipt       PortType = "NamespaceIndexReceipt"
	PortNamespaceLookup             PortType = "NamespaceLookup"
	PortNamespaceCandidates         PortType = "NamespaceCandidates"
	PortIndexMutationBatch          PortType = "IndexMutationBatch"
	PortIndexReceipt                PortType = "IndexReceipt"
	PortSearchQuery                 PortType = "SearchQuery"
	PortSearchCandidates            PortType = "SearchCandidates"
)

var knownPorts = map[PortType]struct{}{
	PortCaptureRequest: {}, PortCaptureSetRecord: {}, PortCaptureSetLifecycleEvent: {},
	PortDetectionRequest: {}, PortDetectionEvidence: {}, PortParseRequest: {},
	PortParseTree: {}, PortComponentInventory: {}, PortExtractionResult: {},
	PortFingerprintRecord: {}, PortEmbeddingRecord: {}, PortDiscoveryCandidate: {},
	PortRepresentationRef: {}, PortValidationEvidence: {}, PortPolicyProposal: {},
	PortStorageReceipt: {}, PortPlacementReceipt: {},
	PortSnapshotTree: {}, PortFileAccess: {}, PortRepositoryReadRequest: {}, PortStorageRangeRequest: {},
	PortRepresentationDecodeRequest: {}, PortExactReadHandle: {},
	PortImmutableRangeRead: {}, PortDecodedReadHandle: {}, PortReadEvidence: {},
	PortDecodeEvidence: {}, PortPackIndexRequest: {}, PortPackSliceCandidates: {},
	PortPackIndexEvidence: {}, PortGatewayRequest: {}, PortGatewaySessionReceipt: {},
	PortNamespaceMutationBatch: {}, PortNamespaceIndexReceipt: {},
	PortNamespaceLookup: {}, PortNamespaceCandidates: {},
	PortIndexMutationBatch: {}, PortIndexReceipt: {}, PortSearchQuery: {},
	PortSearchCandidates: {},
}

type PortDeclaration struct {
	Name     string   `json:"name"`
	Type     PortType `json:"type"`
	SchemaID string   `json:"schema_id"`
	Required bool     `json:"required,omitempty"`
}

func (p PortDeclaration) validate() error {
	if err := validateStableID(p.Name); err != nil {
		return fmt.Errorf("name: %w", err)
	}
	if _, ok := knownPorts[p.Type]; !ok {
		return fmt.Errorf("unknown port type %q", p.Type)
	}
	if strings.TrimSpace(p.SchemaID) != p.SchemaID || p.SchemaID == "" {
		return errors.New("schema_id must be non-empty and trimmed")
	}
	return nil
}

type Platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

func CurrentPlatform() Platform {
	return Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
}

type Dependency struct {
	ID       string `json:"id"`
	Version  string `json:"version,omitempty"`
	Digest   Digest `json:"digest"`
	Required bool   `json:"required"`
}

type PackageTrustState string

const (
	PackageInstalledUnverified PackageTrustState = "INSTALLED_UNVERIFIED"
	PackageInstalledTrusted    PackageTrustState = "INSTALLED_TRUSTED"
	PackageQuarantined         PackageTrustState = "QUARANTINED"
	PackageRevoked             PackageTrustState = "REVOKED"
	PackageRemoved             PackageTrustState = "REMOVED"
)

// PackageManifest contains metadata common to every entry point. Entry-point
// permissions and lifecycle state intentionally do not appear here.
type PackageManifest struct {
	ID                 string            `json:"id"`
	Version            string            `json:"version"`
	Compatibility      string            `json:"compatibility"`
	Publisher          string            `json:"publisher"`
	PublisherKeyID     string            `json:"publisher_key_id"`
	PublisherSignature Digest            `json:"publisher_signature_digest"`
	ArtifactDigest     Digest            `json:"artifact_digest"`
	Platforms          []Platform        `json:"platforms"`
	Dependencies       []Dependency      `json:"dependencies,omitempty"`
	LicenseExpression  string            `json:"license_expression"`
	NoticeDigest       Digest            `json:"notice_digest,omitempty"`
	SBOMDigest         Digest            `json:"sbom_digest"`
	TrustState         PackageTrustState `json:"trust_state"`
}

type RuntimeKind string

const (
	RuntimeBuiltin      RuntimeKind = "BUILTIN"
	RuntimeOutOfProcess RuntimeKind = "OUT_OF_PROCESS"
	RuntimeWASM         RuntimeKind = "WASM"
)

type RuntimeLifecycle string

const (
	RuntimeOneShot RuntimeLifecycle = "ONE_SHOT"
	RuntimeSession RuntimeLifecycle = "SESSION"
)

// RuntimeDescriptor is declarative. In particular, it has no shell command
// field. Future process and WASM adapters must resolve an artifact by digest
// and pass a structured invocation envelope.
type RuntimeDescriptor struct {
	Kind           RuntimeKind      `json:"kind"`
	Lifecycle      RuntimeLifecycle `json:"lifecycle"`
	AdapterID      string           `json:"adapter_id"`
	Protocol       string           `json:"protocol"`
	ArtifactDigest Digest           `json:"artifact_digest,omitempty"`
	Entrypoint     string           `json:"entrypoint"`
	StaticArgs     []string         `json:"static_args,omitempty"`
}

func (r RuntimeDescriptor) validate() error {
	switch r.Kind {
	case RuntimeBuiltin, RuntimeOutOfProcess, RuntimeWASM:
	default:
		return fmt.Errorf("unknown runtime kind %q", r.Kind)
	}
	switch r.Lifecycle {
	case RuntimeOneShot, RuntimeSession:
	default:
		return fmt.Errorf("unknown runtime lifecycle %q", r.Lifecycle)
	}
	if err := validateStableID(r.AdapterID); err != nil {
		return fmt.Errorf("adapter_id: %w", err)
	}
	if err := validateStableID(r.Protocol); err != nil {
		return fmt.Errorf("protocol: %w", err)
	}
	if err := validateStableID(r.Entrypoint); err != nil {
		return fmt.Errorf("entrypoint: %w", err)
	}
	if r.Kind != RuntimeBuiltin {
		if err := r.ArtifactDigest.Validate(); err != nil {
			return fmt.Errorf("artifact_digest: %w", err)
		}
	}
	for i, argument := range r.StaticArgs {
		if strings.ContainsAny(argument, "\x00\r\n") {
			return fmt.Errorf("static_args[%d] contains a control character", i)
		}
	}
	return nil
}

type CapabilityEnablement string

const (
	EnablementDisabled           CapabilityEnablement = "DISABLED"
	EnablementEnabled            CapabilityEnablement = "ENABLED"
	EnablementRestricted         CapabilityEnablement = "RESTRICTED"
	EnablementRetainedForRestore CapabilityEnablement = "RETAINED_FOR_RESTORE"
)

type QualificationState string

const (
	QualificationExperimental      QualificationState = "EXPERIMENTAL_DUAL_WRITE"
	QualificationQualified         QualificationState = "QUALIFIED"
	QualificationDeprecated        QualificationState = "DEPRECATED_WRITE_BLOCKED"
	QualificationMigrationRequired QualificationState = "MIGRATION_REQUIRED"
	QualificationReadOnlyLegacy    QualificationState = "READ_ONLY_LEGACY"
	QualificationRetired           QualificationState = "RETIRED_NO_DEPENDENTS"
)

type SupportDeclaration struct {
	MIMETypes   []string `json:"mime_types,omitempty"`
	Formats     []string `json:"formats,omitempty"`
	Containers  []string `json:"containers,omitempty"`
	Protocols   []string `json:"protocols,omitempty"`
	Platforms   []string `json:"platforms,omitempty"`
	EntityKinds []string `json:"entity_kinds,omitempty"`
}

type EntryPointManifest struct {
	ID                  string               `json:"id"`
	Name                string               `json:"name"`
	Category            Category             `json:"category"`
	TransformationClass TransformationClass  `json:"transformation_class"`
	ExecutionClass      ExecutionClass       `json:"execution_class"`
	Inputs              []PortDeclaration    `json:"inputs"`
	Outputs             []PortDeclaration    `json:"outputs"`
	Support             SupportDeclaration   `json:"support,omitempty"`
	Capabilities        CapabilitySet        `json:"capabilities"`
	Runtime             RuntimeDescriptor    `json:"runtime"`
	Enablement          CapabilityEnablement `json:"enablement"`
	Qualification       QualificationState   `json:"qualification"`
	CanExecuteNewWork   bool                 `json:"can_execute_new_work"`
	CanDecodeHistorical bool                 `json:"can_decode_historical"`
	ConfigurationSchema string               `json:"configuration_schema"`
	ConformanceSuiteID  string               `json:"conformance_suite_id"`
}

type Manifest struct {
	SchemaVersion string               `json:"schema_version"`
	Package       PackageManifest      `json:"package"`
	EntryPoints   []EntryPointManifest `json:"entry_points"`
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != ManifestSchemaV1Alpha1 {
		return fmt.Errorf("schema_version must be %q", ManifestSchemaV1Alpha1)
	}
	if err := m.Package.validate(); err != nil {
		return fmt.Errorf("package: %w", err)
	}
	if len(m.EntryPoints) == 0 {
		return errors.New("manifest must declare at least one entry point")
	}
	seen := make(map[string]struct{}, len(m.EntryPoints))
	for i, entryPoint := range m.EntryPoints {
		if _, duplicate := seen[entryPoint.ID]; duplicate {
			return fmt.Errorf("entry_points[%d]: duplicate id %q", i, entryPoint.ID)
		}
		seen[entryPoint.ID] = struct{}{}
		if err := entryPoint.validate(); err != nil {
			return fmt.Errorf("entry_points[%d]: %w", i, err)
		}
	}
	return nil
}

func (p PackageManifest) validate() error {
	if err := validateStableID(p.ID); err != nil {
		return fmt.Errorf("id: %w", err)
	}
	for name, value := range map[string]string{
		"version": p.Version, "compatibility": p.Compatibility,
		"publisher": p.Publisher, "publisher_key_id": p.PublisherKeyID,
		"license_expression": p.LicenseExpression,
	} {
		if strings.TrimSpace(value) != value || value == "" {
			return fmt.Errorf("%s must be non-empty and trimmed", name)
		}
	}
	for name, digest := range map[string]Digest{
		"publisher_signature_digest": p.PublisherSignature,
		"artifact_digest":            p.ArtifactDigest,
		"sbom_digest":                p.SBOMDigest,
	} {
		if err := digest.Validate(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if p.NoticeDigest != "" {
		if err := p.NoticeDigest.Validate(); err != nil {
			return fmt.Errorf("notice_digest: %w", err)
		}
	}
	if len(p.Platforms) == 0 {
		return errors.New("platforms must not be empty")
	}
	switch p.TrustState {
	case PackageInstalledUnverified, PackageInstalledTrusted, PackageQuarantined,
		PackageRevoked, PackageRemoved:
	default:
		return fmt.Errorf("unknown trust_state %q", p.TrustState)
	}
	for i, platform := range p.Platforms {
		if platform.OS == "" || platform.Arch == "" {
			return fmt.Errorf("platforms[%d] must include os and arch", i)
		}
	}
	seenDependencies := make(map[string]struct{}, len(p.Dependencies))
	for i, dependency := range p.Dependencies {
		if err := validateStableID(dependency.ID); err != nil {
			return fmt.Errorf("dependencies[%d].id: %w", i, err)
		}
		if _, duplicate := seenDependencies[dependency.ID]; duplicate {
			return fmt.Errorf("dependencies[%d]: duplicate id %q", i, dependency.ID)
		}
		seenDependencies[dependency.ID] = struct{}{}
		if err := dependency.Digest.Validate(); err != nil {
			return fmt.Errorf("dependencies[%d].digest: %w", i, err)
		}
	}
	return nil
}

func (e EntryPointManifest) validate() error {
	if err := validateStableID(e.ID); err != nil {
		return fmt.Errorf("id: %w", err)
	}
	if strings.TrimSpace(e.Name) != e.Name || e.Name == "" {
		return errors.New("name must be non-empty and trimmed")
	}
	if _, ok := categories[e.Category]; !ok {
		return fmt.Errorf("unknown category %q", e.Category)
	}
	if _, ok := transformationClasses[e.TransformationClass]; !ok {
		return fmt.Errorf("unknown transformation_class %q", e.TransformationClass)
	}
	if e.Category == CategoryTransformer && e.TransformationClass == TransformationNotApplicable {
		return errors.New("transformer must declare a transformation class")
	}
	if e.Category != CategoryTransformer && e.TransformationClass != TransformationNotApplicable {
		return errors.New("non-transformer must use NOT_APPLICABLE transformation class")
	}
	if !e.ExecutionClass.valid() {
		return fmt.Errorf("unknown execution_class %q", e.ExecutionClass)
	}
	if err := validatePorts(e.Inputs, "inputs"); err != nil {
		return err
	}
	if err := validatePorts(e.Outputs, "outputs"); err != nil {
		return err
	}
	if err := e.Capabilities.Validate(); err != nil {
		return fmt.Errorf("capabilities: %w", err)
	}
	if err := e.Runtime.validate(); err != nil {
		return fmt.Errorf("runtime: %w", err)
	}
	switch e.Enablement {
	case EnablementDisabled, EnablementEnabled, EnablementRestricted, EnablementRetainedForRestore:
	default:
		return fmt.Errorf("unknown enablement %q", e.Enablement)
	}
	switch e.Qualification {
	case QualificationExperimental, QualificationQualified, QualificationDeprecated,
		QualificationMigrationRequired, QualificationReadOnlyLegacy, QualificationRetired:
	default:
		return fmt.Errorf("unknown qualification %q", e.Qualification)
	}
	if e.Enablement == EnablementRetainedForRestore && e.CanExecuteNewWork {
		return errors.New("RETAINED_FOR_RESTORE cannot execute new work")
	}
	if e.Qualification == QualificationRetired && (e.CanExecuteNewWork || e.CanDecodeHistorical) {
		return errors.New("RETIRED_NO_DEPENDENTS cannot execute or decode")
	}
	if strings.TrimSpace(e.ConfigurationSchema) != e.ConfigurationSchema || e.ConfigurationSchema == "" {
		return errors.New("configuration_schema must be non-empty and trimmed")
	}
	if strings.TrimSpace(e.ConformanceSuiteID) != e.ConformanceSuiteID || e.ConformanceSuiteID == "" {
		return errors.New("conformance_suite_id must be non-empty and trimmed")
	}
	if err := validateCategoryPorts(e); err != nil {
		return err
	}
	if err := validateCategoryCapabilities(e); err != nil {
		return err
	}
	return nil
}

func validatePorts(ports []PortDeclaration, field string) error {
	seen := make(map[string]struct{}, len(ports))
	for i, port := range ports {
		if _, duplicate := seen[port.Name]; duplicate {
			return fmt.Errorf("%s[%d]: duplicate name %q", field, i, port.Name)
		}
		seen[port.Name] = struct{}{}
		if err := port.validate(); err != nil {
			return fmt.Errorf("%s[%d]: %w", field, i, err)
		}
	}
	return nil
}

func validateCategoryPorts(e EntryPointManifest) error {
	hasInput := func(portType PortType) bool {
		for _, port := range e.Inputs {
			if port.Type == portType {
				return true
			}
		}
		return false
	}
	hasOutput := func(portType PortType) bool {
		for _, port := range e.Outputs {
			if port.Type == portType {
				return true
			}
		}
		return false
	}
	switch e.Category {
	case CategoryDetector:
		if !hasInput(PortDetectionRequest) || !hasOutput(PortDetectionEvidence) {
			return errors.New("detector requires DetectionRequest input and DetectionEvidence output")
		}
	case CategoryParser:
		if !hasInput(PortParseRequest) || !hasOutput(PortParseTree) {
			return errors.New("parser requires ParseRequest input and ParseTree output")
		}
	case CategoryRepositoryReadAdapter:
		if !hasInput(PortRepositoryReadRequest) || !hasOutput(PortExactReadHandle) ||
			!hasOutput(PortReadEvidence) {
			return errors.New("repository reader requires RepositoryReadRequest input plus ExactReadHandle and ReadEvidence outputs")
		}
	case CategoryStorageRangeReader:
		if !hasInput(PortStorageRangeRequest) || !hasOutput(PortImmutableRangeRead) ||
			!hasOutput(PortReadEvidence) {
			return errors.New("storage range reader requires StorageRangeRequest input plus ImmutableRangeRead and ReadEvidence outputs")
		}
	case CategoryRepresentationDecoder:
		if !hasInput(PortRepresentationDecodeRequest) || !hasInput(PortRepresentationRef) ||
			!hasOutput(PortDecodedReadHandle) || !hasOutput(PortDecodeEvidence) {
			return errors.New("representation decoder requires RepresentationRef and RepresentationDecodeRequest inputs plus DecodedReadHandle and DecodeEvidence outputs")
		}
	case CategoryPackIndexReader:
		if !hasInput(PortPackIndexRequest) || !hasInput(PortRepresentationRef) ||
			!hasOutput(PortPackSliceCandidates) || !hasOutput(PortPackIndexEvidence) {
			return errors.New("pack index reader requires bounded index representation and request inputs plus candidate and evidence outputs")
		}
	case CategoryNamespaceIndexAdapter:
		materializeMode := hasInput(PortNamespaceMutationBatch) && hasOutput(PortNamespaceIndexReceipt)
		lookupMode := hasInput(PortNamespaceLookup) && hasOutput(PortNamespaceCandidates)
		if materializeMode == lookupMode {
			return errors.New("namespace index entry point must implement exactly one of materialization or lookup mode")
		}
	case CategoryNamespaceGatewayAdapter:
		if !hasInput(PortSnapshotTree) || !hasInput(PortFileAccess) || !hasInput(PortGatewayRequest) ||
			!hasOutput(PortGatewaySessionReceipt) {
			return errors.New("namespace gateway requires export-bound SnapshotTree, FileAccess, and GatewayRequest inputs plus GatewaySessionReceipt output")
		}
		if e.Runtime.Lifecycle != RuntimeSession {
			return errors.New("namespace gateway requires SESSION runtime lifecycle")
		}
	case CategorySearchIndexer:
		indexMode := hasInput(PortIndexMutationBatch) && hasOutput(PortIndexReceipt)
		queryMode := hasInput(PortSearchQuery) && hasOutput(PortSearchCandidates)
		if indexMode == queryMode {
			return errors.New("search entry point must implement exactly one of index-mutation or query mode")
		}
	}
	return nil
}

func validateCategoryCapabilities(e EntryPointManifest) error {
	switch e.Category {
	case CategoryDetector:
		if !e.Capabilities.Requires(CapabilityEmitDetection) {
			return errors.New("detector must declare EMIT_DETECTION_EVIDENCE")
		}
		if !e.Capabilities.Requires(CapabilityReadInputMetadata) &&
			!e.Capabilities.Requires(CapabilityReadInputSamples) &&
			!e.Capabilities.Requires(CapabilityReadInputContent) {
			return errors.New("detector must declare a bounded input-read capability")
		}
	case CategoryParser:
		if !e.Capabilities.Requires(CapabilityEmitParse) ||
			!e.Capabilities.Requires(CapabilityReadInputContent) {
			return errors.New("parser must declare READ_INPUT_CONTENT and EMIT_PARSE_EVIDENCE")
		}
	case CategoryRepositoryReadAdapter:
		if !e.Capabilities.Requires(CapabilityReadRepository) {
			return errors.New("repository reader must declare READ_REPOSITORY_OBJECT")
		}
		if err := forbidCapabilities(e, CapabilityWriteStorage, CapabilityManageBackend); err != nil {
			return err
		}
	case CategoryStorageRangeReader:
		if !e.Capabilities.Requires(CapabilityReadStorage) {
			return errors.New("storage range reader must declare READ_STORAGE")
		}
		if err := forbidCapabilities(e, CapabilityWriteStorage, CapabilityManageBackend, CapabilityReadRepository); err != nil {
			return err
		}
	case CategoryRepresentationDecoder:
		if !e.Capabilities.Requires(CapabilityReadRepresentation) {
			return errors.New("representation decoder must declare READ_REPRESENTATION")
		}
		if err := forbidCapabilities(e, CapabilityReadStorage, CapabilityWriteStorage,
			CapabilityManageBackend, CapabilityReadRepository); err != nil {
			return err
		}
	case CategoryPackIndexReader:
		if !e.Capabilities.Requires(CapabilityReadPackIndex) {
			return errors.New("pack index reader must require READ_PACK_INDEX")
		}
		if err := forbidCapabilities(e, CapabilityReadStorage, CapabilityWriteStorage,
			CapabilityManageBackend, CapabilityReadRepository); err != nil {
			return err
		}
	case CategoryNamespaceIndexAdapter:
		materializeMode := hasPortType(e.Inputs, PortNamespaceMutationBatch)
		if materializeMode && !e.Capabilities.Requires(CapabilityWriteNamespaceIndex) {
			return errors.New("namespace materialization entry point must require WRITE_NAMESPACE_INDEX")
		}
		if !materializeMode && !e.Capabilities.Requires(CapabilityReadNamespaceIndex) {
			return errors.New("namespace lookup entry point must require READ_NAMESPACE_INDEX")
		}
		if err := forbidCapabilities(e, CapabilityServeGateway, CapabilityReadStorage,
			CapabilityWriteStorage, CapabilityManageBackend, CapabilityReadRepository,
			CapabilityReadRepresentation, CapabilityReadPackIndex); err != nil {
			return err
		}
	case CategoryNamespaceGatewayAdapter:
		if !e.Capabilities.Requires(CapabilityServeGateway) {
			return errors.New("namespace gateway must require SERVE_GATEWAY_SESSION")
		}
		if err := forbidCapabilities(e, CapabilityReadStorage, CapabilityWriteStorage,
			CapabilityManageBackend, CapabilityReadRepository, CapabilityReadRepresentation); err != nil {
			return err
		}
	case CategorySearchIndexer:
		indexMode := hasPortType(e.Inputs, PortIndexMutationBatch)
		if indexMode && !e.Capabilities.Requires(CapabilityWriteSearchIndex) {
			return errors.New("search mutation entry point must declare WRITE_SEARCH_INDEX")
		}
		if !indexMode && !e.Capabilities.Requires(CapabilityReadSearchIndex) {
			return errors.New("search query entry point must declare READ_SEARCH_INDEX")
		}
	}
	return nil
}

func hasPortType(ports []PortDeclaration, portType PortType) bool {
	for _, port := range ports {
		if port.Type == portType {
			return true
		}
	}
	return false
}

func forbidCapabilities(e EntryPointManifest, names ...CapabilityName) error {
	for _, name := range names {
		if e.Capabilities.Declares(name) {
			return fmt.Errorf("%s must not declare non-coherent capability %s", e.Category, name)
		}
	}
	return nil
}

func validateStableID(value string) error {
	if strings.TrimSpace(value) != value || value == "" {
		return errors.New("must be non-empty and trimmed")
	}
	if len(value) > 200 {
		return errors.New("must not exceed 200 bytes")
	}
	for i, r := range value {
		allowed := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' ||
			r == '.' || r == '-' || r == '_' || r == '/' || r == ':'
		if !allowed {
			return fmt.Errorf("contains unsupported character %q at byte %d", r, i)
		}
	}
	first := value[0]
	if first < 'a' || first > 'z' {
		return errors.New("must begin with a lowercase ASCII letter")
	}
	return nil
}

func validateOpaqueID(value string) error {
	if strings.TrimSpace(value) != value || value == "" {
		return errors.New("must be non-empty and trimmed")
	}
	if len(value) > 512 {
		return errors.New("must not exceed 512 bytes")
	}
	for i, r := range value {
		allowed := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' ||
			r == '/' || r == ':'
		if !allowed {
			return fmt.Errorf("contains unsupported character %q at byte %d", r, i)
		}
	}
	return nil
}

// CanonicalEntryPointIDs returns stable package-qualified IDs without relying
// on declaration order.
func (m Manifest) CanonicalEntryPointIDs() []string {
	ids := make([]string, 0, len(m.EntryPoints))
	for _, entryPoint := range m.EntryPoints {
		ids = append(ids, m.Package.ID+"/"+entryPoint.ID)
	}
	sort.Strings(ids)
	return ids
}
