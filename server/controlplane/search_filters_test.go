package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

// TestSearchQueryTypedStructuredFilters proves search.query accepts typed
// structured filters and narrows lexical results through the dispatcher,
// while an unmatched constraint excludes the subject.
func TestSearchQueryTypedStructuredFilters(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))

	root := t.TempDir()
	payload := []byte("quarterly experiment report")
	if err := writeFile(filepath.Join(root, "docs", "quarterly-report.txt"), payload); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ingestData := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})

	entryTypeFilter := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"query":        "quarterly",
		"filters": map[string]any{
			"entry_type":      "REGULAR_FILE",
			"size_min":        1,
			"size_max":        1 << 20,
			"mtime_before":    1893456000000,
			"protection_mode": "STORE_EXACT",
		},
	}))
	if entryTypeFilter.Status != command.StatusSucceeded {
		t.Fatalf("filtered search = %q: %+v", entryTypeFilter.Status, entryTypeFilter.Reasons)
	}
	var data command.SearchQueryData
	if err := json.Unmarshal(entryTypeFilter.Data, &data); err != nil {
		t.Fatalf("decode filtered search: %v", err)
	}
	if len(data.Hits) != 1 {
		t.Fatalf("filtered hits = %+v", data.Hits)
	}

	// A suffix that does not exist must fail closed with zero hits.
	noMatch := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"query":        "quarterly",
		"filters": map[string]any{
			"suffix": "zzz",
		},
	}))
	if noMatch.Status != command.StatusSucceeded {
		t.Fatalf("no-match search = %q: %+v", noMatch.Status, noMatch.Reasons)
	}
	var noData command.SearchQueryData
	if err := json.Unmarshal(noMatch.Data, &noData); err != nil {
		t.Fatalf("decode no-match search: %v", err)
	}
	if len(noData.Hits) != 0 {
		t.Fatalf("suffix-mismatch hits = %+v", noData.Hits)
	}

	// A filters-only request (no free text) must still be valid.
	filtersOnly := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"filters": map[string]any{
			"entry_type": "REGULAR_FILE",
		},
	}))
	if filtersOnly.Status != command.StatusSucceeded {
		t.Fatalf("filters-only search = %q: %+v", filtersOnly.Status, filtersOnly.Reasons)
	}

	// Invalid filter ranges must fail closed as invalid input.
	invalid := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"filters": map[string]any{
			"size_min": 100,
			"size_max": 10,
		},
	}))
	if invalid.Status != command.StatusFailed || !hasReasonCode(invalid, ReasonCodeInvalidInput) {
		t.Fatalf("invalid range = %q: %+v", invalid.Status, invalid.Reasons)
	}
}

func writeFile(path string, payload []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o600)
}
