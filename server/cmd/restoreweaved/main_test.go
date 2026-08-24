package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/client/transport"
	rwconfig "github.com/ailiheizi/restoreweave/config"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type semanticLeaseFencer struct {
	acquireToken  int64
	acquireCalls  int
	validateCalls int
}

func (f *semanticLeaseFencer) Acquire(context.Context, string, string, string, time.Time, time.Time) (int64, error) {
	f.acquireCalls++
	return f.acquireToken, nil
}

func (f *semanticLeaseFencer) Validate(context.Context, string, string, string, int64, time.Time) error {
	f.validateCalls++
	return nil
}

func (f *semanticLeaseFencer) Release(context.Context, string, string, string, int64, time.Time) error {
	return nil
}

func TestRenewAndValidateSemanticLeaseRenewsAndPreservesToken(t *testing.T) {
	fencer := &semanticLeaseFencer{acquireToken: 7}
	if err := renewAndValidateSemanticLease(context.Background(), fencer, "domain", "owner", "lease", 7, time.Unix(10, 0)); err != nil {
		t.Fatalf("renew and validate: %v", err)
	}
	if fencer.acquireCalls != 1 || fencer.validateCalls != 1 {
		t.Fatalf("calls = acquire %d validate %d, want one each", fencer.acquireCalls, fencer.validateCalls)
	}
}

func TestRenewAndValidateSemanticLeaseRejectsTokenTakeover(t *testing.T) {
	fencer := &semanticLeaseFencer{acquireToken: 8}
	err := renewAndValidateSemanticLease(context.Background(), fencer, "domain", "owner", "lease", 7, time.Unix(10, 0))
	if err == nil || !strings.Contains(err.Error(), "fencing token changed") {
		t.Fatalf("error = %v, want fencing token rejection", err)
	}
	if fencer.validateCalls != 0 {
		t.Fatalf("validate calls = %d, want takeover rejected before validate", fencer.validateCalls)
	}
}

