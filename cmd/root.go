package cmd

import (
	"fmt"
	"os"

	"github.com/Laraakaa/laraka-vpn/internal"
	"github.com/Laraakaa/laraka-vpn/utils"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "vpn-cli",
	Short: "Control VPN",
	RunE: func(cmd *cobra.Command, args []string) error {
		// If no subcommand provided, default to "start"
		if len(args) == 0 {
			return runCmd.RunE(cmd, args)
		}
		return cmd.Help()
	},
}

// connectCmd represents the command to connect to the VPN
var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connect to the VPN",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := internal.NewDaemonClient(cmd.Flag("address").Value.String())
		if err != nil {
			return err
		}
		defer client.Close()

		err = client.Connect()
		if err != nil {
			return err
		}

		fmt.Println("VPN connected successfully.")
		return nil
	},
}

// disconnectCmd represents the command to disconnect from the VPN
var disconnectCmd = &cobra.Command{
	Use:   "disconnect",
	Short: "Disconnect from the VPN",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := internal.NewDaemonClient(cmd.Flag("address").Value.String())
		if err != nil {
			return err
		}
		defer client.Close()

		err = client.Disconnect()
		if err != nil {
			return err
		}

		fmt.Println("VPN disconnected successfully.")
		return nil
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// Init logging
	utils.InitCLILogger()

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.laraka-vpn.yaml)")
	rootCmd.PersistentFlags().StringP("address", "a", "tcp://127.0.0.1:7770", "Address of the daemon")

	// Add subcommands
	rootCmd.AddCommand(connectCmd)
	rootCmd.AddCommand(disconnectCmd)

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	// rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
