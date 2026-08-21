package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/ailiheizi/restoreweave/client/command"
)

func newIngestCommand(env *clientEnv) *cobra.Command {
	var protectionMode string
	var fileProtectionValues []string
	var locatorValues []string
	var locatorKind string
	var credentialRef string
	var rightsEvidenceRef string
	var confirmLinkOnly bool
	exit := newExitCommand(env, "ingest <root>", "Capture a local tree using the configured protection policy",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw ingest <root>\n")
				return 1
			}
			locators := make([]command.IngestLocatorInput, 0, len(locatorValues))
			for _, value := range locatorValues {
				locator, err := parseIngestLocatorFlag(value)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "invalid --locator %q: %v\n", value, err)
					return 1
				}
				locator.Kind = locatorKind
				locator.CredentialRef = credentialRef
				locator.RightsEvidenceRef = rightsEvidenceRef
				locators = append(locators, locator)
			}
			input := map[string]any{
				"root":              args[0],
				"confirm_link_only": confirmLinkOnly,
				"external_locators": locators,
			}
			if strings.TrimSpace(protectionMode) != "" {
				input["protection_mode"] = protectionMode
			}
			if len(fileProtectionValues) > 0 {
				fileProtection := make(map[string]string, len(fileProtectionValues))
				for _, value := range fileProtectionValues {
					pathValue, mode, ok := strings.Cut(value, "=")
					if !ok || strings.TrimSpace(pathValue) == "" || strings.TrimSpace(mode) == "" {
						fmt.Fprintf(cmd.ErrOrStderr(), "invalid --file-protection %q: expected relative-path=MODE\n", value)
						return 1
					}
					fileProtection[pathValue] = strings.TrimSpace(mode)
				}
				input["file_protection"] = fileProtection
			}
			return env.request(cmd, command.OpPlanIngest, input,
				func(cmd *cobra.Command, result command.Result) error {
					var data command.PlanIngestData
					if err := json.Unmarshal(result.Data, &data); err != nil {
						return err
					}
					renderPlanIngest(cmd.OutOrStdout(), data)
					return nil
				})
		})
	exit.Flags().StringVar(&protectionMode, "protection", "", "STORE_EXACT, STORE_EXACT_WITH_EXTERNAL_FALLBACK, LINK_ONLY, or METADATA_ONLY")
	exit.Flags().StringArrayVar(&fileProtectionValues, "file-protection", nil, "per-file protection as relative-path=MODE; repeat for overrides")
	exit.Flags().StringArrayVar(&locatorValues, "locator", nil, "external locator as [relative-path=]URI; repeat for alternatives")
	exit.Flags().StringVar(&locatorKind, "locator-kind", "", "optional locator kind applied to all --locator values")
	exit.Flags().StringVar(&credentialRef, "credential-ref", "", "host credential reference applied to all locators")
	exit.Flags().StringVar(&rightsEvidenceRef, "rights-evidence-ref", "", "rights evidence reference applied to all locators")
	exit.Flags().BoolVar(&confirmLinkOnly, "confirm-link-only", false, "confirm that LINK_ONLY stores no local payload")
	return exit.Command
}

