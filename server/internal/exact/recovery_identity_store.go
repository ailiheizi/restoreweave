package exact

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	SigningIdentitySchemaV1 = "org.restoreweave.signing-identity.v1"
	signingIdentityFileName = "publication-signing-identity.json"
	trustAnchorFileName     = "publication-trust-anchor.json"
)

type persistedSigningIdentity struct {
	Schema            string `json:"schema"`
	PublicationDomain string `json:"publication_domain"`
	WriterIdentity    string `json:"writer_identity"`
	KeyID             string `json:"key_id"`
	PrivateKey        []byte `json:"private_key"`
	PublicKey         []byte `json:"public_key"`
}

// OpenSigningMaterial loads the publication identity and its local public
// anchor copy. When create is true, a missing identity is initialized exactly
// once. Corrupt or mismatched material is never replaced implicitly.
func OpenSigningMaterial(directory, publicationDomain string, create bool) (SigningIdentity, TrustAnchor, error) {
	if strings.TrimSpace(directory) == "" {
		return SigningIdentity{}, TrustAnchor{}, errors.New("recovery records directory is required")
	}
	if strings.TrimSpace(publicationDomain) == "" {
		return SigningIdentity{}, TrustAnchor{}, errors.New("publication domain is required")
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return SigningIdentity{}, TrustAnchor{}, err
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return SigningIdentity{}, TrustAnchor{}, fmt.Errorf("create recovery records directory: %w", err)
	}
	identityPath := filepath.Join(absolute, signingIdentityFileName)
	identity, storedDomain, err := loadSigningIdentity(identityPath)
	if errors.Is(err, os.ErrNotExist) && create {
		identity, err = NewSigningIdentity()
		if err == nil {
			err = persistSigningIdentity(identityPath, publicationDomain, identity)
		}
		storedDomain = publicationDomain
	}
	if err != nil {
		return SigningIdentity{}, TrustAnchor{}, err
	}
	if storedDomain != publicationDomain {
		return SigningIdentity{}, TrustAnchor{}, fmt.Errorf("signing identity publication domain is %q, want %q", storedDomain, publicationDomain)
	}
	anchor, err := NewTrustAnchor(identity, publicationDomain)
	if err != nil {
		return SigningIdentity{}, TrustAnchor{}, err
	}
	anchorPath := filepath.Join(absolute, trustAnchorFileName)
	storedAnchor, anchorErr := loadTrustAnchor(anchorPath)
	if errors.Is(anchorErr, os.ErrNotExist) {
		payload, marshalErr := CanonicalJSON(anchor)
		if marshalErr != nil {
			return SigningIdentity{}, TrustAnchor{}, marshalErr
		}
		if err := writeExclusiveFile(anchorPath, append(payload, '\n'), 0o644); err != nil {
			return SigningIdentity{}, TrustAnchor{}, fmt.Errorf("write trust anchor: %w", err)
		}
	} else if anchorErr != nil {
		return SigningIdentity{}, TrustAnchor{}, anchorErr
	} else {
		want, _ := CanonicalJSON(anchor)
		got, _ := CanonicalJSON(storedAnchor)
		if !bytes.Equal(got, want) {
			return SigningIdentity{}, TrustAnchor{}, errors.New("stored trust anchor does not match the signing identity")
		}
	}
	return identity, anchor, nil
}

// ExportTrustAnchor writes a public anchor for independent retention. It
// never overwrites an existing file and never includes private key material.
func ExportTrustAnchor(anchor TrustAnchor, destination string) (string, error) {
	if err := anchor.validate(); err != nil {
		return "", err
	}
	if strings.TrimSpace(destination) == "" {
		return "", errors.New("trust anchor destination is required")
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return "", err
	}
	payload, err := CanonicalJSON(anchor)
	if err != nil {
		return "", err
	}
	if err := writeExclusiveFile(absolute, append(payload, '\n'), 0o600); err != nil {
		return "", err
	}
	return absolute, nil
}

// LoadTrustAnchor loads independently supplied public verification material.
func LoadTrustAnchor(path string) (TrustAnchor, error) { return loadTrustAnchor(path) }

func loadSigningIdentity(path string) (SigningIdentity, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return SigningIdentity{}, "", err
	}
	if !info.Mode().IsRegular() {
		return SigningIdentity{}, "", errors.New("signing identity is not a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return SigningIdentity{}, "", fmt.Errorf("signing identity permissions are %04o, want no group or other access", info.Mode().Perm())
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return SigningIdentity{}, "", err
	}
	var stored persistedSigningIdentity
	if err := decodeStrictRecord(payload, &stored); err != nil {
		return SigningIdentity{}, "", fmt.Errorf("decode signing identity: %w", err)
	}
	if stored.Schema != SigningIdentitySchemaV1 || strings.TrimSpace(stored.PublicationDomain) == "" {
		return SigningIdentity{}, "", errors.New("unsupported or incomplete signing identity")
	}
	identity := SigningIdentity{
		WriterIdentity: stored.WriterIdentity,
		KeyID:          stored.KeyID,
		PrivateKey:     append(ed25519.PrivateKey(nil), stored.PrivateKey...),
		PublicKey:      append(ed25519.PublicKey(nil), stored.PublicKey...),
	}
	if err := identity.validate(); err != nil {
		return SigningIdentity{}, "", err
	}
	if expected := "ed25519:" + DigestBytes(identity.PublicKey); identity.KeyID != expected || identity.WriterIdentity != "writer:"+expected {
		return SigningIdentity{}, "", errors.New("signing identity names do not match its public key")
	}
	return identity, stored.PublicationDomain, nil
}

func persistSigningIdentity(path, publicationDomain string, identity SigningIdentity) error {
	if err := identity.validate(); err != nil {
		return err
	}
	payload, err := CanonicalJSON(persistedSigningIdentity{
		Schema: SigningIdentitySchemaV1, PublicationDomain: publicationDomain,
		WriterIdentity: identity.WriterIdentity, KeyID: identity.KeyID,
		PrivateKey: identity.PrivateKey, PublicKey: identity.PublicKey,
	})
	if err != nil {
		return err
	}
	return writeExclusiveFile(path, append(payload, '\n'), 0o600)
}

func loadTrustAnchor(path string) (TrustAnchor, error) {
	file, err := openRecoveryInput(path)
	if err != nil {
		return TrustAnchor{}, err
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, portableRecordReadLimit+1))
	if err != nil {
		return TrustAnchor{}, err
	}
	if int64(len(payload)) > portableRecordReadLimit {
		return TrustAnchor{}, fmt.Errorf("trust anchor exceeds %d bytes", portableRecordReadLimit)
	}
	var anchor TrustAnchor
	if err := decodeStrictRecord(payload, &anchor); err != nil {
		return TrustAnchor{}, fmt.Errorf("decode trust anchor: %w", err)
	}
	if err := anchor.validate(); err != nil {
		return TrustAnchor{}, err
	}
	return anchor, nil
}

func writeExclusiveFile(path string, payload []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temp, err := os.CreateTemp(directory, ".restoreweave-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()
	if err := temp.Chmod(mode); err != nil {
		return err
	}
	if _, err := temp.Write(payload); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Link(tempName, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%w: %s already exists", ErrBlocked, path)
		}
		return err
	}
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
