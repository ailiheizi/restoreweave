package repository

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRepositoryBlobReadsRejectSymlinkTargets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("strict no-follow open is platform-specific on Windows")
	}
	ctx := context.Background()
	for _, profile := range []string{RepositoryProfileDirectoryCASDev, RepositoryProfileLocalZstdV1} {
		t.Run(profile, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "repository")
			repo, err := OpenProfile(profile, root)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := repo.Place(ctx, bytes.NewBufferString("blob no-follow"))
			if err != nil {
				t.Fatal(err)
			}
			path := blobPath(root, receipt.ContentID)
			target := filepath.Join(t.TempDir(), "outside-blob")
			if err := os.Rename(path, target); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			if body, err := repo.Open(ctx, receipt.ContentID); err == nil {
				_, _ = io.Copy(io.Discard, body)
				_ = body.Close()
				t.Fatal("Open followed a symlinked blob")
			}
			if err := repo.Verify(ctx, receipt.ContentID); err == nil {
				t.Fatal("Verify followed a symlinked blob")
			}
		})
	}
}

func TestRepositoryRepairRejectsSymlinkTargets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("strict no-follow repair is platform-specific on Windows")
	}
	ctx := context.Background()
	for _, profile := range []string{RepositoryProfileDirectoryCASDev, RepositoryProfileLocalZstdV1} {
		t.Run(profile, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "repository")
			repo, err := OpenProfile(profile, root)
			if err != nil {
				t.Fatal(err)
			}
			payload := []byte("repair no-follow")
			receipt, err := repo.Place(ctx, bytes.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "outside-repair")
			if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
				t.Fatal(err)
			}
			path := blobPath(root, receipt.ContentID)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			repairer, ok := repo.(RepairDriver)
			if !ok {
				t.Fatal("profile does not expose repair")
			}
			if _, err := repairer.Repair(ctx, receipt.ContentID, bytes.NewReader(payload)); err == nil {
				t.Fatal("repair replaced a symlink target")
			}
			got, err := os.ReadFile(target)
			if err != nil || string(got) != "outside" {
				t.Fatalf("outside target changed: %q, err=%v", got, err)
			}
		})
	}
}

func TestRepositoryRecordReadsRejectSymlinkTargets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("strict no-follow open is platform-specific on Windows")
	}
	ctx := context.Background()
	for _, profile := range []string{RepositoryProfileDirectoryCASDev, RepositoryProfileLocalZstdV1} {
		t.Run(profile, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "repository")
			repo, err := OpenProfile(profile, root)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := repo.PlaceRecord(ctx, RecordPublicationCommit, bytes.NewBufferString("record no-follow"))
			if err != nil {
				t.Fatal(err)
			}
			hexDigest, err := parseContentID(receipt.Digest)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, recoveryDirName, recordRoleDir(receipt.Role), AlgorithmSHA256, hexDigest[:hexPrefixLen], hexDigest)
			target := filepath.Join(t.TempDir(), "outside-record")
			if err := os.Rename(path, target); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			if body, err := repo.OpenRecord(ctx, receipt.Role, receipt.Digest); err == nil {
				_, _ = io.Copy(io.Discard, body)
				_ = body.Close()
				t.Fatal("OpenRecord followed a symlinked record")
			}
			if err := repo.VerifyRecord(ctx, receipt); err == nil {
				t.Fatal("VerifyRecord followed a symlinked record")
			}
		})
	}
}

func TestRepositoryNoFollowErrorsRemainNotFoundForMissingObjects(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	missing := "sha256:" + string(bytes.Repeat([]byte{'0'}, 64))
	if _, err := repo.Open(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing blob error = %v, want ErrNotFound", err)
	}
}
