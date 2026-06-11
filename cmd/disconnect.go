package cmd

import (
	"github.com/Laraakaa/laraka-vpn/internal/ipc"
	"github.com/spf13/cobra"
)

// disconnectCmd asks the running agent to tear down the tunnel. The agent
// handles this synchronously (a fast helper round trip), so the printed state
// reflects the post-disconnect status.
var disconnectCmd = &cobra.Command{
	Use:   "disconnect",
	Short: "Ask the agent to disconnect from the VPN",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client := ipc.NewClient(agentSocket(cmd))
		resp, err := client.Do(ipc.Request{Command: ipc.CmdDisconnect})
		if err != nil {
			return err
		}
		return printResponse(resp)
	},
}

func init() {
	rootCmd.AddCommand(disconnectCmd)
}
