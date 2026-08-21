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

const maxPlanDecisionsBytes = 1 << 20

func newPlanCommand(env *clientEnv) *cobra.Command {
	plan := &cobra.Command{
		Use:   "plan",
		Short: "Inspect and apply immutable plans",
		Long: "plan.ingest and plan.restore create immutable, reviewable plans. " +
			"plan.apply performs the digest-bound mutation and can be safely replayed.",
	}
	plan.AddCommand(newPlanGetCommand(env))
	plan.AddCommand(newPlanReviseCommand(env))
	plan.AddCommand(newPlanAbandonCommand(env))
	plan.AddCommand(newPlanApplyCommand(env))
	return plan
}

func newPlanGetCommand(env *clientEnv) *cobra.Command {
	commandNode := newExitCommand(env, "get <plan-id>", "Read one immutable plan",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw plan get <plan-id> [--workspace <id>]\n")
				return 1
			}
			return env.request(cmd, command.OpPlanGet, map[string]any{
				"workspace_id": workspaceValue(cmd),
				"plan_id":      args[0],
			}, func(cmd *cobra.Command, result command.Result) error {
				var data command.PlanGetData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "plan_id:          %s\n", data.PlanID)
				fmt.Fprintf(out, "kind:             %s\n", data.Kind)
				fmt.Fprintf(out, "state:            %s\n", data.State)
				fmt.Fprintf(out, "digest:           %s\n", data.PlanDigest)
				fmt.Fprintf(out, "applied:          %t\n", data.Applied)
				fmt.Fprintf(out, "executable:       %t\n", data.Executable)
				if data.WorkspaceID != "" {
					fmt.Fprintf(out, "workspace:         %s\n", data.WorkspaceID)
				}
				if data.SourceBasisDigest != "" {
					fmt.Fprintf(out, "source basis:      %s\n", data.SourceBasisDigest)
				}
				return nil
			})
		})
	workspaceFlag(commandNode.Command)
	_ = commandNode.MarkFlagRequired("workspace")
	return commandNode.Command
}

func newPlanApplyCommand(env *clientEnv) *cobra.Command {
	commandNode := newExitCommand(env, "apply <plan-id>", "Apply one immutable plan digest",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw plan apply <plan-id> [--workspace <id>] [--digest <sha256>]\n")
				return 1
			}
			return env.request(cmd, command.OpPlanApply, map[string]any{
				"workspace_id": workspaceValue(cmd),
				"plan_id":      args[0],
				"plan_digest":  cmd.Flags().Lookup("digest").Value.String(),
			}, func(cmd *cobra.Command, result command.Result) error {
				var data command.PlanApplyData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				renderPlanApply(cmd.OutOrStdout(), data)
				return nil
			})
		})
	workspaceFlag(commandNode.Command)
	commandNode.Flags().String("digest", "", "expected plan digest")
	_ = commandNode.MarkFlagRequired("workspace")
	_ = commandNode.MarkFlagRequired("digest")
	return commandNode.Command
}

func newPlanReviseCommand(env *clientEnv) *cobra.Command {
	var decisionsPath string
	commandNode := newExitCommand(env, "revise <plan-id>", "Create an immutable successor from typed decisions",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintln(cmd.ErrOrStderr(), "usage: rw plan revise <plan-id> --workspace <id> --digest <sha256> [--decisions <json-file>]")
				return 1
			}
			decisions, err := loadPlanRevisionDecisions(decisionsPath)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "invalid decisions: %v\n", err)
				return 1
			}
			return env.request(cmd, command.OpPlanRevise, map[string]any{
				"workspace_id": workspaceValue(cmd),
				"plan_id":      args[0],
				"plan_digest":  cmd.Flags().Lookup("digest").Value.String(),
				"decisions":    decisions,
			}, func(cmd *cobra.Command, result command.Result) error {
				var data command.PlanReviseData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				renderPlanRevision(cmd.OutOrStdout(), data)
				return nil
			})
		})
	workspaceFlag(commandNode.Command)
	commandNode.Flags().String("digest", "", "expected base plan digest")
	commandNode.Flags().StringVar(&decisionsPath, "decisions", "", "JSON file containing typed file decisions")
	_ = commandNode.MarkFlagRequired("workspace")
	_ = commandNode.MarkFlagRequired("digest")
	return commandNode.Command
}

func loadPlanRevisionDecisions(path string) (json.RawMessage, error) {
	if strings.TrimSpace(path) == "" {
		return json.RawMessage("[]"), nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maxPlanDecisionsBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > maxPlanDecisionsBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", maxPlanDecisionsBytes)
	}
	if !json.Valid(payload) {
		return nil, fmt.Errorf("file is not valid JSON")
	}
	return json.RawMessage(payload), nil
}

