# The Go compiler
GO=go

.PHONY: all
all: build

.PHONY: build
build: build/vpn-cli

build/vpn-cli:
	$(GO) build -o build/vpn-cli

.PHONY: install
install: build
	cp ./build/vpn-cli ~/go/bin/vpn-cli

.PHONY: install-service
install-service: install
	sudo cp ./install/ninja.lara.vpncli.plist /Library/LaunchDaemons
	sudo chmod 644 /Library/LaunchDaemons/ninja.lara.vpncli.plist
	sudo launchctl load -w /Library/LaunchDaemons/ninja.lara.vpncli.plist

.PHONY: uninstall-service
uninstall-service:
	sudo launchctl unload /Library/LaunchDaemons/ninja.lara.vpncli.plist
	sudo rm /Library/LaunchDaemons/ninja.lara.vpncli.plist

.PHONY: reinstall-service
reinstall-service: uninstall-service install-service

.PHONY: run
run: build
	./build/$(BINARY_NAME)

.PHONY: clean
clean:
	$(GO) clean
	rm -rf build/*