package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"sync"
	"time"
)

var (
	ErrExternalPluginsDisabled = errors.New("external plugins are disabled")
	ErrPackageNotTrusted       = errors.New("plugin package is not trusted")
	ErrBuiltinNotPinned        = errors.New("built-in plugin manifest is not pinned")
)

// RuntimeAdmission separates manifest parsing from authority to execute. The
// MVP implementation below admits trusted built-ins only. A later phase can
// provide a different policy after the process/WASM sandboxes are qualified.
type RuntimeAdmission interface {
	Admit(RuntimeDescriptor) error
}

type BuiltinOnlyAdmission struct{}

func (BuiltinOnlyAdmission) Admit(runtime RuntimeDescriptor) error {
	if err := runtime.validate(); err != nil {
		return err
	}
	if runtime.Kind != RuntimeBuiltin {
		return fmt.Errorf("%w: runtime kind %s", ErrExternalPluginsDisabled, runtime.Kind)
	}
	return nil
}

// Catalog stores validated manifests. Registration does not execute package
// initialization code and MVP admission rejects all non-built-in runtimes.
type Catalog struct {
	mu        sync.RWMutex
	admission RuntimeAdmission
	bindings  map[Digest]BuiltinBinding
	packages  map[string]Manifest
}

type BuiltinBinding struct {
	ManifestDigest Digest `json:"manifest_digest"`
	PackageID      string `json:"package_id"`
	ArtifactDigest Digest `json:"artifact_digest"`
}

func (b BuiltinBinding) validate() error {
	if err := b.ManifestDigest.Validate(); err != nil {
		return fmt.Errorf("manifest_digest: %w", err)
	}
	if err := validateStableID(b.PackageID); err != nil {
		return fmt.Errorf("package_id: %w", err)
	}
	if err := b.ArtifactDigest.Validate(); err != nil {
		return fmt.Errorf("artifact_digest: %w", err)
	}
	return nil
}

func NewMVPPluginCatalog(bindings ...BuiltinBinding) (*Catalog, error) {
	catalog := &Catalog{
		admission: BuiltinOnlyAdmission{},
		bindings:  make(map[Digest]BuiltinBinding, len(bindings)),
		packages:  make(map[string]Manifest),
	}
	for i, binding := range bindings {
		if err := binding.validate(); err != nil {
			return nil, fmt.Errorf("bindings[%d]: %w", i, err)
		}
		if _, duplicate := catalog.bindings[binding.ManifestDigest]; duplicate {
			return nil, fmt.Errorf("bindings[%d]: duplicate manifest digest %s", i, binding.ManifestDigest)
		}
		catalog.bindings[binding.ManifestDigest] = binding
	}
	return catalog, nil
}

func (c *Catalog) Register(manifest Manifest) error {
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("invalid plugin manifest: %w", err)
	}
	if manifest.Package.TrustState != PackageInstalledTrusted {
		return fmt.Errorf("%w: state %s", ErrPackageNotTrusted, manifest.Package.TrustState)
	}
	for _, entryPoint := range manifest.EntryPoints {
		if err := c.admission.Admit(entryPoint.Runtime); err != nil {
			return fmt.Errorf("entry point %s: %w", entryPoint.ID, err)
		}
	}
	manifestDigest, err := ManifestContentDigest(manifest)
	if err != nil {
		return err
	}
	binding, pinned := c.bindings[manifestDigest]
	if !pinned || binding.PackageID != manifest.Package.ID ||
		binding.ArtifactDigest != manifest.Package.ArtifactDigest {
		return fmt.Errorf("%w: package %s", ErrBuiltinNotPinned, manifest.Package.ID)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.packages[manifest.Package.ID]; exists {
		return fmt.Errorf("plugin package %q is already registered", manifest.Package.ID)
	}
	c.packages[manifest.Package.ID] = cloneManifest(manifest)
	return nil
}

