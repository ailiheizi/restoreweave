package repository

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MigrationReport is evidence from one explicit source-to-new-target copy.
// The source is never mutated and the target is never treated as authoritative
// until every copied payload and portable record has been verified.
type MigrationReport struct {
	SourceProfile       ProfileDescription
	TargetProfile       ProfileDescription
	SourceRoot          string
	TargetRoot          string
	PayloadObjects      int
	PortableRecords     int
	SnapshotFiles       int
	LogicalBytes        int64
	VerifiedTargetBytes int64
}

// migrationHooks are test-only fault-injection points for crash-boundary
// qualification. The public migration API always supplies an empty value.
// Hooks run after a staged object has verified, or immediately before the
// final atomic publication; they never alter repository data.
type migrationHooks struct {
	afterPayload  func(string) error
	afterRecord   func(RecordRole, string) error
	beforePublish func() error
}

// MigrateProfile copies an existing in-tree repository into a new explicit
// profile. It is intentionally copy-forward: callers retain the source for
// rollback and decide separately when any old copy may be retired.
func MigrateProfile(ctx context.Context, sourceProfile, sourcePath, targetProfile, targetPath string) (MigrationReport, error) {
	return migrateProfileWithHooks(ctx, sourceProfile, sourcePath, targetProfile, targetPath, "", nil, nil, migrationHooks{})
}

// MigrateProfileWithKeyProviders performs the same verified copy-forward while
// keeping encryption credentials host-owned. targetKeyRef is required only
// when creating an encrypted target; neither key nor provider output is
// serialized into the migration report or repository records.
func MigrateProfileWithKeyProviders(ctx context.Context, sourceProfile, sourcePath, targetProfile, targetPath, targetKeyRef string, sourceProvider, targetProvider KeyProvider) (MigrationReport, error) {
	return migrateProfileWithHooks(ctx, sourceProfile, sourcePath, targetProfile, targetPath, targetKeyRef, sourceProvider, targetProvider, migrationHooks{})
}

