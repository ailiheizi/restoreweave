// Command rw is the RestoreWeave client: a thin cobra front end over the
// restoreweaved control plane. Every command performs one real envelope round
// trip over the Unix socket and prints the daemon's honest outcome.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/client/local"
	"github.com/ailiheizi/restoreweave/client/transport"
)

// defaultTimeout bounds one socket round trip.
const defaultTimeout = 15 * time.Second

// exitCommand carries the command's process exit code so tests can execute
// the tree without calling os.Exit. The code lives on the shared clientEnv
// because subcommand nodes are registered as plain cobra commands.
type exitCommand struct {
	*cobra.Command
	env *clientEnv
}

// Code returns the exit code of the last executed command in this tree.
func (r *exitCommand) Code() int { return r.env.exitCode }

func newExitCommand(env *clientEnv, use, short string, run func(cmd *cobra.Command, env *clientEnv, args []string) int) *exitCommand {
	ec := &exitCommand{env: env}
	ec.Command = &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			env.exitCode = run(cmd, env, args)
			return nil
		},
	}
	return ec
}

// clientEnv carries the shared per-invocation settings.
type clientEnv struct {
	socket   string
	jsonOut  bool
	timeout  time.Duration
	exitCode int
}

// do performs one envelope round trip over a fresh connection.
func (e *clientEnv) do(ctx context.Context, operation string, input any) (command.Result, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return command.Result{}, fmt.Errorf("encode input: %w", err)
	}
	conn, err := transport.DialContext(ctx, e.socket)
	if err != nil {
		return command.Result{}, err
	}
	defer conn.Close()
	return conn.Do(ctx, command.Envelope{Operation: operation, Input: payload})
}

// request runs one operation and renders its outcome. A failed round trip
// prints "cannot reach restoreweaved at <socket>" and exits 1. A failed
// result prints its reasons and exits with the result's exit code.
func (e *clientEnv) request(cmd *cobra.Command, operation string, input any, render func(cmd *cobra.Command, result command.Result) error) int {
	ctx, cancel := context.WithTimeout(cmd.Context(), e.timeout)
	defer cancel()
	result, err := e.do(ctx, operation, input)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "cannot reach restoreweaved at %s: %v\n", e.socket, err)
		return 1
	}
	if e.jsonOut {
		payload, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "encode result: %v\n", err)
			return 1
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(payload))
		return result.ExitCode()
	}
	if result.Status != command.StatusSucceeded && result.Status != command.StatusAccepted {
		for _, reason := range result.Reasons {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s\n", reason.Code, reason.Message)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "operation %s failed\n", result.Operation)
		return result.ExitCode()
	}
	if render != nil {
		if err := render(cmd, result); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "render result: %v\n", err)
			return 1
		}
	}
	return 0
}

// NewRootCommand assembles the full rw command tree.
func NewRootCommand() *exitCommand {
	env := &clientEnv{}
	root := &exitCommand{env: env}
	root.Command = &cobra.Command{
		Use:   "rw",
		Short: "RestoreWeave client",
		Long: "rw talks to the restoreweaved daemon over its Unix socket using the " +
			"client/command JSON envelope protocol. Output is plain text by default; " +
			"--json prints the raw command Result.",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().StringVar(&env.socket, "socket", local.DefaultSocketPath(),
		"restoreweaved Unix socket path (overrides RESTOREWEAVE_SOCKET)")
	root.PersistentFlags().BoolVar(&env.jsonOut, "json", false,
		"print the raw command Result as JSON instead of human-readable output")
	root.PersistentFlags().DurationVar(&env.timeout, "timeout", defaultTimeout,
		"socket round-trip timeout")
	root.AddCommand(newStatusCommand(env))
	root.AddCommand(newCapabilityCommand(env))
	root.AddCommand(newNamespaceCommand(env))
	root.AddCommand(newIngestCommand(env))
	root.AddCommand(newSnapshotCommand(env))
	root.AddCommand(newRestoreCommand(env))
	root.AddCommand(newRecoveryCommand(env))
	root.AddCommand(newMountCommand(env))
	root.AddCommand(newUnmountCommand(env))
	root.AddCommand(newSearchCommand(env))
	root.AddCommand(newTagCommand(env))
	root.AddCommand(newNoteCommand(env))
	root.AddCommand(newAnnotationCommand(env))
	root.AddCommand(newRepresentationCommand(env))
	root.AddCommand(newContentCommand(env))
	root.AddCommand(newAudioCommand(env))
	root.AddCommand(newBooksCommand(env))
	root.AddCommand(newMCPCommand(env))
	return root
}
