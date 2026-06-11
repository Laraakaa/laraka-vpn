# macOS Menu Bar Testing Guide

## ✅ What Changed

Migrated from deprecated `menuet` to modern `systray` library:
- ❌ OLD: `menuet` (last updated 2022, uses deprecated NSUserNotification APIs)
- ✅ NEW: `systray` (actively maintained, 3.6k stars, uses modern macOS APIs)

## 🚀 How to Test

Run this command:
```bash
make test-menu
```

## 📍 Where to Look

**TOP-RIGHT corner of your screen** - near WiFi/Battery/Clock icons

You should see a small icon labeled **"VPN"**

## 🖱️ What to Click

1. Click the "VPN" icon
2. Try these menu options:
   - Connect
   - Disconnect
   - Quit

## ❗ Important: Why You Need an App Bundle

On macOS, systray **requires** running as an `.app` bundle. Running `./build/vpn-cli` directly will NOT show the menu bar icon - you'll see the logs but no UI. This is a macOS requirement, not a bug.

## 🛠️ Available Commands

| Command | Description |
|---------|-------------|
| `make build` | Build CLI binary only (no menu bar) |
| `make build-app` | Create the `.app` bundle |
| `make run-app` | Build and launch the app |
| `make test-menu` | Build, launch, and show instructions |

## 📦 What Gets Created

```
build/LarakaVPN.app/
  Contents/
    Info.plist           # Tells macOS this is a menu bar app
    MacOS/
      vpn-cli           # Your executable
```

## 🐛 Troubleshooting

**Problem**: "I don't see the icon!"

**Solutions**:
1. Make sure you ran `make test-menu` or `make run-app` (NOT `./build/vpn-cli`)
2. Check System Preferences > Control Center > Menu Bar Only
3. Try restarting the app with `make run-app`
4. Look for the logs - if you see "Menu bar icon and title set", it's working

**Problem**: "The app closes immediately"

This might happen if the daemon can't bind to the address. Check that port 7770 isn't already in use:
```bash
lsof -i :7770
```

## ✨ Next Steps

To make it more polished:
1. Add a proper app icon (`.icns` file)
2. Update the icon to show VPN status (connected/disconnected)
3. Add keyboard shortcuts
4. Sign the app bundle for distribution
