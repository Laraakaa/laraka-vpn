package cmd

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/Laraakaa/laraka-vpn/internal/agent"
	"github.com/Laraakaa/laraka-vpn/internal/config"
	"github.com/Laraakaa/laraka-vpn/internal/menu"
	"github.com/Laraakaa/laraka-vpn/utils"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// agentCmd runs the user-session agent: the menu-bar UI plus the authentication
// orchestrator (Controller). It also serves the per-user CLI⇄agent control
// socket so connect/disconnect/status work. This is the entrypoint loaded by
// the Aqua LaunchAgent (§11).
var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Run the user-session agent (menu bar + auth orchestrator)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadUserConfig(cmd.Flag("config-dir").Value.String())
		if err != nil {
			return err
		}

		// Socket precedence: explicit --address, then config, then the
		// per-user default. The CLI resolves the same way so they agree.
		socketPath := cmd.Flag("address").Value.String()
		if socketPath == "" {
			socketPath = cfg.AgentSocket
		}
		if socketPath == "" {
			socketPath = agent.DefaultAgentSocket()
		}

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		ctrl := agent.NewController(cfg)

		// Control socket runs in the background; the menu owns the main
		// goroutine because systray must run there on macOS.
		srv := agent.NewServer(ctrl, socketPath)
		go func() {
			if err := srv.Run(ctx); err != nil {
				utils.Logger.Error("agent control server stopped", zap.Error(err))
			}
		}()

		m := menu.New(ctrl, utils.Logger)

		// Break systray.Run when we receive a termination signal. A redundant
		// Quit after a user-initiated quit is tolerated by systray.
		go func() {
			<-ctx.Done()
			m.Quit()
		}()

		m.Run(stop)
		return nil
	},
}

func init() {
	agentCmd.Flags().String("config-dir", "", "Directory containing vpn-agent.yaml (default: current directory)")
	rootCmd.AddCommand(agentCmd)
}
