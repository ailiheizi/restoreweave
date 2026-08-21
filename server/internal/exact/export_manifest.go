package exact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

// ExportManifestSchemaV1 is the frozen export-manifest contract. A manifest
// is the unit of plan, apply, verify, and reproduction; it never re-evaluates
// the live view it was frozen from.
const ExportManifestSchemaV1 = "org.restoreweave.export-manifest.v1"

// ExportItem is one frozen subject output in an ExportManifest. SubjectRef is
// the stable catalog subject; OutputName is the destination-relative name and
// is always safe (no separators, no traversal). ContentID binds exact bytes
// when the subject has a local exact representation. Exact is false when the
// frozen item has no local exact payload; its apply receipt must declare that
// degradation rather than pretend success.
type ExportItem struct {
	SubjectRef  string `json:"subject_ref"`
	OutputName  string `json:"output_name"`
	ContentID   string `json:"content_id,omitempty"`
	LogicalSize int64  `json:"logical_size,omitempty"`
	Exact       bool   `json:"exact"`
}

// ExportManifest is the frozen output intent. ManifestDigest is the canonical
// digest over the frozen field set; changing any frozen member changes the
// digest and therefore the manifest identity. ConfigDigest and
// TargetProfileDigest bind the operator profile at planning time.
type ExportManifest struct {
	Schema              string       `json:"schema"`
	ManifestID          string       `json:"manifest_id"`
	ManifestDigest      string       `json:"manifest_digest,omitempty"`
	ViewID              string       `json:"view_id,omitempty"`
	Representation      string       `json:"representation,omitempty"`
	Sidecars            string       `json:"sidecars,omitempty"`
	Target              string       `json:"target,omitempty"`
	ConfigDigest        string       `json:"config_digest,omitempty"`
	TargetProfileDigest string       `json:"target_profile_digest,omitempty"`
	Items               []ExportItem `json:"items"`
}

// canonicalForDigest returns the deterministic compact JSON the manifest
// digest is computed over. The digest never includes its own value.
func (manifest ExportManifest) canonicalForDigest() ([]byte, error) {
	copy := manifest
	copy.ManifestDigest = ""
	return CanonicalJSON(copy)
}

// Digest computes the canonical manifest digest over the frozen field set.
func (manifest ExportManifest) Digest() (string, error) {
	payload, err := manifest.canonicalForDigest()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Validate checks the frozen shape without touching the repository. Items
// must be unique by output name and subject reference.
func (manifest ExportManifest) Validate() error {
	if manifest.Schema != ExportManifestSchemaV1 {
		return fmt.Errorf("unsupported export manifest schema %q", manifest.Schema)
	}
	if strings.TrimSpace(manifest.ManifestID) == "" {
		return errors.New("export manifest id is required")
	}
	if len(manifest.Items) == 0 {
		return errors.New("export manifest must freeze at least one item")
	}
	byName := make(map[string]struct{}, len(manifest.Items))
	bySubject := make(map[string]struct{}, len(manifest.Items))
	for _, item := range manifest.Items {
		if strings.TrimSpace(item.SubjectRef) == "" {
			return errors.New("export manifest item subject is required")
		}
		if strings.TrimSpace(item.OutputName) == "" {
			return errors.New("export manifest item output name is required")
		}
		if item.OutputName != filepath.Base(item.OutputName) {
			return fmt.Errorf("export output name %q must be a single path component", item.OutputName)
		}
		if item.OutputName == "." || item.OutputName == ".." {
			return fmt.Errorf("export output name %q is unsafe", item.OutputName)
		}
		if _, ok := byName[item.OutputName]; ok {
			return fmt.Errorf("export output name %q is repeated", item.OutputName)
		}
		if _, ok := bySubject[item.SubjectRef]; ok {
			return fmt.Errorf("export subject %q is repeated", item.SubjectRef)
		}
		byName[item.OutputName] = struct{}{}
		bySubject[item.SubjectRef] = struct{}{}
	}
	if manifest.ManifestDigest != "" {
		digest, err := manifest.Digest()
		if err != nil {
			return err
		}
		if digest != manifest.ManifestDigest {
			return fmt.Errorf("export manifest digest mismatch: got %s want %s", digest, manifest.ManifestDigest)
		}
	}
	return nil
}

// ExportApplyResult is returned after materializing a frozen manifest into an
// explicit destination. Verified is true only when the complete destination
// was written and then independently verified byte-for-byte against the
// frozen items. Items counts every frozen item (exact or declared non-exact).
type ExportApplyResult struct {
	ManifestID     string
	ManifestDigest string
	Destination    string
	Items          int
	Bytes          int64
	Verified       bool
}

// exportManifestDestinationBasis mirrors the restore plan destination basis:
// an absent path or a completely empty directory is writable; a non-empty
// directory, a non-directory, or a symlink destination fails closed before
// any bytes are written.
func exportManifestDestinationBasis(destination string) (string, error) {
	type basis struct {
		Schema string `json:"schema"`
		Path   string `json:"path"`
		State  string `json:"state"`
	}
	value := basis{Schema: "org.restoreweave.export-destination-basis.v1", Path: destination}
	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		value.State = "ABSENT"
	} else if err != nil {
		return "", err
	} else {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: export destination is a symbolic link", ErrBlocked)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%w: export destination exists and is not a directory", ErrBlocked)
		}
		entries, readErr := os.ReadDir(destination)
		if readErr != nil {
			return "", readErr
		}
		if len(entries) != 0 {
			return "", fmt.Errorf("%w: export destination is not empty", ErrBlocked)
		}
		value.State = "EMPTY_DIRECTORY"
	}
	payload, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// PrepareExportManifestDigest validates a frozen manifest and returns its