func renderPlanIngest(out io.Writer, data command.PlanIngestData) {
	state := data.State
	if state == "" {
		state = "READY"
	}
	fmt.Fprintf(out, "state:       %s\n", state)
	fmt.Fprintf(out, "executable:  %t\n", data.Executable)
	fmt.Fprintf(out, "workspace:   %s\n", data.WorkspaceID)
	fmt.Fprintf(out, "source:      %s\n", data.SourceID)
	if data.ConfigDigest != "" {
		fmt.Fprintf(out, "config:      %s\n", data.ConfigDigest)
	}
	if data.SourceBasisDigest != "" {
		fmt.Fprintf(out, "source basis: %s\n", data.SourceBasisDigest)
	}
	fmt.Fprintf(out, "protection:  %s\n", data.ProtectionMode)
	if data.ProtectionDigest != "" {
		fmt.Fprintf(out, "protection digest: %s\n", data.ProtectionDigest)
	}
	if len(data.FileProtection) > 0 {
		paths := make([]string, 0, len(data.FileProtection))
		for path := range data.FileProtection {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		fmt.Fprintln(out, "file protection:")
		for _, path := range paths {
			fmt.Fprintf(out, "  %s: %s\n", path, data.FileProtection[path])
		}
	}
	renderProtectionDecisions(out, data.ProtectionDecisions)
	renderBlockedIngestEntries(out, data.BlockedEntries)
	fmt.Fprintf(out, "estimated files: %d\n", data.Files)
	fmt.Fprintf(out, "estimated bytes: %d\n", data.Bytes)
	fmt.Fprintf(out, "local:       %d files / %d bytes\n", data.LocalFiles, data.LocalBytes)
	fmt.Fprintf(out, "new bytes:   %d\n", data.NewBytes)
	if data.LinkOnlyFiles > 0 {
		fmt.Fprintf(out, "link-only:   %d files (UNPROTECTED; no local payload)\n", data.LinkOnlyFiles)
	}
	if data.LocatorCount > 0 {
		fmt.Fprintf(out, "locators:    %d (UNVALIDATED)\n", data.LocatorCount)
	}
	if data.PlanID == "" {
		return
	}
	fmt.Fprintf(out, "plan:        %s\n", data.PlanID)
	fmt.Fprintf(out, "plan digest: %s\n", data.PlanDigest)
	if data.Executable && data.PlanDigest != "" {
		fmt.Fprintf(out, "next:        rw plan apply %s --workspace %s --digest %s\n", data.PlanID, data.WorkspaceID, data.PlanDigest)
	}
}

func renderBlockedIngestEntries(out io.Writer, issues []command.IngestPlanIssueData) {
	if len(issues) == 0 {
		return
	}
	fmt.Fprintln(out, "blocked entries:")
	for _, issue := range issues {
		fmt.Fprintf(out, "  %s: %s -> %s; %s (%s)", issue.RelativePath, issue.Mode,
			issue.PlannedOutcome, issue.State, issue.ReasonCode)
		if issue.Message != "" {
			fmt.Fprintf(out, ": %s", issue.Message)
		}
		fmt.Fprintln(out)
	}
}

func renderProtectionDecisions(out io.Writer, decisions []command.IngestProtectionDecisionData) {
	if len(decisions) == 0 {
		return
	}
	fmt.Fprintln(out, "planned outcomes:")
	for _, decision := range decisions {
		fmt.Fprintf(out, "  %s: %s -> %s (%s), %d bytes, %s",
			decision.RelativePath, decision.Mode, decision.PlannedOutcome,
			decision.ReasonCode, decision.ExpectedLogicalBytes, decision.ExpectedContentID)
		if decision.LocatorCount > 0 {
			fmt.Fprintf(out, ", %d locator(s)", decision.LocatorCount)
		}
		fmt.Fprintln(out)
	}
}

func parseIngestLocatorFlag(value string) (command.IngestLocatorInput, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return command.IngestLocatorInput{}, fmt.Errorf("value is empty")
	}
	pathValue := ""
	locatorValue := value
	if separator := strings.IndexByte(value, '='); separator > 0 {
		candidatePath := strings.TrimSpace(value[:separator])
		candidateLocator := strings.TrimSpace(value[separator+1:])
		if !strings.Contains(candidatePath, "://") {
			if parsed, err := url.Parse(candidateLocator); err == nil && parsed.Scheme != "" {
				pathValue = candidatePath
				locatorValue = candidateLocator
			}
		}
	}
	parsed, err := url.Parse(locatorValue)
	if err != nil || strings.TrimSpace(parsed.Scheme) == "" {
		return command.IngestLocatorInput{}, fmt.Errorf("locator must have an explicit URI scheme")
	}
	if strings.ContainsFunc(locatorValue, unicode.IsControl) {
		return command.IngestLocatorInput{}, fmt.Errorf("locator contains a control character")
	}
	if parsed.User != nil {
		return command.IngestLocatorInput{}, fmt.Errorf("locator must not contain embedded credentials; use --credential-ref")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return command.IngestLocatorInput{}, fmt.Errorf("locator query parameters are not portable; use --credential-ref for access material")
	}
	if parsed.Fragment != "" || strings.ContainsRune(locatorValue, '#') {
		return command.IngestLocatorInput{}, fmt.Errorf("locator fragments are not portable; use --credential-ref for access material")
	}
	return command.IngestLocatorInput{Path: pathValue, Locator: locatorValue}, nil
}

