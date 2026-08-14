package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ailiheizi/restoreweave/client/command"
)

func newSearchCommand(env *clientEnv) *cobra.Command {
	commandNode := newExitCommand(env, "search <query>", "Query the bundled lexical index",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw search <query> [--workspace <id>] [--generation <id>]\n")
				return 1
			}
			input := map[string]any{"query": args[0]}
			if workspace := cmd.Flags().Lookup("workspace").Value.String(); workspace != "" {
				input["workspace_id"] = workspace
			}
			if generation := cmd.Flags().Lookup("generation").Value.String(); generation != "" {
				input["index_generation_ref"] = generation
			}
			return env.request(cmd, command.OpSearchQuery, input, func(cmd *cobra.Command, result command.Result) error {
				var data command.SearchQueryData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				if data.GenerationID != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "generation: %s\n", data.GenerationID)
				}
				if len(data.Hits) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "no hits")
					return nil
				}
				for _, hit := range data.Hits {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", hit.EntryType, hit.SubjectRef, hit.Path, hit.Name)
				}
				return nil
			})
		})
	commandNode.Flags().String("workspace", "", "catalog workspace stable id")
	commandNode.Flags().String("generation", "", "index generation stable id")
	return commandNode.Command
}

func newTagCommand(env *clientEnv) *cobra.Command {
	tag := &cobra.Command{Use: "tag", Short: "List, add, and remove durable tags"}
	tag.AddCommand(newAnnotationSubjectListCommand(env, "list <subject-ref>", "List tags for one subject", "TAG"))
	tag.AddCommand(newTagAddCommand(env))
	tag.AddCommand(newTagRemoveCommand(env))
	return tag
}

func newNoteCommand(env *clientEnv) *cobra.Command {
	note := &cobra.Command{Use: "note", Short: "List, set, and remove durable notes"}
	note.AddCommand(newAnnotationSubjectListCommand(env, "list <subject-ref>", "List notes for one subject", "NOTE"))
	note.AddCommand(newNoteSetCommand(env))
	note.AddCommand(newNoteRemoveCommand(env))
	return note
}

func newAnnotationCommand(env *clientEnv) *cobra.Command {
	annotation := &cobra.Command{Use: "annotation", Short: "List, export, and import durable tags and notes"}
	annotation.AddCommand(newAnnotationListCommand(env))
	annotation.AddCommand(newAnnotationExportCommand(env))
	annotation.AddCommand(newAnnotationImportCommand(env))
	return annotation
}

func newAnnotationSubjectListCommand(env *clientEnv, use, short, kind string) *cobra.Command {
	commandNode := newExitCommand(env, use, short, func(cmd *cobra.Command, env *clientEnv, args []string) int {
		if len(args) != 1 {
			fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw %s [--workspace <id>]\n", use)
			return 1
		}
		return env.request(cmd, command.OpAnnotationList, map[string]any{
			"workspace_id": workspaceValue(cmd),
			"subject_ref":  args[0],
		}, func(cmd *cobra.Command, result command.Result) error {
			return renderAnnotations(cmd, result, kind)
		})
	})
	workspaceFlag(commandNode.Command)
	_ = commandNode.MarkFlagRequired("workspace")
	return commandNode.Command
}

func newTagAddCommand(env *clientEnv) *cobra.Command {
	commandNode := newExitCommand(env, "add <subject-ref> <tag>", "Add a durable tag",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 2 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw tag add <subject-ref> <tag> [--workspace <id>]\n")
				return 1
			}
			return env.request(cmd, command.OpAnnotationUpsert, map[string]any{
				"workspace_id": workspaceValue(cmd),
				"subject_ref":  args[0],
				"kind":         "TAG",
				"body":         args[1],
			}, renderAnnotationUpsert)
		})
	workspaceFlag(commandNode.Command)
	_ = commandNode.MarkFlagRequired("workspace")
	return commandNode.Command
}

func newTagRemoveCommand(env *clientEnv) *cobra.Command {
	commandNode := newExitCommand(env, "remove <subject-ref> <tag>", "Tombstone a durable tag",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 2 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw tag remove <subject-ref> <tag> [--workspace <id>] [--expected-revision <n>]\n")
				return 1
			}
			list, err := env.do(cmd.Context(), command.OpAnnotationList, map[string]any{
				"workspace_id": workspaceValue(cmd),
				"subject_ref":  args[0],
			})
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "cannot reach restoreweaved at %s: %v\n", env.socket, err)
				return 1
			}
			if list.Status != command.StatusSucceeded {
				for _, reason := range list.Reasons {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s\n", reason.Code, reason.Message)
				}
				return list.ExitCode()
			}
			var data command.AnnotationListData
			if err := json.Unmarshal(list.Data, &data); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "decode annotation list: %v\n", err)
				return 1
			}
			var match command.AnnotationData
			for _, item := range data.Annotations {
				if item.Kind == "TAG" && item.Body == args[1] {
					match = item
					break
				}
			}
			if match.ID == "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "tag %q not found\n", args[1])
				return 1
			}
			revision := match.Revision
			if flag := cmd.Flags().Lookup("expected-revision"); flag != nil && flag.Changed {
				parsed, err := cmd.Flags().GetInt64("expected-revision")
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "invalid expected-revision: %v\n", err)
					return 1
				}
				revision = parsed
			}
			return env.request(cmd, command.OpAnnotationDelete, map[string]any{
				"workspace_id":      workspaceValue(cmd),
				"annotation_id":     match.ID,
				"expected_revision": revision,
			}, renderAnnotationUpsert)
		})
	workspaceFlag(commandNode.Command)
	_ = commandNode.MarkFlagRequired("workspace")
	commandNode.Flags().Int64("expected-revision", 0, "expected current revision")
	return commandNode.Command
}

