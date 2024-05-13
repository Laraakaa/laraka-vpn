package cmd

import (
	"github.com/Laraakaa/laraka-vpn/internal"
	"github.com/Laraakaa/laraka-vpn/utils"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "start",
	Short: "Starts the VPN service daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		/* command := exec.Command(
			"openconnect",
			"--protocol=anyconnect",
			"--os=mac-intel",
			"--xmlconfig=/opt/cisco/anyconnect/profile/SWISSCOM-CERTRAS_client_profile.xml",
			"--sslkey=/",
			"--certificate=/",
			"Swisscom Secure RAS - Mobile ID",
		)

		err := command.Start()
		if err != nil {
			return err
		} */

		daemon := internal.NewDaemon(cmd.Flag("address").Value.String())

		go func() {
			err := daemon.Start()
			if err != nil {
				utils.Logger.Error("Error running daemon", zap.Error(err))
			}
		}()

		daemon.StartMenu()

		return nil
	},
}

func init() {
	rootCmd.AddCommand(runCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// runCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// runCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
