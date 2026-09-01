package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/spf13/cobra"
)

// newViewCommand wires the SavedView lifecycle. Saved views are dynamic
// queries; only export.plan freezes their membership into a manifest.
func newViewCommand(env *clientEnv) *cobra.Command {
	view := &cobra.Command{Use: "view", Short: "Manage dynamic saved views"}
	view.AddCommand(newExitCommand(env, "save <name> <query>",
		"Create or revise a dynamic saved view",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) < 2 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw view save <name> <query>\n")
				return 1
			}
			input := map[string]any{"name": args[0], "query": args[1]}
			fields, _ := cmd.Flags().GetStringSlice("field")
			if len(fields) > 0 {
				input["fields"] = fields
			}
			if sort, _ := cmd.Flags().GetString("sort"); sort != "" {
				input["sort"] = sort
			}
			if out, _ := cmd.Flags().GetString("output-names"); out != "" {
				input["output_names"] = out
			}
			return env.request(cmd, command.OpViewSave, input, func(cmd *cobra.Command, result command.Result) error {
				var data command.ViewData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "view:     %s\n", data.ViewID)
				fmt.Fprintf(out, "name:     %s\n", data.Name)
				fmt.Fprintf(out, "query:    %s\n", data.Query)
				fmt.Fprintf(out, "revision: %d\n", data.Revision)
				return nil
			})
		}).Command)
	view.AddCommand(newExitCommand(env, "get <name-or-id>",
		"Read one saved view without evaluating membership",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw view get <name-or-id>\n")
				return 1
			}
			return env.request(cmd, command.OpViewGet, map[string]any{"name": args[0]}, func(cmd *cobra.Command, result command.Result) error {
				var data command.ViewData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s rev=%d query=%q\n", data.ViewID, data.Name, data.Revision, data.Query)
				return nil
			})
		}).Command)
	view.AddCommand(newExitCommand(env, "list",
		"List every current saved view revision",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			return env.request(cmd, command.OpViewList, nil, func(cmd *cobra.Command, result command.Result) error {
				var data []command.ViewData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				for _, v := range data {
					fmt.Fprintf(out, "%-40s %-24s rev=%-4d %s\n", v.ViewID, v.Name, v.Revision, v.Query)
				}
				return nil
			})
		}).Command)
	view.AddCommand(newExitCommand(env, "evaluate <name-or-id>",
		"Evaluate one saved view against the live search generation",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw view evaluate <name-or-id>\n")
				return 1
			}
			input := map[string]any{"name": args[0]}
			if limit, _ := cmd.Flags().GetInt("limit"); limit > 0 {
				input["limit"] = limit
			}
			return env.request(cmd, command.OpViewEvaluate, input, func(cmd *cobra.Command, result command.Result) error {
				var data command.ViewEvaluateData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "view:   %s\n", data.ViewID)
				fmt.Fprintf(out, "query:  %s\n", data.Query)
				fmt.Fprintf(out, "hits:   %d\n", len(data.Hits))
				for _, hit := range data.Hits {
					fmt.Fprintf(out, "  %s  %s\n", hit.SubjectRef, hit.Name)
				}
				return nil
			})
		}).Command)
	view.PersistentFlags().StringSlice("field", nil, "structured fields to constrain")
	view.PersistentFlags().String("sort", "", "sort policy")
	view.PersistentFlags().String("output-names", "", "output naming policy")
	view.PersistentFlags().Int("limit", 0, "result limit for evaluate")
	return view
}

// newExportCommand wires the frozen ExportManifest loop. export.apply and
// export.verify reference the frozen manifest, never the live view.
func newExportCommand(env *clientEnv) *cobra.Command {
	export := &cobra.Command{Use: "export", Short: "Freeze and materialize an export manifest"}
	export.AddCommand(newExitCommand(env, "plan --view <name-or-id>",
		"Freeze one view evaluation or explicit subject set into an immutable manifest",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			input := map[string]any{}
			if viewName, _ := cmd.Flags().GetString("view"); viewName != "" {
				if strings.HasPrefix(viewName, "view_") {
					input["view_id"] = viewName
				} else {
					input["name"] = viewName
				}
			}
			if subjects, _ := cmd.Flags().GetStringSlice("subject"); len(subjects) > 0 {
				input["subjects"] = subjects
			}
			if len(input) == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw export plan --view <name-or-id> | --subject <ref>...\n")
				return 1
			}
			if rep, _ := cmd.Flags().GetString("representation"); rep != "" {
				input["representation"] = rep
			}
			return env.request(cmd, command.OpExportPlan, input, func(cmd *cobra.Command, result command.Result) error {
				var data command.ExportManifestData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "manifest:      %s\n", data.ManifestID)
				fmt.Fprintf(out, "digest:        %s\n", data.ManifestDigest)
				fmt.Fprintf(out, "subjects:      %d\n", data.SubjectCount)
				fmt.Fprintf(out, "representation:%s\n", data.Representation)
				return nil
			})
		}).Command)
	export.AddCommand(newExitCommand(env, "list",
		"List frozen export manifests",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			return env.request(cmd, command.OpExportList, nil, func(cmd *cobra.Command, result command.Result) error {
				var data []command.ExportManifestData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				for _, m := range data {
					fmt.Fprintf(out, "%-40s %s subjects=%d\n", m.ManifestID, m.ManifestDigest, m.SubjectCount)
				}
				return nil
			})
		}).Command)
	export.AddCommand(newExitCommand(env, "apply <manifest-id> <destination>",
		"Materialize a frozen manifest to an explicit destination",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 2 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw export apply <manifest-id> <destination>\n")
				return 1
			}
			return env.request(cmd, command.OpExportApply, map[string]any{
				"manifest_id": args[0],
				"destination": args[1],
			}, func(cmd *cobra.Command, result command.Result) error {
				var data command.ExportApplyVerifyData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "items=%d bytes=%d verified=%t\n", data.Items, data.Bytes, data.Verified)
				return nil
			})
		}).Command)
	export.AddCommand(newExitCommand(env, "verify <manifest-id> <destination>",
		"Verify materialized output against its frozen manifest",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 2 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw export verify <manifest-id> <destination>\n")
				return 1
			}
			return env.request(cmd, command.OpExportVerify, map[string]any{
				"manifest_id": args[0],
				"destination": args[1],
			}, func(cmd *cobra.Command, result command.Result) error {
				var data command.ExportApplyVerifyData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "items=%d bytes=%d verified=%t\n", data.Items, data.Bytes, data.Verified)
				return nil
			})
		}).Command)
	export.PersistentFlags().String("view", "", "saved view to freeze")
	export.PersistentFlags().StringSlice("subject", nil, "explicit subject references")
	export.PersistentFlags().String("representation", "", "selected representation")
	return export
}