func newNoteSetCommand(env *clientEnv) *cobra.Command {
	commandNode := newExitCommand(env, "set <subject-ref>", "Create or revise a note",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw note set <subject-ref> --body <text> [--workspace <id>] [--id <annotation-id>] [--expected-revision <n>]\n")
				return 1
			}
			body := cmd.Flags().Lookup("body").Value.String()
			if body == "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "note body is required\n")
				return 1
			}
			input := map[string]any{
				"workspace_id": workspaceValue(cmd),
				"subject_ref":  args[0],
				"kind":         "NOTE",
				"body":         body,
			}
			if id := cmd.Flags().Lookup("id").Value.String(); id != "" {
				input["annotation_id"] = id
			}
			if flag := cmd.Flags().Lookup("expected-revision"); flag != nil && flag.Changed {
				parsed, err := cmd.Flags().GetInt64("expected-revision")
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "invalid expected-revision: %v\n", err)
					return 1
				}
				input["expected_revision"] = parsed
			}
			return env.request(cmd, command.OpAnnotationUpsert, input, renderAnnotationUpsert)
		})
	workspaceFlag(commandNode.Command)
	_ = commandNode.MarkFlagRequired("workspace")
	commandNode.Flags().String("body", "", "note text")
	commandNode.Flags().String("id", "", "existing note annotation id")
	commandNode.Flags().Int64("expected-revision", 0, "expected current revision")
	return commandNode.Command
}

func newNoteRemoveCommand(env *clientEnv) *cobra.Command {
	commandNode := newExitCommand(env, "remove <annotation-id>", "Tombstone a note",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw note remove <annotation-id> [--workspace <id>] --expected-revision <n>\n")
				return 1
			}
			revision, err := cmd.Flags().GetInt64("expected-revision")
			if err != nil || revision < 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "expected-revision is required\n")
				return 1
			}
			return env.request(cmd, command.OpAnnotationDelete, map[string]any{
				"workspace_id":      workspaceValue(cmd),
				"annotation_id":     args[0],
				"expected_revision": revision,
			}, renderAnnotationUpsert)
		})
	workspaceFlag(commandNode.Command)
	_ = commandNode.MarkFlagRequired("workspace")
	commandNode.Flags().Int64("expected-revision", 0, "expected current revision")
	_ = commandNode.MarkFlagRequired("expected-revision")
	return commandNode.Command
}

func newAnnotationListCommand(env *clientEnv) *cobra.Command {
	commandNode := newExitCommand(env, "list", "List durable tags and notes",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			input := map[string]any{"workspace_id": workspaceValue(cmd)}
			if subject := cmd.Flags().Lookup("subject").Value.String(); subject != "" {
				input["subject_ref"] = subject
			}
			return env.request(cmd, command.OpAnnotationList, input, func(cmd *cobra.Command, result command.Result) error {
				return renderAnnotations(cmd, result, "")
			})
		})
	workspaceFlag(commandNode.Command)
	_ = commandNode.MarkFlagRequired("workspace")
	commandNode.Flags().String("subject", "", "limit to one subject stable id")
	return commandNode.Command
}

func newAnnotationExportCommand(env *clientEnv) *cobra.Command {
	commandNode := newExitCommand(env, "export", "Export a portable annotation bundle",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			input := map[string]any{"workspace_id": workspaceValue(cmd)}
			if subject := cmd.Flags().Lookup("subject").Value.String(); subject != "" {
				input["subject_ref"] = subject
			}
			return env.request(cmd, command.OpAnnotationExport, input, func(cmd *cobra.Command, result command.Result) error {
				var data command.AnnotationExportData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				payload, err := json.MarshalIndent(data, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(payload))
				return nil
			})
		})
	workspaceFlag(commandNode.Command)
	_ = commandNode.MarkFlagRequired("workspace")
	commandNode.Flags().String("subject", "", "limit to one subject stable id")
	return commandNode.Command
}

func newAnnotationImportCommand(env *clientEnv) *cobra.Command {
	return newExitCommand(env, "import", "Import a portable annotation bundle from JSON stdin",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			var bundle command.AnnotationExportData
			if err := json.NewDecoder(cmd.InOrStdin()).Decode(&bundle); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "decode annotation bundle: %v\n", err)
				return 1
			}
			return env.request(cmd, command.OpAnnotationImport, bundle, func(cmd *cobra.Command, result command.Result) error {
				var data command.AnnotationExportData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "imported: %d\n", len(data.Annotations))
				return nil
			})
		}).Command
}

func renderAnnotations(cmd *cobra.Command, result command.Result, kind string) error {
	var data command.AnnotationListData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		return err
	}
	count := 0
	for _, item := range data.Annotations {
		if kind != "" && item.Kind != kind {
			continue
		}
		count++
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\tr%d\t%s\t%s\n", item.Kind, item.ID, item.Revision, item.SubjectRef, item.Body)
	}
	if count == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no annotations")
	}
	return nil
}

func renderAnnotationUpsert(cmd *cobra.Command, result command.Result) error {
	var data command.AnnotationUpsertData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		return err
	}
	item := data.Annotation
	fmt.Fprintf(cmd.OutOrStdout(), "annotation:  %s\n", item.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "kind:        %s\n", item.Kind)
	fmt.Fprintf(cmd.OutOrStdout(), "revision:    %d\n", item.Revision)
	fmt.Fprintf(cmd.OutOrStdout(), "subject:     %s\n", item.SubjectRef)
	fmt.Fprintf(cmd.OutOrStdout(), "body:        %s\n", item.Body)
	if item.Tombstoned {
		fmt.Fprintln(cmd.OutOrStdout(), "tombstoned:  true")
	}
	return nil
}
