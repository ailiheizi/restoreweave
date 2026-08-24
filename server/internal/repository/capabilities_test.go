package repository

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryProfilesDescribeCapabilitiesAndHealth(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name        string
		open        func(string) (DriverRecord, error)
		profile     string
		compression string
	}{
		{name: "raw", open: func(path string) (DriverRecord, error) { return OpenDir(path) }, profile: RepositoryProfileDirectoryCASDev, compression: CompressionIdentity},
		{name: "zstd", open: func(path string) (DriverRecord, error) { return OpenZstdDir(path) }, profile: RepositoryProfileLocalZstdV1, compression: CompressionProfileZstdV1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo, err := test.open(filepath.Join(t.TempDir(), "repo"))
			if err != nil {
				t.Fatal(err)
			}
			reporter, ok := repo.(CapabilityReporter)
			if !ok {
				t.Fatal("directory profile does not report capabilities")
			}
			capability := reporter.DescribeCapabilities()
			if capability.RepositoryFormat != test.profile || capability.Compression != test.compression ||
				capability.Encryption != EncryptionNone || capability.Chunking != ChunkingWholeFile ||
				!capability.SupportsWrite || !capability.SupportsRepair || !capability.SupportsReadOnly {
				t.Fatalf("capability profile = %+v", capability)
			}
			health, err := reporter.DescribeHealthAndCapacity(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !health.Available || !health.ReaderReady || health.KeyState != KeyStateNotRequired ||
				health.CapacityState != CapacityUnknown || health.CorruptionState != CorruptionNotChecked {
				t.Fatalf("health profile = %+v", health)
			}
		})
	}
}

func TestReadOnlyCapabilityReportDisablesMutation(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "repo")
	if _, err := OpenDir(root); err != nil {
		t.Fatal(err)
	}
	readonly, err := OpenProfileReadOnly(RepositoryProfileDirectoryCASDev, root)
	if err != nil {
		t.Fatal(err)
	}
	reporter, ok := readonly.(CapabilityReporter)
	if !ok {
		t.Fatal("read-only repository does not report capabilities")
	}
	capability := reporter.DescribeCapabilities()
	if capability.SupportsWrite || capability.SupportsRepair || !capability.SupportsReadOnly {
		t.Fatalf("read-only capabilities = %+v", capability)
	}
	health, err := reporter.DescribeHealthAndCapacity(ctx)
	if err != nil || !health.Available || !health.ReaderReady {
		t.Fatalf("read-only health = %+v err=%v", health, err)
	}
	if _, err := readonly.Place(ctx, nil); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("read-only placement error = %v", err)
	}
}

func TestHealthReportFailsClosedOnMissingOrMismatchedRepositoryState(t *testing.T) {
	ctx := context.Background()
	missing := &Dir{root: filepath.Join(t.TempDir(), "missing")}
	health, err := missing.DescribeHealthAndCapacity(ctx)
	if err != nil || health.Available || health.ReaderReady || health.Reason == "" {
		t.Fatalf("missing repository health = %+v err=%v", health, err)
	}
	root := filepath.Join(t.TempDir(), "repo")
	if _, err := OpenDir(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, repositoryProfileFile), []byte("tampered-profile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := &Dir{root: root}
	health, err = repo.DescribeHealthAndCapacity(ctx)
	if err != nil || health.Available || health.ReaderReady || health.Reason == "" {
		t.Fatalf("mismatched repository health = %+v err=%v", health, err)
	}
}

func TestHealthReportRejectsRepositorySymlinks(t *testing.T) {
	ctx := context.Background()
	actual := filepath.Join(t.TempDir(), "actual")
	if _, err := OpenDir(actual); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(actual, root); err != nil {
		t.Fatal(err)
	}
	health, err := (&Dir{root: root}).DescribeHealthAndCapacity(ctx)
	if err != nil || health.Available || health.ReaderReady || health.Reason == "" {
		t.Fatalf("symlink repository health = %+v err=%v", health, err)
	}

	marker := filepath.Join(actual, repositoryProfileFile)
	markerTarget := filepath.Join(t.TempDir(), "marker-target")
	if err := os.Rename(marker, markerTarget); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(markerTarget, marker); err != nil {
		t.Fatal(err)
	}
	health, err = (&Dir{root: actual}).DescribeHealthAndCapacity(ctx)
	if err != nil || health.Available || health.ReaderReady || health.Reason == "" {
		t.Fatalf("symlink profile marker health = %+v err=%v", health, err)
	}
}

func TestMemoryCapabilityReportsNoCleanReaderDependency(t *testing.T) {
	ctx := context.Background()
	repo := NewMemory()
	var reporter CapabilityReporter = repo
	capability := reporter.DescribeCapabilities()
	if capability.Reader != ReaderEmbedded || capability.SupportsReadOnly || capability.SupportsRepair {
		t.Fatalf("memory capabilities = %+v", capability)
	}
	if health, err := reporter.DescribeHealthAndCapacity(ctx); err != nil || !health.Available {
		t.Fatalf("memory health = %+v err=%v", health, err)
	}
}

func TestTargetValidationIsReadOnlyAndProfileBound(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "repo")
	repo, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	validator, ok := any(repo).(TargetValidator)
	if !ok {
		t.Fatal("raw profile does not validate targets")
	}
	validation, err := validator.ValidateTarget(ctx)
	if err != nil || !validation.Existing || !validation.Compatible || validation.RepositoryIdentity == "" {
		t.Fatalf("target validation = %+v err=%v", validation, err)
	}
	if _, err := OpenProfileReadOnly(RepositoryProfileLocalZstdV1, root); err == nil {
		t.Fatal("target validation accidentally changed the repository profile")
	}

	missing, err := (&Dir{root: filepath.Join(t.TempDir(), "missing")}).ValidateTarget(ctx)
	if err != nil || missing.Existing || !missing.Compatible || missing.Reason == "" {
		t.Fatalf("missing target validation = %+v err=%v", missing, err)
	}
}