func migrateProfileWithHooks(ctx context.Context, sourceProfile, sourcePath, targetProfile, targetPath, targetKeyRef string, sourceProvider, targetProvider KeyProvider, hooks migrationHooks) (MigrationReport, error) {
	var report MigrationReport
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if strings.TrimSpace(sourcePath) == "" || strings.TrimSpace(targetPath) == "" {
		return report, errors.New("migration source and target paths are required")
	}
	sourceRoot, err := filepath.Abs(sourcePath)
	if err != nil {
		return report, fmt.Errorf("resolve migration source: %w", err)
	}
	targetRoot, err := filepath.Abs(targetPath)
	if err != nil {
		return report, fmt.Errorf("resolve migration target: %w", err)
	}
	if sourceRoot == targetRoot {
		return report, errors.New("migration source and target must differ")
	}
	if migrationPathsOverlap(sourceRoot, targetRoot) {
		return report, errors.New("migration source and target must be independent paths")
	}
	source, err := OpenProfileReadOnlyWithKeyProvider(sourceProfile, sourceRoot, sourceProvider)
	if err != nil {
		return report, fmt.Errorf("open migration source: %w", err)
	}
	if err := validateMigrationSourceInventory(sourceRoot, sourceProfile); err != nil {
		return report, fmt.Errorf("validate migration source inventory: %w", err)
	}
	targetExists := false
	if info, statErr := os.Lstat(targetRoot); statErr == nil {
		targetExists = true
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return report, errors.New("migration target must be a non-symlink directory")
		}
		entries, readErr := os.ReadDir(targetRoot)
		if readErr != nil {
			return report, fmt.Errorf("inspect migration target: %w", readErr)
		}
		if len(entries) != 0 {
			return report, errors.New("migration target must be a new empty directory")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return report, fmt.Errorf("inspect migration target: %w", statErr)
	}
	// Build beside the requested target and publish only after every copied
	// object and record has verified. The target path therefore never exposes a
	// half-migrated repository when a source read, placement, or verification
	// fails. An existing empty directory is retained on failure and replaced
	// only after the staged repository is complete.
	targetParent := filepath.Dir(targetRoot)
	if err := os.MkdirAll(targetParent, 0o700); err != nil {
		return report, fmt.Errorf("create migration target parent: %w", err)
	}
	stageRoot, err := os.MkdirTemp(targetParent, filepath.Base(targetRoot)+".restoreweave-migration-*")
	if err != nil {
		return report, fmt.Errorf("create migration staging directory: %w", err)
	}
	defer os.RemoveAll(stageRoot)

	var target DriverRecord
	if targetProfile == RepositoryProfileLocalZstdEncryptedV1 {
		target, err = OpenEncryptedZstdDir(stageRoot, targetKeyRef, targetProvider)
	} else {
		target, err = OpenProfile(targetProfile, stageRoot)
	}
	if err != nil {
		return report, fmt.Errorf("open migration staging target: %w", err)
	}
	// Portable recovery records authenticate the repository identity. A
	// profile migration is a copy-forward of the same logical repository, so
	// the new target must retain the source identity before records are
	// verified by a clean reader.
	if err := replaceRepositoryIdentity(stageRoot, source.RepositoryIdentity()); err != nil {
		return report, fmt.Errorf("preserve migration repository identity: %w", err)
	}
	if targetProfile == RepositoryProfileLocalZstdEncryptedV1 {
		target, err = OpenEncryptedZstdDir(stageRoot, targetKeyRef, targetProvider)
	} else {
		target, err = OpenProfile(targetProfile, stageRoot)
	}
	if err != nil {
		return report, fmt.Errorf("reopen migration staging target with source identity: %w", err)
	}
	report.SourceProfile = DescribeProfile(source)
	report.TargetProfile = DescribeProfile(target)
	report.SourceRoot = sourceRoot
	report.TargetRoot = targetRoot

	payloads, err := listRepositoryPayloadIDs(sourceRoot)
	if err != nil {
		return report, fmt.Errorf("list migration payloads: %w", err)
	}
	for _, contentID := range payloads {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		body, openErr := source.Open(ctx, contentID)
		if openErr != nil {
			return report, fmt.Errorf("open source payload %s: %w", contentID, openErr)
		}
		receipt, placeErr := target.PlaceExact(ctx, contentID, body)
		closeErr := body.Close()
		if placeErr != nil {
			return report, fmt.Errorf("copy payload %s: %w", contentID, placeErr)
		}
		if closeErr != nil {
			return report, fmt.Errorf("close source payload %s: %w", contentID, closeErr)
		}
		if err := target.Verify(ctx, contentID); err != nil {
			return report, fmt.Errorf("verify target payload %s: %w", contentID, err)
		}
		if hooks.afterPayload != nil {
			if err := hooks.afterPayload(contentID); err != nil {
				return report, fmt.Errorf("migration payload boundary %s: %w", contentID, err)
			}
		}
		report.PayloadObjects++
		report.LogicalBytes += receipt.Bytes
		report.VerifiedTargetBytes += receipt.StoredBytes
	}

	for _, role := range []RecordRole{RecordPreparedClosure, RecordPublicationCommit, RecordProcessorAttemptClosure, RecordPortableFactClosure} {
		digests, listErr := source.ListRecordDigests(ctx, role)
		if listErr != nil {
			return report, fmt.Errorf("list source records %s: %w", role, listErr)
		}
		for _, digest := range digests {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			body, openErr := source.OpenRecord(ctx, role, digest)
			if openErr != nil {
				return report, fmt.Errorf("open source record %s/%s: %w", role, digest, openErr)
			}
			receipt, placeErr := target.PlaceRecord(ctx, role, body)
			closeErr := body.Close()
			if placeErr != nil {
				return report, fmt.Errorf("copy source record %s/%s: %w", role, digest, placeErr)
			}
			if closeErr != nil {
				return report, fmt.Errorf("close source record %s/%s: %w", role, digest, closeErr)
			}
			if receipt.Digest != digest {
				return report, fmt.Errorf("target record digest changed for %s/%s", role, digest)
			}
			if err := target.VerifyRecord(ctx, receipt); err != nil {
				return report, fmt.Errorf("verify target record %s/%s: %w", role, digest, err)
			}
			if hooks.afterRecord != nil {
				if err := hooks.afterRecord(role, digest); err != nil {
					return report, fmt.Errorf("migration record boundary %s/%s: %w", role, digest, err)
				}
			}
			report.PortableRecords++
		}
	}
	snapshotFiles, err := copyMigrationSnapshots(sourceRoot, stageRoot)
	if err != nil {
		return report, fmt.Errorf("copy migration snapshots: %w", err)
	}
	report.SnapshotFiles = snapshotFiles
	if hooks.beforePublish != nil {
		if err := hooks.beforePublish(); err != nil {
			return report, fmt.Errorf("migration publication boundary: %w", err)
		}
	}
	if err := publishMigrationTarget(stageRoot, targetRoot, targetExists); err != nil {
		return report, fmt.Errorf("publish migrated repository: %w", err)
	}
	return report, nil
}

