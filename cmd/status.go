package cmd

import (
	"github.com/Laraakaa/laraka-vpn/internal/ipc"
	"github.com/spf13/cobra"
)

// statusCmd asks the running agent for the current VPN state. The agent
// reconciles with the privileged helper before replying, so the printed state
// reflects the live tunnel status.
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the current VPN status",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client := ipc.NewClient(agentSocket(cmd))
		resp, err := client.Do(ipc.Request{Command: ipc.CmdStatus})
		if err != nil {
			return err
		}
		return printResponse(resp)
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
