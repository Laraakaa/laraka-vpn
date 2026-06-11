package cmd

import (
	"fmt"
	"os"

	"github.com/Laraakaa/laraka-vpn/internal/agent"
	"github.com/Laraakaa/laraka-vpn/internal/ipc"
	"github.com/Laraakaa/laraka-vpn/utils"
	"github.com/spf13/cobra"
)

// rootCmd is the base command. With no subcommand it prints help. The two
// long-running entrypoints are the "agent" (user Aqua session: menu bar + auth
// orchestrator) and "helper" (root LaunchDaemon: tunnel supervisor)
// subcommands; end users drive the agent with connect/disconnect/status.
var rootCmd = &cobra.Command{
	Use:          "vpn-cli",
	Short:        "Control the Laraka VPN client",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// Execute runs the root command. Called by main.main().
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// agentSocket resolves the CLI⇄agent control-socket path: the --address flag
// when set, otherwise the per-user default socket.
func agentSocket(cmd *cobra.Command) string {
	if addr := cmd.Flag("address").Value.String(); addr != "" {
		return addr
	}
	return agent.DefaultAgentSocket()
}

// printResponse renders an agent response for the CLI and surfaces any
// agent-reported error as a non-nil error so the process exits non-zero.
func printResponse(resp ipc.Response) error {
	fmt.Printf("state: %s\n", resp.State)
	if resp.Detail != "" {
		fmt.Printf("detail: %s\n", resp.Detail)
	}
	if resp.Error != "" {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}

func init() {
	utils.InitCLILogger()

	// The control socket is a Unix-domain path now (was a ZeroMQ tcp endpoint).
	// Empty means "resolve the per-user default" at call time.
	rootCmd.PersistentFlags().StringP("address", "a", "", "Path to the agent control socket (default: per-user socket)")
}