// canonical digest. It is the digest binding for plan and apply so the stored
// and supplied digests can never disagree about the frozen item set.
func (manifest ExportManifest) PrepareExportManifestDigest() (string, error) {
	if err := manifest.Validate(); err != nil {
		return "", err
	}
	digest, err := manifest.Digest()
	if err != nil {
		return "", err
	}
	return digest, nil
}

// ApplyExportManifest materializes a frozen manifest into an explicit,
// empty-or-absent destination and then verifies the result byte-for-byte.
// Re-applying the same manifest to the same destination is idempotent only
// through verify: because the destination must be empty at apply time, a
// caller that already has a populated destination uses VerifyExportManifest.
func (s *Service) ApplyExportManifest(ctx context.Context, manifest ExportManifest, destination string) (ExportApplyResult, error) {
	var result ExportApplyResult
	if err := s.requireRepository(); err != nil {
		return result, err
	}
	digest, err := manifest.PrepareExportManifestDigest()
	if err != nil {
		return result, err
	}
	manifest.ManifestDigest = digest
	result = ExportApplyResult{
		ManifestID: manifest.ManifestID, ManifestDigest: digest,
		Items: len(manifest.Items),
	}
	if strings.TrimSpace(destination) == "" {
		return result, fmt.Errorf("%w: export destination is required", ErrBlocked)
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return result, err
	}
	result.Destination = absolute
	if _, err := exportManifestDestinationBasis(absolute); err != nil {
		return result, err
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return result, err
	}
	var bytes int64
	for _, item := range manifest.Items {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !item.Exact {
			// A declared non-exact item has no local payload to materialize;
			// the destination must not contain a fabricated or placeholder
			// file for it. It counts in the item set but writes nothing.
			continue
		}
		n, err := s.materializeExportItem(ctx, absolute, item)
		if err != nil {
			return result, err
		}
		bytes += n
	}
	result.Bytes = bytes
	verified, err := s.VerifyExportManifest(ctx, manifest, absolute)
	if err != nil {
		return result, err
	}
	if !verified {
		return result, fmt.Errorf("%w: export manifest verification failed after materialization", ErrBlocked)
	}
	result.Verified = true
	return result, nil
}

func (s *Service) materializeExportItem(ctx context.Context, destination string, item ExportItem) (int64, error) {
	if !item.Exact || strings.TrimSpace(item.ContentID) == "" {
		return 0, fmt.Errorf("%w: item %s has no local exact payload", ErrBlocked, item.SubjectRef)
	}
	body, err := s.Repo.Open(ctx, item.ContentID)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", item.SubjectRef, err)
	}
	defer body.Close()
	dest := filepath.Join(destination, item.OutputName)
	file, err := createRestoreFile(dest, 0o644)
	if err != nil {
		return 0, err
	}
	digest := sha256.New()
	written, writeErr := io.Copy(io.MultiWriter(file, digest), body)
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(dest)
		return 0, writeErr
	}
	if closeErr != nil {
		_ = os.Remove(dest)
		return 0, closeErr
	}
	got := "sha256:" + hex.EncodeToString(digest.Sum(nil))
	if got != item.ContentID {
		_ = os.Remove(dest)
		return 0, fmt.Errorf("%w: materialized %s digest mismatch: got %s", ErrBlocked, item.OutputName, got)
	}
	if item.LogicalSize > 0 && written != item.LogicalSize {
		_ = os.Remove(dest)
		return 0, fmt.Errorf("%w: materialized %s length mismatch: got %d want %d", ErrBlocked, item.OutputName, written, item.LogicalSize)
	}
	return written, nil
}

