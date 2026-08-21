package exact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/capture"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/scanner"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func newPlannerService(t *testing.T) (*Service, *sqlite.Store, *repository.Dir, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload.txt"), []byte("stable payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Store: store, Repo: repo}, store, repo, root
}

func TestInspectIngestHasNoCatalogOrRepositorySideEffects(t *testing.T) {
	ctx := context.Background()
	service, store, repo, root := newPlannerService(t)

	plan, err := service.InspectIngest(ctx, root, IngestOptions{})
	if err != nil {
		t.Fatalf("inspect ingest: %v", err)
	}
	if plan.CaptureBasisDigest == "" || plan.Estimate.Files != 1 || plan.Estimate.Bytes != int64(len("stable payload")) {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if count, err := store.CountPlans(ctx); err != nil || count != 0 {
		t.Fatalf("plans after inspection = %d, err=%v", count, err)
	}
	if _, err := store.GetWorkspaceByName(ctx, defaultWorkspaceName); !errors.Is(err, sqlite.ErrNotFound) {
		t.Fatalf("inspection created catalog workspace: %v", err)
	}
	if _, err := repo.Open(ctx, planContentID(t, root)); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("inspection created repository object: %v", err)
	}
}

func TestCaptureBasisDigestCoversPortableObservations(t *testing.T) {
	binding := capture.BindingRecord{
		Schema: capture.SchemaBindingV1, Profile: capture.ProfileLocalTree,
		CaptureMode: scanner.CaptureModeRootedFD, DisplayPath: "/capture/root",
		DeviceID: 1, Inode: 2, ConsistencyClaim: capture.ClaimLiveValidated,
	}
	entry := scanner.EntryRecord{
		RawName: []byte("file.txt"), RawRelativePath: []byte("file.txt"), RelativePath: "file.txt",
		Kind: scanner.KindRegularFile, State: scanner.EntryComplete,
		Detection: scanner.DetectionObservation{State: scanner.DetectionSucceeded, Result: scanner.DetectionResult{
			DetectorID: "detector", DetectorVersion: "v1", FormatID: "text/plain",
		}},
		Issues: []scanner.Issue{{Stage: scanner.StageDetection, Code: "WARNING", Message: "observed warning"}},
	}
	base, err := captureBasisDigest(binding, []scanner.EntryRecord{entry})
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*scanner.EntryRecord){
		"raw name": func(candidate *scanner.EntryRecord) { candidate.RawName = []byte("renamed.txt") },
		"detection": func(candidate *scanner.EntryRecord) {
			candidate.Detection.Result.FormatID = "application/octet-stream"
		},
		"issue": func(candidate *scanner.EntryRecord) {
			candidate.Issues = []scanner.Issue{{Stage: scanner.StageDetection, Code: "OTHER"}}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := entry
			mutate(&candidate)
			got, err := captureBasisDigest(binding, []scanner.EntryRecord{candidate})
			if err != nil {
				t.Fatal(err)
			}
			if got == base {
				t.Fatalf("capture digest did not change after %s mutation", name)
			}
		})
	}
}

func TestApplyIngestPlanRejectsStaleSourceBeforePublicationOrBlob(t *testing.T) {
	ctx := context.Background()
	service, store, repo, root := newPlannerService(t)
	plan, err := service.InspectIngest(ctx, root, IngestOptions{})
	if err != nil {
		t.Fatalf("inspect ingest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "payload.txt"), []byte("changed payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyIngestPlan(ctx, plan); !errors.Is(err, ErrIngestPlanStale) {
		t.Fatalf("apply stale plan error = %v, want ErrIngestPlanStale", err)
	}
	if publications, err := store.ListPublications(ctx); err != nil || len(publications) != 0 {
		t.Fatalf("publications after stale apply = %d, err=%v", len(publications), err)
	}
	if _, err := store.GetWorkspaceByName(ctx, defaultWorkspaceName); !errors.Is(err, sqlite.ErrNotFound) {
		t.Fatalf("stale apply created catalog workspace: %v", err)
	}
	if _, err := repo.Open(ctx, planContentID(t, root)); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("stale apply created repository object: %v", err)
	}
}

func TestApplyIngestPlanRejectsChangedConfigBeforePublicationOrBlob(t *testing.T) {
	ctx := context.Background()
	service, store, repo, root := newPlannerService(t)
	service.ConfigDigest = "sha256:config-a"
	plan, err := service.InspectIngest(ctx, root, IngestOptions{})
	if err != nil {
		t.Fatalf("inspect ingest: %v", err)
	}
	service.ConfigDigest = "sha256:config-b"

	if _, err := service.ApplyIngestPlan(ctx, plan); !errors.Is(err, ErrIngestPlanConfigChanged) {
		t.Fatalf("apply changed-config plan error = %v, want ErrIngestPlanConfigChanged", err)
	}
	if publications, err := store.ListPublications(ctx); err != nil || len(publications) != 0 {
		t.Fatalf("publications after changed-config apply = %d, err=%v", len(publications), err)
	}
	if _, err := store.GetWorkspaceByName(ctx, defaultWorkspaceName); !errors.Is(err, sqlite.ErrNotFound) {
		t.Fatalf("changed-config apply created catalog workspace: %v", err)
	}
	if _, err := repo.Open(ctx, planContentID(t, root)); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("changed-config apply created repository object: %v", err)
	}
}

