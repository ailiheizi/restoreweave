package plugin

import (
	"errors"
	"reflect"
	"testing"
)

func TestCapabilityGrantMayNarrowButNotEscalate(t *testing.T) {
	declarations := CapabilitySet{
		{Name: CapabilityReadInputContent, Required: true},
		{Name: CapabilityWriteStaging, ScopeKinds: []ScopeKind{ScopeStagingArea}},
	}
	if err := ValidateGrants(declarations, CapabilityGrants{
		{Name: CapabilityReadInputContent},
		{Name: CapabilityWriteStaging, Scopes: []CapabilityScope{{Kind: ScopeStagingArea, ID: "job-output"}}},
	}); err != nil {
		t.Fatalf("valid narrowed grant rejected: %v", err)
	}

	err := ValidateGrants(declarations, CapabilityGrants{
		{Name: CapabilityReadInputContent},
		{Name: CapabilityWriteStaging, Scopes: []CapabilityScope{{Kind: ScopeStorageTarget, ID: "target-1"}}},
	})
	if !errors.Is(err, ErrCapabilityEscalation) {
		t.Fatalf("escalating grant error = %v, want ErrCapabilityEscalation", err)
	}
}

func TestCapabilityGrantRequiresMandatoryAuthority(t *testing.T) {
	err := ValidateGrants(CapabilitySet{
		{Name: CapabilityReadInputSamples, Required: true},
		{Name: CapabilityEmitDetection, Required: true},
	}, CapabilityGrants{{Name: CapabilityReadInputSamples}})
	if err == nil {
		t.Fatal("missing required grant was accepted")
	}
}

func TestCapabilityGrantBindsConcreteIDAtRuntime(t *testing.T) {
	declarations := CapabilitySet{{
		Name: CapabilityReadRepository, Required: true,
		ScopeKinds: []ScopeKind{ScopeRepository},
	}}
	grants := CapabilityGrants{{
		Name:   CapabilityReadRepository,
		Scopes: []CapabilityScope{{Kind: ScopeRepository, ID: "repository-prod-a"}},
	}}
	if err := ValidateGrants(declarations, grants); err != nil {
		t.Fatalf("ValidateGrants() error = %v", err)
	}
	if !grants.Allows(CapabilityReadRepository, ScopeRepository, "repository-prod-a") {
		t.Fatal("runtime grant did not authorize its concrete scope ID")
	}
	if grants.Allows(CapabilityReadRepository, ScopeRepository, "repository-prod-b") {
		t.Fatal("runtime grant authorized a different deployment scope ID")
	}
}

func TestCapabilitySetRejectsWildcardScopes(t *testing.T) {
	declarations := CapabilitySet{{
		Name: CapabilityExternalAcquisition, ScopeKinds: []ScopeKind{ScopeAcquisitionRoute},
	}}
	err := ValidateGrants(declarations, CapabilityGrants{{
		Name:   CapabilityExternalAcquisition,
		Scopes: []CapabilityScope{{Kind: ScopeAcquisitionRoute, ID: "route-*"}},
	}})
	if err == nil {
		t.Fatal("wildcard authority scope was accepted")
	}
}

func TestCapabilityCanonicalizationDoesNotMutateCaller(t *testing.T) {
	original := CapabilitySet{
		{Name: CapabilityWriteStaging, ScopeKinds: []ScopeKind{ScopeStagingArea}},
		{Name: CapabilityReadInputContent},
	}
	canonical := original.Canonical()
	want := CapabilitySet{
		{Name: CapabilityReadInputContent},
		{Name: CapabilityWriteStaging, ScopeKinds: []ScopeKind{ScopeStagingArea}},
	}
	if !reflect.DeepEqual(canonical, want) {
		t.Fatalf("Canonical() = %#v, want %#v", canonical, want)
	}
	canonical[1].ScopeKinds[0] = ScopeBackend
	if original[0].ScopeKinds[0] != ScopeStagingArea {
		t.Fatal("Canonical() mutated caller-owned scopes")
	}
}
