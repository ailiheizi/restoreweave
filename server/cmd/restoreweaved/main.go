// Command restoreweaved runs the RestoreWeave control-plane daemon. It
// exposes the client/command envelope protocol over a Unix socket, and
// optionally a loopback OpenSubsonic/OPDS/Inbox facade over the same ABI.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

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
		catalogPath = flag.String("catalog", controlplane.DefaultCatalogPath(),
			"SQLite catalog path (overrides RESTOREWEAVE_CATALOG)")
		repositoryPath = flag.String("repository", controlplane.DefaultRepositoryPath(),
			"Exact-lane repository path (overrides RESTOREWEAVE_REPOSITORY)")
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

	store, err := sqlite.Open(ctx, *catalogPath, sqlite.Options{})
	if err != nil {
		return fmt.Errorf("open catalog: %w", err)
	}
	defer store.Close()

	repo, err := repository.OpenDir(*repositoryPath)
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}
	exactLane := &exact.Service{Store: store, Repo: repo}
	dispatcher := controlplane.NewDispatcher(store, *catalogPath, *socketPath, controlplane.WithExact(exactLane))
	server, err := controlplane.NewServer(dispatcher, *socketPath,
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
	log.Printf("restoreweaved listening on %s (catalog %s, repository %s)", server.SocketPath(), *catalogPath, *repositoryPath)

	var facadeServer *http.Server
	if strings.TrimSpace(*facadeListen) != "" {
		facade, err := protocol.New(dispatcher.Handle, protocol.Options{
			WorkspaceID: *facadeWorkspace,
			SnapshotRef: *facadeSnapshot,
			Token:       *facadeToken,
			Listen:      *facadeListen,
		})
		if err != nil {
			_ = server.Close()
			return fmt.Errorf("protocol facade: %w", err)
		}
		facadeServer = &http.Server{Addr: *facadeListen, Handler: facade.Handler()}
		go func() {
			log.Printf("protocol facade listening on %s (OpenSubsonic /opds /inbox, loopback only)", *facadeListen)
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
