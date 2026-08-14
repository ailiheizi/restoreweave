package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ailiheizi/restoreweave/client/command"
)

func newAudioCommand(env *clientEnv) *cobra.Command {
	audio := &cobra.Command{Use: "audio", Short: "Browse extracted audio tags over the exact catalog"}
	audio.AddCommand(newAudioListCommand(env))
	return audio
}

func newAudioListCommand(env *clientEnv) *cobra.Command {
	commandNode := newExitCommand(env, "list",
		"List admitted audio tag artifacts for one workspace",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			workspace := workspaceValue(cmd)
			if workspace == "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw audio list --workspace <id>\n")
				return 1
			}
			input := map[string]any{"workspace_id": workspace}
			if snapshot := cmd.Flags().Lookup("snapshot").Value.String(); snapshot != "" {
				input["snapshot_ref"] = snapshot
			}
			return env.request(cmd, command.OpAudioList, input, func(cmd *cobra.Command, result command.Result) error {
				var data command.AudioListData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				if len(data.Tracks) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "no tracks")
					return nil
				}
				for _, album := range data.Albums {
					fmt.Fprintf(cmd.OutOrStdout(), "album\t%s\t%s\t%d\n",
						album.Artist, album.Title, len(album.SubjectRefs))
				}
				for _, track := range data.Tracks {
					fmt.Fprintf(cmd.OutOrStdout(), "track\t%s\t%d\t%s\t%s\t%s\n",
						track.Name, track.Track, track.Title, track.Artist, track.Album)
				}
				return nil
			})
		})
	workspaceFlag(commandNode.Command)
	commandNode.Flags().String("snapshot", "", "optional snapshot ref")
	return commandNode.Command
}