func TestApplyIngestPlanRejectsBlockedEntriesBeforeAnyMutation(t *testing.T) {
	ctx := context.Background()
	service, store, repo, root := newPlannerService(t)
	plan, err := service.InspectIngest(ctx, root, IngestOptions{})
	if err != nil {
		t.Fatalf("inspect ingest: %v", err)
	}
	plan.Executable = false
	plan.BlockedEntries = []IngestPlanIssue{{
		RelativePath: "payload.txt",
		State:        "UNSTABLE",
		ReasonCode:   "HANDLE_CHANGED_DURING_READ",
		Message:      "file changed while it was being read",
	}}
	if _, err := service.ApplyIngestPlan(ctx, plan); !errors.Is(err, ErrIngestPlanBlocked) {
		t.Fatalf("blocked plan error = %v, want ErrIngestPlanBlocked", err)
	}
	if publications, err := store.ListPublications(ctx); err != nil || len(publications) != 0 {
		t.Fatalf("publications after blocked apply = %d, err=%v", len(publications), err)
	}
	if _, err := store.GetWorkspaceByName(ctx, defaultWorkspaceName); !errors.Is(err, sqlite.ErrNotFound) {
		t.Fatalf("blocked apply created catalog workspace: %v", err)
	}
	if _, err := repo.Open(ctx, planContentID(t, root)); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("blocked apply created repository object: %v", err)
	}
}

func TestBlockedIngestEntriesCarryProtectionOutcome(t *testing.T) {
	issues := blockedIngestEntries([]scanner.EntryRecord{
		{
			RelativePath: "changing.bin", RawRelativePath: []byte("changing.bin"),
			State:  scanner.EntryUnstable,
			Issues: []scanner.Issue{{Code: "HANDLE_CHANGED_DURING_READ", Message: "changed"}},
		},
		{
			RelativePath: "denied.bin", RawRelativePath: []byte("denied.bin"),
			State:  scanner.EntryFailed,
			Issues: []scanner.Issue{{Code: "OPEN_FAILED", Message: "permission denied"}},
		},
	}, ingestPolicy{
		mode: sqlite.ProtectionStoreExact,
		fileModes: map[string]sqlite.ProtectionMode{
			"denied.bin": sqlite.ProtectionMetadataOnly,
		},
	})
	if len(issues) != 2 {
		t.Fatalf("blocked issues = %+v", issues)
	}
	if issues[0].RelativePath != "changing.bin" || issues[0].Mode != sqlite.ProtectionStoreExact ||
		issues[0].PlannedOutcome != sqlite.ProtectionBlocked {
		t.Fatalf("unstable issue = %+v", issues[0])
	}
	if issues[1].RelativePath != "denied.bin" || issues[1].Mode != sqlite.ProtectionMetadataOnly ||
		issues[1].PlannedOutcome != sqlite.ProtectionUnavailable {
		t.Fatalf("failed issue = %+v", issues[1])
	}
}