// ManifestContentDigest hashes the exact validated wire manifest. Built-in
// bindings should be generated as release metadata and compiled into the host;
// they must never be derived from an untrusted manifest at admission time.
func ManifestContentDigest(manifest Manifest) (Digest, error) {
	if err := manifest.Validate(); err != nil {
		return "", fmt.Errorf("invalid plugin manifest: %w", err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("marshal plugin manifest: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return Digest("sha256:" + hex.EncodeToString(digest[:])), nil
}

func (c *Catalog) Manifest(packageID string) (Manifest, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	manifest, ok := c.packages[packageID]
	if !ok {
		return Manifest{}, false
	}
	return cloneManifest(manifest), true
}

func cloneManifest(manifest Manifest) Manifest {
	clone := manifest
	clone.Package.Platforms = append([]Platform(nil), manifest.Package.Platforms...)
	clone.Package.Dependencies = append([]Dependency(nil), manifest.Package.Dependencies...)
	clone.EntryPoints = make([]EntryPointManifest, len(manifest.EntryPoints))
	for i, entryPoint := range manifest.EntryPoints {
		clone.EntryPoints[i] = entryPoint
		clone.EntryPoints[i].Inputs = append([]PortDeclaration(nil), entryPoint.Inputs...)
		clone.EntryPoints[i].Outputs = append([]PortDeclaration(nil), entryPoint.Outputs...)
		clone.EntryPoints[i].Capabilities = entryPoint.Capabilities.Canonical()
		clone.EntryPoints[i].Runtime.StaticArgs = append([]string(nil), entryPoint.Runtime.StaticArgs...)
		clone.EntryPoints[i].Support.MIMETypes = append([]string(nil), entryPoint.Support.MIMETypes...)
		clone.EntryPoints[i].Support.Formats = append([]string(nil), entryPoint.Support.Formats...)
		clone.EntryPoints[i].Support.Containers = append([]string(nil), entryPoint.Support.Containers...)
		clone.EntryPoints[i].Support.Protocols = append([]string(nil), entryPoint.Support.Protocols...)
		clone.EntryPoints[i].Support.Platforms = append([]string(nil), entryPoint.Support.Platforms...)
		clone.EntryPoints[i].Support.EntityKinds = append([]string(nil), entryPoint.Support.EntityKinds...)
	}
	return clone
}

// ByteSample is a host-bounded view of an input. Detector plugins receive
// samples, never an arbitrary path or an unbounded file descriptor.
type ByteSample struct {
	Offset uint64 `json:"offset"`
	Data   []byte `json:"data"`
}

func (s ByteSample) Range() ByteRange {
	return ByteRange{Offset: s.Offset, Length: uint64(len(s.Data))}
}

func (s ByteSample) validate() error {
	return s.Range().validate(false)
}

type DetectionRequest struct {
	Subject      SubjectRef   `json:"subject"`
	DisplayName  string       `json:"display_name,omitempty"`
	DeclaredMIME string       `json:"declared_mime,omitempty"`
	LogicalSize  *uint64      `json:"logical_size,omitempty"`
	Samples      []ByteSample `json:"samples,omitempty"`
}

func (r DetectionRequest) Validate() error {
	if err := r.Subject.validate(); err != nil {
		return fmt.Errorf("subject: %w", err)
	}
	for i, sample := range r.Samples {
		if err := sample.validate(); err != nil {
			return fmt.Errorf("samples[%d]: %w", i, err)
		}
	}
	ranges := make([]ByteRange, 0, len(r.Samples))
	for _, sample := range r.Samples {
		ranges = append(ranges, sample.Range())
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Offset < ranges[j].Offset })
	for i := 1; i < len(ranges); i++ {
		previousEnd, _ := ranges[i-1].End()
		if ranges[i].Offset < previousEnd {
			return errors.New("samples must not overlap")
		}
	}
	return nil
}

type Detector interface {
	EntryPoint() EntryPointRef
	Detect(context.Context, DetectionRequest) (DetectionResult, error)
}

// BoundedReader is a capability handle issued for a single invocation. Its ID
// is safe to serialize into a future RPC/WASI envelope; the handle itself is
// retained and enforced by the host.
type BoundedReader interface {
	ID() string
	Size() (uint64, bool)
	ReadAt(context.Context, []byte, uint64) (int, error)
}

type ParseRequest struct {
	Subject        SubjectRef
	Candidate      FormatCandidate
	Input          BoundedReader
	ByteBudget     uint64
	MemberBudget   uint64
	RecursionDepth uint32
}

func (r ParseRequest) Validate() error {
	if err := r.Subject.validate(); err != nil {
		return fmt.Errorf("subject: %w", err)
	}
	if err := r.Candidate.validate(); err != nil {
		return fmt.Errorf("candidate: %w", err)
	}
	if r.Input == nil {
		return errors.New("input capability handle is required")
	}
	if r.ByteBudget == 0 {
		return errors.New("byte budget must be greater than zero")
	}
	if r.MemberBudget == 0 {
		return errors.New("member budget must be greater than zero")
	}
	return nil
}

type Parser interface {
	EntryPoint() EntryPointRef
	Parse(context.Context, ParseRequest) (ParserEvidence, error)
}

type Operation string

const (
	OperationProbe    Operation = "PROBE"
	OperationEstimate Operation = "ESTIMATE"
	OperationExecute  Operation = "EXECUTE"
	OperationVerify   Operation = "VERIFY"
)

type ResourceLimits struct {
	Deadline       time.Time `json:"deadline"`
	MaxMemoryBytes uint64    `json:"max_memory_bytes"`
	MaxInputBytes  uint64    `json:"max_input_bytes"`
	MaxOutputBytes uint64    `json:"max_output_bytes"`
	MaxFiles       uint64    `json:"max_files"`
}

// WireValue is the stable typed envelope boundary for future out-of-process
// and WASM adapters. PayloadDigest authenticates the exact encoded bytes.
type WireValue struct {
	PortName      string `json:"port_name"`
	SchemaID      string `json:"schema_id"`
	MediaType     string `json:"media_type"`
	Payload       []byte `json:"payload"`
	PayloadDigest Digest `json:"payload_digest"`
}

type Invocation struct {
	InvocationID string           `json:"invocation_id"`
	EntryPoint   EntryPointRef    `json:"entry_point"`
	Operation    Operation        `json:"operation"`
	Inputs       []WireValue      `json:"inputs"`
	Grants       CapabilityGrants `json:"grants"`
	Limits       ResourceLimits   `json:"limits"`
	ConfigDigest Digest           `json:"config_digest"`
}

type InvocationResult struct {
	Outputs     []WireValue     `json:"outputs"`
	ResourceUse ResourceUse     `json:"resource_use"`
	Issues      []EvidenceIssue `json:"issues,omitempty"`
}

// RuntimeAdapter is the deliberately narrow seam for a future native-RPC or
// WASM/WASI implementation. The MVP ships no adapter whose Kind is external.
type RuntimeAdapter interface {
	Kind() RuntimeKind
	Invoke(context.Context, Invocation) (InvocationResult, error)
}

type SessionState string

const (
	SessionStarting SessionState = "STARTING"
	SessionActive   SessionState = "ACTIVE"
	SessionDraining SessionState = "DRAINING"
	SessionStopped  SessionState = "STOPPED"
	SessionFailed   SessionState = "FAILED"
)

type SessionLease struct {
	LeaseID         string    `json:"lease_id"`
	ExpiresAt       time.Time `json:"expires_at"`
	RenewAfter      time.Time `json:"renew_after"`
	RevocationEpoch uint64    `json:"revocation_epoch"`
}

func (l SessionLease) Validate(now time.Time) error {
	if err := validateOpaqueID(l.LeaseID); err != nil {
		return fmt.Errorf("lease_id: %w", err)
	}
	if l.ExpiresAt.IsZero() || l.RenewAfter.IsZero() || !l.RenewAfter.Before(l.ExpiresAt) {
		return errors.New("renew_after must precede expires_at")
	}
	if !l.ExpiresAt.After(now) {
		return errors.New("session lease is expired")
	}
	return nil
}

type SessionReceipt struct {
	SessionID string          `json:"session_id"`
	Lease     SessionLease    `json:"lease"`
	State     SessionState    `json:"state"`
	Outputs   []WireValue     `json:"outputs,omitempty"`
	Issues    []EvidenceIssue `json:"issues,omitempty"`
}

func (r SessionReceipt) Validate(now time.Time) error {
	if err := validateOpaqueID(r.SessionID); err != nil {
		return fmt.Errorf("session_id: %w", err)
	}
	switch r.State {
	case SessionStarting, SessionActive, SessionDraining, SessionStopped, SessionFailed:
	default:
		return fmt.Errorf("unknown session state %q", r.State)
	}
	if r.State == SessionStarting || r.State == SessionActive || r.State == SessionDraining {
		if err := r.Lease.Validate(now); err != nil {
			return fmt.Errorf("lease: %w", err)
		}
	} else {
		if err := validateOpaqueID(r.Lease.LeaseID); err != nil {
			return fmt.Errorf("lease.lease_id: %w", err)
		}
		if r.Lease.ExpiresAt.IsZero() || r.Lease.RenewAfter.IsZero() ||
			!r.Lease.RenewAfter.Before(r.Lease.ExpiresAt) {
			return errors.New("lease has invalid timestamps")
		}
	}
	return nil
}

// SessionRuntimeAdapter is separate from RuntimeAdapter because a gateway,
// mount, or service must survive the start call and remain lease/revocation
// controlled. Future native-RPC/WASM hosts must implement explicit renew,
// inspect, and stop operations rather than hiding a daemon behind Invoke.
type SessionRuntimeAdapter interface {
	Kind() RuntimeKind
	StartSession(context.Context, Invocation, SessionLease) (SessionReceipt, error)
	RenewSession(context.Context, string, SessionLease) (SessionReceipt, error)
	InspectSession(context.Context, string) (SessionReceipt, error)
	StopSession(context.Context, string, uint64) (SessionReceipt, error)
}

// ReaderAtCapability adapts a bounded io.ReaderAt into a host capability. It
// never reads beyond the declared size even if the underlying object is larger.
type ReaderAtCapability struct {
	HandleID string
	Reader   io.ReaderAt
	Length   uint64
}

func (r ReaderAtCapability) ID() string { return r.HandleID }

func (r ReaderAtCapability) Size() (uint64, bool) { return r.Length, true }

func (r ReaderAtCapability) ReadAt(ctx context.Context, destination []byte, offset uint64) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if r.Reader == nil {
		return 0, errors.New("reader capability has no reader")
	}
	if offset >= r.Length {
		return 0, io.EOF
	}
	if offset > math.MaxInt64 {
		return 0, errors.New("reader offset exceeds io.ReaderAt range")
	}
	remaining := r.Length - offset
	if uint64(len(destination)) > remaining {
		destination = destination[:remaining]
	}
	return r.Reader.ReadAt(destination, int64(offset))
}
