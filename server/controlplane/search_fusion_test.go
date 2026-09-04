package controlplane

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestResolveFusedWorkspaceRejectsMixedGenerationWorkspaces(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	first := testutil.SeedNamespace(t, store)
	second := testutil.SeedNamespace(t, store)
	firstGenerationID, err := sqlite.NewStableID(sqlite.IDPrefixIndexGeneration)
	if err != nil {
		t.Fatal(err)
	}
	secondGenerationID, err := sqlite.NewStableID(sqlite.IDPrefixIndexGeneration)
	if err != nil {
		t.Fatal(err)
	}
	for _, generation := range []sqlite.IndexGeneration{
		{ID: firstGenerationID, WorkspaceID: first.WorkspaceID, SnapshotRef: "snapshot:first", NamespaceRootID: first.RootID, DBPath: "/tmp/first.sqlite", Dimension: search.DimensionLexical},
		{ID: secondGenerationID, WorkspaceID: second.WorkspaceID, SnapshotRef: "snapshot:second", NamespaceRootID: second.RootID, DBPath: "/tmp/second.sqlite", Dimension: search.DimensionSemantic},
	} {
		if err := store.InsertIndexGeneration(ctx, &generation); err != nil {
			t.Fatalf("insert generation: %v", err)
		}
	}
	dispatcher := &Dispatcher{store: store}
	_, err = dispatcher.resolveFusedWorkspace(ctx, "", []search.Component{
		{Dimension: search.DimensionLexical, GenerationID: firstGenerationID, Status: string(command.StatusSucceeded)},
		{Dimension: search.DimensionSemantic, GenerationID: secondGenerationID, Status: string(command.StatusSucceeded)},
	})
	if err == nil || !strings.Contains(err.Error(), "belongs to workspace") {
		t.Fatalf("mixed fused workspaces error = %v, want rejection", err)
	}
}
