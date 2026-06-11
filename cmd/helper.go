package cmd

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/Laraakaa/laraka-vpn/internal/config"
	"github.com/Laraakaa/laraka-vpn/internal/helper"
	"github.com/spf13/cobra"
)

// helperCmd runs the privileged tunnel supervisor: it listens on the root-owned
// socket, authorizes the single allowed UID, and supervises the openconnect
// tunnel using only the opaque cookie plus the allowlist-validated host. This is
// the entrypoint loaded by the root LaunchDaemon (§11). It requires the root
// config to be root-owned and not group/world writable.
var helperCmd = &cobra.Command{
	Use:   "helper",
	Short: "Run the privileged tunnel supervisor (root LaunchDaemon)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadRootConfig(cmd.Flag("config-dir").Value.String(), true)
		if err != nil {
			return err
		}

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		sup := helper.NewSupervisor(cfg)
		return sup.Run(ctx)
	},
}

func init() {
	helperCmd.Flags().String("config-dir", "", "Directory containing vpn-daemon.yaml (default: /etc/vpn-cli)")
	rootCmd.AddCommand(helperCmd)
}
