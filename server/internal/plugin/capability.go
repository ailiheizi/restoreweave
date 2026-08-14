package plugin

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// CapabilityName names one host-mediated authority. There is intentionally no
// ambient-network capability: networked entry points must request a broker
// capability with an explicit scope.
type CapabilityName string

const (
	CapabilityReadInputMetadata   CapabilityName = "READ_INPUT_METADATA"
	CapabilityReadInputSamples    CapabilityName = "READ_INPUT_SAMPLES"
	CapabilityReadInputContent    CapabilityName = "READ_INPUT_CONTENT"
	CapabilityEmitDetection       CapabilityName = "EMIT_DETECTION_EVIDENCE"
	CapabilityEmitParse           CapabilityName = "EMIT_PARSE_EVIDENCE"
	CapabilityWriteStaging        CapabilityName = "WRITE_STAGING"
	CapabilityUseTemporarySpace   CapabilityName = "USE_TEMPORARY_SPACE"
	CapabilityUseAccelerator      CapabilityName = "USE_ACCELERATOR"
	CapabilityUseRandomness       CapabilityName = "USE_RANDOMNESS"
	CapabilityExecutePinned       CapabilityName = "EXECUTE_PINNED_BINARY"
	CapabilityUseSecret           CapabilityName = "USE_SECRET_HANDLE"
	CapabilityReadStorage         CapabilityName = "READ_STORAGE"
	CapabilityWriteStorage        CapabilityName = "WRITE_STORAGE"
	CapabilityManageBackend       CapabilityName = "MANAGE_BACKEND"
	CapabilityReadRepository      CapabilityName = "READ_REPOSITORY_OBJECT"
	CapabilityReadRepresentation  CapabilityName = "READ_REPRESENTATION"
	CapabilityReadPackIndex       CapabilityName = "READ_PACK_INDEX"
	CapabilityReadNamespaceIndex  CapabilityName = "READ_NAMESPACE_INDEX"
	CapabilityWriteNamespaceIndex CapabilityName = "WRITE_NAMESPACE_INDEX"
	CapabilityServeGateway        CapabilityName = "SERVE_GATEWAY_SESSION"
	CapabilityReadSearchIndex     CapabilityName = "READ_SEARCH_INDEX"
	CapabilityWriteSearchIndex    CapabilityName = "WRITE_SEARCH_INDEX"
	CapabilityCreateCapture       CapabilityName = "CREATE_CAPTURE_SET"
	CapabilityReleaseCapture      CapabilityName = "RELEASE_CAPTURE_SET"

	CapabilityExternalDiscovery   CapabilityName = "BROKER_EXTERNAL_DISCOVERY"
	CapabilityExternalAcquisition CapabilityName = "BROKER_EXTERNAL_ACQUISITION"
	CapabilityPayloadUpload       CapabilityName = "BROKER_PAYLOAD_UPLOAD"
	CapabilityPublicAnnouncement  CapabilityName = "BROKER_PUBLIC_ANNOUNCEMENT"
)

type ScopeKind string

const (
	ScopeStagingArea       ScopeKind = "STAGING_AREA"
	ScopeTemporarySpace    ScopeKind = "TEMPORARY_SPACE"
	ScopeAccelerator       ScopeKind = "ACCELERATOR"
	ScopeExecutable        ScopeKind = "EXECUTABLE"
	ScopeSecret            ScopeKind = "SECRET"
	ScopeStorageTarget     ScopeKind = "STORAGE_TARGET"
	ScopeBackend           ScopeKind = "BACKEND"
	ScopeRepository        ScopeKind = "REPOSITORY"
	ScopeRepresentation    ScopeKind = "REPRESENTATION"
	ScopePackIndex         ScopeKind = "PACK_INDEX"
	ScopeNamespaceIndex    ScopeKind = "NAMESPACE_INDEX"
	ScopeGatewayListener   ScopeKind = "GATEWAY_LISTENER"
	ScopeSearchIndex       ScopeKind = "SEARCH_INDEX"
	ScopeCaptureSource     ScopeKind = "CAPTURE_SOURCE"
	ScopeDiscoveryProvider ScopeKind = "DISCOVERY_PROVIDER"
	ScopeAcquisitionRoute  ScopeKind = "ACQUISITION_ROUTE"
	ScopeNetworkProfile    ScopeKind = "NETWORK_PROFILE"
)

type capabilityDefinition struct {
	scopeKinds []ScopeKind
}

