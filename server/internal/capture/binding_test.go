package capture

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/scanner"
)

func TestLocalTreeDriverEmitsDurableBindingWithoutDescriptor(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	driver := &LocalTreeDriver{Now: func() time.Time {
		return time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	}}
	session, err := driver.Open(root)
	if err != nil {
		t.Fatalf("open capture: %v", err)
	}
	defer session.Close()

	binding := session.Binding()
	if err := binding.Validate(); err != nil {
		t.Fatalf("validate binding: %v", err)
	}
	if binding.CaptureMode != scanner.CaptureModeRootedFD {
		t.Fatalf("capture mode = %q", binding.CaptureMode)
	}
	if binding.DisplayPath == "" || binding.DeviceID == 0 || binding.Inode == 0 {
		t.Fatalf("incomplete identity: %+v", binding)
	}

	raw, err := json.Marshal(binding)
	if err != nil {
		t.Fatalf("marshal binding: %v", err)
	}
	encoded := string(raw)
	if strings.Contains(encoded, `"fd"`) || strings.Contains(encoded, `"descriptor"`) {
		t.Fatalf("binding serialized a runtime descriptor: %s", encoded)
	}

	var decoded BindingRecord
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal binding: %v", err)
	}
	if decoded.IdentityDigest() != binding.IdentityDigest() {
		t.Fatalf("identity digest changed across serialization")
	}

	later, err := driver.Open(root)
	if err != nil {
		t.Fatalf("reopen capture: %v", err)
	}
	defer later.Close()
	if later.Binding().IdentityDigest() != binding.IdentityDigest() {
		t.Fatalf("identity digest is not stable across sessions")
	}
}

func TestBindingRejectsPathStringMode(t *testing.T) {
	record := BindingRecord{
		Schema:           SchemaBindingV1,
		Profile:          ProfileLocalTree,
		CaptureMode:      scanner.CaptureModePathString,
		DisplayPath:      "/tmp",
		DeviceID:         1,
		Inode:            2,
		ConsistencyClaim: ClaimLiveValidated,
	}
	if err := record.Validate(); !errors.Is(err, ErrNotRooted) {
		t.Fatalf("validate error = %v, want ErrNotRooted", err)
	}
	if err := MustRooted(scanner.CaptureModePathString); !errors.Is(err, ErrNotRooted) {
		t.Fatalf("MustRooted error = %v, want ErrNotRooted", err)
	}
}

func TestLocalTreeDriverRejectsSymlinkRoot(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	link := filepath.Join(dir, "link")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := (&LocalTreeDriver{}).Open(link)
	if err == nil {
		t.Fatal("symlink root unexpectedly succeeded")
	}
}
