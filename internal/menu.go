package internal

import (
	"fmt"

	"github.com/Laraakaa/laraka-vpn/internal/icon"
	"github.com/Laraakaa/laraka-vpn/utils"
	"github.com/getlantern/systray"
	"go.uber.org/zap"
)

func (d *Daemon) StartMenu() {
	onReady := func() {
		utils.Logger.Info("Menu bar ready, setting up UI")
		d.onReady()
	}

	onExit := func() {
		utils.Logger.Info("Menu bar application exiting")
	}

	systray.Run(onReady, onExit)
}

func (d *Daemon) onReady() {
	systray.SetTemplateIcon(icon.Data, icon.Data)
	systray.SetTitle("VPN")
	systray.SetTooltip("Laraka VPN Client")
	mQuitOrig := systray.AddMenuItem("Quit", "Quit the whole app")

	go func() {
		<-mQuitOrig.ClickedCh
		fmt.Println("Requesting quit")
		systray.Quit()
		fmt.Println("Finished quitting")
	}()

	// We can manipulate the systray in other goroutines
	go func() {
		systray.SetTemplateIcon(icon.Data, icon.Data)
		systray.SetTitle("VPN")
		systray.SetTooltip("Laraka VPN Client - Status: " + string(d.status.Status))

		mConnect := systray.AddMenuItem("Connect", "Connect to VPN")
		mDisconnect := systray.AddMenuItem("Disconnect", "Disconnect from VPN")

		systray.AddSeparator()

		mQuit := systray.AddMenuItem("Quit", "Quit the VPN client")

		utils.Logger.Info("Menu items created successfully")

		for {
			select {
			case <-mConnect.ClickedCh:
				utils.Logger.Info("Connect clicked")
				d.Connect()
			case <-mDisconnect.ClickedCh:
				utils.Logger.Info("Disconnect clicked")
				d.Disconnect()
			case <-mQuit.ClickedCh:
				utils.Logger.Info("Quit clicked")
				systray.Quit()
				fmt.Println("Quit now...")
				return
			}
		}
	}()
}

func (d *Daemon) MenuUpdate() {
	systray.SetTitle("VPN")
	systray.SetTooltip("Laraka VPN - " + string(d.status.Status))
	utils.Logger.Debug("Menu updated", zap.String("status", string(d.status.Status)))
}