func migrationPathsOverlap(sourceRoot, targetRoot string) bool {
	within := func(parent, candidate string) bool {
		rel, err := filepath.Rel(parent, candidate)
		if err != nil {
			return true
		}
		return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
	}
	return within(sourceRoot, targetRoot) || within(targetRoot, sourceRoot)
}

func publishMigrationTarget(stageRoot, targetRoot string, targetExists bool) error {
	if targetExists {
		entries, err := os.ReadDir(targetRoot)
		if err != nil {
			return fmt.Errorf("recheck migration target: %w", err)
		}
		if len(entries) != 0 {
			return errors.New("migration target became non-empty during copy")
		}
		if err := os.Remove(targetRoot); err != nil {
			return fmt.Errorf("remove empty migration target: %w", err)
		}
	}
	if err := os.Rename(stageRoot, targetRoot); err != nil {
		return fmt.Errorf("rename migration staging directory: %w", err)
	}
	return syncFilesystemParentChain(filepath.Dir(targetRoot))
}

func replaceRepositoryIdentity(root, identity string) error {
	validated, err := validateRepositoryIdentity(identity)
	if err != nil {
		return err
	}
	path := filepath.Join(root, repositoryIdentityFile)
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect target repository identity: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("target repository identity is not a regular file")
	}
	temp, err := os.CreateTemp(root, "repository-identity-migration-*")
	if err != nil {
		return fmt.Errorf("create target repository identity: %w", err)
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.WriteString(validated + "\n"); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("replace target repository identity: %w", err)
	}
	return syncFilesystemParentChain(root)
}

func listRepositoryPayloadIDs(root string) ([]string, error) {
	base := filepath.Join(root, blobDirName, AlgorithmSHA256)
	var ids []string
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) && path == base {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("repository payload %q is not a regular file", path)
		}
		if _, err := parseContentID(AlgorithmSHA256 + ":" + entry.Name()); err != nil {
			return fmt.Errorf("repository payload %q has invalid content id: %w", path, err)
		}
		ids = append(ids, AlgorithmSHA256+":"+entry.Name())
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(ids)
	return ids, nil
}

// validateMigrationSourceInventory makes the copy-forward boundary explicit.
// A repository is authoritative only when every in-tree file belongs to the
// selected profile. In particular, a leftover temporary placement or an
// unknown recovery role is an interrupted/unsupported state, not something a
// migration may silently omit.
func validateMigrationSourceInventory(root, profile string) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("migration source root is required")
	}
	allowEncryptionMetadata := profile == RepositoryProfileLocalZstdEncryptedV1
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(relative, string(filepath.Separator))
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("migration source entry %q is a symlink", relative)
		}
		if entry.IsDir() {
			if migrationDirectoryAllowed(parts) {
				return nil
			}
			return fmt.Errorf("migration source contains unknown directory %q", relative)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("migration source entry %q is not a regular file", relative)
		}
		if migrationRootFileAllowed(parts, allowEncryptionMetadata) {
			return nil
		}
		if len(parts) == 4 && parts[0] == blobDirName && parts[1] == AlgorithmSHA256 {
			return validateMigrationObjectPath(relative, parts[2], parts[3], "payload")
		}
		if len(parts) == 5 && parts[0] == recoveryDirName && parts[2] == AlgorithmSHA256 {
			if !migrationRecordRoleAllowed(parts[1]) {
				return fmt.Errorf("migration source contains unknown recovery role %q", parts[1])
			}
			return validateMigrationObjectPath(relative, parts[3], parts[4], "portable record")
		}
		if len(parts) == 2 && parts[0] == "snapshots" {
			return validateMigrationSnapshotPath(relative, parts[1])
		}
		if len(parts) == 3 && parts[0] == recoveryDirName && parts[1] == "locks" {
			if validPublicationLockName(parts[2]) {
				return nil
			}
			return fmt.Errorf("migration source lock %q has invalid name", relative)
		}
		return fmt.Errorf("migration source contains unregistered file %q", relative)
	})
	if err != nil {
		return err
	}
	return nil
}

