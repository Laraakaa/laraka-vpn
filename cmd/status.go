package cmd

import (
	"fmt"

	"github.com/Laraakaa/laraka-vpn/internal"
	"github.com/spf13/cobra"
)

// statusCmd represents the status command
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current VPN status",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := internal.NewDaemonClient(cmd.Flag("address").Value.String())
		if err != nil {
			return err
		}

		status, err := client.GetStatus()
		if err != nil {
			return err
		}

		fmt.Println("Current VPN Status:", status.Status)
		fmt.Println("Uptime:", status.Uptime)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// statusCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// statusCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
