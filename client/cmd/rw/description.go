package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ailiheizi/restoreweave/client/command"
)

const descriptionCLIInputLimit = 16 << 20

func newDescriptionCommand(env *clientEnv) *cobra.Command {
	description := &cobra.Command{Use: "description", Short: "Create and inspect durable description revisions"}
	description.AddCommand(newDescriptionListCommand(env))
	description.AddCommand(newDescriptionGetCommand(env))
	description.AddCommand(newDescriptionCreateCommand(env))
	return description
}

func newDescriptionListCommand(env *clientEnv) *cobra.Command {
	commandNode := newExitCommand(env, "list [subject-ref]", "List bounded description revision summaries",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) > 1 {
				fmt.Fprintln(cmd.ErrOrStderr(), "usage: rw description list [subject-ref] --workspace <id> [--limit <n>]")
				return 1
			}
			input := map[string]any{
				"workspace_id": workspaceValue(cmd),
				"limit":        mustIntFlag(cmd, "limit"),
			}
			if len(args) == 1 {
				input["subject_ref"] = args[0]
			}
			return env.request(cmd, command.OpDescriptionList, input, renderDescriptionList)
		})
	workspaceFlag(commandNode.Command)
	commandNode.Flags().Int("limit", 100, "maximum summaries to return (1-1000)")
	_ = commandNode.MarkFlagRequired("workspace")
	return commandNode.Command
}

func newDescriptionGetCommand(env *clientEnv) *cobra.Command {
	commandNode := newExitCommand(env, "get <description-id>", "Read one full description revision and its segments",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintln(cmd.ErrOrStderr(), "usage: rw description get <description-id> --workspace <id>")
				return 1
			}
			return env.request(cmd, command.OpDescriptionGet, map[string]any{
				"workspace_id":            workspaceValue(cmd),
				"description_document_id": args[0],
			}, renderDescriptionGet)
		})
	workspaceFlag(commandNode.Command)
	_ = commandNode.MarkFlagRequired("workspace")
	return commandNode.Command
}

func newDescriptionCreateCommand(env *clientEnv) *cobra.Command {
	commandNode := newExitCommand(env, "create <subject-ref>", "Create a description or an immutable successor revision",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintln(cmd.ErrOrStderr(), "usage: rw description create <subject-ref> --workspace <id> --kind <kind> (--body <text> | --body-file <path|->)")
				return 1
			}
			body, err := readDescriptionBody(cmd)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "description body: %v\n", err)
				return 1
			}
			input := map[string]any{
				"workspace_id": workspaceValue(cmd),
				"subject_ref":  args[0],
				"kind":         cmd.Flags().Lookup("kind").Value.String(),
				"body":         body,
			}
			for flag, field := range map[string]string{
				"title": "title", "language": "language", "source-ref": "source_ref",
				"producer-profile": "producer_profile", "visibility": "visibility",
				"predecessor": "predecessor_id",
			} {
				if value := cmd.Flags().Lookup(flag).Value.String(); value != "" {
					input[field] = value
				}
			}
			for flag, field := range map[string]string{"confidence": "confidence", "coverage": "coverage"} {
				if cmd.Flags().Changed(flag) {
					value, getErr := cmd.Flags().GetFloat64(flag)
					if getErr != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "invalid --%s: %v\n", flag, getErr)
						return 1
					}
					input[field] = value
				}
			}
			if cmd.Flags().Changed("accepted") {
				input["accepted"], _ = cmd.Flags().GetBool("accepted")
			}
			if raw := strings.TrimSpace(cmd.Flags().Lookup("metadata").Value.String()); raw != "" {
				if !json.Valid([]byte(raw)) {
					fmt.Fprintln(cmd.ErrOrStderr(), "--metadata must be valid JSON")
					return 1
				}
				input["metadata"] = json.RawMessage(raw)
			}
			return env.request(cmd, command.OpDescriptionCreate, input, renderDescriptionCreate)
		})
	workspaceFlag(commandNode.Command)
	commandNode.Flags().String("kind", "", "USER, IMPORTED, EXTRACTED, AI_SUMMARY, or AI_ANALYSIS")
	commandNode.Flags().String("body", "", "description text for short inputs")
	commandNode.Flags().String("body-file", "", "read description text from a file, or - for stdin")
	commandNode.Flags().String("title", "", "description title")
	commandNode.Flags().String("language", "", "BCP-47-like language label")
	commandNode.Flags().String("source-ref", "", "source or author provenance reference")
	commandNode.Flags().String("producer-profile", "", "producer/model profile reference")
	commandNode.Flags().Float64("confidence", 0, "confidence from 0 to 1")
	commandNode.Flags().Float64("coverage", 0, "source coverage from 0 to 1")
	commandNode.Flags().String("visibility", "", "visibility policy label")
	commandNode.Flags().Bool("accepted", false, "mark this revision as operator accepted")
	commandNode.Flags().String("predecessor", "", "description revision this immutable successor replaces")
	commandNode.Flags().String("metadata", "", "additional provenance metadata as JSON")
	_ = commandNode.MarkFlagRequired("workspace")
	_ = commandNode.MarkFlagRequired("kind")
	return commandNode.Command
}

