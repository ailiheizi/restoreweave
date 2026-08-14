package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ailiheizi/restoreweave/client/command"
)

func newBooksCommand(env *clientEnv) *cobra.Command {
	books := &cobra.Command{Use: "books", Short: "Browse extracted book metadata over the exact catalog"}
	books.AddCommand(newBooksListCommand(env))
	return books
}

func newBooksListCommand(env *clientEnv) *cobra.Command {
	commandNode := newExitCommand(env, "list",
		"List admitted book metadata and text extracts for one workspace",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			workspace := workspaceValue(cmd)
			if workspace == "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw books list --workspace <id>\n")
				return 1
			}
			input := map[string]any{"workspace_id": workspace}
			if snapshot := cmd.Flags().Lookup("snapshot").Value.String(); snapshot != "" {
				input["snapshot_ref"] = snapshot
			}
			return env.request(cmd, command.OpBooksList, input, func(cmd *cobra.Command, result command.Result) error {
				var data command.BookListData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				if len(data.Works) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "no works")
					return nil
				}
				for _, author := range data.Authors {
					fmt.Fprintf(cmd.OutOrStdout(), "author\t%s\t%d\n", author.Name, len(author.SubjectRefs))
				}
				for _, work := range data.Works {
					fmt.Fprintf(cmd.OutOrStdout(), "work\t%s\t%s\t%s\t%s\t%s\n",
						work.Kind, work.Name, work.Title, work.Author, work.Year)
				}
				return nil
			})
		})
	workspaceFlag(commandNode.Command)
	commandNode.Flags().String("snapshot", "", "optional snapshot ref")
	return commandNode.Command
}