func TestPlacementEstimateIsIndependentAndProfileAware(t *testing.T) {
	ctx := context.Background()
	payload := bytes.Repeat([]byte("estimate payload "), 128)
	for _, test := range []struct {
		name     string
		open     func(string) (Driver, error)
		compress bool
	}{
		{name: "raw", open: func(path string) (Driver, error) { return OpenDir(path) }, compress: false},
		{name: "zstd", open: func(path string) (Driver, error) { return OpenZstdDir(path) }, compress: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, err := test.open(filepath.Join(t.TempDir(), "repo"))
			if err != nil {
				t.Fatal(err)
			}
			estimator, ok := repo.(PlacementEstimator)
			if !ok {
				t.Fatal("profile does not estimate placements")
			}
			estimate, err := estimator.EstimatePlacement(ctx, "", bytes.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			if estimate.LogicalBytes != int64(len(payload)) || estimate.ContentID == "" ||
				!estimate.Supported || estimate.Existing || estimate.ProbableNewPhysicalBytes <= 0 {
				t.Fatalf("initial estimate = %+v", estimate)
			}
			if !test.compress && estimate.ProbableNewPhysicalBytes != estimate.LogicalBytes {
				t.Fatalf("raw estimate = %+v", estimate)
			}
			receipt, err := repo.PlaceExact(ctx, estimate.ContentID, bytes.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			if receipt.StoredBytes != estimate.ProbableNewPhysicalBytes {
				t.Fatalf("estimated physical bytes = %d, stored = %d", estimate.ProbableNewPhysicalBytes, receipt.StoredBytes)
			}
			existing, err := estimator.EstimatePlacement(ctx, estimate.ContentID, bytes.NewReader(payload))
			if err != nil || !existing.Existing || existing.ProbableNewPhysicalBytes != 0 {
				t.Fatalf("existing estimate = %+v err=%v", existing, err)
			}
			if test.compress {
				if _, err := repo.PlaceExact(ctx, estimate.ContentID, bytes.NewReader(payload)); err != nil {
					t.Fatal(err)
				}
				opened, err := repo.Open(ctx, estimate.ContentID)
				if err != nil {
					t.Fatal(err)
				}
				stored, err := io.ReadAll(opened)
				_ = opened.Close()
				if err != nil || !bytes.Equal(stored, payload) {
					t.Fatalf("stored zstd payload readback: len=%d err=%v", len(stored), err)
				}
			}
			if _, err := estimator.EstimatePlacement(ctx, estimate.ContentID, bytes.NewReader([]byte("wrong"))); !errors.Is(err, ErrDigestMismatch) {
				t.Fatalf("wrong identity estimate error = %v", err)
			}
		})
	}
}

func TestReadOnlyPlacementEstimateDoesNotExposeMutation(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "repo")
	writable, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	readonly, err := OpenProfileReadOnly(RepositoryProfileDirectoryCASDev, root)
	if err != nil {
		t.Fatal(err)
	}
	estimator, ok := readonly.(PlacementEstimator)
	if !ok {
		t.Fatal("read-only profile does not expose estimator boundary")
	}
	payload := []byte("no mutation")
	estimate, err := estimator.EstimatePlacement(ctx, "", bytes.NewReader(payload))
	if err != nil || estimate.LogicalBytes != int64(len(payload)) || estimate.Existing {
		t.Fatalf("read-only estimate = %+v err=%v", estimate, err)
	}
	if _, err := writable.Open(ctx, estimate.ContentID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read-only estimate created object: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == tmpDirName {
			children, readErr := os.ReadDir(filepath.Join(root, entry.Name()))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(children) != 0 {
				t.Fatalf("read-only estimate left temporary entries: %v", children)
			}
		}
	}
}

func TestPlacementEstimateRejectsCorruptExistingObject(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "repo")
	repo, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("estimate corruption")
	receipt, err := repo.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	path := blobPath(root, receipt.ContentID)
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stored[0] ^= 0xff
	if err := os.WriteFile(path, stored, 0o600); err != nil {
		t.Fatal(err)
	}
	var estimator PlacementEstimator = repo
	if _, err := estimator.EstimatePlacement(ctx, receipt.ContentID, bytes.NewReader(payload)); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("corrupt existing estimate error = %v", err)
	}
}
