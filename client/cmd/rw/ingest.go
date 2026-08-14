package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ailiheizi/restoreweave/client/command"
)

func newIngestCommand(env *clientEnv) *cobra.Command {
	return newExitCommand(env, "ingest <root>", "Capture a local tree into the exact archive",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw ingest <root>\n")
				return 1
			}
			return env.request(cmd, command.OpPlanIngest, map[string]any{"root": args[0]},
				func(cmd *cobra.Command, result command.Result) error {
					var data command.PlanIngestData
					if err := json.Unmarshal(result.Data, &data); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "snapshot:   %s\n", data.SnapshotRef)
					fmt.Fprintf(cmd.OutOrStdout(), "workspace:  %s\n", data.WorkspaceID)
					fmt.Fprintf(cmd.OutOrStdout(), "root:       %s\n", data.RootID)
					fmt.Fprintf(cmd.OutOrStdout(), "files:      %d\n", data.Files)
					fmt.Fprintf(cmd.OutOrStdout(), "bytes:      %d\n", data.Bytes)
					fmt.Fprintf(cmd.OutOrStdout(), "digest:     %s\n", data.ManifestDigest)
					return nil
				})
		}).Command
}

func newRestoreCommand(env *clientEnv) *cobra.Command {
	return newExitCommand(env, "restore <snapshot-ref> <destination>",
		"Restore a published snapshot to an empty directory without requiring the live catalog",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 2 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw restore <snapshot-ref> <destination>\n")
				return 1
			}
			return env.request(cmd, command.OpPlanRestore, map[string]any{
				"snapshot_ref": args[0],
				"destination":  args[1],
			}, func(cmd *cobra.Command, result command.Result) error {
				var data command.PlanRestoreData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "snapshot:     %s\n", data.SnapshotRef)
				fmt.Fprintf(cmd.OutOrStdout(), "destination:  %s\n", data.Destination)
				fmt.Fprintf(cmd.OutOrStdout(), "files:        %d\n", data.Files)
				fmt.Fprintf(cmd.OutOrStdout(), "bytes:        %d\n", data.Bytes)
				return nil
			})
		}).Command
}

func newSnapshotCommand(env *clientEnv) *cobra.Command {
	snapshot := &cobra.Command{Use: "snapshot", Short: "List and verify published snapshots"}
	snapshot.AddCommand(newExitCommand(env, "list", "List portable snapshots in the repository",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			return env.request(cmd, command.OpSnapshotList, map[string]any{},
				func(cmd *cobra.Command, result command.Result) error {
					var data command.SnapshotListData
					if err := json.Unmarshal(result.Data, &data); err != nil {
						return err
					}
					if len(data.Snapshots) == 0 {
						fmt.Fprintln(cmd.OutOrStdout(), "no snapshots")
						return nil
					}
					for _, item := range data.Snapshots {
						fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s\n", item.SnapshotRef, item.CreatedAt, item.DisplayPath)
					}
					return nil
				})
		}).Command)
	snapshot.AddCommand(newExitCommand(env, "diff <from-ref> <to-ref>",
		"Compare two portable snapshots by original path",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 2 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw snapshot diff <from-ref> <to-ref>\n")
				return 1
			}
			return env.request(cmd, command.OpSnapshotDiff, map[string]any{
				"from_snapshot_ref": args[0],
				"to_snapshot_ref":   args[1],
			}, func(cmd *cobra.Command, result command.Result) error {
				var data command.SnapshotDiffData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				if len(data.Changes) == 0 {
					fmt.Fprintln(out, "no changes")
					return nil
				}
				for _, change := range data.Changes {
					path := change.Path
					if change.Kind == command.DiffMoved {
						path = change.FromPath + " -> " + change.ToPath
					}
					fmt.Fprintf(out, "%s\t%s\t%s\n", change.Kind, change.EntryType, path)
				}
				return nil
			})
		}).Command)
	snapshot.AddCommand(newExitCommand(env, "verify <snapshot-ref>", "Verify snapshot blob digests",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw snapshot verify <snapshot-ref>\n")
				return 1
			}
			return env.request(cmd, command.OpSnapshotVerify, map[string]any{"snapshot_ref": args[0]},
				func(cmd *cobra.Command, result command.Result) error {
					var data command.SnapshotVerifyData
					if err := json.Unmarshal(result.Data, &data); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "snapshot:  %s\n", data.SnapshotRef)
					fmt.Fprintf(cmd.OutOrStdout(), "ok:        %t\n", data.OK)
					fmt.Fprintf(cmd.OutOrStdout(), "files:     %d\n", data.Files)
					fmt.Fprintf(cmd.OutOrStdout(), "bytes:     %d\n", data.Bytes)
					return nil
				})
		}).Command)
	return snapshot
}

func newRecoveryCommand(env *clientEnv) *cobra.Command {
	recovery := &cobra.Command{Use: "recovery", Short: "Export an independently retainable recovery reference"}
	recovery.AddCommand(newExitCommand(env, "export <snapshot-ref> <destination>",
		"Copy the portable snapshot JSON to a new file without overwriting",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 2 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw recovery export <snapshot-ref> <destination>\n")
				return 1
			}
			return env.request(cmd, command.OpRecoveryExport, map[string]any{
				"snapshot_ref": args[0],
				"destination":  args[1],
			}, func(cmd *cobra.Command, result command.Result) error {
				var data command.RecoveryExportData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "snapshot:      %s\n", data.SnapshotRef)
				fmt.Fprintf(out, "artifact:      %s\n", data.ArtifactPath)
				fmt.Fprintf(out, "digest:        %s\n", data.ManifestDigest)
				fmt.Fprintf(out, "length:        %d\n", data.Length)
				fmt.Fprintf(out, "files:         %d\n", data.Files)
				fmt.Fprintf(out, "independent:   %t\n", data.IndependentlyStored)
				return nil
			})
		}).Command)
	return recovery
}
