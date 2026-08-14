package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ailiheizi/restoreweave/client/command"
)

func newMountCommand(env *clientEnv) *cobra.Command {
	return newExitCommand(env, "mount <snapshot-ref> <mountpoint>",
		"Refused: restore the snapshot, then mount with another tool",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 2 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw mount <snapshot-ref> <mountpoint>\n")
				return 1
			}
			return env.request(cmd, command.OpGatewayMount, map[string]any{
				"snapshot_ref": args[0],
				"mountpoint":   args[1],
			}, func(cmd *cobra.Command, result command.Result) error {
				var data command.GatewayMountData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "mount_id:     %s\n", data.MountID)
				fmt.Fprintf(cmd.OutOrStdout(), "snapshot:     %s\n", data.SnapshotRef)
				fmt.Fprintf(cmd.OutOrStdout(), "mountpoint:   %s\n", data.Mountpoint)
				fmt.Fprintf(cmd.OutOrStdout(), "platform:     %s\n", data.Platform)
				return nil
			})
		}).Command
}

func newUnmountCommand(env *clientEnv) *cobra.Command {
	return newExitCommand(env, "unmount <mount-id-or-path>",
		"Refused: RestoreWeave does not own a mount",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw unmount <mount-id-or-path>\n")
				return 1
			}
			input := map[string]any{}
			if len(args[0]) > 4 && args[0][:4] == "mnt_" {
				input["mount_id"] = args[0]
			} else {
				input["mountpoint"] = args[0]
			}
			return env.request(cmd, command.OpGatewayUnmount, input, func(cmd *cobra.Command, result command.Result) error {
				var data command.GatewayUnmountData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "unmounted: %t\n", data.Unmounted)
				return nil
			})
		}).Command
}
