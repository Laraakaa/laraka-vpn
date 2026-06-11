package cmd

import (
	"github.com/Laraakaa/laraka-vpn/internal/ipc"
	"github.com/spf13/cobra"
)

// connectCmd asks the running agent to begin authentication. The agent treats
// connect as fire-and-forget (the keychain-signing --authenticate phase may
// block on a Mobile ID push far longer than the IPC deadline), so this returns
// as soon as the request is accepted; observe progress with "status".
var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Ask the agent to connect to the VPN",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client := ipc.NewClient(agentSocket(cmd))
		resp, err := client.Do(ipc.Request{Command: ipc.CmdConnect})
		if err != nil {
			return err
		}
		return printResponse(resp)
	},
}

func init() {
	rootCmd.AddCommand(connectCmd)
}
