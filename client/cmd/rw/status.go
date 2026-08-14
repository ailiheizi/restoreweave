package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ailiheizi/restoreweave/client/command"
)

func newStatusCommand(env *clientEnv) *cobra.Command {
	return newExitCommand(env, "status", "Report daemon status", func(cmd *cobra.Command, env *clientEnv, args []string) int {
		return env.request(cmd, command.OpStatusGet, map[string]any{}, func(cmd *cobra.Command, result command.Result) error {
			var data command.StatusData
			if err := json.Unmarshal(result.Data, &data); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "controller:    %s\n", data.Controller)
			fmt.Fprintf(out, "catalog path:  %s\n", data.Catalog.Path)
			fmt.Fprintf(out, "catalog ok:    %t\n", data.Catalog.OK)
			fmt.Fprintf(out, "identify id:   %s\n", data.Identify.ID)
			fmt.Fprintf(out, "rules digest:  %s\n", data.Identify.RulesDigest)
			if data.Listen != "" {
				fmt.Fprintf(out, "listen:        %s\n", data.Listen)
			}
			fmt.Fprintf(out, "unimplemented: %s\n", strings.Join(data.Unimplemented, ", "))
			return nil
		})
	}).Command
}
