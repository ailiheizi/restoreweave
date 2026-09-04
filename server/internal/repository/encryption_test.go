package repository

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testKeyProvider(keys map[string][]byte) KeyProvider {
	return KeyProviderFunc(func(_ context.Context, ref string) ([]byte, error) {
		key, ok := keys[ref]
		if !ok {
			return nil, errors.New("missing test key")
		}
		return append([]byte(nil), key...), nil
	})
}

func TestEncryptedZstdExactIdentityAndCleanReaderDependency(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{0x31}, 32)
	provider := testKeyProvider(map[string][]byte{"key://one": key})
	root := filepath.Join(t.TempDir(), "encrypted")
	repo, err := OpenEncryptedZstdDir(root, "key://one", provider)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("encrypted exact payload\n"), 256)
	receipt, err := repo.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Bytes != int64(len(payload)) || receipt.ContentID == "" || receipt.StoredBytes <= 0 {
		t.Fatalf("encrypted receipt = %+v", receipt)
	}
	testEstimate, err := repo.EstimatePlacement(ctx, receipt.ContentID, bytes.NewReader(payload))
	if err != nil || !testEstimate.Existing || testEstimate.ProbableNewPhysicalBytes != 0 {
		t.Fatalf("encrypted existing estimate = %+v err=%v", testEstimate, err)
	}
	if err := repo.Verify(ctx, receipt.ContentID); err != nil {
		t.Fatal(err)
	}
	body, err := repo.Open(ctx, receipt.ContentID)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(body)
	closeErr := body.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(got, payload) {
		t.Fatalf("encrypted readback len=%d read=%v close=%v", len(got), readErr, closeErr)
	}
	metadata, err := os.ReadFile(filepath.Join(root, repositoryEncryptionFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(metadata, key) || bytes.Contains(metadata, payload) || !bytes.Contains(metadata, []byte("key://one")) {
		t.Fatalf("encryption metadata leaked secret or omitted key ref: %q", metadata)
	}
	blob, err := os.ReadFile(blobPath(root, receipt.ContentID))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, payload) {
		t.Fatal("encrypted blob contains plaintext payload")
	}

	if _, err := OpenProfileReadOnly(RepositoryProfileLocalZstdEncryptedV1, root); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("clean reader without provider error = %v, want ErrKeyUnavailable", err)
	}
	readonly, err := OpenProfileReadOnlyWithKeyProvider(RepositoryProfileLocalZstdEncryptedV1, root, provider)
	if err != nil {
		t.Fatal(err)
	}
	if err := readonly.Verify(ctx, receipt.ContentID); err != nil {
		t.Fatal(err)
	}
	capability := readonly.(CapabilityReporter).DescribeCapabilities()
	if capability.RepositoryFormat != RepositoryProfileLocalZstdEncryptedV1 || capability.Encryption != EncryptionAES256GCM {
		t.Fatalf("encrypted capabilities = %+v", capability)
	}
	health, err := readonly.(CapabilityReporter).DescribeHealthAndCapacity(ctx)
	if err != nil || !health.Available || !health.ReaderReady || health.KeyState != KeyStateAvailable {
		t.Fatalf("encrypted health = %+v err=%v", health, err)
	}
}

func TestEncryptedZstdMissingKeyDoesNotInitializeRepository(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing-key")
	if _, err := OpenEncryptedZstdDir(root, "key://missing", nil); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("missing key open error = %v, want ErrKeyUnavailable", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("missing-key open created repository state: %v", err)
	}
}

func TestEncryptedZstdMissingWrongKeyAndCorruptionFailClosed(t *testing.T) {
	ctx := context.Background()
	correct := bytes.Repeat([]byte{0x41}, 32)
	wrong := bytes.Repeat([]byte{0x42}, 32)
	root := filepath.Join(t.TempDir(), "encrypted")
	provider := testKeyProvider(map[string][]byte{"key://correct": correct})
	repo, err := OpenEncryptedZstdDir(root, "key://correct", provider)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("wrong key must not expose plaintext")
	receipt, err := repo.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	wrongRepo, err := OpenEncryptedZstdDir(root, "key://correct", testKeyProvider(map[string][]byte{"key://correct": wrong}))
	if err != nil {
		t.Fatal(err)
	}
	if err := wrongRepo.Verify(ctx, receipt.ContentID); !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("wrong key verify error = %v, want ErrKeyMismatch", err)
	}
	wrongHealth, err := wrongRepo.DescribeHealthAndCapacity(ctx)
	if err != nil || wrongHealth.ReaderReady || wrongHealth.KeyState != KeyStateRejected {
		t.Fatalf("wrong key health = %+v err=%v", wrongHealth, err)
	}
	if body, err := wrongRepo.Open(ctx, receipt.ContentID); err == nil {
		decrypted, readErr := io.ReadAll(body)
		_ = body.Close()
		if readErr == nil || bytes.Contains(decrypted, payload) {
			t.Fatalf("wrong key returned plaintext: %q read=%v", decrypted, readErr)
		}
	}
	if _, err := OpenEncryptedZstdDir(root, "key://correct", testKeyProvider(nil)); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("missing key open error = %v, want ErrKeyUnavailable", err)
	}

	blobPathName := blobPath(root, receipt.ContentID)
	blob, err := os.ReadFile(blobPathName)
	if err != nil {
		t.Fatal(err)
	}
	blob[len(blob)/2] ^= 0x7f
	if err := os.WriteFile(blobPathName, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := repo.Verify(ctx, receipt.ContentID); err == nil {
		t.Fatal("corrupted encrypted object verified")
	}
}