func renderPlanRevision(out io.Writer, data command.PlanReviseData) {
	fmt.Fprintf(out, "state:            %s\n", data.State)
	fmt.Fprintf(out, "executable:       %t\n", data.Executable)
	fmt.Fprintf(out, "plan_id:          %s\n", data.PlanID)
	fmt.Fprintf(out, "plan digest:      %s\n", data.PlanDigest)
	if data.WorkspaceID != "" {
		fmt.Fprintf(out, "workspace:        %s\n", data.WorkspaceID)
	}
	fmt.Fprintf(out, "base plan:        %s\n", data.BasePlanID)
	fmt.Fprintf(out, "base digest:      %s\n", data.BaseDigest)
	if data.Executable && data.PlanID != "" && data.PlanDigest != "" && data.WorkspaceID != "" {
		fmt.Fprintf(out, "next:              rw plan apply %s --workspace %s --digest %s\n", data.PlanID, data.WorkspaceID, data.PlanDigest)
	}
}

func newPlanAbandonCommand(env *clientEnv) *cobra.Command {
	commandNode := newExitCommand(env, "abandon <plan-id>", "Mark one unapplied plan abandoned",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintln(cmd.ErrOrStderr(), "usage: rw plan abandon <plan-id> --workspace <id> [--digest <sha256>]")
				return 1
			}
			input := map[string]any{
				"workspace_id": workspaceValue(cmd),
				"plan_id":      args[0],
			}
			if digest := strings.TrimSpace(cmd.Flags().Lookup("digest").Value.String()); digest != "" {
				input["plan_digest"] = digest
			}
			return env.request(cmd, command.OpPlanAbandon, input,
				func(cmd *cobra.Command, result command.Result) error {
					var data command.PlanAbandonData
					if err := json.Unmarshal(result.Data, &data); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "plan_id:          %s\n", data.PlanID)
					fmt.Fprintf(cmd.OutOrStdout(), "plan digest:      %s\n", data.PlanDigest)
					fmt.Fprintf(cmd.OutOrStdout(), "abandoned plan:   %s\n", data.AbandonedPlanID)
					fmt.Fprintf(cmd.OutOrStdout(), "already abandoned: %t\n", data.AlreadyAbandoned)
					return nil
				})
		})
	workspaceFlag(commandNode.Command)
	commandNode.Flags().String("digest", "", "optional expected plan digest")
	_ = commandNode.MarkFlagRequired("workspace")
	return commandNode.Command
}

func renderPlanApply(out io.Writer, data command.PlanApplyData) {
	state := data.State
	if state == "" {
		state = "SUCCEEDED"
	}
	result := "applied"
	if data.AlreadyApplied {
		result = "already-applied (replayed)"
	}
	fmt.Fprintf(out, "state:            %s\n", state)
	fmt.Fprintf(out, "result:           %s\n", result)
	fmt.Fprintf(out, "plan_id:          %s\n", data.PlanID)
	fmt.Fprintf(out, "plan digest:      %s\n", data.PlanDigest)
	if data.JobID != "" {
		fmt.Fprintf(out, "job:              %s\n", data.JobID)
	}
	if data.WorkspaceID != "" {
		fmt.Fprintf(out, "workspace:        %s\n", data.WorkspaceID)
	}
	if data.SourceID != "" {
		fmt.Fprintf(out, "source:           %s\n", data.SourceID)
	}
	if data.ScanID != "" {
		fmt.Fprintf(out, "scan:             %s\n", data.ScanID)
	}
	if data.RootID != "" {
		fmt.Fprintf(out, "root:             %s\n", data.RootID)
	}
	if data.SnapshotRef != "" {
		fmt.Fprintf(out, "snapshot:         %s\n", data.SnapshotRef)
	}
	if data.ManifestDigest != "" {
		fmt.Fprintf(out, "manifest digest:  %s\n", data.ManifestDigest)
	}
	if data.ProtectionDigest != "" {
		fmt.Fprintf(out, "protection digest: %s\n", data.ProtectionDigest)
	}
	renderProtectionDecisions(out, data.ProtectionDecisions)
	if data.Destination != "" {
		fmt.Fprintf(out, "destination:      %s\n", data.Destination)
	}
	if data.Files != 0 || data.Bytes != 0 {
		fmt.Fprintf(out, "files:            %d\n", data.Files)
		fmt.Fprintf(out, "bytes:            %d\n", data.Bytes)
	}
	for _, warning := range data.Warnings {
		fmt.Fprintf(out, "warning:          %s\n", warning)
	}
}