var knownCapabilities = map[CapabilityName]capabilityDefinition{
	CapabilityReadInputMetadata:   {},
	CapabilityReadInputSamples:    {},
	CapabilityReadInputContent:    {},
	CapabilityEmitDetection:       {},
	CapabilityEmitParse:           {},
	CapabilityWriteStaging:        {scopeKinds: []ScopeKind{ScopeStagingArea}},
	CapabilityUseTemporarySpace:   {scopeKinds: []ScopeKind{ScopeTemporarySpace}},
	CapabilityUseAccelerator:      {scopeKinds: []ScopeKind{ScopeAccelerator}},
	CapabilityUseRandomness:       {},
	CapabilityExecutePinned:       {scopeKinds: []ScopeKind{ScopeExecutable}},
	CapabilityUseSecret:           {scopeKinds: []ScopeKind{ScopeSecret}},
	CapabilityReadStorage:         {scopeKinds: []ScopeKind{ScopeStorageTarget}},
	CapabilityWriteStorage:        {scopeKinds: []ScopeKind{ScopeStorageTarget}},
	CapabilityManageBackend:       {scopeKinds: []ScopeKind{ScopeBackend}},
	CapabilityReadRepository:      {scopeKinds: []ScopeKind{ScopeRepository}},
	CapabilityReadRepresentation:  {scopeKinds: []ScopeKind{ScopeRepresentation}},
	CapabilityReadPackIndex:       {scopeKinds: []ScopeKind{ScopePackIndex}},
	CapabilityReadNamespaceIndex:  {scopeKinds: []ScopeKind{ScopeNamespaceIndex}},
	CapabilityWriteNamespaceIndex: {scopeKinds: []ScopeKind{ScopeNamespaceIndex}},
	CapabilityServeGateway:        {scopeKinds: []ScopeKind{ScopeGatewayListener}},
	CapabilityReadSearchIndex:     {scopeKinds: []ScopeKind{ScopeSearchIndex}},
	CapabilityWriteSearchIndex:    {scopeKinds: []ScopeKind{ScopeSearchIndex}},
	CapabilityCreateCapture:       {scopeKinds: []ScopeKind{ScopeCaptureSource}},
	CapabilityReleaseCapture:      {scopeKinds: []ScopeKind{ScopeCaptureSource}},
	CapabilityExternalDiscovery:   {scopeKinds: []ScopeKind{ScopeDiscoveryProvider}},
	CapabilityExternalAcquisition: {scopeKinds: []ScopeKind{ScopeAcquisitionRoute}},
	CapabilityPayloadUpload:       {scopeKinds: []ScopeKind{ScopeNetworkProfile}},
	CapabilityPublicAnnouncement:  {scopeKinds: []ScopeKind{ScopeNetworkProfile}},
}

// CapabilityDeclaration is signed package metadata. It declares only kinds
// of authority. Concrete deployment IDs are issued later in CapabilityGrant
// records, so a package manifest is portable and cannot name its own targets.
type CapabilityDeclaration struct {
	Name       CapabilityName `json:"name"`
	Required   bool           `json:"required,omitempty"`
	ScopeKinds []ScopeKind    `json:"scope_kinds,omitempty"`
}

// CapabilitySet is the complete authority declaration for one entry point.
// Capabilities are never inherited from another entry point in the package.
type CapabilitySet []CapabilityDeclaration

func (s CapabilitySet) Validate() error {
	seen := make(map[CapabilityName]struct{}, len(s))
	for i, declaration := range s {
		definition, ok := knownCapabilities[declaration.Name]
		if !ok {
			return fmt.Errorf("capabilities[%d]: unknown capability %q", i, declaration.Name)
		}
		if _, duplicate := seen[declaration.Name]; duplicate {
			return fmt.Errorf("capabilities[%d]: duplicate capability %q", i, declaration.Name)
		}
		seen[declaration.Name] = struct{}{}

		if len(definition.scopeKinds) > 0 && len(declaration.ScopeKinds) == 0 {
			return fmt.Errorf("capabilities[%d]: %s requires at least one scope kind", i, declaration.Name)
		}
		if len(definition.scopeKinds) == 0 && len(declaration.ScopeKinds) != 0 {
			return fmt.Errorf("capabilities[%d]: %s does not accept scope kinds", i, declaration.Name)
		}
		if err := validateScopeKinds(declaration.ScopeKinds, definition.scopeKinds); err != nil {
			return fmt.Errorf("capabilities[%d]: %w", i, err)
		}
	}
	return nil
}

func validateScopeKinds(declared, supported []ScopeKind) error {
	allowed := make(map[ScopeKind]struct{}, len(supported))
	for _, kind := range supported {
		allowed[kind] = struct{}{}
	}
	seen := make(map[ScopeKind]struct{}, len(declared))
	for i, kind := range declared {
		if _, ok := allowed[kind]; !ok {
			return fmt.Errorf("scope_kinds[%d] %q is not valid for this capability", i, kind)
		}
		if _, duplicate := seen[kind]; duplicate {
			return fmt.Errorf("scope_kinds[%d] duplicates %q", i, kind)
		}
		seen[kind] = struct{}{}
	}
	return nil
}

func (s CapabilitySet) Declares(name CapabilityName) bool {
	for _, declaration := range s {
		if declaration.Name == name {
			return true
		}
	}
	return false
}