func readDescriptionBody(cmd *cobra.Command) (string, error) {
	body, err := cmd.Flags().GetString("body")
	if err != nil {
		return "", err
	}
	bodyFile, err := cmd.Flags().GetString("body-file")
	if err != nil {
		return "", err
	}
	if body != "" && bodyFile != "" {
		return "", fmt.Errorf("use exactly one of --body or --body-file")
	}
	if body == "" && bodyFile == "" {
		return "", fmt.Errorf("use exactly one of --body or --body-file")
	}
	if bodyFile == "" {
		if len(body) > descriptionCLIInputLimit {
			return "", fmt.Errorf("input exceeds %d bytes", descriptionCLIInputLimit)
		}
		return body, nil
	}
	var reader io.Reader
	var file *os.File
	if bodyFile == "-" {
		reader = cmd.InOrStdin()
	} else {
		file, err = os.Open(bodyFile)
		if err != nil {
			return "", err
		}
		defer file.Close()
		reader = file
	}
	payload, err := io.ReadAll(io.LimitReader(reader, descriptionCLIInputLimit+1))
	if err != nil {
		return "", err
	}
	if len(payload) > descriptionCLIInputLimit {
		return "", fmt.Errorf("input exceeds %d bytes", descriptionCLIInputLimit)
	}
	return string(payload), nil
}

func renderDescriptionList(cmd *cobra.Command, result command.Result) error {
	var data command.DescriptionListData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		return err
	}
	if len(data.Documents) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no descriptions")
		return nil
	}
	for _, item := range data.Documents {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\tr%d\t%s\t%s\n", item.Kind, item.ID, item.Revision, item.SubjectRef, item.Title)
	}
	if data.Truncated {
		fmt.Fprintln(cmd.OutOrStdout(), "truncated: true")
	}
	return nil
}

func renderDescriptionGet(cmd *cobra.Command, result command.Result) error {
	var data command.DescriptionGetData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		return err
	}
	renderDescriptionHeader(cmd, data.Document)
	fmt.Fprintln(cmd.OutOrStdout(), "body:")
	fmt.Fprintln(cmd.OutOrStdout(), data.Document.Body)
	return nil
}

func renderDescriptionCreate(cmd *cobra.Command, result command.Result) error {
	var data command.DescriptionCreateData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		return err
	}
	renderDescriptionHeader(cmd, data.Document)
	return nil
}

func renderDescriptionHeader(cmd *cobra.Command, item command.DescriptionDocumentData) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "description: %s\n", item.ID)
	fmt.Fprintf(out, "subject:     %s\n", item.SubjectRef)
	fmt.Fprintf(out, "kind:        %s\n", item.Kind)
	fmt.Fprintf(out, "revision:    %d\n", item.Revision)
	fmt.Fprintf(out, "language:    %s\n", item.Language)
	fmt.Fprintf(out, "digest:      %s\n", item.BodyDigest)
	fmt.Fprintf(out, "segments:    %d\n", len(item.Segments))
	if item.PredecessorID != "" {
		fmt.Fprintf(out, "predecessor: %s\n", item.PredecessorID)
	}
}

func mustIntFlag(cmd *cobra.Command, name string) int {
	value, _ := cmd.Flags().GetInt(name)
	return value
}
