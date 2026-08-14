package controlplane

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestDispatcherGatewayMountIsNotAProduct(t *testing.T) {
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))
	result := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpGatewayMount, map[string]any{
		"snapshot_ref": "snap:any",
		"mountpoint":   filepath.Join(t.TempDir(), "mnt"),
	}))
	if result.Status != command.StatusFailed || !hasReasonCode(result, ReasonCodeUnimplemented) {
		t.Fatalf("mount = %q reasons=%+v, want unimplemented", result.Status, result.Reasons)
	}
	if !strings.Contains(result.Reasons[0].Message, "plan.restore") {
		t.Fatalf("mount refusal should name plan.restore: %+v", result.Reasons)
	}
	listed := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpStatusGet, map[string]any{}))
	var status command.StatusData
	if err := json.Unmarshal(listed.Data, &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	found := false
	for _, operation := range status.Unimplemented {
		if operation == command.OpGatewayMount {
			found = true
		}
	}
	if !found {
		t.Fatalf("status.unimplemented missing %s: %v", command.OpGatewayMount, status.Unimplemented)
	}
}