func migrationDirectoryAllowed(parts []string) bool {
	switch len(parts) {
	case 1:
		return parts[0] == blobDirName || parts[0] == recoveryDirName || parts[0] == tmpDirName || parts[0] == "snapshots"
	case 2:
		return (parts[0] == blobDirName && parts[1] == AlgorithmSHA256) ||
			(parts[0] == recoveryDirName && (migrationRecordRoleAllowed(parts[1]) || parts[1] == "locks"))
	case 3:
		return (parts[0] == blobDirName && parts[1] == AlgorithmSHA256 && validDigestPrefix(parts[2])) ||
			(parts[0] == recoveryDirName && migrationRecordRoleAllowed(parts[1]) && parts[2] == AlgorithmSHA256)
	case 4:
		return parts[0] == recoveryDirName && migrationRecordRoleAllowed(parts[1]) && parts[2] == AlgorithmSHA256 && validDigestPrefix(parts[3])
	default:
		return false
	}
}

func copyMigrationSnapshots(sourceRoot, stageRoot string) (int, error) {
	sourceDir := filepath.Join(sourceRoot, "snapshots")
	entries, err := os.ReadDir(sourceDir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if len(entries) == 0 {
		return 0, nil
	}
	targetDir := filepath.Join(stageRoot, "snapshots")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") {
			return 0, fmt.Errorf("snapshot entry %q is not a regular json file", entry.Name())
		}
		payload, err := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
		if err != nil {
			return 0, err
		}
		destination := filepath.Join(targetDir, entry.Name())
		if err := writeMigrationFileDurably(targetDir, destination, payload); err != nil {
			return 0, err
		}
		count++
	}
	return count, nil
}

func writeMigrationFileDurably(parent, destination string, payload []byte) error {
	temp, err := os.CreateTemp(parent, "migration-snapshot-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()
	if err := temp.Chmod(0o600); err != nil {
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
	if err := os.Rename(tempName, destination); err != nil {
		return err
	}
	return syncFilesystemParentChain(parent)
}

func validateMigrationSnapshotPath(relative, name string) error {
	if name == "" || name == "." || name == ".." || !strings.HasSuffix(name, ".json") || strings.ContainsRune(name, filepath.Separator) {
		return fmt.Errorf("migration source snapshot %q has invalid name", relative)
	}
	return nil
}

func validPublicationLockName(name string) bool {
	return len(name) == len("publication-")+64+len(".lock") &&
		strings.HasPrefix(name, "publication-") &&
		strings.HasSuffix(name, ".lock") &&
		validDigestPrefix(name[len("publication-") : len(name)-len(".lock")][:2]) &&
		isLowerHex(name[len("publication-"):len(name)-len(".lock")])
}

func isLowerHex(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func migrationRootFileAllowed(parts []string, allowEncryptionMetadata bool) bool {
	if len(parts) != 1 {
		return false
	}
	switch parts[0] {
	case repositoryProfileFile, repositoryIdentityFile:
		return true
	case repositoryEncryptionFile:
		return allowEncryptionMetadata
	default:
		return false
	}
}

func migrationRecordRoleAllowed(role string) bool {
	switch role {
	case recordRoleDir(RecordPreparedClosure), recordRoleDir(RecordPublicationCommit), recordRoleDir(RecordProcessorAttemptClosure), recordRoleDir(RecordPortableFactClosure):
		return true
	default:
		return false
	}
}

func validDigestPrefix(prefix string) bool {
	if len(prefix) != hexPrefixLen {
		return false
	}
	for _, r := range prefix {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func validateMigrationObjectPath(relative, prefix, name, kind string) error {
	if !validDigestPrefix(prefix) || len(name) != 64 || !strings.HasPrefix(name, prefix) {
		return fmt.Errorf("migration source %s %q has invalid digest placement", kind, relative)
	}
	if _, err := parseContentID(AlgorithmSHA256 + ":" + name); err != nil {
		return fmt.Errorf("migration source %s %q has invalid digest: %w", kind, relative, err)
	}
	return nil
}
