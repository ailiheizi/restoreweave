package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ailiheizi/restoreweave/client/mcp"
	"github.com/ailiheizi/restoreweave/client/transport"
)

func newMCPCommand(env *clientEnv) *cobra.Command {
	command := newExitCommand(env, "mcp", "Run the local MCP server over stdio",
		func(cmd *cobra.Command, env *clientEnv, args []string) int {
			// Dial with a bounded timeout so an unreachable daemon fails
			// fast, then serve for as long as the peer keeps the session.
			dialCtx, cancelDial := context.WithTimeout(cmd.Context(), env.timeout)
			conn, err := transport.DialContext(dialCtx, env.socket)
			cancelDial()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "cannot reach restoreweaved at %s: %v\n", env.socket, err)
				return 1
			}
			defer conn.Close()
			server := mcp.New(conn)
			if err := server.Run(cmd.Context(), &mcpsdk.StdioTransport{}); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "mcp server: %v\n", err)
				return 1
			}
			return 0
		})
	return command.Command
}
