// Package capture implements the local-tree CaptureDriver profile.
//
// A capture session binds one retained directory descriptor and emits a
// durable CaptureRootBindingRecord that never contains a runtime file
// descriptor number. Only ROOTED_FD bindings are eligible to become
// authoritative ingest input.
package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/scanner"
)

const (
	SchemaBindingV1         = "org.restoreweave.capture.binding.v1"
	ProfileLocalTree        = "local-tree"
	ClaimLiveValidated      = "LIVE_VALIDATED_OBSERVATION"
	identityDigestAlgorithm = "sha256"
)

var (
	ErrInvalidBinding = errors.New("invalid capture-root binding")
	ErrNotRooted      = errors.New("capture is not descriptor-rooted")
)

// BindingRecord is the durable capture-root identity. It is safe to serialize
// into the operational catalog and into a portable snapshot: it records the
// bound directory's device/inode and display path, never a descriptor number.
type BindingRecord struct {
	Schema           string              `json:"schema"`
	Profile          string              `json:"profile"`
	CaptureMode      scanner.CaptureMode `json:"capture_mode"`
	DisplayPath      string              `json:"display_path"`
	DeviceID         uint64              `json:"device_id"`
	Inode            uint64              `json:"inode"`
	ConsistencyClaim string              `json:"consistency_claim"`
	BoundAt          time.Time           `json:"bound_at"`
}

// Validate rejects records that could not authorize an exact ingest.
func (record BindingRecord) Validate() error {
	if record.Schema != SchemaBindingV1 {
		return fmt.Errorf("%w: schema %q", ErrInvalidBinding, record.Schema)
	}
	if record.Profile != ProfileLocalTree {
		return fmt.Errorf("%w: profile %q", ErrInvalidBinding, record.Profile)
	}
	if record.CaptureMode != scanner.CaptureModeRootedFD {
		return fmt.Errorf("%w: %w", ErrInvalidBinding, ErrNotRooted)
	}
	if strings.TrimSpace(record.DisplayPath) == "" {
		return fmt.Errorf("%w: display path is required", ErrInvalidBinding)
	}
	if record.DeviceID == 0 && record.Inode == 0 {
		return fmt.Errorf("%w: device/inode identity is required", ErrInvalidBinding)
	}
	if record.ConsistencyClaim != ClaimLiveValidated {
		return fmt.Errorf("%w: consistency claim %q", ErrInvalidBinding, record.ConsistencyClaim)
	}
	return nil
}

// IdentityDigest is a stable digest of the bound directory identity. BoundAt
// is excluded so two sessions against the same inode produce the same digest.
func (record BindingRecord) IdentityDigest() string {
	payload, err := json.Marshal(bindingIdentity{
		Schema:           record.Schema,
		Profile:          record.Profile,
		CaptureMode:      record.CaptureMode,
		DisplayPath:      record.DisplayPath,
		DeviceID:         record.DeviceID,
		Inode:            record.Inode,
		ConsistencyClaim: record.ConsistencyClaim,
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return identityDigestAlgorithm + ":" + hex.EncodeToString(sum[:])
}

type bindingIdentity struct {
	Schema           string              `json:"schema"`
	Profile          string              `json:"profile"`
	CaptureMode      scanner.CaptureMode `json:"capture_mode"`
	DisplayPath      string              `json:"display_path"`
	DeviceID         uint64              `json:"device_id"`
	Inode            uint64              `json:"inode"`
	ConsistencyClaim string              `json:"consistency_claim"`
}

// SourceFingerprint is the durable source identity derived from the binding.
func (record BindingRecord) SourceFingerprint() string {
	return record.IdentityDigest()
}
