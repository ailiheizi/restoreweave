package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	rwconfig "github.com/ailiheizi/restoreweave/config"
)

// newConfigCommand owns the small, local configuration lifecycle.  It does
// not require restoreweaved: first-run setup must work before a daemon exists.
func newConfigCommand(env *clientEnv) *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Create and inspect the persisted RestoreWeave profile",
	}

	var path string
	initCmd := newExitCommand(env, "init", "Create a default config.toml without overwriting an existing file",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			resolved, err := rwconfig.Init(strings.TrimSpace(path))
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config init: %v\n", err)
				return 1
			}
			fmt.Fprintf(cmd.OutOrStdout(), "config:  %s\n", resolved.ConfigPath)
			fmt.Fprintf(cmd.OutOrStdout(), "digest:  %s\n", resolved.Digest)
			return 0
		})
	initCmd.Flags().StringVar(&path, "path", "", "config file path (overrides RESTOREWEAVE_CONFIG)")
	configCmd.AddCommand(initCmd.Command)

	var validatePath string
	validateCmd := newExitCommand(env, "validate", "Validate a persisted config and print its digest",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			resolved, err := rwconfig.LoadEffective(rwconfig.LoadOptions{Path: strings.TrimSpace(validatePath)})
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config validate: %v\n", err)
				return 1
			}
			if env.jsonOut {
				payload, err := resolved.EffectiveJSON()
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "config validate: %v\n", err)
					return 1
				}
				var pretty any
				if err := json.Unmarshal(payload, &pretty); err != nil {
					return 1
				}
				encoded, _ := json.MarshalIndent(pretty, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
				return 0
			}
			fmt.Fprintf(cmd.OutOrStdout(), "valid:   %s\n", resolved.ConfigPath)
			fmt.Fprintf(cmd.OutOrStdout(), "digest:  %s\n", resolved.Digest)
			return 0
		})
	validateCmd.Flags().StringVar(&validatePath, "path", "", "config file path (overrides RESTOREWEAVE_CONFIG)")
	configCmd.AddCommand(validateCmd.Command)

	var showPath string
	var effective bool
	showCmd := newExitCommand(env, "show", "Show the effective redacted configuration",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			resolved, err := rwconfig.LoadEffective(rwconfig.LoadOptions{Path: strings.TrimSpace(showPath)})
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config show: %v\n", err)
				return 1
			}
			if env.jsonOut {
				payload, err := resolved.EffectiveJSON()
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "config show: %v\n", err)
					return 1
				}
				var pretty any
				if err := json.Unmarshal(payload, &pretty); err != nil {
					return 1
				}
				encoded, _ := json.MarshalIndent(pretty, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
				return 0
			}
			var payload []byte
			var marshalErr error
			if ext := strings.ToLower(filepath.Ext(resolved.ConfigPath)); ext == ".yaml" || ext == ".yml" {
				payload, marshalErr = rwconfig.MarshalYAML(resolved.Config)
			} else {
				payload, marshalErr = rwconfig.MarshalTOML(resolved.Config)
			}
			if marshalErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config show: %v\n", marshalErr)
				return 1
			}
			fmt.Fprintln(cmd.OutOrStdout(), strings.TrimSpace(string(payload)))
			return 0
		})
	showCmd.Flags().StringVar(&showPath, "path", "", "config file path (overrides RESTOREWEAVE_CONFIG)")
	showCmd.Flags().BoolVar(&effective, "effective", true, "resolve environment overrides and absolute data paths")
	configCmd.AddCommand(showCmd.Command)
	return configCmd
}
