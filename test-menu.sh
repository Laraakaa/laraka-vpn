#!/bin/bash

# Test script for the menu bar application
# This will run the VPN client and show you where to look for it

echo "🚀 Starting Laraka VPN Client..."
echo ""
echo "📍 WHERE TO FIND IT ON macOS:"
echo "   Look at the TOP-RIGHT corner of your screen"
echo "   The menu bar icon should appear near:"
echo "   - WiFi icon"
echo "   - Battery icon" 
echo "   - Clock"
echo ""
echo "   You should see a small square icon labeled 'VPN'"
echo ""
echo "🖱️  WHAT TO DO:"
echo "   1. Click on the VPN icon in the menu bar"
echo "   2. You'll see a dropdown menu with:"
echo "      - Connect"
echo "      - Disconnect"
echo "      - Quit"
echo ""
echo "⌨️  Press Ctrl+C to stop the application"
echo ""
echo "Starting now..."
echo "----------------------------------------"

./build/vpn-cli start --address="tcp://127.0.0.1:5555"
