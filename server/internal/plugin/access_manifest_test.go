package plugin

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAccessEntryPointBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		category   Category
		inputs     []PortType
		outputs    []PortType
		capability CapabilityDeclaration
	}{
		{
			name: "repository exact read", category: CategoryRepositoryReadAdapter,
			inputs:     []PortType{PortRepositoryReadRequest},
			outputs:    []PortType{PortExactReadHandle, PortReadEvidence},
			capability: CapabilityDeclaration{Name: CapabilityReadRepository, Required: true, ScopeKinds: []ScopeKind{ScopeRepository}},
		},
		{
			name: "immutable storage ranges", category: CategoryStorageRangeReader,
			inputs:     []PortType{PortStorageRangeRequest},
			outputs:    []PortType{PortImmutableRangeRead, PortReadEvidence},
			capability: CapabilityDeclaration{Name: CapabilityReadStorage, Required: true, ScopeKinds: []ScopeKind{ScopeStorageTarget}},
		},
		{
			name: "representation decoder", category: CategoryRepresentationDecoder,
			inputs:     []PortType{PortRepresentationRef, PortRepresentationDecodeRequest},
			outputs:    []PortType{PortDecodedReadHandle, PortDecodeEvidence},
			capability: CapabilityDeclaration{Name: CapabilityReadRepresentation, Required: true, ScopeKinds: []ScopeKind{ScopeRepresentation}},
		},
		{
			name: "native pack index reader", category: CategoryPackIndexReader,
			inputs:     []PortType{PortRepresentationRef, PortPackIndexRequest},
			outputs:    []PortType{PortPackSliceCandidates, PortPackIndexEvidence},
			capability: CapabilityDeclaration{Name: CapabilityReadPackIndex, Required: true, ScopeKinds: []ScopeKind{ScopePackIndex}},
		},
		{
			name: "namespace materialization", category: CategoryNamespaceIndexAdapter,
			inputs:     []PortType{PortNamespaceMutationBatch},
			outputs:    []PortType{PortNamespaceIndexReceipt},
			capability: CapabilityDeclaration{Name: CapabilityWriteNamespaceIndex, Required: true, ScopeKinds: []ScopeKind{ScopeNamespaceIndex}},
		},
		{
			name: "namespace lookup", category: CategoryNamespaceIndexAdapter,
			inputs:     []PortType{PortNamespaceLookup},
			outputs:    []PortType{PortNamespaceCandidates},
			capability: CapabilityDeclaration{Name: CapabilityReadNamespaceIndex, Required: true, ScopeKinds: []ScopeKind{ScopeNamespaceIndex}},
		},
		{
			name: "namespace gateway", category: CategoryNamespaceGatewayAdapter,
			inputs:     []PortType{PortSnapshotTree, PortFileAccess, PortGatewayRequest},
			outputs:    []PortType{PortGatewaySessionReceipt},
			capability: CapabilityDeclaration{Name: CapabilityServeGateway, Required: true, ScopeKinds: []ScopeKind{ScopeGatewayListener}},
		},
		{
			name: "search mutation", category: CategorySearchIndexer,
			inputs: []PortType{PortIndexMutationBatch}, outputs: []PortType{PortIndexReceipt},
			capability: CapabilityDeclaration{Name: CapabilityWriteSearchIndex, Required: true, ScopeKinds: []ScopeKind{ScopeSearchIndex}},
		},
		{
			name: "search query", category: CategorySearchIndexer,
			inputs: []PortType{PortSearchQuery}, outputs: []PortType{PortSearchCandidates},
			capability: CapabilityDeclaration{Name: CapabilityReadSearchIndex, Required: true, ScopeKinds: []ScopeKind{ScopeSearchIndex}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entryPoint := testAccessEntryPoint(test.category, test.inputs, test.outputs, CapabilitySet{test.capability})
			if err := entryPoint.validate(); err != nil {
				t.Fatalf("validate() error = %v", err)
			}
		})
	}
}

func TestAccessManifestDeclaresScopeKindNotDeploymentID(t *testing.T) {
	entryPoint := testAccessEntryPoint(
		CategoryRepositoryReadAdapter,
		[]PortType{PortRepositoryReadRequest},
		[]PortType{PortExactReadHandle, PortReadEvidence},
		CapabilitySet{{
			Name: CapabilityReadRepository, Required: true,
			ScopeKinds: []ScopeKind{ScopeRepository},
		}},
	)
	encoded, err := json.Marshal(entryPoint)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if strings.Contains(string(encoded), "repository-prod-a") {
		t.Fatal("signed entry-point manifest contains a deployment-specific scope ID")
	}
	if !strings.Contains(string(encoded), `"scope_kinds":["REPOSITORY"]`) {
		t.Fatalf("manifest does not contain repository scope kind: %s", encoded)
	}
}

func TestNamespaceGatewayCannotAcquireStorageAuthority(t *testing.T) {
	entryPoint := testAccessEntryPoint(
		CategoryNamespaceGatewayAdapter,
		[]PortType{PortSnapshotTree, PortFileAccess, PortGatewayRequest},
		[]PortType{PortGatewaySessionReceipt},
		CapabilitySet{
			{Name: CapabilityServeGateway, Required: true, ScopeKinds: []ScopeKind{ScopeGatewayListener}},
			{Name: CapabilityReadStorage, ScopeKinds: []ScopeKind{ScopeStorageTarget}},
		},
	)
	err := entryPoint.validate()
	if err == nil || !strings.Contains(err.Error(), "non-coherent capability") {
		t.Fatalf("validate() error = %v, want storage-authority rejection", err)
	}
}