func TestMetadataOnlyResolutionRequiresExplicitStableRootedEvidence(t *testing.T) {
	before := scanner.MetadataSnapshot{Version: scanner.MetadataVersion, Size: 7, Mode: 0o600, ModTime: time.Unix(10, 0)}
	after := before
	entry := scanner.EntryRecord{
		RelativePath:    "denied.txt",
		RawRelativePath: []byte("denied.txt"),
		Kind:            scanner.KindRegularFile,
		State:           scanner.EntryFailed,
		Before:          &before,
		After:           &after,
		Boundary:        scanner.BoundaryObservation{Checked: true, Action: scanner.BoundaryInclude},
		Issues:          []scanner.Issue{{Stage: scanner.StageRead, Code: "CONTENT_READ_FAILED"}},
	}
	policy := ingestPolicy{
		mode:                    sqlite.ProtectionStoreExact,
		fileModes:               map[string]sqlite.ProtectionMode{"denied.txt": sqlite.ProtectionMetadataOnly},
		metadataOnlyResolutions: map[string]struct{}{"denied.txt": {}},
	}
	if !metadataOnlyResolutionQualified(scanner.CaptureModeRootedFD, entry, policy) {
		t.Fatal("stable rooted metadata was not accepted")
	}
	if metadataOnlyResolutionQualified(scanner.CaptureModePathString, entry, policy) {
		t.Fatal("path-string capture was accepted for metadata resolution")
	}
	delete(policy.metadataOnlyResolutions, "denied.txt")
	if metadataOnlyResolutionQualified(scanner.CaptureModeRootedFD, entry, policy) {
		t.Fatal("implicit metadata-only downgrade was accepted")
	}
	policy.metadataOnlyResolutions["denied.txt"] = struct{}{}
	changed := after
	changed.Size++
	entry.After = &changed
	if metadataOnlyResolutionQualified(scanner.CaptureModeRootedFD, entry, policy) {
		t.Fatal("changed metadata was accepted for metadata resolution")
	}
	entry.After = &after
	entry.State = scanner.EntryUnstable
	if metadataOnlyResolutionQualified(scanner.CaptureModeRootedFD, entry, policy) {
		t.Fatal("unstable entry was accepted for metadata resolution")
	}
	entry.State = scanner.EntryFailed
	entry.Issues = []scanner.Issue{{Stage: scanner.StageStability, Code: "SIZE_READ_MISMATCH"}}
	if metadataOnlyResolutionQualified(scanner.CaptureModeRootedFD, entry, policy) {
		t.Fatal("stability failure was accepted for metadata resolution")
	}
	if metadataOnlyScanResolved(scanner.CaptureModeRootedFD, nil, policy) {
		t.Fatal("empty issue set was treated as a metadata-only resolution")
	}
}

func TestMetadataOnlyResolutionAddsPortableDecisionWithoutContent(t *testing.T) {
	before := scanner.MetadataSnapshot{Version: scanner.MetadataVersion, Size: 11, Mode: 0o600}
	after := before
	entry := scanner.EntryRecord{
		RelativePath:    "denied.txt",
		RawRelativePath: []byte("denied.txt"),
		Kind:            scanner.KindRegularFile,
		State:           scanner.EntryFailed,
		Before:          &before,
		After:           &after,
		Boundary:        scanner.BoundaryObservation{Checked: true, Action: scanner.BoundaryInclude},
		Issues:          []scanner.Issue{{Stage: scanner.StageOpen, Code: "OPEN_NOFOLLOW_FAILED"}},
	}
	policy := ingestPolicy{
		mode:                    sqlite.ProtectionStoreExact,
		fileModes:               map[string]sqlite.ProtectionMode{"denied.txt": sqlite.ProtectionMetadataOnly},
		metadataOnlyResolutions: map[string]struct{}{"denied.txt": {}},
	}
	decisions, err := buildProtectionDecisionsWithResolutions([]scanner.EntryRecord{entry}, policy, nil)
	if err != nil {
		t.Fatalf("build decisions: %v", err)
	}
	if len(decisions) != 1 || decisions[0].PlannedOutcome != sqlite.ProtectionExplicitlyUnprotected || decisions[0].ExpectedContentID != "" || decisions[0].ExpectedLogicalBytes != 11 {
		t.Fatalf("metadata-only decision = %+v", decisions)
	}
	knownDigest := sha256.Sum256([]byte("keep"))
	readable := scanner.EntryRecord{
		RelativePath: "keep.bin", RawRelativePath: []byte("keep.bin"), PathID: "path:keep",
		Kind: scanner.KindRegularFile, State: scanner.EntryComplete,
		Content: &scanner.ContentDigest{ContentID: "sha256:" + hex.EncodeToString(knownDigest[:]), BytesRead: 4},
	}
	policy.fileModes["keep.bin"] = sqlite.ProtectionStoreExactWithExternalFallback
	policy.locators = []IngestLocator{{Path: "keep.bin", Locator: "https://example.test/keep.bin"}}
	bound, err := bindIngestLocators([]scanner.EntryRecord{entry, readable}, policy)
	if err != nil {
		t.Fatalf("bind locator beside metadata-only resolution: %v", err)
	}
	if len(bound[readable.PathID]) != 1 {
		t.Fatalf("bound locators = %+v", bound)
	}
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	estimate, err := (&Service{Repo: repo}).estimateIngest(context.Background(), []scanner.EntryRecord{entry, readable}, policy)
	if err != nil {
		t.Fatalf("estimate metadata-only resolution: %v", err)
	}
	if estimate.Files != 2 || estimate.Bytes != 15 || estimate.LocalFiles != 1 || estimate.LocalBytes != 4 {
		t.Fatalf("metadata-only estimate = %+v", estimate)
	}
}

