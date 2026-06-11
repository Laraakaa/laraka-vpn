//go:build linux || darwin
// +build linux darwin

package menu

import (
	"context"
	"time"

	"fyne.io/systray"
	"github.com/Laraakaa/laraka-vpn/internal/icon"
	"go.uber.org/zap"
)

// refreshInterval is how often the menu polls the controller to reconcile the
// displayed state with the helper's actual tunnel state.
const refreshInterval = 5 * time.Second

// Menu owns the systray lifecycle for the agent. Construct it with New and run
// it on the main goroutine via Run (systray requires the main thread on macOS).
type Menu struct {
	act      actions
	interval time.Duration

	mStatus     *systray.MenuItem
	mConnect    *systray.MenuItem
	mDisconnect *systray.MenuItem
	mQuit       *systray.MenuItem
}

// New builds a Menu that drives the given controller. A nil logger is replaced
// with a no-op logger so the menu never panics on logging.
func New(ctrl Controller, log *zap.Logger) *Menu {
	if log == nil {
		log = zap.NewNop()
	}
	return &Menu{
		act:      actions{ctrl: ctrl, log: log},
		interval: refreshInterval,
	}
}

// Run starts the systray event loop. It blocks until the menu is quit, then
// invokes onExit (if non-nil) after the loop returns. Must be called from the
// main goroutine.
func (m *Menu) Run(onExit func()) {
	systray.Run(m.onReady, func() {
		m.act.log.Info("menu: exiting")
		if onExit != nil {
			onExit()
		}
	})
}

// Quit asks the systray loop to terminate.
func (m *Menu) Quit() { systray.Quit() }

// onReady builds the entire menu synchronously (BUG2 fix: no goroutine builds
// items), wires a single Quit item (BUG1 fix: no duplicate Quit), and starts
// one background loop that handles clicks and periodic refreshes.
func (m *Menu) onReady() {
	systray.SetTemplateIcon(icon.Data, icon.Data)
	systray.SetTitle("VPN")
	systray.SetTooltip("Laraka VPN Client")

	m.mStatus = systray.AddMenuItem("Status: starting", "Current VPN status")
	m.mStatus.Disable()
	systray.AddSeparator()
	m.mConnect = systray.AddMenuItem("Connect", "Authenticate and bring up the VPN")
	m.mDisconnect = systray.AddMenuItem("Disconnect", "Tear down the VPN tunnel")
	systray.AddSeparator()
	m.mQuit = systray.AddMenuItem("Quit", "Quit the VPN agent")

	// Reflect the controller's current state immediately, then keep it fresh.
	m.render()
	go m.loop()
}

// loop is the single event pump: it reacts to menu clicks and ticks a periodic
// refresh. mConnect/mDisconnect drive the Controller (BUG3 fix: no direct
// daemon calls), and every action re-renders the menu afterwards.
func (m *Menu) loop() {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.mConnect.ClickedCh:
			m.act.connect(context.Background())
			m.render()
		case <-m.mDisconnect.ClickedCh:
			m.act.disconnect(context.Background())
			m.render()
		case <-ticker.C:
			m.act.refresh(context.Background())
			m.render()
		case <-m.mQuit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

// render pulls the controller's current state and applies the derived view to
// the live systray items.
func (m *Menu) render() {
	v := viewFor(m.act.ctrl.State(), m.act.ctrl.Message())
	m.mStatus.SetTitle(v.status)
	systray.SetTooltip(v.tooltip)
	setEnabled(m.mConnect, v.connectEnabled)
	setEnabled(m.mDisconnect, v.disconnectEnabled)
}

// setEnabled toggles a menu item's clickable state.
func setEnabled(item *systray.MenuItem, enabled bool) {
	if enabled {
		item.Enable()
		return
	}
	item.Disable()
}
