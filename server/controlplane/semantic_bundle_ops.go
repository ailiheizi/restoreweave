package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/search"
)

// handleSemanticBundleInstall admits the one pinned local semantic bundle.
// Installation is explicit and independent from search generation building:
// this operation never hot-loads an embedding worker or rebuilds zvec.
func (d *Dispatcher) handleSemanticBundleInstall(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if err := requireEmptyObject(env.Input); err != nil {
		return invalidInputResult(env, started, err)
	}
	data := command.SemanticBundleInstallData{
		ProfileID:       search.SemanticBundleBGEProfileID,
		RestartRequired: true,
	}
	if d == nil || d.semanticBundleInstaller == nil || strings.TrimSpace(d.semanticBundleModelsRoot) == "" {
		return failed(env, started, newReason(ReasonCodeUnavailable, "fixed semantic bundle installer is unavailable"))
	}

	// The installer itself stages atomically, while this mutex prevents two
	// requests in one daemon from racing over the same persisted model root.
	d.semanticBundleInstallMu.Lock()
	defer d.semanticBundleInstallMu.Unlock()
	receipt, err := d.semanticBundleInstaller(ctx, d.semanticBundleModelsRoot)
	if err != nil {
		return failed(env, started, newReason(ReasonCodeUnavailable, "semantic bundle installation unavailable: "+err.Error()))
	}
	if err := validateSemanticBundleInstallReceipt(receipt, d.semanticBundleModelsRoot); err != nil {
		return failed(env, started, newReason(ReasonCodeUnavailable, "semantic bundle installation returned invalid admission: "+err.Error()))
	}

	profile := receipt.Admission.Descriptor.ProfileID
	digest := receipt.Admission.ProfileDigest
	data.ProfileID = profile
	data.ProfileDigest = digest
	data.Destination = receipt.Destination
	data.Changed = receipt.Changed

	// Only the independent model-bundle capability changes here. In particular,
	// no provider, search index, or semantic dimension is activated by install.
	d.semanticBundleMu.Lock()
	d.semanticBundle = command.Capability{
		Kind:    modelBundleCapabilityKind,
		ID:      profile,
		State:   command.CapabilityAvailable,
		Version: digest,
		Source:  receipt.Admission.Descriptor.ModelID,
		Notes:   "RESTART_REQUIRED: bundle is installed and locally verified; restart required before semantic worker/index use",
	}
	d.semanticBundleMu.Unlock()
	return succeeded(env, started, data)
}

func validateSemanticBundleInstallReceipt(receipt SemanticBundleInstallReceipt, modelsRoot string) error {
	if err := receipt.Admission.Validate(); err != nil {
		return fmt.Errorf("admission: %w", err)
	}
	if receipt.Admission.Descriptor.ProfileID != search.SemanticBundleBGEProfileID {
		return fmt.Errorf("profile must be %q", search.SemanticBundleBGEProfileID)
	}
	expectedDestination, err := search.DefaultSemanticBundleDestination(modelsRoot)
	if err != nil {
		return err
	}
	if receipt.Destination != expectedDestination && !sameResolvedPath(expectedDestination, receipt.Destination) {
		return fmt.Errorf("destination must be the pinned profile path %q", expectedDestination)
	}
	if err := search.ValidateSemanticBundleInstallDestination(modelsRoot, receipt.Destination); err != nil {
		return err
	}
	loaded, err := search.LoadSemanticBundle(receipt.Destination)
	if err != nil {
		return fmt.Errorf("destination admission: %w", err)
	}
	if !reflect.DeepEqual(loaded, receipt.Admission) {
		return errors.New("receipt admission does not match destination admission")
	}
	return nil
}

func sameResolvedPath(left, right string) bool {
	leftResolved, leftErr := filepath.EvalSymlinks(left)
	rightResolved, rightErr := filepath.EvalSymlinks(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftResolved) == filepath.Clean(rightResolved)
}

// requireEmptyObject enforces the typed operation's intentionally closed input
// shape. json.Unmarshal into an empty struct would silently accept fields, and
// null would also decode successfully, so both cases are rejected explicitly.
func requireEmptyObject(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("input must be exactly an empty JSON object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return fmt.Errorf("input must be exactly an empty JSON object: %w", err)
	}
	if fields == nil || len(fields) != 0 {
		return errors.New("input must be exactly an empty JSON object")
	}
	return nil
}