func newRestoreCommand(env *clientEnv) *cobra.Command {
	return newExitCommand(env, "restore <snapshot-ref> [destination]",
		"Create a restore plan; apply it separately to write an empty directory",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 && len(args) != 2 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw restore <snapshot-ref> [destination]\n")
				return 1
			}
			input := map[string]any{"snapshot_ref": args[0]}
			if len(args) == 2 {
				input["destination"] = args[1]
			}
			return env.request(cmd, command.OpPlanRestore, input, func(cmd *cobra.Command, result command.Result) error {
				var data command.PlanRestoreData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				renderPlanRestore(cmd.OutOrStdout(), data)
				return nil
			})
		}).Command
}

func renderPlanRestore(out io.Writer, data command.PlanRestoreData) {
	state := data.State
	if state == "" {
		state = "READY"
	}
	fmt.Fprintf(out, "state:        %s\n", state)
	fmt.Fprintf(out, "executable:   %t\n", data.Executable)
	if data.WorkspaceID != "" {
		fmt.Fprintf(out, "workspace:    %s\n", data.WorkspaceID)
	}
	fmt.Fprintf(out, "snapshot:     %s\n", data.SnapshotRef)
	fmt.Fprintf(out, "wrote:        %t (plan only; apply separately)\n", data.Wrote)
	if data.Destination != "" {
		fmt.Fprintf(out, "destination:  %s\n", data.Destination)
	}
	fmt.Fprintf(out, "files:        %d\n", data.Files)
	fmt.Fprintf(out, "bytes:        %d\n", data.Bytes)
	if data.PlanID == "" {
		return
	}
	fmt.Fprintf(out, "plan:         %s\n", data.PlanID)
	fmt.Fprintf(out, "plan digest:  %s\n", data.PlanDigest)
	if data.Executable && data.Destination != "" && data.PlanDigest != "" && data.WorkspaceID != "" {
		fmt.Fprintf(out, "next:         rw plan apply %s --workspace %s --digest %s\n", data.PlanID, data.WorkspaceID, data.PlanDigest)
	}
}