func TestEncryptedZstdRelocationAndKeyRotationCopyForward(t *testing.T) {
	ctx := context.Background()
	keyOne := bytes.Repeat([]byte{0x11}, 32)
	keyTwo := bytes.Repeat([]byte{0x22}, 32)
	providerOne := testKeyProvider(map[string][]byte{"key://one": keyOne})
	providerTwo := testKeyProvider(map[string][]byte{"key://two": keyTwo})
	sourceRoot := filepath.Join(t.TempDir(), "source")
	source, err := OpenEncryptedZstdDir(sourceRoot, "key://one", providerOne)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("rotation payload "), 128)
	receipt, err := source.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	record, err := source.PlaceRecord(ctx, RecordPublicationCommit, strings.NewReader(`{"commit":"rotation"}`))
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := filepath.Join(t.TempDir(), "target")
	if _, err := MigrateProfileWithKeyProviders(ctx, RepositoryProfileLocalZstdEncryptedV1, sourceRoot, RepositoryProfileLocalZstdEncryptedV1, targetRoot, "key://two", providerOne, providerTwo); err != nil {
		t.Fatal(err)
	}
	target, err := OpenProfileReadOnlyWithKeyProvider(RepositoryProfileLocalZstdEncryptedV1, targetRoot, providerTwo)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Verify(ctx, receipt.ContentID); err != nil {
		t.Fatal(err)
	}
	if err := target.VerifyRecord(ctx, record); err != nil {
		t.Fatal(err)
	}
	if source.RepositoryIdentity() != target.RepositoryIdentity() {
		t.Fatalf("repository identity changed across key rotation: %q != %q", source.RepositoryIdentity(), target.RepositoryIdentity())
	}
	if err := source.Verify(ctx, receipt.ContentID); err != nil {
		t.Fatalf("source became unreadable after copy-forward: %v", err)
	}
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(targetRoot, moved); err != nil {
		t.Fatal(err)
	}
	relocated, err := OpenProfileReadOnlyWithKeyProvider(RepositoryProfileLocalZstdEncryptedV1, moved, providerTwo)
	if err != nil {
		t.Fatal(err)
	}
	if err := relocated.Verify(ctx, receipt.ContentID); err != nil {
		t.Fatalf("relocated encrypted reader verify: %v", err)
	}
	if _, err := OpenProfileReadOnlyWithKeyProvider(RepositoryProfileLocalZstdEncryptedV1, moved, providerOne); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("old key provider open error = %v, want ErrKeyUnavailable", err)
	}
}

