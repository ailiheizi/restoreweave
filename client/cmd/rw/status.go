package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ailiheizi/restoreweave/client/command"
)

func newStatusCommand(env *clientEnv) *cobra.Command {
	return newExitCommand(env, "status", "Report daemon status", func(cmd *cobra.Command, env *clientEnv, args []string) int {
		return env.request(cmd, command.OpStatusGet, map[string]any{}, func(cmd *cobra.Command, result command.Result) error {
			var data command.StatusData
			if err := json.Unmarshal(result.Data, &data); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "controller:    %s\n", data.Controller)
			if data.ConfigDigest != "" {
				fmt.Fprintf(out, "config digest: %s\n", data.ConfigDigest)
			}
			fmt.Fprintf(out, "catalog path:  %s\n", data.Catalog.Path)
			fmt.Fprintf(out, "catalog ok:    %t\n", data.Catalog.OK)
			fmt.Fprintf(out, "identify id:   %s\n", data.Identify.ID)
			fmt.Fprintf(out, "rules digest:  %s\n", data.Identify.RulesDigest)
			if data.Listen != "" {
				fmt.Fprintf(out, "listen:        %s\n", data.Listen)
			}
			if data.Repository != nil {
				fmt.Fprintf(out, "repository:    %s\n", data.Repository.Path)
				if data.Repository.RepositoryProfile != "" {
					fmt.Fprintf(out, "repo profile:  %s\n", data.Repository.RepositoryProfile)
				}
				if data.Repository.CompressionProfile != "" {
					fmt.Fprintf(out, "compression:   %s\n", data.Repository.CompressionProfile)
				}
				fmt.Fprintf(out, "repository ok: %t\n", data.Repository.OK)
				fmt.Fprintf(out, "snapshots:     %d\n", data.Repository.Snapshots)
			}
			fmt.Fprintf(out, "publications:  %d\n", data.Publications)
			fmt.Fprintf(out, "plans:         %d\n", data.Plans)
			for _, plan := range data.RecentPlans {
				fmt.Fprintf(out, "plan:          %s %s %s\n", plan.PlanID, plan.Kind, plan.State)
			}
			fmt.Fprintf(out, "jobs:          %d\n", data.Jobs)
			if data.OpenHandles > 0 || data.ReapedHandles > 0 {
				fmt.Fprintf(out, "open handles:  %d\n", data.OpenHandles)
				fmt.Fprintf(out, "reaped:        %d\n", data.ReapedHandles)
			}
			for _, job := range data.RecentJobs {
				fmt.Fprintf(out, "job:           %s %s %s\n", job.JobID, job.Kind, job.State)
			}
			fmt.Fprintf(out, "unimplemented: %s\n", strings.Join(data.Unimplemented, ", "))
			return nil
		})
	}).Command
}
