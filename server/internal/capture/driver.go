package capture

import (
	"fmt"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/scanner"
)

// Session is one live capture against a bound local directory. Close releases
// the retained descriptor. The Binding may be retained after Close.
type Session struct {
	root    *scanner.CaptureRoot
	binding BindingRecord
}

// Root returns the live descriptor-rooted binding used for traversal.
func (session *Session) Root() *scanner.CaptureRoot { return session.root }

// Binding returns the durable record for this session.
func (session *Session) Binding() BindingRecord { return session.binding }

// Close releases the retained descriptor. It is idempotent.
func (session *Session) Close() error {
	if session == nil || session.root == nil {
		return nil
	}
	return session.root.Close()
}

// LocalTreeDriver is the generic local or mounted-tree CaptureDriver profile.
// It does not claim snapshot atomicity: the consistency claim is live
// validated observation, enforced by the scanner's before/after metadata
// checks and by CaptureRoot replacement detection.
type LocalTreeDriver struct {
	Now func() time.Time
}

// Open binds path as a capture root and emits a durable BindingRecord.
func (driver *LocalTreeDriver) Open(path string) (*Session, error) {
	root, err := scanner.OpenCaptureRoot(path)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if driver != nil && driver.Now != nil {
		now = driver.Now().UTC()
	}
	device, inode := root.Identity()
	binding := BindingRecord{
		Schema:           SchemaBindingV1,
		Profile:          ProfileLocalTree,
		CaptureMode:      scanner.CaptureModeRootedFD,
		DisplayPath:      root.Path(),
		DeviceID:         device,
		Inode:            inode,
		ConsistencyClaim: ClaimLiveValidated,
		BoundAt:          now,
	}
	if err := binding.Validate(); err != nil {
		_ = root.Close()
		return nil, err
	}
	return &Session{root: root, binding: binding}, nil
}

// MustRooted reports whether a scan result may be adopted as authoritative.
func MustRooted(mode scanner.CaptureMode) error {
	if mode != scanner.CaptureModeRootedFD {
		return fmt.Errorf("%w: capture mode %q", ErrNotRooted, mode)
	}
	return nil
}