// VerifyExportManifest checks a materialized destination against a frozen
// manifest. Every exact frozen output must exist with the expected SHA-256 and
// logical length, no extra path may exist, and a path claimed by a declared
// non-exact item must not be materialized. A manifest with only non-exact
// items verifies an empty destination honestly: it declared no exact output to
// reproduce. Any deviation fails closed.
func (s *Service) VerifyExportManifest(ctx context.Context, manifest ExportManifest, destination string) (bool, error) {
	if err := manifest.Validate(); err != nil {
		return false, err
	}
	if strings.TrimSpace(destination) == "" {
		return false, fmt.Errorf("%w: export destination is required", ErrBlocked)
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(absolute)
	if errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("%w: export destination does not exist", ErrBlocked)
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, fmt.Errorf("%w: export destination is not a directory", ErrBlocked)
	}
	expected := make(map[string]ExportItem, len(manifest.Items))
	for _, item := range manifest.Items {
		expected[item.OutputName] = item
	}
	seen := make(map[string]struct{}, len(manifest.Items))
	walkErr := filepath.WalkDir(absolute, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == absolute {
			return nil
		}
		rel, err := filepath.Rel(absolute, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		item, ok := expected[rel]
		if !ok {
			return fmt.Errorf("%w: export destination contains unexpected path %q", ErrBlocked, rel)
		}
		if entry.IsDir() {
			return fmt.Errorf("%w: export output %q is a directory", ErrBlocked, rel)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: export output %q is a symbolic link", ErrBlocked, rel)
		}
		if !item.Exact {
			return fmt.Errorf("%w: declared non-exact item %q must not be materialized", ErrBlocked, rel)
		}
		seen[rel] = struct{}{}
		if strings.TrimSpace(item.ContentID) == "" {
			return fmt.Errorf("%w: exact item %q lacks a content identity", ErrBlocked, rel)
		}
		if item.LogicalSize > 0 {
			if info, statErr := entry.Info(); statErr != nil {
				return statErr
			} else if info.Size() != item.LogicalSize {
				return fmt.Errorf("%w: export output %q length mismatch: got %d want %d", ErrBlocked, rel, info.Size(), item.LogicalSize)
			}
		}
		got, _, hashErr := hashRestoreFile(path)
		if hashErr != nil {
			return hashErr
		}
		if got != item.ContentID {
			return fmt.Errorf("%w: export output %q digest mismatch: got %s want %s", ErrBlocked, rel, got, item.ContentID)
		}
		return nil
	})
	if walkErr != nil {
		return false, walkErr
	}
	for rel, item := range expected {
		if !item.Exact {
			// Declared non-exact items are not required outputs; they are
			// enforced only as "must not be materialized" above.
			continue
		}
		if _, ok := seen[rel]; !ok {
			return false, fmt.Errorf("%w: export destination is missing %q", ErrBlocked, rel)
		}
	}
	return true, nil
}

// ExportManifestReceipt is the per-item materialization receipt returned by
// apply and verify. Exact declares whether the item was materialized with
// exact local bytes; a non-exact item carries no fabricated file.
type ExportManifestReceipt struct {
	SubjectRef string `json:"subject_ref"`
	OutputName string `json:"output_name"`
	Exact      bool   `json:"exact"`
	Bytes      int64  `json:"bytes,omitempty"`
}

// DescribeExportManifestProfile binds the repository and compression profiles
// into the frozen manifest digest so a later apply cannot reinterpret output
// across a repository tuple change.
func DescribeExportManifestProfile(driver repository.Driver) string {
	if driver == nil {
		return "unavailable"
	}
	profile := repository.DescribeProfile(driver)
	return profile.Repository + "+" + profile.Compression
}