func TestResolvedMetadataOnlyEntryPublishesExplicitCoverageGap(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	filePath := filepath.Join(root, "unreadable.bin")
	if err := os.WriteFile(filePath, []byte("unreadable fixture"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	fileInfo, err := os.Lstat(filePath)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		t.Fatalf("stat root: %v", err)
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	session, err := (&capture.LocalTreeDriver{}).Open(root)
	if err != nil {
		t.Fatalf("open rooted capture: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	sourceID, err := sqlite.NewStableID(sqlite.IDPrefixSource)
	if err != nil {
		t.Fatal(err)
	}
	scanID, err := sqlite.NewStableID(sqlite.IDPrefixScanGeneration)
	if err != nil {
		t.Fatal(err)
	}
	rootMetadata := scanner.MetadataSnapshot{
		Version: scanner.MetadataVersion, Size: rootInfo.Size(), Mode: uint32(rootInfo.Mode()), ModTime: rootInfo.ModTime().UTC(),
	}
	fileMetadata := scanner.MetadataSnapshot{
		Version: scanner.MetadataVersion, Size: fileInfo.Size(), Mode: uint32(fileInfo.Mode()), ModTime: fileInfo.ModTime().UTC(),
	}
	entries := []scanner.EntryRecord{
		{
			GenerationID: scanID, SourceID: sourceID, PathID: "path:root",
			AbsolutePath: root, RelativePath: ".", Name: filepath.Base(root), RawName: []byte(filepath.Base(root)),
			Kind: scanner.KindDirectory, State: scanner.EntryComplete, Before: &rootMetadata, After: &rootMetadata,
			Boundary: scanner.BoundaryObservation{Checked: true, Action: scanner.BoundaryInclude},
		},
		{
			GenerationID: scanID, SourceID: sourceID, PathID: "path:file", ParentPathID: "path:root",
			AbsolutePath: filePath, RelativePath: "unreadable.bin", Name: "unreadable.bin",
			RawName: []byte("unreadable.bin"), RawRelativePath: []byte("unreadable.bin"),
			Kind: scanner.KindRegularFile, State: scanner.EntryFailed, Before: &fileMetadata, After: &fileMetadata,
			Boundary: scanner.BoundaryObservation{Checked: true, Action: scanner.BoundaryInclude},
			Issues:   []scanner.Issue{{Stage: scanner.StageOpen, Code: "OPEN_NOFOLLOW_FAILED", Message: "permission denied"}},
		},
	}
	policy := ingestPolicy{
		mode:                    sqlite.ProtectionStoreExact,
		fileModes:               map[string]sqlite.ProtectionMode{"unreadable.bin": sqlite.ProtectionMetadataOnly},
		metadataOnlyResolutions: map[string]struct{}{"unreadable.bin": {}},
	}
	service := &Service{Store: store, Repo: repo}
	result, err := service.executeCapturedIngest(ctx, capturedIngest{
		session: session, binding: session.Binding(), sourceID: sourceID, scanID: scanID,
		sink: &memorySink{
			start:   scanner.ScanStart{CaptureMode: scanner.CaptureModeRootedFD},
			entries: entries,
		},
		scanResult: scanner.ScanResult{State: scanner.ScanIncomplete, FailedEntries: 1},
	}, policy, map[string][]IngestLocator{})
	if err != nil {
		t.Fatalf("execute metadata-only resolution: %v", err)
	}
	if result.Files != 1 || result.Bytes != fileInfo.Size() || result.LocalFiles != 0 || result.LocalBytes != 0 {
		t.Fatalf("metadata-only result = %+v", result)
	}
	scan, err := store.GetScanGeneration(ctx, result.WorkspaceID, result.ScanID)
	if err != nil {
		t.Fatalf("get scan: %v", err)
	}
	if scan.State != sqlite.ScanIncomplete || scan.FullTraversal {
		t.Fatalf("published scan authority = state=%s full=%t", scan.State, scan.FullTraversal)
	}
	nodes, err := store.ListNamespaceSubtree(ctx, result.WorkspaceID, result.RootID, "")
	if err != nil || len(nodes) != 1 {
		t.Fatalf("metadata-only namespace = %+v, err=%v", nodes, err)
	}
	node := nodes[0].Entry
	if node.EntryType != sqlite.EntryFile || node.ContentID != "" || node.FileVersionID != "" || node.ObservationID == "" {
		t.Fatalf("metadata-only namespace entry = %+v", node)
	}
	observation, err := store.GetObservation(ctx, result.WorkspaceID, node.ObservationID)
	if err != nil || observation.ReadState != string(scanner.EntryFailed) || observation.ContentID != "" || observation.FileVersionID != "" {
		t.Fatalf("metadata-only observation = %+v, err=%v", observation, err)
	}
	protection, err := store.GetProtectionRecordBySubject(ctx, result.WorkspaceID, node.ID)
	if err != nil {
		t.Fatalf("get protection: %v", err)
	}
	if protection.Mode != sqlite.ProtectionMetadataOnly || protection.Outcome != sqlite.ProtectionExplicitlyUnprotected || protection.ExpectedContentID != "" || protection.LocalRepresentationID != "" {
		t.Fatalf("metadata-only protection = %+v", protection)
	}
	manifest, err := readManifest(repo.Root(), result.SnapshotRef)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if len(manifest.Entries) != 2 || manifest.Entries[1].Protection.Outcome != string(sqlite.ProtectionExplicitlyUnprotected) || manifest.Entries[1].ContentID != "" {
		t.Fatalf("metadata-only manifest = %+v", manifest.Entries)
	}
	if _, err := service.VerifyMode(ctx, result.SnapshotRef, VerifyAuthenticatedMetadata, ""); err == nil {
		t.Fatal("authenticated metadata verification hid the unprotected coverage gap")
	}
	destination := filepath.Join(t.TempDir(), "restore")
	if _, err := service.Restore(ctx, result.SnapshotRef, destination); !errors.Is(err, ErrBlocked) {
		t.Fatalf("metadata-only restore error = %v, want ErrBlocked", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("blocked restore created destination: %v", err)
	}
}

func TestApplyIngestPlanWithMatchingBasisPublishes(t *testing.T) {
	ctx := context.Background()
	service, store, repo, root := newPlannerService(t)
	plan, err := service.InspectIngest(ctx, root, IngestOptions{})
	if err != nil {
		t.Fatalf("inspect ingest: %v", err)
	}
	result, err := service.ApplyIngestPlan(ctx, plan)
	if err != nil {
		t.Fatalf("apply ingest plan: %v", err)
	}
	if result.SnapshotRef == "" || result.Files != 1 || result.NewBytes != int64(len("stable payload")) {
		t.Fatalf("unexpected result: %+v", result)
	}
	publications, err := store.ListPublications(ctx)
	if err != nil || len(publications) != 1 {
		t.Fatalf("publications after apply = %d, err=%v", len(publications), err)
	}
	if _, err := repo.Open(ctx, planContentID(t, root)); err != nil {
		t.Fatalf("published repository object missing: %v", err)
	}
}

func TestPerFileProtectionDecisionsAreAppliedAndBoundToPlan(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	payloads := map[string][]byte{
		"exact.txt": []byte("retain locally"),
		"link.txt":  []byte("reacquire externally"),
		"meta.txt":  []byte("record metadata only"),
	}
	for name, payload := range payloads {
		if err := os.WriteFile(filepath.Join(root, name), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{Store: store, Repo: repo, AllowLinkOnly: true, LinkOnlyRequiresConfirmation: true}
	options := IngestOptions{
		ProtectionMode:  sqlite.ProtectionMetadataOnly,
		ConfirmLinkOnly: true,
		FileProtection: map[string]sqlite.ProtectionMode{
			"exact.txt": sqlite.ProtectionStoreExact,
			"link.txt":  sqlite.ProtectionLinkOnly,
		},
		ExternalLocators: []IngestLocator{{Path: "link.txt", Locator: "https://example.test/link.txt"}},
	}
	plan, err := service.InspectIngest(ctx, root, options)
	if err != nil {
		t.Fatalf("inspect mixed protection plan: %v", err)
	}
	if plan.ProtectionDigest == "" || plan.Estimate.LocalFiles != 1 || plan.Estimate.LinkOnlyFiles != 1 {
		t.Fatalf("mixed protection plan = %+v", plan)
	}
	if len(plan.ProtectionDecisions) != 3 {
		t.Fatalf("protection decisions = %+v", plan.ProtectionDecisions)
	}
	for _, decision := range plan.ProtectionDecisions {
		if decision.ExpectedContentID == "" || decision.ReasonCode == "" || decision.PlannedOutcome == "" {
			t.Fatalf("incomplete protection decision = %+v", decision)
		}
	}
	unsigned := plan
	unsigned.ProtectionDigest = ""
	if _, err := service.ApplyIngestPlan(ctx, unsigned); !errors.Is(err, ErrInvalidIngestPlan) {
		t.Fatalf("unsigned protection plan error = %v, want ErrInvalidIngestPlan", err)
	}
	result, err := service.ApplyIngestPlan(ctx, plan)
	if err != nil {
		t.Fatalf("apply mixed protection plan: %v", err)
	}
	if result.ProtectionDigest != plan.ProtectionDigest {
		t.Fatalf("result protection digest = %q, plan = %q", result.ProtectionDigest, plan.ProtectionDigest)
	}
	manifest, err := readManifest(repo.Root(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ProtectionDigest != plan.ProtectionDigest {
		t.Fatalf("manifest protection digest = %q, plan = %q", manifest.ProtectionDigest, plan.ProtectionDigest)
	}
	wantOutcomes := map[string]string{
		"exact.txt": string(sqlite.ProtectionExactFallback),
		"link.txt":  string(sqlite.ProtectionLinkOnlyUnprotected),
		"meta.txt":  string(sqlite.ProtectionExplicitlyUnprotected),
	}
	for _, entry := range manifest.Entries {
		if want, ok := wantOutcomes[entry.RelativePath]; ok && entry.Protection.Outcome != want {
			t.Fatalf("%s outcome = %q, want %q", entry.RelativePath, entry.Protection.Outcome, want)
		}
	}
	if body, err := repo.Open(ctx, planContentIDFor(t, filepath.Join(root, "exact.txt"))); err != nil {
		t.Fatalf("exact payload was not retained: %v", err)
	} else if body != nil {
		_ = body.Close()
	}
	for name := range payloads {
		if name == "exact.txt" {
			continue
		}
		id := planContentIDFor(t, filepath.Join(root, name))
		body, openErr := repo.Open(ctx, id)
		if body != nil {
			_ = body.Close()
		}
		if !errors.Is(openErr, repository.ErrNotFound) {
			t.Fatalf("%s was unexpectedly retained: %v", name, openErr)
		}
	}

	plan.FileProtection["exact.txt"] = sqlite.ProtectionMetadataOnly
	if _, err := service.ApplyIngestPlan(ctx, plan); !errors.Is(err, ErrIngestPlanProtectionChanged) {
		t.Fatalf("tampered protection plan error = %v, want ErrIngestPlanProtectionChanged", err)
	}
}

func planContentIDFor(t *testing.T, filename string) string {
	t.Helper()
	body, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func planContentID(t *testing.T, root string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, "payload.txt"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(digest[:])
}
