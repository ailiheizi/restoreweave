// Command restoreweaved runs the RestoreWeave control-plane daemon. It
// exposes the client/command envelope protocol over a Unix socket, and
// optionally a loopback OpenSubsonic/OPDS/Inbox facade over the same ABI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	rwconfig "github.com/ailiheizi/restoreweave/config"
	"github.com/ailiheizi/restoreweave/server/controlplane"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/gateway/protocol"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("restoreweaved: %v", err)
	}
}

func run() error {
	var (
		socketPath = flag.String("socket", controlplane.DefaultSocketPath(),
			"Unix socket path (overrides RESTOREWEAVE_SOCKET)")
		recoveryReader = flag.Bool("recovery-reader", false,
			"serve a catalog-free read-only recovery reader")
		recoveryReference = flag.String("recovery-reference", "",
			"v2 portable recovery reference for --recovery-reader")
		trustAnchorPath = flag.String("trust-anchor", "",
			"independently retained public trust anchor for --recovery-reader")
		configPath = flag.String("config", "",
			"persisted RestoreWeave config path (overrides RESTOREWEAVE_CONFIG)")
		catalogPath = flag.String("catalog", "",
			"SQLite catalog path (overrides persisted config and RESTOREWEAVE_CATALOG)")
		repositoryPath = flag.String("repository", "",
			"Exact-lane repository path (overrides persisted config and RESTOREWEAVE_REPOSITORY)")
		facadeListen = flag.String("facade-listen", "",
			"Loopback OpenSubsonic/OPDS listen address (empty disables the facade)")
		facadeToken = flag.String("facade-token", os.Getenv("RESTOREWEAVE_FACADE_TOKEN"),
			"Shared token for the protocol facade (or RESTOREWEAVE_FACADE_TOKEN)")
		facadeWorkspace = flag.String("facade-workspace", "",
			"Workspace pinned to the protocol facade")
		facadeSnapshot = flag.String("facade-snapshot", "",
			"Optional snapshot pin for the protocol facade")
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runWithOptions(ctx, daemonOptions{
		socketPath:        *socketPath,
		recoveryReader:    *recoveryReader,
		recoveryReference: *recoveryReference,
		trustAnchorPath:   *trustAnchorPath,
		configPath:        *configPath,
		catalogPath:       *catalogPath,
		repositoryPath:    *repositoryPath,
		facadeListen:      *facadeListen,
		facadeToken:       *facadeToken,
		facadeWorkspace:   *facadeWorkspace,
		facadeSnapshot:    *facadeSnapshot,
	})
}

type daemonOptions struct {
	socketPath        string
	recoveryReader    bool
	recoveryReference string
	trustAnchorPath   string
	configPath        string
	catalogPath       string
	repositoryPath    string
	facadeListen      string
	facadeToken       string
	facadeWorkspace   string
	facadeSnapshot    string
}

func runWithOptions(ctx context.Context, options daemonOptions) error {
	if options.recoveryReader {
		return runRecoveryReader(ctx, options)
	}

	resolved, err := rwconfig.LoadEffective(rwconfig.LoadOptions{
		Path:         options.configPath,
		AllowMissing: true,
		ResolveOptions: rwconfig.ResolveOptions{
			Overrides: rwconfig.PathOverrides{Catalog: options.catalogPath, Repository: options.repositoryPath},
		},
	})
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := validateRuntimeStorageProfile(resolved.Config); err != nil {
		return err
	}
	catalogPath := resolved.Config.Paths.Catalog
	repositoryPath := resolved.Config.Paths.Repository

	store, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		return fmt.Errorf("open catalog: %w", err)
	}
	defer store.Close()

	repo, err := repository.OpenProfileWithCompression(
		resolved.Config.Storage.RepositoryProfile,
		resolved.Config.Storage.CompressionProfile,
		repositoryPath,
	)
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}
	commits, err := repo.ListRecordDigests(ctx, repository.RecordPublicationCommit)
	if err != nil {
		return fmt.Errorf("inspect publication commits: %w", err)
	}
	identity, anchor, err := exact.OpenSigningMaterial(
		resolved.Config.Paths.RecoveryRecords, resolved.Config.Recovery.PublicationDomain, len(commits) == 0,
	)
	if err != nil {
		return fmt.Errorf("open publication signing material: %w", err)
	}
	exactLane := &exact.Service{
		Store:                        store,
		Repo:                         repo,
		ConfigDigest:                 resolved.Digest,
		DefaultProtection:            sqlite.ProtectionMode(resolved.Config.Storage.DefaultProtection),
		AllowLinkOnly:                resolved.Config.Storage.AllowLinkOnly,
		LinkOnlyRequiresConfirmation: resolved.Config.Storage.LinkOnlyRequiresConfirmation,
		SigningIdentity:              &identity,
		TrustAnchor:                  &anchor,
		PublicationDomain:            resolved.Config.Recovery.PublicationDomain,
		RequireSignedPublication:     true,
	}
	dispatcher := controlplane.NewDispatcher(store, catalogPath, options.socketPath,
		controlplane.WithConfigDigest(resolved.Digest), controlplane.WithExact(exactLane))
	server, err := controlplane.NewServer(dispatcher, options.socketPath,
		controlplane.WithErrorHandler(func(err error) { log.Printf("%v", err) }))
	if err != nil {
		return err
	}

	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		if err := server.Serve(ctx); err != nil {
			log.Printf("serve: %v", err)
		}
	}()
	log.Printf("restoreweaved listening on %s (catalog %s, repository %s)", server.SocketPath(), catalogPath, repositoryPath)

	var facadeServer *http.Server
	if strings.TrimSpace(options.facadeListen) != "" {
		facade, err := protocol.New(dispatcher.Handle, protocol.Options{
			WorkspaceID: options.facadeWorkspace,
			SnapshotRef: options.facadeSnapshot,
			Token:       options.facadeToken,
			Listen:      options.facadeListen,
		})
		if err != nil {
			_ = server.Close()
			return fmt.Errorf("protocol facade: %w", err)
		}
		facadeServer = &http.Server{Addr: options.facadeListen, Handler: facade.Handler()}
		go func() {
			log.Printf("protocol facade listening on %s (OpenSubsonic /opds /inbox, loopback only)", options.facadeListen)
			if err := facadeServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("protocol facade: %v", err)
			}
		}()
	}

	<-ctx.Done()
	log.Printf("shutting down")
	if facadeServer != nil {
		_ = facadeServer.Shutdown(context.Background())
	}
	if err := server.Close(); err != nil {
		return fmt.Errorf("close control plane: %w", err)
	}
	<-serveDone
	return nil
}

