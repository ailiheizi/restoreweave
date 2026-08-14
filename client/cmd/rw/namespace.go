package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ailiheizi/restoreweave/client/command"
)

func newNamespaceCommand(env *clientEnv) *cobra.Command {
	namespace := &cobra.Command{
		Use:   "namespace",
		Short: "Browse the captured namespace",
		Long: "namespace commands address catalog stable IDs. resolve walks display-name " +
			"components and does not follow symbolic links.",
	}
	namespace.AddCommand(newNamespaceListCommand(env))
	namespace.AddCommand(newNamespaceResolveCommand(env))
	namespace.AddCommand(newNamespaceStatCommand(env))
	namespace.AddCommand(newNamespaceReadlinkCommand(env))
	return namespace
}

func workspaceFlag(cmd *cobra.Command) *string {
	return cmd.Flags().String("workspace", "", "catalog workspace stable id (required)")
}

// workspaceValue reads the already-declared --workspace flag.
func workspaceValue(cmd *cobra.Command) string {
	return cmd.Flags().Lookup("workspace").Value.String()
}

func newNamespaceListCommand(env *clientEnv) *cobra.Command {
	command := newExitCommand(env, "list <root-id>", "List namespace entries under a root or parent entry",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw namespace list <root-id> [--parent <entry-id>] [--workspace <id>]\n")
				return 1
			}
			workspace := workspaceValue(cmd)
			parent := cmd.Flags().Lookup("parent").Value.String()
			return env.request(cmd, command.OpNamespaceList, map[string]any{
				"workspace_id": workspace,
				"root_id":      args[0],
				"parent_id":    parent,
			}, func(cmd *cobra.Command, result command.Result) error {
				var data command.NamespaceListData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				if len(data.Entries) == 0 {
					fmt.Fprintln(out, "no entries")
					return nil
				}
				for _, entry := range data.Entries {
					size := ""
					if entry.LogicalSize != nil {
						size = fmt.Sprintf("%d", *entry.LogicalSize)
					}
					fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", entry.EntryType, entry.ID, size, entry.DisplayName)
				}
				return nil
			})
		})
	command.Flags().String("parent", "", "parent namespace entry stable id")
	workspaceFlag(command.Command)
	_ = command.MarkFlagRequired("workspace")
	return command.Command
}

func newNamespaceResolveCommand(env *clientEnv) *cobra.Command {
	command := newExitCommand(env, "resolve <path>", "Resolve display-path components to a catalog entry id",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw namespace resolve <path> [--workspace <id>] [--root <id>|--snapshot <ref>]\n")
				return 1
			}
			input := map[string]any{
				"workspace_id": workspaceValue(cmd),
				"path":         args[0],
			}
			if root := cmd.Flags().Lookup("root").Value.String(); root != "" {
				input["root_id"] = root
			}
			if snapshot := cmd.Flags().Lookup("snapshot").Value.String(); snapshot != "" {
				input["snapshot_ref"] = snapshot
			}
			return env.request(cmd, command.OpNamespaceResolve, input, func(cmd *cobra.Command, result command.Result) error {
				var data command.NamespaceResolveData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "path_ref:     %s\n", data.PathRef)
				fmt.Fprintf(out, "path:         %s\n", data.Path)
				fmt.Fprintf(out, "entry_type:   %s\n", data.Entry.EntryType)
				fmt.Fprintf(out, "display_name: %s\n", data.Entry.DisplayName)
				if data.Entry.ContentID != "" {
					fmt.Fprintf(out, "content_id:   %s\n", data.Entry.ContentID)
				}
				return nil
			})
		})
	workspaceFlag(command.Command)
	command.Flags().String("root", "", "catalog namespace root stable id")
	command.Flags().String("snapshot", "", "published snapshot ref used to locate the namespace root")
	_ = command.MarkFlagRequired("workspace")
	return command.Command
}

func newNamespaceStatCommand(env *clientEnv) *cobra.Command {
	command := newExitCommand(env, "stat <entry-id>", "Stat one namespace entry",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw namespace stat <entry-id> [--workspace <id>]\n")
				return 1
			}
			workspace := workspaceValue(cmd)
			return env.request(cmd, command.OpNamespaceStat, map[string]any{
				"workspace_id": workspace,
				"entry_id":     args[0],
			}, func(cmd *cobra.Command, result command.Result) error {
				var data command.NamespaceStatData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				entry := data.Entry
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "entry_id:        %s\n", entry.ID)
				fmt.Fprintf(out, "root_id:         %s\n", entry.RootID)
				if entry.ParentID != "" {
					fmt.Fprintf(out, "parent_id:       %s\n", entry.ParentID)
				}
				fmt.Fprintf(out, "display_name:    %s\n", entry.DisplayName)
				fmt.Fprintf(out, "entry_type:      %s\n", entry.EntryType)
				if entry.ContentID != "" {
					fmt.Fprintf(out, "content_id:      %s\n", entry.ContentID)
				}
				if entry.FileVersionID != "" {
					fmt.Fprintf(out, "file_version_id: %s\n", entry.FileVersionID)
				}
				if entry.LogicalSize != nil {
					fmt.Fprintf(out, "logical_size:    %d\n", *entry.LogicalSize)
				}
				if entry.AllocatedSize != nil {
					fmt.Fprintf(out, "allocated_size:  %d\n", *entry.AllocatedSize)
				}
				return nil
			})
		})
	workspaceFlag(command.Command)
	_ = command.MarkFlagRequired("workspace")
	return command.Command
}

func newNamespaceReadlinkCommand(env *clientEnv) *cobra.Command {
	command := newExitCommand(env, "readlink <entry-id>", "Read one symlink entry's captured target",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw namespace readlink <entry-id> [--workspace <id>]\n")
				return 1
			}
			workspace := workspaceValue(cmd)
			return env.request(cmd, command.OpNamespaceReadlink, map[string]any{
				"workspace_id": workspace,
				"entry_id":     args[0],
			}, func(cmd *cobra.Command, result command.Result) error {
				var data command.NamespaceReadlinkData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "entry_id: %s\n", data.EntryID)
				fmt.Fprintf(cmd.OutOrStdout(), "target:   %s\n", data.TargetDisplay)
				return nil
			})
		})
	workspaceFlag(command.Command)
	_ = command.MarkFlagRequired("workspace")
	return command.Command
}
