package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
)

// recoveryReaderState retains only ephemeral restore workflow state. The
// repository and authenticated recovery records remain the authority; a clean
// reader neither opens nor creates an operational catalog.
type recoveryReaderState struct {
	mu          sync.Mutex
	workspaceID string
	plans       map[string]*recoveryReaderPlan
	byDigest    map[string]string
}

type recoveryReaderPlan struct {
	id       string
	digest   string
	plan     exact.RestorePlan
	applying bool
	applied  bool
	result   exact.RestoreResult
}

// NewRecoveryDispatcher creates the narrow socket surface used by a clean
// installation. It exposes authenticated discovery, verification, import, and
// digest-bound restore only. Catalog, processor, search, annotation, export,
// signing, and repository-write operations remain unavailable.
func NewRecoveryDispatcher(socketPath string, service *exact.Service) (*Dispatcher, error) {
	if service == nil || service.Repo == nil || service.TrustAnchor == nil ||
		strings.TrimSpace(service.PublicationDomain) == "" || !service.RequireSignedPublication {
		return nil, errors.New("recovery dispatcher requires a signed repository reader and independent trust anchor")
	}
	if service.Store != nil || service.SigningIdentity != nil {
		return nil, errors.New("recovery dispatcher must not receive a catalog or signing identity")
	}
	workspaceID := recoveryStableID("wsp", service.PublicationDomain+"\x00"+service.Repo.Root())
	implemented := map[string]bool{
		command.OpPlanRestore:    true,
		command.OpPlanApply:      true,
		command.OpSnapshotList:   true,
		command.OpSnapshotDiff:   true,
		command.OpSnapshotVerify: true,
		command.OpRecoveryImport: true,
	}
	d := &Dispatcher{
		socketPath:  socketPath,
		now:         time.Now,
		exact:       service,
		implemented: implemented,
		recoveryReader: &recoveryReaderState{
			workspaceID: workspaceID,
			plans:       make(map[string]*recoveryReaderPlan),
			byDigest:    make(map[string]string),
		},
	}
	for _, operation := range command.KnownOperations() {
		if !implemented[operation] {
			d.unimplemented = append(d.unimplemented, operation)
		}
	}
	sort.Strings(d.unimplemented)
	return d, nil
}

func recoveryStableID(prefix, basis string) string {
	digest := sha256.Sum256([]byte(basis))
	return prefix + "_" + hex.EncodeToString(digest[:16])
}

func (d *Dispatcher) recordRecoveryRestorePlan(inspected exact.RestorePlan) (string, string, string, error) {
	if d.recoveryReader == nil {
		return "", "", "", errors.New("recovery reader state is unavailable")
	}
	payload, err := json.Marshal(struct {
		Schema string            `json:"schema"`
		Kind   string            `json:"kind"`
		Plan   exact.RestorePlan `json:"restore"`
	}{Schema: planSchemaV2, Kind: "RESTORE", Plan: inspected})
	if err != nil {
		return "", "", "", err
	}
	digestBytes := sha256.Sum256(payload)
	digest := "sha256:" + hex.EncodeToString(digestBytes[:])
	state := d.recoveryReader
	state.mu.Lock()
	defer state.mu.Unlock()
	if id := state.byDigest[digest]; id != "" {
		return id, digest, state.workspaceID, nil
	}
	id := recoveryStableID("pln", digest)
	state.plans[id] = &recoveryReaderPlan{id: id, digest: digest, plan: inspected}
	state.byDigest[digest] = id
	return id, digest, state.workspaceID, nil
}

func (d *Dispatcher) applyRecoveryRestorePlan(ctx context.Context, env command.Envelope, started time.Time, input planApplyInput) command.Result {
	state := d.recoveryReader
	if state == nil {
		return unimplementedResult(env, started)
	}
	planID := firstNonEmpty(input.PlanID, input.PlanRef)
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("plan_id", planID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if strings.TrimSpace(input.PlanDigest) == "" {
		return invalidInputResult(env, started, errString("plan_digest is required"))
	}
	state.mu.Lock()
	entry := state.plans[planID]
	if input.WorkspaceID != state.workspaceID || entry == nil {
		state.mu.Unlock()
		return notFoundResult(env, started, "recovery restore plan not found")
	}
	if entry.digest != input.PlanDigest {
		state.mu.Unlock()
		return conflictResult(env, started, "plan digest does not match")
	}
	if entry.applied {
		result := entry.result
		state.mu.Unlock()
		return succeeded(env, started, recoveryPlanApplyData(entry, result, true))
	}
	if entry.applying {
		state.mu.Unlock()
		return conflictResult(env, started, "plan apply is already running")
	}
	entry.applying = true
	plan := entry.plan
	state.mu.Unlock()

	result, reconcileErr := d.exact.ReconcileRestorePlan(ctx, plan)
	if errors.Is(reconcileErr, exact.ErrRestoreNotExecuted) {
		result, reconcileErr = d.exact.ApplyRestorePlan(ctx, plan)
	}

	state.mu.Lock()
	entry.applying = false
	if reconcileErr == nil {
		entry.applied = true
		entry.result = result
	}
	state.mu.Unlock()
	if reconcileErr != nil {
		return exactOperationErrorResult(env, started, reconcileErr)
	}
	return succeeded(env, started, recoveryPlanApplyData(entry, result, false))
}

func recoveryPlanApplyData(entry *recoveryReaderPlan, result exact.RestoreResult, replayed bool) command.PlanApplyData {
	return command.PlanApplyData{
		PlanID: entry.id, PlanDigest: entry.digest, AlreadyApplied: replayed,
		SnapshotRef: result.SnapshotRef, State: "SUCCEEDED",
		Destination: result.Destination, Files: result.Files, Bytes: result.Bytes,
	}
}

func (d *Dispatcher) recoveryReaderSummary() string {
	if d == nil || d.recoveryReader == nil {
		return ""
	}
	return fmt.Sprintf("catalog-free recovery reader (%s)", d.recoveryReader.workspaceID)
}