func runRecoveryReader(ctx context.Context, options daemonOptions) error {
	if strings.TrimSpace(options.repositoryPath) == "" ||
		strings.TrimSpace(options.recoveryReference) == "" ||
		strings.TrimSpace(options.trustAnchorPath) == "" {
		return errors.New("--recovery-reader requires --repository, --recovery-reference, and --trust-anchor")
	}
	profile, err := repository.DetectProfileReadOnly(options.repositoryPath)
	if err != nil {
		return fmt.Errorf("detect repository profile: %w", err)
	}
	repo, err := repository.OpenProfileReadOnly(profile, options.repositoryPath)
	if err != nil {
		return fmt.Errorf("open recovery repository read-only: %w", err)
	}
	anchor, err := exact.LoadTrustAnchor(options.trustAnchorPath)
	if err != nil {
		return fmt.Errorf("load recovery trust anchor: %w", err)
	}
	reference, err := exact.LoadRecoveryReference(options.recoveryReference)
	if err != nil {
		return fmt.Errorf("load recovery reference: %w", err)
	}
	service := &exact.Service{
		Repo:                     repo,
		TrustAnchor:              &anchor,
		PublicationDomain:        reference.PublicationDomain,
		RequireSignedPublication: true,
	}
	imported, err := service.ImportRecoveryArtifact(ctx, options.recoveryReference, options.trustAnchorPath, reference.PublicationDomain)
	if err != nil {
		return fmt.Errorf("admit recovery reference: %w", err)
	}
	if imported.Schema != exact.RecoveryReferenceSchemaV2 {
		return fmt.Errorf("recovery reader requires %s, got %s", exact.RecoveryReferenceSchemaV2, imported.Schema)
	}
	dispatcher, err := controlplane.NewRecoveryDispatcher(options.socketPath, service)
	if err != nil {
		return fmt.Errorf("create recovery reader: %w", err)
	}
	server, err := controlplane.NewServer(dispatcher, options.socketPath,
		controlplane.WithErrorHandler(func(err error) { log.Printf("%v", err) }))
	if err != nil {
		return err
	}
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		if err := server.Serve(ctx); err != nil {
			log.Printf("serve recovery reader: %v", err)
		}
	}()
	log.Printf("restoreweaved recovery reader listening on %s (repository %s, profile %s)", server.SocketPath(), repo.Root(), profile)
	<-ctx.Done()
	if err := server.Close(); err != nil {
		return fmt.Errorf("close recovery reader: %w", err)
	}
	<-serveDone
	return nil
}

func validateRuntimeStorageProfile(cfg rwconfig.Config) error {
	if cfg.Recovery.PublicationSigning != rwconfig.PublicationSigningLocalEd25519 {
		return fmt.Errorf(
			"recovery publication signing profile %q is not available in this build (available: %s)",
			cfg.Recovery.PublicationSigning, rwconfig.PublicationSigningLocalEd25519,
		)
	}
	switch cfg.Storage.RepositoryProfile {
	case rwconfig.RepositoryProfileDirectoryCASDev:
		if cfg.Storage.CompressionProfile != rwconfig.CompressionProfileIdentity {
			return fmt.Errorf(
				"storage compression profile %q is not available with repository profile %q (required: %s)",
				cfg.Storage.CompressionProfile, cfg.Storage.RepositoryProfile, rwconfig.CompressionProfileIdentity,
			)
		}
	case rwconfig.RepositoryProfileLocalZstdV1:
		if cfg.Storage.CompressionProfile != rwconfig.CompressionProfileZstdV1 {
			return fmt.Errorf(
				"storage compression profile %q is not available with repository profile %q (required: %s)",
				cfg.Storage.CompressionProfile, cfg.Storage.RepositoryProfile, rwconfig.CompressionProfileZstdV1,
			)
		}
	default:
		return fmt.Errorf(
			"storage repository profile %q is not available in this build (available: %s, %s)",
			cfg.Storage.RepositoryProfile, rwconfig.RepositoryProfileDirectoryCASDev, rwconfig.RepositoryProfileLocalZstdV1,
		)
	}
	if cfg.Storage.NeuralCodec != rwconfig.NeuralCodecDisabled {
		return fmt.Errorf(
			"storage neural codec %q is not available in this build (available: %s)",
			cfg.Storage.NeuralCodec, rwconfig.NeuralCodecDisabled,
		)
	}
	return nil
}
