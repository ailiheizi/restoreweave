package main

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ailiheizi/restoreweave/client/command"
)

func newCapabilityCommand(env *clientEnv) *cobra.Command {
	capability := &cobra.Command{
		Use:   "capability",
		Short: "Inspect server-side capabilities",
	}
	capability.AddCommand(newExitCommand(env, "list", "List every known operation and its availability state",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			return env.request(cmd, command.OpCapabilityList, map[string]any{}, func(cmd *cobra.Command, result command.Result) error {
				var data command.CapabilityListData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
				fmt.Fprintln(writer, "KIND\tID\tSTATE\tVERSION\tNOTES")
				for _, item := range data.Capabilities {
					fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
						item.Kind, item.ID, item.State, item.Version, item.Notes)
				}
				return writer.Flush()
			})
		}).Command)
	return capability
}
