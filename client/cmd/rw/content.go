package main

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/ailiheizi/restoreweave/client/command"
)

// newContentCommand forwards content.open/read/close to the daemon.
func newContentCommand(env *clientEnv) *cobra.Command {
	content := &cobra.Command{
		Use:   "content",
		Short: "Open and read exact content handles",
		Long: "content.open/read/close talk to restoreweaved. Without the exact " +
			"lane the daemon reports unimplemented; with it, open reads CAS bytes " +
			"for one regular file entry.",
	}
	content.AddCommand(newContentOpenCommand(env))
	content.AddCommand(newExitCommand(env, "read <handle> [offset] [length]", "Read bytes from a content handle",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) < 1 || len(args) > 3 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw content read <handle> [offset] [length]\n")
				return 1
			}
			input := map[string]any{"handle": args[0]}
			if len(args) >= 2 {
				offset, err := strconv.ParseInt(args[1], 10, 64)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "invalid offset %q\n", args[1])
					return 1
				}
				input["offset"] = offset
			}
			if len(args) == 3 {
				length, err := strconv.ParseInt(args[2], 10, 64)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "invalid length %q\n", args[2])
					return 1
				}
				input["length"] = length
			}
			return env.request(cmd, command.OpContentRead, input, func(cmd *cobra.Command, result command.Result) error {
				var data command.ContentReadData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "handle:  %s\n", data.Handle)
				fmt.Fprintf(cmd.OutOrStdout(), "offset:  %d\n", data.Offset)
				fmt.Fprintf(cmd.OutOrStdout(), "length:  %d\n", data.Length)
				fmt.Fprintf(cmd.OutOrStdout(), "eof:     %t\n", data.EOF)
				if len(data.Bytes) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "bytes:   %s\n", string(data.Bytes))
				}
				return nil
			})
		}).Command)
	content.AddCommand(newExitCommand(env, "close <handle>", "Close a content handle",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw content close <handle>\n")
				return 1
			}
			return env.request(cmd, command.OpContentClose, map[string]any{"handle": args[0]}, nil)
		}).Command)
	return content
}

func newContentOpenCommand(env *clientEnv) *cobra.Command {
	commandNode := newExitCommand(env, "open [entry-id]", "Open a content handle for one regular file",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			input := map[string]any{}
			if len(args) == 1 {
				input["entry_id"] = args[0]
			} else if len(args) > 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw content open [entry-id] [--workspace <id>]\n")
				return 1
			}
			if lookup := cmd.Flags().Lookup("workspace"); lookup != nil {
				if workspace := lookup.Value.String(); workspace != "" {
					input["workspace_id"] = workspace
				}
			}
			return env.request(cmd, command.OpContentOpen, input, func(cmd *cobra.Command, result command.Result) error {
				var data command.ContentOpenData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "handle:        %s\n", data.Handle)
				fmt.Fprintf(cmd.OutOrStdout(), "entry_id:      %s\n", data.EntryID)
				fmt.Fprintf(cmd.OutOrStdout(), "content_id:    %s\n", data.ContentID)
				fmt.Fprintf(cmd.OutOrStdout(), "logical_size:  %d\n", data.LogicalSize)
				fmt.Fprintf(cmd.OutOrStdout(), "max_read:      %d\n", data.MaxRead)
				return nil
			})
		})
	commandNode.Flags().String("workspace", "", "catalog workspace stable id")
	return commandNode.Command
}