func TestKeepSemanticLeaseAliveInvalidatesOnRenewalFailure(t *testing.T) {
	done := make(chan struct{})
	invalidated := make(chan error, 1)
	go keepSemanticLeaseAlive(context.Background(), time.Millisecond, func(context.Context) error {
		return errors.New("lease renewal failed")
	}, func(err error) {
		invalidated <- err
	}, done)

	select {
	case err := <-invalidated:
		if err == nil || !strings.Contains(err.Error(), "renewal failed") {
			t.Fatalf("invalidation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("keepalive did not invalidate after renewal failure")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("keepalive did not stop after renewal failure")
	}
}

func TestValidateRuntimeStorageProfileRejectsSilentlyIgnoredProfiles(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*rwconfig.Config)
		want   string
	}{
		{
			name: "repository",
			mutate: func(cfg *rwconfig.Config) {
				cfg.Storage.RepositoryProfile = "local-qualified"
			},
			want: "repository profile",
		},
		{
			name: "compression",
			mutate: func(cfg *rwconfig.Config) {
				cfg.Storage.CompressionProfile = "lossless-default"
			},
			want: "compression profile",
		},
		{
			name: "neural codec",
			mutate: func(cfg *rwconfig.Config) {
				cfg.Storage.NeuralCodec = "rwkv-experimental"
			},
			want: "neural codec",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := rwconfig.Default()
			test.mutate(&cfg)
			if err := validateRuntimeStorageProfile(cfg); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validate error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateRuntimeStorageProfileAcceptsCurrentDevelopmentProfile(t *testing.T) {
	if err := validateRuntimeStorageProfile(rwconfig.Default()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRuntimeStorageProfileAcceptsLocalZstdCandidate(t *testing.T) {
	cfg := rwconfig.Default()
	cfg.Storage.RepositoryProfile = rwconfig.RepositoryProfileLocalZstdV1
	cfg.Storage.CompressionProfile = rwconfig.CompressionProfileZstdV1
	if err := validateRuntimeStorageProfile(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestConfigureSemanticBindingRequiresLocalZvecSelection(t *testing.T) {
	resolved := rwconfig.ResolvedConfig{Config: rwconfig.Default(), Digest: "sha256:" + strings.Repeat("a", 64)}
	for _, test := range []struct {
		name   string
		mutate func(*rwconfig.Config)
		want   string
	}{
		{
			name: "online embedding",
			mutate: func(cfg *rwconfig.Config) {
				cfg.Semantic.EmbeddingMode = "online"
			},
			want: "embedding_mode",
		},
		{
			name: "wrong local profile",
			mutate: func(cfg *rwconfig.Config) {
				cfg.Semantic.LocalProfile = "other-profile"
			},
			want: "local_profile",
		},
		{
			name: "wrong vector backend",
			mutate: func(cfg *rwconfig.Config) {
				cfg.Semantic.VectorBackend = "qdrant"
			},
			want: "vector_backend",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := resolved
			test.mutate(&candidate.Config)
			_, _, err := configureSemanticBinding(context.Background(), candidate, nil, filepath.Join(t.TempDir(), "missing-bundle"))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("configure error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRealDaemonAndCLIEndToEndConfiguredIngestAndCleanRecovery(t *testing.T) {
	workspaceRoot := testWorkspaceRoot(t)
	binDir := t.TempDir()
	daemonBin := buildTestBinary(t, workspaceRoot, "./server/cmd/restoreweaved", filepath.Join(binDir, "restoreweaved"))
	rwBin := buildTestBinary(t, workspaceRoot, "./client/cmd/rw", filepath.Join(binDir, "rw"))

	root := t.TempDir()
	dataHome := filepath.Join(root, "xdg-data")
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	want := []byte("configured daemon publication and clean recovery")
	if err := os.WriteFile(filepath.Join(source, "nested", "payload.bin"), want, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config", "config.yaml")
	cliEnv := append(os.Environ(), "XDG_DATA_HOME="+dataHome)
	initConfig := exec.Command(rwBin, "config", "init", "--path", configPath)
	initConfig.Env = cliEnv
	initOutput, err := initConfig.CombinedOutput()
	if err != nil {
		t.Fatalf("rw config init: %v\n%s", err, initOutput)
	}
	if !strings.Contains(string(initOutput), configPath) || !strings.Contains(string(initOutput), "sha256:") {
		t.Fatalf("rw config init output = %s", initOutput)
	}
	validateConfig := exec.Command(rwBin, "--json", "config", "validate", "--path", configPath)
	validateConfig.Env = cliEnv
	validateOutput, err := validateConfig.CombinedOutput()
	if err != nil {
		t.Fatalf("rw config validate: %v\n%s", err, validateOutput)
	}
	if !strings.Contains(string(validateOutput), "config_digest") || !strings.Contains(string(validateOutput), dataHome) {
		t.Fatalf("rw config validate output = %s", validateOutput)
	}
	cfg, err := rwconfig.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Storage.RepositoryProfile = rwconfig.RepositoryProfileLocalZstdV1
	cfg.Storage.CompressionProfile = rwconfig.CompressionProfileZstdV1
	if err := rwconfig.Save(configPath, cfg); err != nil {
		t.Fatalf("save zstd candidate config: %v", err)
	}
	cfg, err = rwconfig.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.RepositoryProfile != rwconfig.RepositoryProfileLocalZstdV1 ||
		cfg.Storage.CompressionProfile != rwconfig.CompressionProfileZstdV1 {
		t.Fatalf("persisted storage tuple = %q/%q", cfg.Storage.RepositoryProfile, cfg.Storage.CompressionProfile)
	}
	validateZstdConfig := exec.Command(rwBin, "--json", "config", "validate", "--path", configPath)
	validateZstdConfig.Env = cliEnv
	validateZstdOutput, err := validateZstdConfig.CombinedOutput()
	if err != nil {
		t.Fatalf("rw config validate zstd candidate: %v\n%s", err, validateZstdOutput)
	}
	if !strings.Contains(string(validateZstdOutput), rwconfig.RepositoryProfileLocalZstdV1) ||
		!strings.Contains(string(validateZstdOutput), rwconfig.CompressionProfileZstdV1) {
		t.Fatalf("rw config validate zstd output = %s", validateZstdOutput)
	}
	for _, path := range []string{cfg.Paths.Catalog, cfg.Paths.Repository, cfg.Paths.Vectors, cfg.Paths.RecoveryRecords} {
		if !strings.HasPrefix(path, dataHome+string(filepath.Separator)) {
			t.Fatalf("persisted config path escaped XDG_DATA_HOME: %s", path)
		}
	}

	socketPath := fmt.Sprintf("/tmp/rw-e2e-%d-%d.sock", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	daemon, daemonDone, daemonLog := startDaemonProcessWithEnv(t, cliEnv, daemonBin, socketPath, "--config", configPath)
	defer stopDaemonProcess(t, daemon, daemonDone, daemonLog)
	waitForCLI(t, rwBin, socketPath, daemonDone, daemonLog, "status")

	capabilityResult := runRWProcess(t, rwBin, socketPath, "capability", "list")
	var capabilityData command.CapabilityListData
	decodeProcessResult(t, capabilityResult, &capabilityData)
	semanticUnavailable := false
	for _, capability := range capabilityData.Capabilities {
		if capability.Kind == search.CapabilityKindDimension && capability.ID == search.DimensionSemantic {
			semanticUnavailable = capability.State == command.CapabilityUnavailable &&
				strings.Contains(capability.Notes, search.SemanticIndexUnavailableReason)
		}
	}
	if !semanticUnavailable {
		t.Fatalf("real daemon did not report unavailable semantic dependency: %+v", capabilityData.Capabilities)
	}

	planned := runRWProcess(t, rwBin, socketPath, "ingest", source)
	var ingest command.PlanIngestData
	decodeProcessResult(t, planned, &ingest)
	if ingest.State != "READY" || !ingest.Executable || ingest.PlanID == "" || ingest.PlanDigest == "" || ingest.Files != 1 {
		t.Fatalf("real CLI ingest plan = %+v", ingest)
	}
	if ingest.ConfigDigest == "" || ingest.SourceBasisDigest == "" || ingest.ProtectionDigest == "" {
		t.Fatalf("ingest plan missing authenticated planning inputs = %+v", ingest)
	}
	if len(ingest.ProtectionDecisions) != 1 ||
		(ingest.ProtectionDecisions[0].PlannedOutcome != "EXACT_PROTECTED" && ingest.ProtectionDecisions[0].PlannedOutcome != "EXACT_FALLBACK") {
		t.Fatalf("ingest protection decisions = %+v", ingest.ProtectionDecisions)
	}

	applied := runRWProcess(t, rwBin, socketPath, "plan", "apply", ingest.PlanID,
		"--workspace", ingest.WorkspaceID, "--digest", ingest.PlanDigest)
	var appliedData command.PlanApplyData
	decodeProcessResult(t, applied, &appliedData)
	if appliedData.SnapshotRef == "" || appliedData.ManifestDigest == "" || appliedData.ProtectionDigest == "" || appliedData.Files != 1 {
		t.Fatalf("real CLI plan apply did not publish signed exact result = %+v", appliedData)
	}
	resolved := runRWProcess(t, rwBin, socketPath, "namespace", "resolve", "nested/payload.bin",
		"--workspace", appliedData.WorkspaceID, "--root", appliedData.RootID)
	var resolvedData command.NamespaceResolveData
	decodeProcessResult(t, resolved, &resolvedData)
	if resolvedData.PathRef == "" || resolvedData.Entry.ContentID == "" {
		t.Fatalf("real CLI namespace resolve = %+v", resolvedData)
	}
	created := runRWProcess(t, rwBin, socketPath, "description", "create", resolvedData.PathRef,
		"--workspace", appliedData.WorkspaceID, "--kind", "USER", "--language", "en",
		"--title", "Recovery note", "--body", "A flooded city archive is ready for recovery.", "--accepted")
	var createdData command.DescriptionCreateData
	decodeProcessResult(t, created, &createdData)
	if createdData.Document.ID == "" || createdData.Document.SubjectRef != resolvedData.PathRef || len(createdData.Document.Segments) == 0 {
		t.Fatalf("real CLI description create = %+v", createdData.Document)
	}
	searched := runRWProcess(t, rwBin, socketPath, "search", "flooded",
		"--workspace", appliedData.WorkspaceID, "--filter", "language=EN", "--filter", "suffix=bin")
	var searchData command.SearchQueryData
	decodeProcessResult(t, searched, &searchData)
	if len(searchData.Hits) != 1 || searchData.Hits[0].SubjectRef != resolvedData.PathRef || len(searchData.Hits[0].Segments) == 0 {
		t.Fatalf("real CLI filtered search = %+v", searchData)
	}
	segment := searchData.Hits[0].Segments[0]
	if segment.DescriptionDocumentID != createdData.Document.ID || segment.MatchedText == "" ||
		segment.Producer == "" || !strings.EqualFold(segment.Language, "en") || !segment.Accepted {
		t.Fatalf("real CLI segment provenance = %+v", segment)
	}
	wrongLanguage := runRWProcess(t, rwBin, socketPath, "search", "flooded",
		"--workspace", appliedData.WorkspaceID, "--filter", "language=fr", "--filter", "suffix=bin")
	var wrongLanguageData command.SearchQueryData
	decodeProcessResult(t, wrongLanguage, &wrongLanguageData)
	if len(wrongLanguageData.Hits) != 0 {
		t.Fatalf("real CLI wrong-language filter hits = %+v, want none", wrongLanguageData.Hits)
	}
	tagged := runRWProcess(t, rwBin, socketPath, "tag", "add", resolvedData.PathRef, "curated",
		"--workspace", appliedData.WorkspaceID)
	var taggedData command.AnnotationUpsertData
	decodeProcessResult(t, tagged, &taggedData)
	if taggedData.Annotation.ID == "" || taggedData.Annotation.Kind != "TAG" ||
		taggedData.Annotation.Body != "curated" || taggedData.Annotation.SubjectRef != resolvedData.PathRef {
		t.Fatalf("real CLI tag add = %+v", taggedData.Annotation)
	}
	noted := runRWProcess(t, rwBin, socketPath, "note", "set", resolvedData.PathRef,
		"--workspace", appliedData.WorkspaceID, "--body", "retain exact bytes")
	var notedData command.AnnotationUpsertData
	decodeProcessResult(t, noted, &notedData)
	if notedData.Annotation.ID == "" || notedData.Annotation.Kind != "NOTE" ||
		notedData.Annotation.Body != "retain exact bytes" || notedData.Annotation.SubjectRef != resolvedData.PathRef {
		t.Fatalf("real CLI note set = %+v", notedData.Annotation)
	}
	annotationSearch := runRWProcess(t, rwBin, socketPath, "search", "curated",
		"--workspace", appliedData.WorkspaceID, "--axis", "tags")
	var annotationSearchData command.SearchQueryData
	decodeProcessResult(t, annotationSearch, &annotationSearchData)
	if len(annotationSearchData.Hits) != 1 || annotationSearchData.Hits[0].SubjectRef != resolvedData.PathRef {
		t.Fatalf("real CLI annotation search = %+v", annotationSearchData.Hits)
	}
	viewSaved := runRWProcess(t, rwBin, socketPath, "view", "save", "recoverable", "flooded")
	var viewData command.ViewData
	decodeProcessResult(t, viewSaved, &viewData)
	if viewData.ViewID == "" || viewData.Name != "recoverable" || viewData.Query != "flooded" || viewData.Revision != 1 {
		t.Fatalf("real CLI view save = %+v", viewData)
	}
	viewEvaluated := runRWProcess(t, rwBin, socketPath, "view", "evaluate", "recoverable", "--limit", "10")
	var viewEvaluateData command.ViewEvaluateData
	decodeProcessResult(t, viewEvaluated, &viewEvaluateData)
	if viewEvaluateData.ViewID != viewData.ViewID || len(viewEvaluateData.Hits) != 1 ||
		viewEvaluateData.Hits[0].SubjectRef != resolvedData.PathRef {
		t.Fatalf("real CLI view evaluate = %+v", viewEvaluateData)
	}
	manifestPlanned := runRWProcess(t, rwBin, socketPath, "export", "plan", "--view", "recoverable")
	var manifestData command.ExportManifestData
	decodeProcessResult(t, manifestPlanned, &manifestData)
	if manifestData.ManifestID == "" || manifestData.ManifestDigest == "" || manifestData.ViewID != viewData.ViewID ||
		manifestData.SubjectCount != 1 || len(manifestData.Items) != 1 {
		t.Fatalf("real CLI export plan = %+v", manifestData)
	}
	exportDestination := filepath.Join(root, "exported")
	materialized := runRWProcess(t, rwBin, socketPath, "export", "apply", manifestData.ManifestID, exportDestination)
	var materializedData command.ExportApplyVerifyData
	decodeProcessResult(t, materialized, &materializedData)
	if !materializedData.Verified || materializedData.ManifestID != manifestData.ManifestID || materializedData.Items != 1 {
		t.Fatalf("real CLI export apply = %+v", materializedData)
	}
	exportVerified := runRWProcess(t, rwBin, socketPath, "export", "verify", manifestData.ManifestID, exportDestination)
	var exportVerifiedData command.ExportApplyVerifyData
	decodeProcessResult(t, exportVerified, &exportVerifiedData)
	if !exportVerifiedData.Verified || exportVerifiedData.ManifestDigest != manifestData.ManifestDigest || exportVerifiedData.Items != 1 {
		t.Fatalf("real CLI export verify = %+v", exportVerifiedData)
	}
	exportedBytes, err := os.ReadFile(filepath.Join(exportDestination, "payload.bin"))
	if err != nil || !bytes.Equal(exportedBytes, want) {
		t.Fatalf("real CLI exported bytes = %q, err=%v", exportedBytes, err)
	}
	profileMarker, err := os.ReadFile(filepath.Join(cfg.Paths.Repository, "repository.profile"))
	if err != nil {
		t.Fatalf("read configured repository profile marker: %v", err)
	}
	if strings.TrimSpace(string(profileMarker)) != rwconfig.RepositoryProfileLocalZstdV1 {
		t.Fatalf("configured repository profile marker = %q", strings.TrimSpace(string(profileMarker)))
	}
	if !repositoryHasZstdPayload(t, cfg.Paths.Repository) {
		t.Fatalf("configured zstd repository has no zstd payload")
	}

	referencePath := filepath.Join(root, "portable", "recovery-reference.json")
	anchorPath := filepath.Join(root, "portable", "trust-anchor.json")
	exported := runRWProcess(t, rwBin, socketPath, "recovery", "export", appliedData.SnapshotRef, referencePath)
	var exportData command.RecoveryExportData
	decodeProcessResult(t, exported, &exportData)
	if !exportData.IndependentlyStored || exportData.ManifestDigest != appliedData.ManifestDigest || exportData.Files != 1 {
		t.Fatalf("real CLI recovery export = %+v", exportData)
	}
	anchored := runRWProcess(t, rwBin, socketPath, "recovery", "anchor", "export", anchorPath)
	var anchorData command.RecoveryAnchorExportData
	decodeProcessResult(t, anchored, &anchorData)
	if anchorData.ArtifactPath != anchorPath || anchorData.KeyID == "" || anchorData.PublicKeyDigest == "" {
		t.Fatalf("real CLI trust-anchor export = %+v", anchorData)
	}

	if err := daemon.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-daemonDone:
		if err != nil {
			t.Fatalf("configured daemon exit: %v\n%s", err, daemonLog.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("configured daemon did not stop\n%s", daemonLog.String())
	}
	effective, err := rwconfig.LoadEffective(rwconfig.LoadOptions{
		Path: configPath,
		ResolveOptions: rwconfig.ResolveOptions{
			Environ: map[string]string{"XDG_DATA_HOME": dataHome},
		},
	})
	if err != nil {
		t.Fatalf("load effective config for generation binding: %v", err)
	}
	catalog, err := sqlite.Open(context.Background(), cfg.Paths.Catalog, sqlite.Options{})
	if err != nil {
		t.Fatalf("open configured catalog for generation binding: %v", err)
	}
	generation, generationErr := catalog.LatestIndexGeneration(context.Background(), appliedData.WorkspaceID, search.DimensionLexical)
	closeErr := catalog.Close()
	if generationErr != nil {
		t.Fatalf("read configured lexical generation: %v", generationErr)
	}
	if closeErr != nil {
		t.Fatalf("close configured catalog: %v", closeErr)
	}
	wantLexicalProfile := search.ProfileDigest(search.DimensionLexical, search.LexicalProfileV1)
	if generation.ConfigDigest != effective.Digest || generation.ProviderProfileDigest != wantLexicalProfile {
		t.Fatalf("real daemon lexical generation binding = %+v, want config=%s profile=%s",
			generation, effective.Digest, wantLexicalProfile)
	}
	vectorRoot := filepath.Clean(cfg.Paths.Vectors)
	generationPath := filepath.Clean(generation.DBPath)
	vectorPrefix := vectorRoot + string(filepath.Separator)
	if !strings.HasPrefix(generationPath, vectorPrefix) {
		t.Fatalf("real daemon lexical generation path = %q, want under configured vectors %q", generation.DBPath, cfg.Paths.Vectors)
	}
	if strings.HasPrefix(generationPath, filepath.Join(cfg.Paths.Repository, "indexes")+string(filepath.Separator)) {
		t.Fatalf("real daemon lexical generation unexpectedly uses repository-relative indexes: %q", generation.DBPath)
	}
	if _, err := os.Stat(generation.DBPath); err != nil {
		t.Fatalf("configured lexical generation file is not readable: %v", err)
	}
	if err := os.Remove(cfg.Paths.Catalog); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(cfg.Paths.RecoveryRecords); err != nil {
		t.Fatal(err)
	}

	cleanSocket := fmt.Sprintf("/tmp/rw-e2e-clean-%d-%d.sock", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() { _ = os.Remove(cleanSocket) })
	repositoryBefore := recoveryProcessTree(t, cfg.Paths.Repository)
	cleanDaemon, cleanDone, cleanLog := startDaemonProcessWithEnv(t, cliEnv, daemonBin, cleanSocket,
		"--recovery-reader", "--repository", cfg.Paths.Repository,
		"--recovery-reference", referencePath, "--trust-anchor", anchorPath)
	defer stopDaemonProcess(t, cleanDaemon, cleanDone, cleanLog)
	waitForCLI(t, rwBin, cleanSocket, cleanDone, cleanLog, "snapshot", "list")

	imported := runRWProcess(t, rwBin, cleanSocket, "recovery", "import", referencePath, anchorPath)
	var importData command.RecoveryImportData
	decodeProcessResult(t, imported, &importData)
	if importData.Schema != exact.RecoveryReferenceSchemaV2 || importData.SnapshotRef != appliedData.SnapshotRef ||
		importData.FactHealth != exact.RecoveryFactHealthComplete || importData.CatalogCreated {
		t.Fatalf("clean-install import = %+v", importData)
	}
	verified := runRWProcess(t, rwBin, cleanSocket, "snapshot", "verify", appliedData.SnapshotRef, "--mode", command.VerifyFullBytes)
	var verifyData command.SnapshotVerifyData
	decodeProcessResult(t, verified, &verifyData)
	if !verifyData.OK || verifyData.CatalogUsed || verifyData.PassedFiles != 1 {
		t.Fatalf("clean-install verify = %+v", verifyData)
	}
	destination := filepath.Join(root, "restored")
	plannedRestore := runRWProcess(t, rwBin, cleanSocket, "restore", appliedData.SnapshotRef, destination)
	var restorePlan command.PlanRestoreData
	decodeProcessResult(t, plannedRestore, &restorePlan)
	if restorePlan.Wrote || !restorePlan.Executable || restorePlan.PlanID == "" || restorePlan.PlanDigest == "" {
		t.Fatalf("clean-install restore plan = %+v", restorePlan)
	}
	rawApplied := runRWProcess(t, rwBin, cleanSocket, "plan", "apply", restorePlan.PlanID,
		"--workspace", restorePlan.WorkspaceID, "--digest", restorePlan.PlanDigest)
	var restoreData command.PlanApplyData
	decodeProcessResult(t, rawApplied, &restoreData)
	if restoreData.SnapshotRef != appliedData.SnapshotRef || restoreData.Files != 1 {
		t.Fatalf("clean-install restore apply = %+v", restoreData)
	}
	got, err := os.ReadFile(filepath.Join(destination, "nested", "payload.bin"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("clean-install restored bytes = %q, err=%v", got, err)
	}
	if _, err := os.Stat(cfg.Paths.Catalog); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clean-install reader created catalog: %v", err)
	}
	if repositoryAfter := recoveryProcessTree(t, cfg.Paths.Repository); !reflect.DeepEqual(repositoryAfter, repositoryBefore) {
		t.Fatalf("clean-install reader changed repository tree\nbefore=%v\nafter=%v", repositoryBefore, repositoryAfter)
	}
}

// TestRealDaemonCrossProcessFenceSerializesPublication proves the production
// daemon wiring, rather than only the exact package seam: two independent
// restoreweaved processes sharing one catalog and repository can publish
// concurrently, but signed root commits remain one authenticated generation
// chain with strictly increasing fence tokens.
func TestRealDaemonCrossProcessFenceSerializesPublication(t *testing.T) {
	workspaceRoot := testWorkspaceRoot(t)
	binDir := t.TempDir()
	daemonBin := buildTestBinary(t, workspaceRoot, "./server/cmd/restoreweaved", filepath.Join(binDir, "restoreweaved"))
	rwBin := buildTestBinary(t, workspaceRoot, "./client/cmd/rw", filepath.Join(binDir, "rw"))

	root := t.TempDir()
	cfg := rwconfig.Default()
	cfg.Paths = rwconfig.Paths{
		Catalog:         filepath.Join(root, "catalog.sqlite"),
		Repository:      filepath.Join(root, "repository"),
		Vectors:         filepath.Join(root, "vectors"),
		RecoveryRecords: filepath.Join(root, "recovery"),
	}
	cfg.Recovery.PublicationDomain = "workspace:daemon-fence-test"
	configPath := filepath.Join(root, "config.yaml")
	if err := rwconfig.Save(configPath, cfg); err != nil {
		t.Fatalf("save daemon fence config: %v", err)
	}
	if _, _, err := exact.OpenSigningMaterial(cfg.Paths.RecoveryRecords, cfg.Recovery.PublicationDomain, true); err != nil {
		t.Fatalf("initialize daemon fence signing material: %v", err)
	}

	sourceA := filepath.Join(root, "source-a")
	sourceB := filepath.Join(root, "source-b")
	if err := os.MkdirAll(sourceA, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sourceB, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceA, "a.txt"), []byte("daemon fence A"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceB, "b.txt"), []byte("daemon fence B"), 0o600); err != nil {
		t.Fatal(err)
	}

	socketPrefix := fmt.Sprintf("/tmp/rw-fence-%d-%d", os.Getpid(), time.Now().UnixNano())
	socketA := socketPrefix + "-a.sock"
	socketB := socketPrefix + "-b.sock"
	t.Cleanup(func() {
		_ = os.Remove(socketA)
		_ = os.Remove(socketB)
	})
	processA, doneA, logA := startDaemonProcess(t, daemonBin, socketA, "--config", configPath)
	waitForCLI(t, rwBin, socketA, doneA, logA, "status")
	processB, doneB, logB := startDaemonProcess(t, daemonBin, socketB, "--config", configPath)
	defer stopDaemonProcess(t, processA, doneA, logA)
	defer stopDaemonProcess(t, processB, doneB, logB)
	waitForCLI(t, rwBin, socketB, doneB, logB, "status")

	plannedA := runRWProcess(t, rwBin, socketA, "ingest", sourceA)
	plannedB := runRWProcess(t, rwBin, socketB, "ingest", sourceB)
	var planA, planB command.PlanIngestData
	decodeProcessResult(t, plannedA, &planA)
	decodeProcessResult(t, plannedB, &planB)
	if planA.WorkspaceID == "" || planA.PlanID == "" || planA.PlanDigest == "" ||
		planB.WorkspaceID == "" || planB.PlanID == "" || planB.PlanDigest == "" {
		t.Fatalf("daemon fence plans are incomplete: A=%+v B=%+v", planA, planB)
	}
	if planA.WorkspaceID != planB.WorkspaceID {
		t.Fatalf("shared daemon workspace IDs differ: %q vs %q", planA.WorkspaceID, planB.WorkspaceID)
	}

	type applyOutcome struct {
		result command.Result
		err    error
		output []byte
	}
	apply := func(socket string, plan command.PlanIngestData, outcomes chan<- applyOutcome, start <-chan struct{}) {
		<-start
		cmd := exec.Command(rwBin, "--socket", socket, "--json", "plan", "apply", plan.PlanID,
			"--workspace", plan.WorkspaceID, "--digest", plan.PlanDigest)
		output, err := cmd.CombinedOutput()
		outcome := applyOutcome{err: err, output: output}
		if unmarshalErr := json.Unmarshal(output, &outcome.result); unmarshalErr != nil && err == nil {
			outcome.err = unmarshalErr
		}
		outcomes <- outcome
	}
	start := make(chan struct{})
	outcomes := make(chan applyOutcome, 2)
	go apply(socketA, planA, outcomes, start)
	go apply(socketB, planB, outcomes, start)
	close(start)
	first, second := <-outcomes, <-outcomes
	for i, outcome := range []applyOutcome{first, second} {
		if outcome.err != nil || (outcome.result.Status != command.StatusSucceeded && outcome.result.Status != command.StatusDegraded) {
			t.Fatalf("daemon fence apply %d failed: err=%v result=%+v output=%s", i, outcome.err, outcome.result, outcome.output)
		}
	}

	if err := processA.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := processB.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-doneA:
		if err != nil {
			t.Fatalf("daemon A exit: %v\n%s", err, logA.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("daemon A did not stop\n%s", logA.String())
	}
	select {
	case err := <-doneB:
		if err != nil {
			t.Fatalf("daemon B exit: %v\n%s", err, logB.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("daemon B did not stop\n%s", logB.String())
	}

	anchor, err := exact.LoadTrustAnchor(filepath.Join(cfg.Paths.RecoveryRecords, "publication-trust-anchor.json"))
	if err != nil {
		t.Fatalf("load daemon fence anchor: %v", err)
	}
	repo, err := repository.OpenDir(cfg.Paths.Repository)
	if err != nil {
		t.Fatalf("open daemon fence repository: %v", err)
	}
	digests, err := repo.ListRecordDigests(context.Background(), repository.RecordPublicationCommit)
	if err != nil {
		t.Fatalf("list daemon fence commits: %v", err)
	}
	if len(digests) != 2 {
		t.Fatalf("daemon fence commit count = %d, want two serialized publications", len(digests))
	}
	commits := make([]exact.PublicationCommitRecord, 0, len(digests))
	for _, digest := range digests {
		body, openErr := repo.OpenRecord(context.Background(), repository.RecordPublicationCommit, digest)
		if openErr != nil {
			t.Fatalf("open daemon fence commit %s: %v", digest, openErr)
		}
		payload, readErr := io.ReadAll(body)
		_ = body.Close()
		if readErr != nil {
			t.Fatalf("read daemon fence commit %s: %v", digest, readErr)
		}
		var commit exact.PublicationCommitRecord
		if err := json.Unmarshal(payload, &commit); err != nil {
			t.Fatalf("decode daemon fence commit %s: %v", digest, err)
		}
		if err := commit.Verify(anchor); err != nil {
			t.Fatalf("verify daemon fence commit %s: %v", digest, err)
		}
		commits = append(commits, commit)
	}
	sort.Slice(commits, func(i, j int) bool { return commits[i].Generation < commits[j].Generation })
	if commits[0].Generation != 1 || commits[1].Generation != 2 ||
		commits[0].FenceToken < 1 || commits[1].FenceToken <= commits[0].FenceToken ||
		commits[1].ParentCommitDigest == "" {
		t.Fatalf("daemon fence lineage = %+v, want generations 1->2 and strictly increasing tokens with parent", commits)
	}
}

func TestRecoveryReaderDaemonIsCatalogAndSigningMaterialFree(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	want := []byte("real recovery reader daemon")
	if err := os.WriteFile(filepath.Join(source, "payload.bin"), want, 0o600); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(t.TempDir(), "catalog.sqlite")
	store, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Join(t.TempDir(), "repository")
	repo, err := repository.OpenDir(repoRoot)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	identityDir := t.TempDir()
	identity, anchor, err := exact.OpenSigningMaterial(identityDir, exact.DefaultPublicationDomain, true)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	writer := &exact.Service{
		Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: exact.DefaultPublicationDomain, RequireSignedPublication: true,
	}
	plan, err := writer.InspectIngest(ctx, source, exact.IngestOptions{})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	result, err := writer.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:recovery-reader-daemon-plan")
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "recovery-bundle.json")
	if _, err := writer.ExportRecovery(ctx, result.SnapshotRef, bundlePath); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	referencePath := filepath.Join(t.TempDir(), "recovery-reference.json")
	if _, err := writer.ExportRecoveryReference(ctx, result.SnapshotRef, referencePath); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	anchorPath := filepath.Join(t.TempDir(), "trust-anchor.json")
	if _, err := exact.ExportTrustAnchor(anchor, anchorPath); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(catalogPath); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(identityDir); err != nil {
		t.Fatal(err)
	}

	socketPath := filepath.Join("/tmp", "rw-recovery-"+filepath.Base(t.TempDir())+".sock")
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	readerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	readerErr := make(chan error, 1)
	go func() {
		readerErr <- runWithOptions(readerCtx, daemonOptions{
			socketPath: socketPath, recoveryReader: true,
			recoveryReference: referencePath, trustAnchorPath: anchorPath,
			repositoryPath: repoRoot,
		})
	}()

	conn := waitForRecoveryReader(t, socketPath, readerErr)
	defer conn.Close()
	call := func(operation string, input any) command.Result {
		t.Helper()
		payload, marshalErr := json.Marshal(input)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		result, callErr := conn.Do(context.Background(), command.Envelope{Operation: operation, Input: payload})
		if callErr != nil {
			t.Fatal(callErr)
		}
		return result
	}
	imported := call(command.OpRecoveryImport, map[string]any{
		"artifact_path": bundlePath, "trust_anchor_path": anchorPath,
	})
	if imported.Status != command.StatusSucceeded {
		t.Fatalf("recovery.import = %s, reasons=%+v", imported.Status, imported.Reasons)
	}
	listed := call(command.OpSnapshotList, map[string]any{})
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("snapshot.list = %s, reasons=%+v", listed.Status, listed.Reasons)
	}
	verified := call(command.OpSnapshotVerify, map[string]any{
		"snapshot_ref": result.SnapshotRef, "mode": command.VerifyFullBytes,
	})
	if verified.Status != command.StatusSucceeded {
		t.Fatalf("snapshot.verify = %s, reasons=%+v", verified.Status, verified.Reasons)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	planned := call(command.OpPlanRestore, map[string]any{
		"snapshot_ref": result.SnapshotRef, "destination": destination,
	})
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.restore = %s, reasons=%+v", planned.Status, planned.Reasons)
	}
	var restorePlan command.PlanRestoreData
	if err := json.Unmarshal(planned.Data, &restorePlan); err != nil {
		t.Fatal(err)
	}
	applied := call(command.OpPlanApply, map[string]any{
		"workspace_id": restorePlan.WorkspaceID, "plan_id": restorePlan.PlanID,
		"plan_digest": restorePlan.PlanDigest,
	})
	if applied.Status != command.StatusSucceeded {
		t.Fatalf("plan.apply = %s, reasons=%+v", applied.Status, applied.Reasons)
	}
	got, err := os.ReadFile(filepath.Join(destination, "payload.bin"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("restored payload = %q, err=%v", got, err)
	}
	if _, err := os.Stat(catalogPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery reader created catalog: stat err=%v", err)
	}
	for _, path := range []string{identityDir, filepath.Join(repoRoot, "indexes"), filepath.Join(repoRoot, "staging")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("recovery reader created runtime state %s: stat err=%v", path, err)
		}
	}
	cancel()
	select {
	case err := <-readerErr:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recovery reader did not stop")
	}
}

func TestRecoveryReaderCleanInstallUsesRealDaemonAndCLIProcesses(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	want := []byte("clean install across real daemon and CLI processes")
	if err := os.WriteFile(filepath.Join(source, "payload.bin"), want, 0o600); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(t.TempDir(), "writer-catalog.sqlite")
	store, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Join(t.TempDir(), "repository")
	repo, err := repository.OpenDir(repoRoot)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	signingRoot := t.TempDir()
	identity, anchor, err := exact.OpenSigningMaterial(signingRoot, exact.DefaultPublicationDomain, true)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	writer := &exact.Service{
		Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: exact.DefaultPublicationDomain, RequireSignedPublication: true,
	}
	plan, err := writer.InspectIngest(ctx, source, exact.IngestOptions{})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	published, err := writer.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:real-process-clean-install")
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	referencePath := filepath.Join(t.TempDir(), "recovery-reference.json")
	if _, err := writer.ExportRecoveryReference(ctx, published.SnapshotRef, referencePath); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	anchorPath := filepath.Join(t.TempDir(), "trust-anchor.json")
	if _, err := exact.ExportTrustAnchor(anchor, anchorPath); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(catalogPath); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(signingRoot); err != nil {
		t.Fatal(err)
	}
	repositoryBefore := recoveryProcessTree(t, repoRoot)

	workspaceRoot := testWorkspaceRoot(t)
	binDir := t.TempDir()
	daemonBin := buildTestBinary(t, workspaceRoot, "./server/cmd/restoreweaved", filepath.Join(binDir, "restoreweaved"))
	rwBin := buildTestBinary(t, workspaceRoot, "./client/cmd/rw", filepath.Join(binDir, "rw"))
	socketPath := fmt.Sprintf("/tmp/rw-clean-%d-%d.sock", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	probeRoot := t.TempDir()
	catalogProbe := filepath.Join(probeRoot, "must-not-create", "catalog.sqlite")
	recoveryProbe := filepath.Join(probeRoot, "must-not-create", "signing")
	vectorsProbe := filepath.Join(probeRoot, "must-not-create", "vectors")
	daemon := exec.Command(daemonBin,
		"--recovery-reader", "--repository", repoRoot,
		"--recovery-reference", referencePath, "--trust-anchor", anchorPath,
		"--socket", socketPath,
	)
	daemon.Env = append(os.Environ(),
		"RESTOREWEAVE_CONFIG="+filepath.Join(probeRoot, "missing-config.yaml"),
		"RESTOREWEAVE_CATALOG="+catalogProbe,
		"RESTOREWEAVE_RECOVERY_RECORDS="+recoveryProbe,
		"RESTOREWEAVE_VECTORS="+vectorsProbe,
	)
	var daemonLog bytes.Buffer
	daemon.Stdout = &daemonLog
	daemon.Stderr = &daemonLog
	if err := daemon.Start(); err != nil {
		t.Fatalf("start recovery reader process: %v", err)
	}
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- daemon.Wait() }()
	stopped := false
	defer func() {
		if stopped {
			return
		}
		_ = daemon.Process.Kill()
		<-daemonDone
	}()
	waitForReaderCLI(t, rwBin, socketPath, daemonDone, &daemonLog)

	imported := runRWProcess(t, rwBin, socketPath, "recovery", "import", referencePath, anchorPath)
	var importData command.RecoveryImportData
	decodeProcessResult(t, imported, &importData)
	if importData.Schema != exact.RecoveryReferenceSchemaV2 || importData.SnapshotRef != published.SnapshotRef || importData.CatalogCreated {
		t.Fatalf("real CLI recovery.import = %+v", importData)
	}
	listed := runRWProcess(t, rwBin, socketPath, "snapshot", "list")
	var listData command.SnapshotListData
	decodeProcessResult(t, listed, &listData)
	if len(listData.Snapshots) != 1 || listData.Snapshots[0].SnapshotRef != published.SnapshotRef {
		t.Fatalf("real CLI snapshot.list = %+v", listData)
	}
	verified := runRWProcess(t, rwBin, socketPath, "snapshot", "verify", published.SnapshotRef, "--mode", command.VerifyFullBytes)
	var verifyData command.SnapshotVerifyData
	decodeProcessResult(t, verified, &verifyData)
	if !verifyData.OK || verifyData.CatalogUsed {
		t.Fatalf("real CLI snapshot.verify = %+v", verifyData)
	}

	destination := filepath.Join(t.TempDir(), "restored")
	planned := runRWProcess(t, rwBin, socketPath, "restore", published.SnapshotRef, destination)
	var restorePlan command.PlanRestoreData
	decodeProcessResult(t, planned, &restorePlan)
	if restorePlan.Wrote || restorePlan.PlanID == "" || restorePlan.PlanDigest == "" || restorePlan.WorkspaceID == "" {
		t.Fatalf("real CLI restore plan = %+v", restorePlan)
	}
	applied := runRWProcess(t, rwBin, socketPath, "plan", "apply", restorePlan.PlanID,
		"--workspace", restorePlan.WorkspaceID, "--digest", restorePlan.PlanDigest)
	var applyData command.PlanApplyData
	decodeProcessResult(t, applied, &applyData)
	if applyData.SnapshotRef != published.SnapshotRef || applyData.Files != 1 {
		t.Fatalf("real CLI plan.apply = %+v", applyData)
	}
	got, err := os.ReadFile(filepath.Join(destination, "payload.bin"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("real process restored payload = %q, err=%v", got, err)
	}
	if after := recoveryProcessTree(t, repoRoot); !reflect.DeepEqual(after, repositoryBefore) {
		t.Fatalf("recovery reader process changed repository tree\nbefore=%v\nafter=%v", repositoryBefore, after)
	}
	for _, path := range []string{catalogPath, signingRoot, catalogProbe, recoveryProbe, vectorsProbe, filepath.Join(repoRoot, "indexes"), filepath.Join(repoRoot, "staging")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("recovery reader process created forbidden state %s: %v", path, err)
		}
	}

	if err := daemon.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("stop recovery reader process: %v", err)
	}
	select {
	case err := <-daemonDone:
		stopped = true
		if err != nil {
			t.Fatalf("recovery reader process exit: %v\n%s", err, daemonLog.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("recovery reader process did not stop\n%s", daemonLog.String())
	}
}

func testWorkspaceRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("workspace go.mod not found")
		}
		dir = parent
	}
}

func buildTestBinary(t *testing.T, workspaceRoot, target, destination string) string {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", destination, target)
	cmd.Dir = workspaceRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", target, err, output)
	}
	return destination
}

func startDaemonProcess(t *testing.T, daemonBin, socketPath string, args ...string) (*exec.Cmd, <-chan error, *bytes.Buffer) {
	return startDaemonProcessWithEnv(t, os.Environ(), daemonBin, socketPath, args...)
}

func startDaemonProcessWithEnv(t *testing.T, env []string, daemonBin, socketPath string, args ...string) (*exec.Cmd, <-chan error, *bytes.Buffer) {
	t.Helper()
	commandArgs := append([]string{}, args...)
	commandArgs = append(commandArgs, "--socket", socketPath)
	process := exec.Command(daemonBin, commandArgs...)
	process.Env = env
	var log bytes.Buffer
	process.Stdout = &log
	process.Stderr = &log
	if err := process.Start(); err != nil {
		t.Fatalf("start restoreweaved process: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	return process, done, &log
}

func stopDaemonProcess(t *testing.T, process *exec.Cmd, done <-chan error, daemonLog *bytes.Buffer) {
	t.Helper()
	if process == nil || process.Process == nil {
		return
	}
	if err := process.Process.Signal(os.Interrupt); err != nil {
		return
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("restoreweaved process exit: %v\n%s", err, daemonLog.String())
		}
	case <-time.After(5 * time.Second):
		_ = process.Process.Kill()
		t.Errorf("restoreweaved process did not stop\n%s", daemonLog.String())
	}
}

func waitForCLI(t *testing.T, rwBin, socketPath string, daemonDone <-chan error, daemonLog *bytes.Buffer, args ...string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-daemonDone:
			t.Fatalf("restoreweaved exited before readiness: %v\n%s", err, daemonLog.String())
		default:
		}
		commandArgs := append([]string{"--socket", socketPath, "--json"}, args...)
		if err := exec.Command(rwBin, commandArgs...).Run(); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("restoreweaved CLI readiness timed out\n%s", daemonLog.String())
}

func waitForReaderCLI(t *testing.T, rwBin, socketPath string, daemonDone <-chan error, daemonLog *bytes.Buffer) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-daemonDone:
			t.Fatalf("recovery reader exited before readiness: %v\n%s", err, daemonLog.String())
		default:
		}
		cmd := exec.Command(rwBin, "--socket", socketPath, "--json", "snapshot", "list")
		if err := cmd.Run(); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("recovery reader CLI readiness timed out\n%s", daemonLog.String())
}

func runRWProcess(t *testing.T, rwBin, socketPath string, args ...string) command.Result {
	t.Helper()
	fullArgs := append([]string{"--socket", socketPath, "--json"}, args...)
	cmd := exec.Command(rwBin, fullArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rw %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	var result command.Result
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode rw %s result: %v\n%s", strings.Join(args, " "), err, output)
	}
	if result.Status != command.StatusSucceeded {
		t.Fatalf("rw %s status = %s, reasons=%+v", strings.Join(args, " "), result.Status, result.Reasons)
	}
	return result
}

func decodeProcessResult(t *testing.T, result command.Result, target any) {
	t.Helper()
	if err := json.Unmarshal(result.Data, target); err != nil {
		t.Fatal(err)
	}
}

func recoveryProcessTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := make(map[string]string)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(payload)
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		tree[rel] = hex.EncodeToString(digest[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func repositoryHasZstdPayload(t *testing.T, root string) bool {
	t.Helper()
	found := false
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.Contains(filepath.ToSlash(path), "/blobs/sha256/") {
			return nil
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.HasPrefix(payload, []byte{0x28, 0xb5, 0x2f, 0xfd}) {
			found = true
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan zstd repository payloads: %v", err)
	}
	return found
}

func waitForRecoveryReader(t *testing.T, socketPath string, readerErr <-chan error) *transport.Conn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-readerErr:
			t.Fatalf("recovery reader exited before listen: %v", err)
		default:
		}
		conn, err := transport.Dial(socketPath)
		if err == nil {
			return conn
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("recovery reader did not listen on %s", socketPath)
	return nil
}
