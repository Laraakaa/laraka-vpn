package internal

import (
	"github.com/caseymrm/menuet"
)

func (d *Daemon) StartMenu() {
	menuet.App().Label = "ninja.lara.vpn"
	menuet.App().Children = func() []menuet.MenuItem { return d.menuItems() }

	d.MenuUpdate()
	menuet.App().RunApplication()
}

func (d *Daemon) MenuUpdate() {
	menuet.App().SetMenuState(&menuet.MenuState{
		Title: "VPN: " + string(d.status.Status),
	})
}

func (d *Daemon) menuItems() []menuet.MenuItem {
	return []menuet.MenuItem{
		{
			Text: "Actions",
			Children: func() []menuet.MenuItem {
				return []menuet.MenuItem{
					{
						Text: "Connect",
						Clicked: func() {
							d.Connect()
						},
					},
					{
						Text: "Disconnect",
						Clicked: func() {
							d.Disconnect()
						},
					},
				}
			},
		},
	}
}
