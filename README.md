# Laraka-VPN

## Setup

Make sure certificates have been extracted.

The following files are required:

- certificate - /etc/vpn-cli/cert.pem
- private key - /etc/vpn-cli/cert.key

To run chainbreaker and obtain certificates:

1. `chainbreaker -f $HOME/Library/Keychains/login.keychain-db --password password > info.txt`
2. Open info.txt, look for the user certificate hexdump and store it as cert.hex