func TestSearchEntryPointCannotCombineMutationAndQueryAuthority(t *testing.T) {
	entryPoint := testAccessEntryPoint(
		CategorySearchIndexer,
		[]PortType{PortIndexMutationBatch, PortSearchQuery},
		[]PortType{PortIndexReceipt, PortSearchCandidates},
		CapabilitySet{
			{Name: CapabilityWriteSearchIndex, Required: true, ScopeKinds: []ScopeKind{ScopeSearchIndex}},
			{Name: CapabilityReadSearchIndex, Required: true, ScopeKinds: []ScopeKind{ScopeSearchIndex}},
		},
	)
	if err := entryPoint.validate(); err == nil {
		t.Fatal("validate() accepted a combined search mutation/query entry point")
	}
}

func TestNamespaceIndexEntryPointCannotCombineMaterializationAndLookup(t *testing.T) {
	entryPoint := testAccessEntryPoint(
		CategoryNamespaceIndexAdapter,
		[]PortType{PortNamespaceMutationBatch, PortNamespaceLookup},
		[]PortType{PortNamespaceIndexReceipt, PortNamespaceCandidates},
		CapabilitySet{
			{Name: CapabilityWriteNamespaceIndex, Required: true, ScopeKinds: []ScopeKind{ScopeNamespaceIndex}},
			{Name: CapabilityReadNamespaceIndex, Required: true, ScopeKinds: []ScopeKind{ScopeNamespaceIndex}},
		},
	)
	if err := entryPoint.validate(); err == nil {
		t.Fatal("validate() accepted a combined namespace materialization/lookup entry point")
	}
}

func TestPackIndexReaderCannotAcquireStorageAuthority(t *testing.T) {
	entryPoint := testAccessEntryPoint(
		CategoryPackIndexReader,
		[]PortType{PortRepresentationRef, PortPackIndexRequest},
		[]PortType{PortPackSliceCandidates, PortPackIndexEvidence},
		CapabilitySet{
			{Name: CapabilityReadPackIndex, Required: true, ScopeKinds: []ScopeKind{ScopePackIndex}},
			{Name: CapabilityReadStorage, ScopeKinds: []ScopeKind{ScopeStorageTarget}},
		},
	)
	err := entryPoint.validate()
	if err == nil || !strings.Contains(err.Error(), "non-coherent capability") {
		t.Fatalf("validate() error = %v, want storage-authority rejection", err)
	}
}

func TestNamespaceGatewayRequiresSessionLifecycle(t *testing.T) {
	entryPoint := testAccessEntryPoint(
		CategoryNamespaceGatewayAdapter,
		[]PortType{PortSnapshotTree, PortFileAccess, PortGatewayRequest},
		[]PortType{PortGatewaySessionReceipt},
		CapabilitySet{{
			Name: CapabilityServeGateway, Required: true,
			ScopeKinds: []ScopeKind{ScopeGatewayListener},
		}},
	)
	entryPoint.Runtime.Lifecycle = RuntimeOneShot
	if err := entryPoint.validate(); err == nil || !strings.Contains(err.Error(), "SESSION runtime lifecycle") {
		t.Fatalf("validate() error = %v, want session-lifecycle rejection", err)
	}
}

func testAccessEntryPoint(category Category, inputs, outputs []PortType, capabilities CapabilitySet) EntryPointManifest {
	ports := func(types []PortType, prefix string) []PortDeclaration {
		result := make([]PortDeclaration, 0, len(types))
		for i, portType := range types {
			result = append(result, PortDeclaration{
				Name: prefix + string(rune('a'+i)), Type: portType,
				SchemaID: "restoreweave.test/" + string(portType) + "/v1", Required: true,
			})
		}
		return result
	}
	return EntryPointManifest{
		ID: "adapter.test.v1", Name: "Test access adapter", Category: category,
		TransformationClass: TransformationNotApplicable,
		ExecutionClass:      ExecutionByteDeterministic,
		Inputs:              ports(inputs, "input-"),
		Outputs:             ports(outputs, "output-"),
		Capabilities:        capabilities,
		Runtime: RuntimeDescriptor{
			Kind: RuntimeBuiltin, Lifecycle: accessRuntimeLifecycle(category), AdapterID: "restoreweave.builtin.v1",
			Protocol: "restoreweave.internal/v1", Entrypoint: "adapter.test.v1",
		},
		Enablement: EnablementEnabled, Qualification: QualificationExperimental,
		CanExecuteNewWork: true, ConfigurationSchema: "restoreweave.test.config/v1",
		ConformanceSuiteID: "restoreweave.test.conformance/v1",
	}
}

func accessRuntimeLifecycle(category Category) RuntimeLifecycle {
	if category == CategoryNamespaceGatewayAdapter {
		return RuntimeSession
	}
	return RuntimeOneShot
}