func (s CapabilitySet) Requires(name CapabilityName) bool {
	for _, declaration := range s {
		if declaration.Name == name {
			return declaration.Required
		}
	}
	return false
}

// Canonical returns an independently allocated, deterministically ordered
// capability set suitable for hashing or serialization.
func (s CapabilitySet) Canonical() CapabilitySet {
	canonical := make(CapabilitySet, len(s))
	for i, declaration := range s {
		canonical[i] = declaration
		canonical[i].ScopeKinds = append([]ScopeKind(nil), declaration.ScopeKinds...)
		sort.Slice(canonical[i].ScopeKinds, func(left, right int) bool {
			return canonical[i].ScopeKinds[left] < canonical[i].ScopeKinds[right]
		})
	}
	sort.Slice(canonical, func(i, j int) bool {
		return canonical[i].Name < canonical[j].Name
	})
	return canonical
}

type CapabilityScope struct {
	Kind ScopeKind `json:"kind"`
	ID   string    `json:"id"`
}

func (s CapabilityScope) validate() error {
	if err := validateOpaqueID(s.ID); err != nil {
		return fmt.Errorf("id: %w", err)
	}
	if strings.ContainsAny(s.ID, "*?") {
		return errors.New("id must not contain wildcards")
	}
	return nil
}

// CapabilityGrant is the job-scoped authority actually issued by the host.
// A grant binds a declared scope kind to concrete opaque deployment IDs.
type CapabilityGrant struct {
	Name   CapabilityName    `json:"name"`
	Scopes []CapabilityScope `json:"scopes,omitempty"`
}

type CapabilityGrants []CapabilityGrant

var ErrCapabilityEscalation = errors.New("capability grant exceeds declaration")

// ValidateGrants checks both non-escalation and satisfaction of capabilities
// marked Required. The host should run this check immediately before invoking
// an entry point, after policy has produced the job-specific grants.
func ValidateGrants(declared CapabilitySet, granted CapabilityGrants) error {
	if err := declared.Validate(); err != nil {
		return fmt.Errorf("invalid declarations: %w", err)
	}

	declarations := make(map[CapabilityName]CapabilityDeclaration, len(declared))
	for _, declaration := range declared {
		declarations[declaration.Name] = declaration
	}

	seen := make(map[CapabilityName]struct{}, len(granted))
	for i, grant := range granted {
		if _, duplicate := seen[grant.Name]; duplicate {
			return fmt.Errorf("grants[%d]: duplicate capability %q", i, grant.Name)
		}
		seen[grant.Name] = struct{}{}

		declaration, ok := declarations[grant.Name]
		if !ok {
			return fmt.Errorf("%w: %s is not declared", ErrCapabilityEscalation, grant.Name)
		}
		definition := knownCapabilities[grant.Name]
		if len(definition.scopeKinds) == 0 && len(grant.Scopes) != 0 {
			return fmt.Errorf("%w: %s does not accept scopes", ErrCapabilityEscalation, grant.Name)
		}
		if len(definition.scopeKinds) > 0 {
			if len(grant.Scopes) == 0 {
				return fmt.Errorf("grants[%d]: %s requires at least one scope", i, grant.Name)
			}
			allowed := make(map[ScopeKind]struct{}, len(declaration.ScopeKinds))
			for _, kind := range declaration.ScopeKinds {
				allowed[kind] = struct{}{}
			}
			seenScopes := make(map[CapabilityScope]struct{}, len(grant.Scopes))
			for scopeIndex, scope := range grant.Scopes {
				if err := scope.validate(); err != nil {
					return fmt.Errorf("grants[%d].scopes[%d]: %w", i, scopeIndex, err)
				}
				if _, ok := allowed[scope.Kind]; !ok {
					return fmt.Errorf("%w: %s scope kind %q is not declared", ErrCapabilityEscalation, grant.Name, scope.Kind)
				}
				if _, duplicate := seenScopes[scope]; duplicate {
					return fmt.Errorf("grants[%d].scopes[%d] is duplicated", i, scopeIndex)
				}
				seenScopes[scope] = struct{}{}
			}
		}
	}

	for _, declaration := range declared {
		if !declaration.Required {
			continue
		}
		if _, ok := seen[declaration.Name]; !ok {
			return fmt.Errorf("required capability %s was not granted", declaration.Name)
		}
	}
	return nil
}

func (g CapabilityGrants) Allows(name CapabilityName, kind ScopeKind, id string) bool {
	for _, grant := range g {
		if grant.Name != name {
			continue
		}
		if len(grant.Scopes) == 0 {
			return kind == "" && id == ""
		}
		for _, grantedScope := range grant.Scopes {
			if grantedScope.Kind == kind && grantedScope.ID == id {
				return true
			}
		}
	}
	return false
}
