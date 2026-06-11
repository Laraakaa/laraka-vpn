//go:build darwin

package menu

/*
#cgo darwin CFLAGS: -x objective-c -fobjc-arc
#cgo darwin LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

// setAccessoryActivationPolicy promotes the running process to a UI-element
// ("accessory") app so it can own an NSStatusBar item. We do this in code
// rather than relying solely on the bundle's LSUIElement key because launchd
// starts the agent by posix_spawn'ing the inner Mach-O directly, and on recent
// macOS that path does not reliably apply the bundle's LSUIElement, leaving
// NSApp at NSApplicationActivationPolicyProhibited (status item never renders).
// systray itself never calls setActivationPolicy, so without this the menu-bar
// icon silently fails to appear. -[NSApplication sharedApplication] is
// idempotent, so systray reuses this same instance when it sets its delegate.
// Must be called on the main thread before the systray event loop starts.
static void setAccessoryActivationPolicy(void) {
	[NSApplication sharedApplication];
	[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
}
*/
import "C"

// ensureMenuBarApp configures the process so a status-bar item can render,
// independent of how the binary was launched (bundle double-click vs. launchd
// direct exec). Call it on the main goroutine before starting systray.
func ensureMenuBarApp() {
	C.setAccessoryActivationPolicy()
}