func TestEncryptedZstdMigrationProcessCrashBeforePublishCanRetryWithCleanReaders(t *testing.T) {
	ctx := context.Background()
	keyOne := bytes.Repeat([]byte{0x11}, 32)
	keyTwo := bytes.Repeat([]byte{0x22}, 32)
	providerOne := testKeyProvider(map[string][]byte{"key://one": keyOne})
	providerTwo := testKeyProvider(map[string][]byte{"key://two": keyTwo})
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	targetRoot := filepath.Join(root, "target")
	markerPath := filepath.Join(root, "after-payload.marker")
	source, err := OpenEncryptedZstdDir(sourceRoot, "key://one", providerOne)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("encrypted process crash migration payload\n"), 128)
	receipt, err := source.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	record, err := source.PlaceRecord(ctx, RecordPublicationCommit, strings.NewReader(`{"commit":"encrypted-crash"}`))
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestEncryptedZstdMigrationCrashHelperProcess")
	cmd.Env = append(os.Environ(),
		"RW_ENCRYPTED_MIGRATION_CRASH_HELPER=1",
		"RW_ENCRYPTED_MIGRATION_SOURCE_ROOT="+sourceRoot,
		"RW_ENCRYPTED_MIGRATION_TARGET_ROOT="+targetRoot,
		"RW_ENCRYPTED_MIGRATION_MARKER="+markerPath,
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	waitComplete := false
	defer func() {
		if !waitComplete {
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	deadline := time.NewTimer(15 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			t.Fatalf("encrypted migration helper exited before marker: %v (%s)", err, output.String())
		case <-ticker.C:
			if _, err := os.Stat(markerPath); err == nil {
				goto crash
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("stat encrypted migration marker: %v", err)
			}
		case <-deadline.C:
			_ = cmd.Process.Kill()
			<-done
			waitComplete = true
			t.Fatal("timed out waiting for encrypted migration crash marker")
		}
	}

crash:
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("encrypted migration helper unexpectedly exited cleanly")
	}
	waitComplete = true
	if _, err := os.Lstat(targetRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("crashed encrypted migration published target: %v", err)
	}

	verifyAndRead := func(label string, repo DriverRecord) {
		t.Helper()
		if err := repo.Verify(ctx, receipt.ContentID); err != nil {
			t.Fatalf("%s payload verify: %v", label, err)
		}
		body, err := repo.Open(ctx, receipt.ContentID)
		if err != nil {
			t.Fatalf("%s payload open: %v", label, err)
		}
		got, readErr := io.ReadAll(body)
		closeErr := body.Close()
		if readErr != nil || closeErr != nil || !bytes.Equal(got, payload) {
			t.Fatalf("%s payload read: bytes=%d readErr=%v closeErr=%v", label, len(got), readErr, closeErr)
		}
		if err := repo.VerifyRecord(ctx, record); err != nil {
			t.Fatalf("%s record verify: %v", label, err)
		}
	}

	sourceReader, err := OpenProfileReadOnlyWithKeyProvider(RepositoryProfileLocalZstdEncryptedV1, sourceRoot, providerOne)
	if err != nil {
		t.Fatal(err)
	}
	verifyAndRead("source after crash", sourceReader)
	if _, err := MigrateProfileWithKeyProviders(ctx, RepositoryProfileLocalZstdEncryptedV1, sourceRoot, RepositoryProfileLocalZstdEncryptedV1, targetRoot, "key://two", providerOne, providerTwo); err != nil {
		t.Fatalf("retry encrypted migration: %v", err)
	}
	targetReader, err := OpenProfileReadOnlyWithKeyProvider(RepositoryProfileLocalZstdEncryptedV1, targetRoot, providerTwo)
	if err != nil {
		t.Fatal(err)
	}
	verifyAndRead("target after retry", targetReader)
	if sourceReader.RepositoryIdentity() != targetReader.RepositoryIdentity() {
		t.Fatalf("repository identity changed: source=%q target=%q", sourceReader.RepositoryIdentity(), targetReader.RepositoryIdentity())
	}
	sourceReader, err = OpenProfileReadOnlyWithKeyProvider(RepositoryProfileLocalZstdEncryptedV1, sourceRoot, providerOne)
	if err != nil {
		t.Fatal(err)
	}
	verifyAndRead("source reopened after retry", sourceReader)
	if _, err := OpenProfileReadOnlyWithKeyProvider(RepositoryProfileLocalZstdEncryptedV1, targetRoot, providerOne); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("old key provider target open error = %v, want ErrKeyUnavailable", err)
	}
	wrongReader, err := OpenProfileReadOnlyWithKeyProvider(RepositoryProfileLocalZstdEncryptedV1, targetRoot, testKeyProvider(map[string][]byte{"key://two": bytes.Repeat([]byte{0x23}, 32)}))
	if err != nil {
		t.Fatal(err)
	}
	if err := wrongReader.Verify(ctx, receipt.ContentID); !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("wrong key target verify error = %v, want ErrKeyMismatch", err)
	}
	if body, err := wrongReader.Open(ctx, receipt.ContentID); err == nil {
		decrypted, readErr := io.ReadAll(body)
		_ = body.Close()
		if readErr == nil || bytes.Contains(decrypted, payload) {
			t.Fatalf("wrong key target returned plaintext: %q read=%v", decrypted, readErr)
		}
	}
}

func TestEncryptedZstdMigrationCrashHelperProcess(t *testing.T) {
	if os.Getenv("RW_ENCRYPTED_MIGRATION_CRASH_HELPER") != "1" {
		return
	}
	keyOne := bytes.Repeat([]byte{0x11}, 32)
	keyTwo := bytes.Repeat([]byte{0x22}, 32)
	providerOne := testKeyProvider(map[string][]byte{"key://one": keyOne})
	providerTwo := testKeyProvider(map[string][]byte{"key://two": keyTwo})
	_, err := migrateProfileWithHooks(context.Background(), RepositoryProfileLocalZstdEncryptedV1,
		os.Getenv("RW_ENCRYPTED_MIGRATION_SOURCE_ROOT"), RepositoryProfileLocalZstdEncryptedV1,
		os.Getenv("RW_ENCRYPTED_MIGRATION_TARGET_ROOT"), "key://two", providerOne, providerTwo, migrationHooks{
			afterPayload: func(string) error {
				if err := os.WriteFile(os.Getenv("RW_ENCRYPTED_MIGRATION_MARKER"), []byte("after-payload\n"), 0o600); err != nil {
					return err
				}
				time.Sleep(time.Hour)
				return nil
			},
		})
	if err != nil {
		t.Fatalf("encrypted migration crash helper: %v", err)
	}
}
