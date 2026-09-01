package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type sourceListInput struct {
	WorkspaceID string `json:"workspace_id"`
}

// sourceScanSummary mirrors the scanner summary stored on a scan generation.
// Keeping this projection local lets the catalog retain forward-compatible
// summary fields without making source.list expose scanner internals.
type sourceScanSummary struct {
	Entries           uint64 `json:"entries"`
	RegularFiles      uint64 `json:"regular_files"`
	Directories       uint64 `json:"directories"`
	Symlinks          uint64 `json:"symlinks"`
	SpecialFiles      uint64 `json:"special_files"`
	BytesHashed       int64  `json:"bytes_hashed"`
	FailedEntries     uint64 `json:"failed_entries"`
	UnstableEntries   uint64 `json:"unstable_entries"`
	DetectionFailures uint64 `json:"detection_failures"`
}

func (d *Dispatcher) handleSourceList(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	var input sourceListInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if _, err := d.store.GetWorkspace(ctx, input.WorkspaceID); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return notFoundResult(env, started, "workspace not found")
		}
		return catalogErrorResult(env, started, err)
	}
	sources, err := d.store.ListSources(ctx, input.WorkspaceID)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	checkedAt := d.now().UTC().Format(time.RFC3339)
	projected := make([]command.SourceSummaryData, 0, len(sources))
	for _, source := range sources {
		reachability, message := probeSourcePath(source.Locator)
		item := command.SourceSummaryData{
			SourceRef:             source.ID,
			Kind:                  source.Kind,
			Locator:               source.Locator,
			State:                 string(source.State),
			Reachability:          reachability,
			ReachabilityCheckedAt: checkedAt,
			ReachabilityMessage:   message,
		}
		if scan, scanErr := d.store.LatestScanGeneration(ctx, input.WorkspaceID, source.ID); scanErr == nil {
			var summary sourceScanSummary
			if err := json.Unmarshal(scan.Summary, &summary); err != nil {
				return catalogErrorResult(env, started, err)
			}
			item.LatestScan = &command.SourceScanData{
				ScanRef:           scan.ID,
				Generation:        scan.Generation,
				State:             string(scan.State),
				FullTraversal:     scan.FullTraversal,
				StartedAt:         scan.StartedAt.UTC().Format(time.RFC3339),
				Entries:           summary.Entries,
				RegularFiles:      summary.RegularFiles,
				Directories:       summary.Directories,
				Symlinks:          summary.Symlinks,
				SpecialFiles:      summary.SpecialFiles,
				BytesHashed:       summary.BytesHashed,
				FailedEntries:     summary.FailedEntries,
				UnstableEntries:   summary.UnstableEntries,
				DetectionFailures: summary.DetectionFailures,
			}
			if scan.FinishedAt != nil {
				item.LatestScan.FinishedAt = scan.FinishedAt.UTC().Format(time.RFC3339)
			}
		} else if !errors.Is(scanErr, sqlite.ErrNotFound) {
			return catalogErrorResult(env, started, scanErr)
		}
		if publication, pubErr := d.store.LatestPublicationForSource(ctx, input.WorkspaceID, source.ID); pubErr == nil {
			item.LatestSnapshotRef = publication.SnapshotRef
			item.LatestNamespaceRootID = publication.NamespaceRootID
		} else if !errors.Is(pubErr, sqlite.ErrNotFound) {
			return catalogErrorResult(env, started, pubErr)
		}
		projected = append(projected, item)
	}
	return succeeded(env, started, command.SourceListData{WorkspaceID: input.WorkspaceID, Sources: projected})
}

// probeSourcePath is deliberately transient and read-only. In particular it
// never updates Source.State: a missing mount is not proof that a source was
// permanently lost or decommissioned.
func probeSourcePath(path string) (string, string) {
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return "AVAILABLE", ""
		}
		return "UNAVAILABLE", "source path is not a directory"
	}
	if errors.Is(err, fs.ErrNotExist) {
		return "UNAVAILABLE", "source path is not accessible"
	}
	return "UNKNOWN", "source path could not be checked: " + err.Error()
}
