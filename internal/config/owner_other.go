//go:build !darwin && !linux

package config

import (
	"fmt"
	"os"
)

// checkOwnerRoot is unimplemented off unix; it fails closed so a non-unix build
// never silently trusts a config file's ownership.
func checkOwnerRoot(_ os.FileInfo) error {
	return fmt.Errorf("owner check unsupported on this platform")
}
