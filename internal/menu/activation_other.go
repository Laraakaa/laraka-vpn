//go:build !darwin

package menu

// ensureMenuBarApp is a no-op off macOS, where the NSApplication activation
// policy has no equivalent. The systray library handles tray registration on
// other platforms without any process-level promotion.
func ensureMenuBarApp() {}
