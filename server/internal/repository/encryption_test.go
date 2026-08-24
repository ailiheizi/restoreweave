package repository

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
