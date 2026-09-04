package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMigrateProfileCopiesVerifiedPayloadsAndRecordsWithoutMutatingSource(t *testing.T) {
	ctx := context.Background()
	sourceRoot := filepath.Join(t.TempDir(), "source")
	source, err := OpenDir(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("profile migration payload "), 256)
	receipt, err := source.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	record, err := source.PlaceRecord(ctx, RecordPublicationCommit, bytes.NewBufferString(`{"migration":true}`))
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(blobPath(sourceRoot, receipt.ContentID))
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := filepath.Join(t.TempDir(), "target")
	report, err := MigrateProfile(ctx, RepositoryProfileDirectoryCASDev, sourceRoot, RepositoryProfileLocalZstdV1, targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	if report.PayloadObjects != 1 || report.PortableRecords != 1 || report.LogicalBytes != int64(len(payload)) || report.SourceRoot != sourceRoot || report.TargetRoot != targetRoot {
		t.Fatalf("migration report = %+v", report)
	}
	after, err := os.ReadFile(blobPath(sourceRoot, receipt.ContentID))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("migration mutated source payload")
	}
	readonly, err := OpenProfileReadOnly(RepositoryProfileLocalZstdV1, targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	if readonly.RepositoryIdentity() != source.RepositoryIdentity() {
		t.Fatalf("target repository identity = %q, want source identity %q", readonly.RepositoryIdentity(), source.RepositoryIdentity())
	}
	if err := readonly.Verify(ctx, receipt.ContentID); err != nil {
		t.Fatal(err)
	}
	body, err := readonly.Open(ctx, receipt.ContentID)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(body)
	_ = body.Close()
	if readErr != nil || !bytes.Equal(got, payload) {
		t.Fatalf("target payload = %d bytes, err=%v", len(got), readErr)
	}
	digests, err := readonly.ListRecordDigests(ctx, RecordPublicationCommit)
	if err != nil || len(digests) != 1 || digests[0] != record.Digest {
		t.Fatalf("target record digests = %v, err=%v", digests, err)
	}
	if err := readonly.VerifyRecord(ctx, RecordReceipt{RepositoryID: readonly.RepositoryIdentity(), Role: record.Role, Digest: record.Digest, Bytes: record.Bytes}); err != nil {
		t.Fatalf("target record verify: %v", err)
	}
	targetBlob := blobPath(targetRoot, receipt.ContentID)
	targetBytes, err := os.ReadFile(targetBlob)
	if err != nil {
		t.Fatal(err)
	}
	targetBytes[len(targetBytes)/2] ^= 0xff
	if err := os.WriteFile(targetBlob, targetBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := readonly.Verify(ctx, receipt.ContentID); err == nil {
		t.Fatal("tampered migration target verified")
	}
	if err := source.Verify(ctx, receipt.ContentID); err != nil {
		t.Fatalf("source rollback copy was affected by target tamper: %v", err)
	}
}

func TestMigrateProfileRejectsNonEmptyTarget(t *testing.T) {
	ctx := context.Background()
	sourceRoot := filepath.Join(t.TempDir(), "source")
	if _, err := OpenDir(sourceRoot); err != nil {
		t.Fatal(err)
	}
	targetRoot := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(targetRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetRoot, "marker"), []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateProfile(ctx, RepositoryProfileDirectoryCASDev, sourceRoot, RepositoryProfileLocalZstdV1, targetRoot); err == nil {
		t.Fatal("migration accepted a non-empty target")
	}
}

func TestMigrateProfileRejectsOverlappingPaths(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	if _, err := OpenDir(sourceRoot); err != nil {
		t.Fatal(err)
	}
	for _, targetRoot := range []string{
		filepath.Join(sourceRoot, "nested-target"),
		root,
	} {
		if _, err := MigrateProfile(ctx, RepositoryProfileDirectoryCASDev, sourceRoot, RepositoryProfileLocalZstdV1, targetRoot); err == nil {
			t.Fatalf("migration accepted overlapping target %q", targetRoot)
		}
	}
}

func TestMigrateProfileRejectsMalformedPayloadInsteadOfDroppingIt(t *testing.T) {
	ctx := context.Background()
	sourceRoot := filepath.Join(t.TempDir(), "source")
	source, err := OpenDir(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := source.Place(ctx, bytes.NewBufferString("valid payload"))
	if err != nil {
		t.Fatal(err)
	}
	malformed := filepath.Join(sourceRoot, blobDirName, AlgorithmSHA256, "aa", "not-a-content-id")
	if err := os.MkdirAll(filepath.Dir(malformed), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(malformed, []byte("must not be silently ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	targetRoot := filepath.Join(t.TempDir(), "target")
	if _, err := MigrateProfile(ctx, RepositoryProfileDirectoryCASDev, sourceRoot, RepositoryProfileLocalZstdV1, targetRoot); err == nil {
		t.Fatal("migration accepted malformed source payload")
	}
	if _, err := os.Lstat(targetRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed migration published target state: %v", err)
	}
	if err := source.Verify(ctx, receipt.ContentID); err != nil {
		t.Fatalf("source was changed by rejected migration: %v", err)
	}
}

func TestMigrateProfileRejectsUnregisteredSourceEntries(t *testing.T) {
	cases := []struct {
		name string
		path func(string, string) string
	}{
		{
			name: "root file",
			path: func(root, _ string) string { return filepath.Join(root, "unregistered.marker") },
		},
		{
			name: "temporary placement",
			path: func(root, _ string) string { return filepath.Join(root, tmpDirName, "place-incomplete.blob") },
		},
		{
			name: "unknown blob namespace",
			path: func(root, _ string) string { return filepath.Join(root, blobDirName, "unknown-algorithm", "object") },
		},
		{
			name: "unknown recovery role",
			path: func(root, contentID string) string {
				return filepath.Join(root, recoveryDirName, "unknown-role", AlgorithmSHA256, contentID[7:9], contentID[7:])
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			sourceRoot := filepath.Join(t.TempDir(), "source")
			source, err := OpenDir(sourceRoot)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := source.Place(ctx, bytes.NewBufferString("source remains authoritative"))
			if err != nil {
				t.Fatal(err)
			}
			unknown := tc.path(sourceRoot, receipt.ContentID)
			if err := os.MkdirAll(filepath.Dir(unknown), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(unknown, []byte("unregistered source state"), 0o600); err != nil {
				t.Fatal(err)
			}
			targetRoot := filepath.Join(t.TempDir(), "target")
			if _, err := MigrateProfile(ctx, RepositoryProfileDirectoryCASDev, sourceRoot, RepositoryProfileLocalZstdV1, targetRoot); err == nil {
				t.Fatal("migration accepted unregistered source state")
			}
			if _, err := os.Lstat(targetRoot); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("rejected migration published target state: %v", err)
			}
			if err := source.Verify(ctx, receipt.ContentID); err != nil {
				t.Fatalf("source verification after rejected migration: %v", err)
			}
		})
	}
}

type cancelAfterMigrationChecks struct {
	context.Context
	remaining int
}

func (ctx *cancelAfterMigrationChecks) Err() error {
	if ctx.remaining == 0 {
		return context.Canceled
	}
	ctx.remaining--
	return ctx.Context.Err()
}

func TestMigrateProfileInterruptedCopyCanRetryWithoutPublishingPartialTarget(t *testing.T) {
	sourceRoot := filepath.Join(t.TempDir(), "source")
	source, err := OpenDir(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := source.Place(ctx, bytes.NewBufferString("first interruptible payload"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := source.Place(ctx, bytes.NewBufferString("second interruptible payload"))
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := filepath.Join(t.TempDir(), "target")
	interrupted := &cancelAfterMigrationChecks{Context: ctx, remaining: 5}
	if _, err := MigrateProfile(interrupted, RepositoryProfileDirectoryCASDev, sourceRoot, RepositoryProfileLocalZstdV1, targetRoot); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted migration error = %v, want context.Canceled", err)
	}
	if _, err := os.Lstat(targetRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted migration published target state: %v", err)
	}
	if err := source.Verify(ctx, first.ContentID); err != nil {
		t.Fatalf("source first payload after interruption: %v", err)
	}
	if err := source.Verify(ctx, second.ContentID); err != nil {
		t.Fatalf("source second payload after interruption: %v", err)
	}
	if _, err := MigrateProfile(ctx, RepositoryProfileDirectoryCASDev, sourceRoot, RepositoryProfileLocalZstdV1, targetRoot); err != nil {
		t.Fatalf("retry migration: %v", err)
	}
	target, err := OpenProfileReadOnly(RepositoryProfileLocalZstdV1, targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, contentID := range []string{first.ContentID, second.ContentID} {
		if err := target.Verify(ctx, contentID); err != nil {
			t.Fatalf("retry target verify %s: %v", contentID, err)
		}
	}
}

func TestMigrateProfileCrashBoundariesLeaveNoPublishedTargetAndRetry(t *testing.T) {
	sentinel := errors.New("injected migration interruption")
	cases := []struct {
		name  string
		hooks func(*int) migrationHooks
	}{
		{
			name: "after payload",
			hooks: func(calls *int) migrationHooks {
				return migrationHooks{afterPayload: func(string) error {
					*calls++
					return sentinel
				}}
			},
		},
		{
			name: "after record",
			hooks: func(calls *int) migrationHooks {
				return migrationHooks{afterRecord: func(RecordRole, string) error {
					*calls++
					return sentinel
				}}
			},
		},
		{
			name: "before publish",
			hooks: func(calls *int) migrationHooks {
				return migrationHooks{beforePublish: func() error {
					*calls++
					return sentinel
				}}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			sourceRoot := filepath.Join(t.TempDir(), "source")
			source, err := OpenDir(sourceRoot)
			if err != nil {
				t.Fatal(err)
			}
			payload := []byte("crash boundary payload")
			receipt, err := source.Place(ctx, bytes.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			record, err := source.PlaceRecord(ctx, RecordPublicationCommit, bytes.NewBufferString(`{"boundary":true}`))
			if err != nil {
				t.Fatal(err)
			}
			targetRoot := filepath.Join(t.TempDir(), "target")
			calls := 0
			if _, err := migrateProfileWithHooks(ctx, RepositoryProfileDirectoryCASDev, sourceRoot, RepositoryProfileLocalZstdV1, targetRoot, "", nil, nil, tc.hooks(&calls)); !errors.Is(err, sentinel) {
				t.Fatalf("boundary migration error = %v, want sentinel", err)
			}
			if calls != 1 {
				t.Fatalf("boundary hook calls = %d, want 1", calls)
			}
			if _, err := os.Lstat(targetRoot); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("boundary failure published target state: %v", err)
			}
			if err := source.Verify(ctx, receipt.ContentID); err != nil {
				t.Fatalf("source payload after boundary failure: %v", err)
			}
			if err := source.VerifyRecord(ctx, record); err != nil {
				t.Fatalf("source record after boundary failure: %v", err)
			}
			if _, err := MigrateProfile(ctx, RepositoryProfileDirectoryCASDev, sourceRoot, RepositoryProfileLocalZstdV1, targetRoot); err != nil {
				t.Fatalf("retry migration: %v", err)
			}
			target, err := OpenProfileReadOnly(RepositoryProfileLocalZstdV1, targetRoot)
			if err != nil {
				t.Fatal(err)
			}
			if err := target.Verify(ctx, receipt.ContentID); err != nil {
				t.Fatalf("retry target payload: %v", err)
			}
			if err := target.VerifyRecord(ctx, record); err != nil {
				t.Fatalf("retry target record: %v", err)
			}
		})
	}
}

func TestMigrateProfileInterruptedRetryReopensBothReaders(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("injected migration interruption")
	sourceRoot := filepath.Join(t.TempDir(), "source")
	source, err := OpenDir(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("reopen after migration payload\n"), 64)
	receipt, err := source.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	record, err := source.PlaceRecord(ctx, RecordPublicationCommit, bytes.NewBufferString(`{"reopen":true}`))
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := filepath.Join(t.TempDir(), "target")

	if _, err := migrateProfileWithHooks(ctx, RepositoryProfileDirectoryCASDev, sourceRoot, RepositoryProfileLocalZstdV1, targetRoot, "", nil, nil, migrationHooks{
		afterPayload: func(string) error { return sentinel },
	}); !errors.Is(err, sentinel) {
		t.Fatalf("interrupted migration error = %v, want sentinel", err)
	}
	if _, err := os.Lstat(targetRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted migration published target state: %v", err)
	}

	sourceAfterInterruption, err := OpenProfileReadOnly(RepositoryProfileDirectoryCASDev, sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceAfterInterruption.Verify(ctx, receipt.ContentID); err != nil {
		t.Fatalf("source payload after interruption: %v", err)
	}
	if err := sourceAfterInterruption.VerifyRecord(ctx, record); err != nil {
		t.Fatalf("source record after interruption: %v", err)
	}

	if _, err := MigrateProfile(ctx, RepositoryProfileDirectoryCASDev, sourceRoot, RepositoryProfileLocalZstdV1, targetRoot); err != nil {
		t.Fatalf("retry migration: %v", err)
	}
	targetAfterRetry, err := OpenProfileReadOnly(RepositoryProfileLocalZstdV1, targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := targetAfterRetry.Verify(ctx, receipt.ContentID); err != nil {
		t.Fatalf("reopened target payload: %v", err)
	}
	if err := targetAfterRetry.VerifyRecord(ctx, record); err != nil {
		t.Fatalf("reopened target record: %v", err)
	}
	sourceAfterRetry, err := OpenProfileReadOnly(RepositoryProfileDirectoryCASDev, sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceAfterRetry.Verify(ctx, receipt.ContentID); err != nil {
		t.Fatalf("reopened source payload: %v", err)
	}
	if err := sourceAfterRetry.VerifyRecord(ctx, record); err != nil {
		t.Fatalf("reopened source record: %v", err)
	}
}

func TestMigrateProfileProcessCrashBeforePublishCanRetryAndReadersReopen(t *testing.T) {
	for _, tc := range []struct {
		name         string
		boundary     string
		targetExists bool
	}{
		{name: "after payload", boundary: "after-payload"},
		{name: "after record", boundary: "after-record"},
		{name: "before publish", boundary: "before-publish", targetExists: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			sourceRoot := filepath.Join(root, "source")
			targetRoot := filepath.Join(root, "target")
			markerPath := filepath.Join(root, "migration-boundary.marker")
			source, err := OpenDir(sourceRoot)
			if err != nil {
				t.Fatal(err)
			}
			payloads := [][]byte{
				bytes.Repeat([]byte("process crash migration payload one\n"), 128),
				bytes.Repeat([]byte("process crash migration payload two\n"), 96),
			}
			receipts := make([]Receipt, 0, len(payloads))
			for _, payload := range payloads {
				receipt, placeErr := source.Place(ctx, bytes.NewReader(payload))
				if placeErr != nil {
					t.Fatal(placeErr)
				}
				receipts = append(receipts, receipt)
			}
			records := make(map[RecordRole]RecordReceipt)
			for _, role := range []RecordRole{RecordPreparedClosure, RecordPublicationCommit, RecordProcessorAttemptClosure, RecordPortableFactClosure} {
				record, placeErr := source.PlaceRecord(ctx, role, strings.NewReader(fmt.Sprintf(`{"role":%q}`, role)))
				if placeErr != nil {
					t.Fatal(placeErr)
				}
				records[role] = record
			}
			if tc.targetExists {
				if err := os.MkdirAll(targetRoot, 0o700); err != nil {
					t.Fatal(err)
				}
			}

			cmd := exec.Command(os.Args[0], "-test.run=TestMigrateProfileProcessCrashHelperProcess")
			cmd.Env = append(os.Environ(),
				"RW_MIGRATION_CRASH_HELPER=1",
				"RW_MIGRATION_CRASH_BOUNDARY="+tc.boundary,
				"RW_MIGRATION_SOURCE_ROOT="+sourceRoot,
				"RW_MIGRATION_TARGET_ROOT="+targetRoot,
				"RW_MIGRATION_MARKER="+markerPath,
			)
			var helperOutput bytes.Buffer
			cmd.Stdout = &helperOutput
			cmd.Stderr = &helperOutput
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
					waitComplete = true
					t.Fatalf("crash helper exited before marker: %v (%s)", err, helperOutput.String())
				case <-ticker.C:
					if _, err := os.Stat(markerPath); err == nil {
						goto crash
					} else if !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("stat crash marker: %v", err)
					}
				case <-deadline.C:
					_ = cmd.Process.Kill()
					<-done
					waitComplete = true
					t.Fatal("timed out waiting for crash helper marker")
				}
			}

		crash:
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err == nil {
				t.Fatal("crash helper unexpectedly exited cleanly")
			}
			waitComplete = true
			if tc.targetExists {
				entries, err := os.ReadDir(targetRoot)
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) != 0 {
					t.Fatalf("crashed migration replaced existing empty target: %v", entries)
				}
			} else if _, err := os.Lstat(targetRoot); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("crashed migration published target state: %v", err)
			}

			verifyRepository := func(label string, repo DriverRecord) {
				t.Helper()
				payloadIDs, err := listRepositoryPayloadIDs(repo.Root())
				if err != nil {
					t.Fatalf("%s payload inventory: %v", label, err)
				}
				wantPayloads := make(map[string]bool, len(receipts))
				for _, receipt := range receipts {
					wantPayloads[receipt.ContentID] = true
				}
				if len(payloadIDs) != len(wantPayloads) {
					t.Fatalf("%s payload inventory = %v, want %d entries", label, payloadIDs, len(wantPayloads))
				}
				for _, contentID := range payloadIDs {
					if !wantPayloads[contentID] {
						t.Fatalf("%s payload inventory contains unexpected %s", label, contentID)
					}
				}
				for i, receipt := range receipts {
					if err := repo.Verify(ctx, receipt.ContentID); err != nil {
						t.Fatalf("%s payload %d verify: %v", label, i, err)
					}
					body, err := repo.Open(ctx, receipt.ContentID)
					if err != nil {
						t.Fatalf("%s payload %d open: %v", label, i, err)
					}
					got, readErr := io.ReadAll(body)
					closeErr := body.Close()
					if readErr != nil || closeErr != nil || !bytes.Equal(got, payloads[i]) || int64(len(got)) != receipt.Bytes {
						t.Fatalf("%s payload %d read: bytes=%d want=%d readErr=%v closeErr=%v", label, i, len(got), receipt.Bytes, readErr, closeErr)
					}
				}
				for role, record := range records {
					digests, listErr := repo.ListRecordDigests(ctx, role)
					if listErr != nil || len(digests) != 1 || digests[0] != record.Digest {
						t.Fatalf("%s record %s inventory = %v, err=%v; want [%s]", label, role, digests, listErr, record.Digest)
					}
					if err := repo.VerifyRecord(ctx, record); err != nil {
						t.Fatalf("%s record %s verify: %v", label, role, err)
					}
				}
			}

			sourceReader, err := OpenProfileReadOnly(RepositoryProfileDirectoryCASDev, sourceRoot)
			if err != nil {
				t.Fatal(err)
			}
			verifyRepository("source after crash", sourceReader)
			report, err := MigrateProfile(ctx, RepositoryProfileDirectoryCASDev, sourceRoot, RepositoryProfileLocalZstdV1, targetRoot)
			if err != nil {
				t.Fatalf("retry migration: %v", err)
			}
			if report.PayloadObjects != len(receipts) || report.PortableRecords != len(records) || report.SnapshotFiles != 0 || report.LogicalBytes != receipts[0].Bytes+receipts[1].Bytes || report.VerifiedTargetBytes <= 0 || report.SourceRoot != sourceRoot || report.TargetRoot != targetRoot {
				t.Fatalf("retry migration report = %+v", report)
			}
			targetReader, err := OpenProfileReadOnly(RepositoryProfileLocalZstdV1, targetRoot)
			if err != nil {
				t.Fatal(err)
			}
			verifyRepository("target after retry", targetReader)
			sourceReader, err = OpenProfileReadOnly(RepositoryProfileDirectoryCASDev, sourceRoot)
			if err != nil {
				t.Fatal(err)
			}
			verifyRepository("source reopened after retry", sourceReader)
		})
	}
}

func TestMigrateProfileProcessCrashHelperProcess(t *testing.T) {
	if os.Getenv("RW_MIGRATION_CRASH_HELPER") != "1" {
		return
	}
	ctx := context.Background()
	_, err := migrateProfileWithHooks(ctx, RepositoryProfileDirectoryCASDev,
		os.Getenv("RW_MIGRATION_SOURCE_ROOT"), RepositoryProfileLocalZstdV1,
		os.Getenv("RW_MIGRATION_TARGET_ROOT"), "", nil, nil, migrationHooks{
			afterPayload: func(string) error {
				if os.Getenv("RW_MIGRATION_CRASH_BOUNDARY") != "after-payload" {
					return nil
				}
				if err := os.WriteFile(os.Getenv("RW_MIGRATION_MARKER"), []byte("after-payload\n"), 0o600); err != nil {
					return err
				}
				time.Sleep(time.Hour)
				return nil
			},
			afterRecord: func(RecordRole, string) error {
				if os.Getenv("RW_MIGRATION_CRASH_BOUNDARY") != "after-record" {
					return nil
				}
				if err := os.WriteFile(os.Getenv("RW_MIGRATION_MARKER"), []byte("after-record\n"), 0o600); err != nil {
					return err
				}
				time.Sleep(time.Hour)
				return nil
			},
			beforePublish: func() error {
				if os.Getenv("RW_MIGRATION_CRASH_BOUNDARY") != "before-publish" {
					return nil
				}
				if err := os.WriteFile(os.Getenv("RW_MIGRATION_MARKER"), []byte("before-publish\n"), 0o600); err != nil {
					return err
				}
				time.Sleep(time.Hour)
				return nil
			},
		})
	if err != nil {
		t.Fatalf("crash helper migration: %v", err)
	}
}

func TestMigrateProfileCopyFailureDoesNotPublishPartialTarget(t *testing.T) {
	ctx := context.Background()
	sourceRoot := filepath.Join(t.TempDir(), "source")
	source, err := OpenDir(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	first, err := source.Place(ctx, bytes.NewBufferString("first migration payload"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := source.Place(ctx, bytes.NewBufferString("second migration payload"))
	if err != nil {
		t.Fatal(err)
	}
	corruptPath := blobPath(sourceRoot, second.ContentID)
	corrupt, err := os.ReadFile(corruptPath)
	if err != nil {
		t.Fatal(err)
	}
	corrupt[0] ^= 0xff
	if err := os.WriteFile(corruptPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}

	targetRoot := filepath.Join(t.TempDir(), "target")
	if _, err := MigrateProfile(ctx, RepositoryProfileDirectoryCASDev, sourceRoot, RepositoryProfileLocalZstdV1, targetRoot); err == nil {
		t.Fatal("migration accepted a corrupt payload during copy")
	}
	if _, err := os.Lstat(targetRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("copy failure published partial target: %v", err)
	}
	if err := source.Verify(ctx, first.ContentID); err != nil {
		t.Fatalf("source healthy payload changed after failed migration: %v", err)
	}
}

func TestMigrateProfileFailurePreservesExistingEmptyTarget(t *testing.T) {
	ctx := context.Background()
	sourceRoot := filepath.Join(t.TempDir(), "source")
	source, err := OpenDir(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	healthy, err := source.Place(ctx, bytes.NewBufferString("healthy rollback payload"))
	if err != nil {
		t.Fatal(err)
	}
	corrupt, err := source.Place(ctx, bytes.NewBufferString("corrupt rollback payload"))
	if err != nil {
		t.Fatal(err)
	}
	corruptPath := blobPath(sourceRoot, corrupt.ContentID)
	corruptBytes, err := os.ReadFile(corruptPath)
	if err != nil {
		t.Fatal(err)
	}
	corruptBytes[0] ^= 0xff
	if err := os.WriteFile(corruptPath, corruptBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	targetRoot := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(targetRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateProfile(ctx, RepositoryProfileDirectoryCASDev, sourceRoot, RepositoryProfileLocalZstdV1, targetRoot); err == nil {
		t.Fatal("migration accepted a corrupt source with an existing empty target")
	}
	entries, err := os.ReadDir(targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed migration published into existing target: %v", entries)
	}
	if err := source.Verify(ctx, healthy.ContentID); err != nil {
		t.Fatalf("source healthy payload changed after rollback-preserving failure: %v", err)
	}
}

func TestMigrateProfileZstdToRawPreservesLogicalBytes(t *testing.T) {
	ctx := context.Background()
	sourceRoot := filepath.Join(t.TempDir(), "zstd-source")
	source, err := OpenZstdDir(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("zstd to raw migration "), 128)
	receipt, err := source.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := filepath.Join(t.TempDir(), "raw-target")
	if _, err := MigrateProfile(ctx, RepositoryProfileLocalZstdV1, sourceRoot, RepositoryProfileDirectoryCASDev, targetRoot); err != nil {
		t.Fatal(err)
	}
	target, err := OpenProfileReadOnly(RepositoryProfileDirectoryCASDev, targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Verify(ctx, receipt.ContentID); err != nil {
		t.Fatal(err)
	}
}

// TestMigrateProfileRawReaderSurvivesUnavailableSourceProfile proves the
// Phase 5 fallback contract: once a payload has been copied and verified into
// the raw exact profile, losing access to the experimental zstd source does
// not make the retained bytes unreadable.
func TestMigrateProfileRawReaderSurvivesUnavailableSourceProfile(t *testing.T) {
	ctx := context.Background()
	sourceRoot := filepath.Join(t.TempDir(), "zstd-source")
	source, err := OpenZstdDir(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("codec fallback remains exact\n"), 256)
	receipt, err := source.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := filepath.Join(t.TempDir(), "raw-target")
	if _, err := MigrateProfile(ctx, RepositoryProfileLocalZstdV1, sourceRoot, RepositoryProfileDirectoryCASDev, targetRoot); err != nil {
		t.Fatalf("migrate zstd to raw: %v", err)
	}

	// Simulate the experimental source profile and its reader becoming
	// unavailable without deleting the source fixture.
	unavailableRoot := sourceRoot + ".unavailable"
	if err := os.Rename(sourceRoot, unavailableRoot); err != nil {
		t.Fatalf("quarantine source profile: %v", err)
	}
	defer os.Rename(unavailableRoot, sourceRoot)

	target, err := OpenProfileReadOnly(RepositoryProfileDirectoryCASDev, targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Verify(ctx, receipt.ContentID); err != nil {
		t.Fatalf("raw fallback verify: %v", err)
	}
	body, err := target.Open(ctx, receipt.ContentID)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(body)
	closeErr := body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("raw fallback bytes = %d, want %d", len(got), len(payload))
	}
}