func newSnapshotCommand(env *clientEnv) *cobra.Command {
	snapshot := &cobra.Command{Use: "snapshot", Short: "List and verify published snapshots"}
	snapshot.AddCommand(newExitCommand(env, "list", "List portable snapshots in the repository",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			return env.request(cmd, command.OpSnapshotList, map[string]any{},
				func(cmd *cobra.Command, result command.Result) error {
					var data command.SnapshotListData
					if err := json.Unmarshal(result.Data, &data); err != nil {
						return err
					}
					if len(data.Snapshots) == 0 {
						fmt.Fprintln(cmd.OutOrStdout(), "no snapshots")
						return nil
					}
					for _, item := range data.Snapshots {
						fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s\n", item.SnapshotRef, item.CreatedAt, item.DisplayPath)
					}
					return nil
				})
		}).Command)
	snapshot.AddCommand(newExitCommand(env, "diff <from-ref> <to-ref>",
		"Compare two portable snapshots by original path",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 2 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw snapshot diff <from-ref> <to-ref>\n")
				return 1
			}
			return env.request(cmd, command.OpSnapshotDiff, map[string]any{
				"from_snapshot_ref": args[0],
				"to_snapshot_ref":   args[1],
			}, func(cmd *cobra.Command, result command.Result) error {
				var data command.SnapshotDiffData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				if len(data.Changes) == 0 {
					fmt.Fprintln(out, "no changes")
					return nil
				}
				for _, change := range data.Changes {
					path := change.Path
					if change.Kind == command.DiffMoved {
						path = change.FromPath + " -> " + change.ToPath
					}
					fmt.Fprintf(out, "%s\t%s\t%s\n", change.Kind, change.EntryType, path)
				}
				return nil
			})
		}).Command)
	verify := newExitCommand(env, "verify <snapshot-ref>", "Verify a published snapshot at one declared level",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw snapshot verify <snapshot-ref> [--mode ...] [--destination <dir>]\n")
				return 1
			}
			input := map[string]any{"snapshot_ref": args[0], "mode": cmd.Flags().Lookup("mode").Value.String()}
			if dest := strings.TrimSpace(cmd.Flags().Lookup("destination").Value.String()); dest != "" {
				input["destination"] = dest
			}
			return env.request(cmd, command.OpSnapshotVerify, input,
				func(cmd *cobra.Command, result command.Result) error {
					var data command.SnapshotVerifyData
					if err := json.Unmarshal(result.Data, &data); err != nil {
						return err
					}
					out := cmd.OutOrStdout()
					fmt.Fprintf(out, "snapshot:         %s\n", data.SnapshotRef)
					fmt.Fprintf(out, "mode:             %s\n", data.Mode)
					fmt.Fprintf(out, "accepted level:   %s\n", data.AcceptedLevel)
					fmt.Fprintf(out, "ok:               %t\n", data.OK)
					fmt.Fprintf(out, "files:            %d\n", data.Files)
					fmt.Fprintf(out, "bytes:            %d\n", data.Bytes)
					fmt.Fprintf(out, "attempted files:  %d\n", data.AttemptedFiles)
					fmt.Fprintf(out, "passed files:     %d\n", data.PassedFiles)
					if data.RestoreVerified {
						fmt.Fprintln(out, "restore verified: true")
					}
					return nil
				})
		})
	verify.Flags().String("mode", command.VerifyFullBytes, "authenticated-metadata|sampled-content|full-bytes|restore-drill|clean-recovery")
	verify.Flags().String("destination", "", "required for restore-drill")
	snapshot.AddCommand(verify.Command)
	return snapshot
}

