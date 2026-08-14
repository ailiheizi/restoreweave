package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ailiheizi/restoreweave/client/command"
)

func newRepresentationCommand(env *clientEnv) *cobra.Command {
	representation := &cobra.Command{
		Use:   "representation",
		Short: "List representations for one catalog subject",
		Long: "representation list reports catalog records and, when the exact " +
			"lane is present, whether the content id is still in the repository. " +
			"It does not open a content handle.",
	}
	representation.AddCommand(newRepresentationListCommand(env))
	return representation
}

func newRepresentationListCommand(env *clientEnv) *cobra.Command {
	commandNode := newExitCommand(env, "list <entry-id>", "List representations for one subject or file version",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw representation list <entry-id> [--workspace <id>] [--file-version <id>]\n")
				return 1
			}
			input := map[string]any{
				"workspace_id": workspaceValue(cmd),
				"subject_ref":  args[0],
			}
			if version := cmd.Flags().Lookup("file-version").Value.String(); version != "" {
				input["file_version_id"] = version
			}
			return env.request(cmd, command.OpRepresentationList, input, func(cmd *cobra.Command, result command.Result) error {
				var data command.RepresentationListData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				if len(data.Representations) == 0 {
					fmt.Fprintln(out, "no representations")
					return nil
				}
				for _, item := range data.Representations {
					auth := ""
					if item.Authoritative {
						auth = "authoritative"
					}
					fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\t%s\n",
						item.Class, item.ID, item.CodecProfileRef, item.Placement, item.ContentID, auth)
				}
				return nil
			})
		})
	workspaceFlag(commandNode.Command)
	commandNode.Flags().String("file-version", "", "file version stable id")
	_ = commandNode.MarkFlagRequired("workspace")
	return commandNode.Command
}
