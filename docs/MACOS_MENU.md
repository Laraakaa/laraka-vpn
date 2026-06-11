# Running the VPN Client on macOS

## Quick Start

The VPN client uses a **menu bar icon** (systray) on macOS. To see it, you must run it as a proper macOS application bundle:

```bash
make test-menu
```

This will:
1. Build the app bundle
2. Launch it automatically
3. Show you where to look for the menu bar icon

## Where to Find the Menu Bar Icon

Look at the **TOP-RIGHT corner** of your macOS screen, near:
- WiFi icon
- Battery icon
- Clock

You should see a small icon labeled **"VPN"**

## Using the Menu

1. **Click** the VPN icon in the menu bar
2. You'll see a dropdown menu with:
   - **Connect** - Connect to VPN
   - **Disconnect** - Disconnect from VPN
   - **Quit** - Exit the application

## Build Commands

- `make build` - Build the CLI binary only
- `make build-app` - Build the macOS app bundle
- `make run-app` - Build and launch the app
- `make test-menu` - Build, launch, and show helpful instructions

## Why an App Bundle?

On macOS, the `systray` library (used for menu bar icons) requires the application to run as a proper `.app` bundle. Running the binary directly from the command line won't show the menu bar icon - this is a macOS security/UI requirement.

## Technical Details

The app bundle structure:
```
LarakaVPN.app/
  Contents/
    Info.plist          # App configuration
    MacOS/
      vpn-cli          # The actual executable
    Resources/
      (future: app icon)
```

The `Info.plist` includes:
- `LSUIElement=1` - Hides the app from the Dock (menu bar only)
- `NSHighResolutionCapable=True` - Crisp icons on Retina displays