func newRecoveryCommand(env *clientEnv) *cobra.Command {
	recovery := &cobra.Command{Use: "recovery", Short: "Export an independently retainable recovery reference"}
	recovery.AddCommand(newExitCommand(env, "export <snapshot-ref> <destination>",
		"Copy the portable snapshot JSON to a new file without overwriting",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 2 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw recovery export <snapshot-ref> <destination>\n")
				return 1
			}
			return env.request(cmd, command.OpRecoveryExport, map[string]any{
				"snapshot_ref": args[0],
				"destination":  args[1],
			}, func(cmd *cobra.Command, result command.Result) error {
				var data command.RecoveryExportData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "snapshot:      %s\n", data.SnapshotRef)
				fmt.Fprintf(out, "artifact:      %s\n", data.ArtifactPath)
				fmt.Fprintf(out, "digest:        %s\n", data.ManifestDigest)
				fmt.Fprintf(out, "length:        %d\n", data.Length)
				fmt.Fprintf(out, "files:         %d\n", data.Files)
				fmt.Fprintf(out, "independent:   %t\n", data.IndependentlyStored)
				return nil
			})
		}).Command)
	anchor := &cobra.Command{Use: "anchor", Short: "Manage the public recovery trust anchor"}
	anchor.AddCommand(newExitCommand(env, "export <destination>",
		"Export the public recovery trust anchor without overwriting",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw recovery anchor export <destination>\n")
				return 1
			}
			return env.request(cmd, command.OpRecoveryAnchorExport, map[string]any{
				"destination": args[0],
			}, func(cmd *cobra.Command, result command.Result) error {
				var data command.RecoveryAnchorExportData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "anchor:        %s\n", data.ArtifactPath)
				fmt.Fprintf(out, "schema:        %s\n", data.Schema)
				fmt.Fprintf(out, "domain:        %s\n", data.PublicationDomain)
				fmt.Fprintf(out, "key id:        %s\n", data.KeyID)
				fmt.Fprintf(out, "key digest:    %s\n", data.PublicKeyDigest)
				fmt.Fprintf(out, "algorithm:     %s\n", data.Algorithm)
				return nil
			})
		}).Command)
	recovery.AddCommand(anchor)
	recovery.AddCommand(newExitCommand(env, "import <artifact> <trust-anchor>",
		"Verify a portable recovery artifact against an independently retained trust anchor",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) != 2 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw recovery import <artifact> <trust-anchor>\n")
				return 1
			}
			return env.request(cmd, command.OpRecoveryImport, map[string]any{
				"artifact_path":     args[0],
				"trust_anchor_path": args[1],
			}, func(cmd *cobra.Command, result command.Result) error {
				var data command.RecoveryImportData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "schema:         %s\n", data.Schema)
				fmt.Fprintf(out, "snapshot:       %s\n", data.SnapshotRef)
				fmt.Fprintf(out, "manifest:       %s\n", data.ManifestDigest)
				fmt.Fprintf(out, "commit:         %s\n", data.CommitDigest)
				fmt.Fprintf(out, "prepared:       %s\n", data.PreparedClosureDigest)
				fmt.Fprintf(out, "generation:     %d\n", data.Generation)
				fmt.Fprintf(out, "anchor digest:  %s\n", data.TrustAnchorDigest)
				fmt.Fprintf(out, "fact health:    %s\n", data.FactHealth)
				fmt.Fprintf(out, "files:          %d\n", data.Files)
				fmt.Fprintf(out, "bytes:          %d\n", data.Bytes)
				fmt.Fprintf(out, "catalog created: %t\n", data.CatalogCreated)
				return nil
			})
		}).Command)
	token := &cobra.Command{Use: "token", Short: "Export deterministic recovery proof envelopes"}
	token.AddCommand(newExitCommand(env, "export <snapshot-ref> <subject-path>",
		"Export a deterministic proof envelope over one subject's recovery reference",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			if len(args) < 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "usage: rw recovery token export <snapshot-ref> <subject-path>\n")
				return 1
			}
			input := map[string]any{"snapshot_ref": args[0]}
			if len(args) >= 2 {
				input["subject_path"] = args[1]
			}
			return env.request(cmd, command.OpRecoveryTokenExport, input, func(cmd *cobra.Command, result command.Result) error {
				var data command.RecoveryTokenData
				if err := json.Unmarshal(result.Data, &data); err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "token schema:      %s\n", data.TokenSchema)
				fmt.Fprintf(out, "snapshot:          %s\n", data.SnapshotRef)
				fmt.Fprintf(out, "subject:           %s\n", data.SubjectRef)
				fmt.Fprintf(out, "reference id:      %s\n", data.RecoveryReferenceID)
				fmt.Fprintf(out, "expected digest:   %s\n", data.ExpectedContentID)
				fmt.Fprintf(out, "expected length:   %d\n", data.ExpectedLength)
				fmt.Fprintf(out, "commit:            %s\n", data.PublicationCommitRef)
				fmt.Fprintf(out, "anchor:            %s\n", data.TrustAnchorRef)
				fmt.Fprintf(out, "token digest:      %s\n", data.TokenDigest)
				return nil
			})
		}).Command)
	recovery.AddCommand(token)
	return recovery
}
