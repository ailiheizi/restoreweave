package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ailiheizi/restoreweave/client/command"
)

func newSearchCommand(env *clientEnv) *cobra.Command {
	var filterValues []string
	var language, suffix, entryType, contentID, duplicateGroup, protectionMode string
	var sizeMin, sizeMax, mtimeAfter, mtimeBefore int64
	commandNode := newExitCommand(env, "search <query>", "Query the bundled lexical index",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw search <query> [--workspace <id>] [--filter key=value]\n")
				return 1
			}
			input := map[string]any{"query": args[0]}
			if workspace := cmd.Flags().Lookup("workspace").Value.String(); workspace != "" {
				input["workspace_id"] = workspace
			}
			if generation := cmd.Flags().Lookup("generation").Value.String(); generation != "" {
				input["index_generation_ref"] = generation
			}
			if dimension := cmd.Flags().Lookup("dimension").Value.String(); dimension != "" {
				input["dimension"] = dimension
			}
			if axes, err := cmd.Flags().GetStringSlice("axis"); err == nil && len(axes) > 0 {
				input["construct_axes"] = axes
			}
			if fuse, err := cmd.Flags().GetStringSlice("fuse"); err == nil && len(fuse) > 0 {
				input["fuse"] = fuse
			}
			filters, err := collectSearchFilters(filterValues, searchFilterFlags{
				Language: language, Suffix: suffix, EntryType: entryType,
				ContentID: contentID, DuplicateGroup: duplicateGroup,
				ProtectionMode: protectionMode,
				SizeMin:        explicitInt64Flag(cmd, "size-min", sizeMin),
				SizeMax:        explicitInt64Flag(cmd, "size-max", sizeMax),
				MtimeAfter:     explicitInt64Flag(cmd, "mtime-after", mtimeAfter),
				MtimeBefore:    explicitInt64Flag(cmd, "mtime-before", mtimeBefore),
			})
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "invalid search filter: %v\n", err)
				return 1
			}
			if len(filters) > 0 {
				input["filters"] = filters
			}
			return env.request(cmd, command.OpSearchQuery, input, func(cmd *cobra.Command, result command.Result) error {
				var data command.SearchQueryData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				if data.Dimension != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "dimension: %s\n", data.Dimension)
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
	commandNode.Flags().String("dimension", "", "declared index dimension (default lexical-metadata-fts)")
	commandNode.Flags().StringSlice("axis", nil, "restrict lexical construct axes (path,name,suffix,tags,notes,extracted)")
	commandNode.Flags().StringSlice("fuse", nil, "host-owned fusion of two or more declared dimensions")
	commandNode.Flags().StringArrayVar(&filterValues, "filter", nil, "typed filter as key=value; repeat for entry_type, content_id, duplicate_group, protection_mode, language, suffix, size_min, size_max, mtime_after, or mtime_before")
	commandNode.Flags().StringVar(&language, "language", "", "exact language facet")
	commandNode.Flags().StringVar(&suffix, "suffix", "", "exact filename suffix facet")
	commandNode.Flags().StringVar(&entryType, "entry-type", "", "exact entry type facet")
	commandNode.Flags().StringVar(&contentID, "content-id", "", "exact content identity facet")
	commandNode.Flags().StringVar(&duplicateGroup, "duplicate-group", "", "exact duplicate group facet")
	commandNode.Flags().StringVar(&protectionMode, "protection-mode", "", "protection mode facet")
	commandNode.Flags().Int64Var(&sizeMin, "size-min", 0, "minimum logical size facet")
	commandNode.Flags().Int64Var(&sizeMax, "size-max", 0, "maximum logical size facet")
	commandNode.Flags().Int64Var(&mtimeAfter, "mtime-after", 0, "minimum modification time in Unix milliseconds")
	commandNode.Flags().Int64Var(&mtimeBefore, "mtime-before", 0, "maximum modification time in Unix milliseconds")
	return commandNode.Command
}

type searchFilterFlags struct {
	Language, Suffix, EntryType, ContentID, DuplicateGroup, ProtectionMode string
	SizeMin, SizeMax, MtimeAfter, MtimeBefore                              *int64
}

func explicitInt64Flag(cmd *cobra.Command, name string, value int64) *int64 {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	return &value
}

// collectSearchFilters accepts the public search.filters field names and
// keeps numeric facets typed all the way to the command envelope.
func collectSearchFilters(values []string, flags searchFilterFlags) (map[string]any, error) {
	filters := make(map[string]any)
	for _, raw := range values {
		key, value, ok := strings.Cut(raw, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			return nil, fmt.Errorf("expected key=value, got %q", raw)
		}
		if _, exists := filters[key]; exists {
			return nil, fmt.Errorf("duplicate %q", key)
		}
		parsed, err := parseSearchFilterValue(key, value)
		if err != nil {
			return nil, err
		}
		filters[key] = parsed
	}
	for key, value := range map[string]string{
		"language": flags.Language, "suffix": flags.Suffix, "entry_type": flags.EntryType,
		"content_id": flags.ContentID, "duplicate_group": flags.DuplicateGroup,
		"protection_mode": flags.ProtectionMode,
	} {
		if value != "" {
			if _, exists := filters[key]; exists {
				return nil, fmt.Errorf("duplicate %q", key)
			}
			filters[key] = value
		}
	}
	for key, value := range map[string]*int64{
		"size_min": flags.SizeMin, "size_max": flags.SizeMax,
		"mtime_after": flags.MtimeAfter, "mtime_before": flags.MtimeBefore,
	} {
		if value != nil {
			if _, exists := filters[key]; exists {
				return nil, fmt.Errorf("duplicate %q", key)
			}
			filters[key] = *value
		}
	}
	return filters, nil
}

func parseSearchFilterValue(key, value string) (any, error) {
	switch key {
	case "entry_type", "content_id", "duplicate_group", "protection_mode", "language", "suffix":
		return value, nil
	case "size_min", "size_max", "mtime_after", "mtime_before":
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s must be an integer", key)
		}
		return parsed, nil
	default:
		return nil, fmt.Errorf("unknown field %q", key)
	}
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
	commandNode := newExitCommand(env, "import", "Import a portable annotation bundle from JSON stdin",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			var bundle command.AnnotationExportData
			if err := json.NewDecoder(cmd.InOrStdin()).Decode(&bundle); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "decode annotation bundle: %v\n", err)
				return 1
			}
			if conflict := cmd.Flags().Lookup("conflict").Value.String(); conflict != "" {
				bundle.Conflict = conflict
			}
			return env.request(cmd, command.OpAnnotationImport, bundle, func(cmd *cobra.Command, result command.Result) error {
				var data command.AnnotationExportData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "imported: %d\n", len(data.Annotations))
				if data.Conflict != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "conflict: %s\n", data.Conflict)
				}
				return nil
			})
		})
	commandNode.Flags().String("conflict", command.AnnotationConflictFail, "fail|keep-local|keep-imported")
	return commandNode.Command
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
