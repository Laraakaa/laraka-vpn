# The Go compiler
GO=go
APP_NAME=Laraka VPN
BUNDLE_NAME=LarakaVPN.app

.PHONY: all
all: build

.PHONY: build
build:
	$(GO) build -o build/vpn-cli

.PHONY: build-app
build-app: build
	@echo "Building macOS app bundle..."
	rm -rf build/$(BUNDLE_NAME)
	mkdir -p build/$(BUNDLE_NAME)/Contents/MacOS
	mkdir -p build/$(BUNDLE_NAME)/Contents/Resources
	cp build/vpn-cli build/$(BUNDLE_NAME)/Contents/MacOS/
	cp Info.plist build/$(BUNDLE_NAME)/Contents/
	@echo "✓ App bundle created at build/$(BUNDLE_NAME)"
	@echo "  Run with: open build/$(BUNDLE_NAME)"

.PHONY: run-app
run-app: build-app
	@echo "Launching $(APP_NAME)..."
	open build/$(BUNDLE_NAME)

.PHONY: install
install: build
	ln -sf $(CURDIR)/build/vpn-cli ~/go/bin/vpn-cli

HELPER_PLIST=ninja.lara.vpncli.helper.plist
AGENT_PLIST=ninja.lara.vpncli.agent.plist
LAUNCH_DAEMONS=/Library/LaunchDaemons
LAUNCH_AGENTS=$(HOME)/Library/LaunchAgents

.PHONY: install-service
install-service: install
	sudo cp ./install/$(HELPER_PLIST) $(LAUNCH_DAEMONS)
	sudo chown root:wheel $(LAUNCH_DAEMONS)/$(HELPER_PLIST)
	sudo chmod 644 $(LAUNCH_DAEMONS)/$(HELPER_PLIST)
	sudo launchctl load -w $(LAUNCH_DAEMONS)/$(HELPER_PLIST)
	mkdir -p $(LAUNCH_AGENTS)
	cp ./install/$(AGENT_PLIST) $(LAUNCH_AGENTS)
	chmod 644 $(LAUNCH_AGENTS)/$(AGENT_PLIST)
	launchctl load -w $(LAUNCH_AGENTS)/$(AGENT_PLIST)

.PHONY: uninstall-service
uninstall-service:
	-launchctl unload $(LAUNCH_AGENTS)/$(AGENT_PLIST)
	-rm $(LAUNCH_AGENTS)/$(AGENT_PLIST)
	-sudo launchctl unload $(LAUNCH_DAEMONS)/$(HELPER_PLIST)
	-sudo rm $(LAUNCH_DAEMONS)/$(HELPER_PLIST)

.PHONY: reinstall-service
reinstall-service: uninstall-service install-service

.PHONY: run
run: build
	./build/$(BINARY_NAME)

.PHONY: clean
clean:
	$(GO) clean
	rm -rf build/*

.PHONY: test-menu
test-menu: build-app
	@echo ""
	@echo "========================================="
	@echo "  Testing $(APP_NAME) Menu Bar"
	@echo "========================================="
	@echo ""
	@echo "📍 WHERE TO LOOK:"
	@echo "   Check the TOP-RIGHT corner of your screen"
	@echo "   Look near the WiFi, Battery, and Clock icons"
	@echo "   You should see a small icon labeled 'VPN'"
	@echo ""
	@echo "🖱️  WHAT TO DO:"
	@echo "   1. Click the VPN icon in the menu bar"
	@echo "   2. Try the Connect/Disconnect options"
	@echo "   3. Click 'Quit' when done"
	@echo ""
	@echo "Launching now..."
	@echo "========================================="
	@echo ""
	open build/$(BUNDLE_NAME)