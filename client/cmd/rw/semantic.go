package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ailiheizi/restoreweave/client/command"
)

// newSemanticCommand exposes the fixed semantic bundle installation operation
// through the same thin command envelope as the other operator workflows.
func newSemanticCommand(env *clientEnv) *cobra.Command {
	semantic := &cobra.Command{Use: "semantic", Short: "Manage the local semantic provider"}
	bundle := &cobra.Command{Use: "bundle", Short: "Install the pinned BGE/ONNX and zvec bundle"}
	install := newExitCommand(env, "install", "Install the semantic bundle; optionally use a local offline archive",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			archivePath, err := cmd.Flags().GetString("archive")
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "read archive flag: %v\n", err)
				return 1
			}
			input := command.SemanticBundleInstallInput{}
			if archivePath != "" {
				input.ArchivePath = archivePath
			}
			return env.request(cmd, command.OpSemanticBundleInstall, input, func(cmd *cobra.Command, result command.Result) error {
				var data command.SemanticBundleInstallData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "profile:     %s\n", data.ProfileID)
				fmt.Fprintf(cmd.OutOrStdout(), "digest:      %s\n", data.ProfileDigest)
				fmt.Fprintf(cmd.OutOrStdout(), "destination: %s\n", data.Destination)
				fmt.Fprintf(cmd.OutOrStdout(), "changed:     %t\n", data.Changed)
				if data.RestartRequired {
					fmt.Fprintln(cmd.OutOrStdout(), "restart:     required before semantic worker/index use")
				}
				return nil
			})
		})
	install.Flags().String("archive", "", "local absolute offline semantic bundle archive (.tar.gz)")
	bundle.AddCommand(install.Command)
	semantic.AddCommand(bundle)
	return semantic
}
