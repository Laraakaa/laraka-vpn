//go:build darwin || linux

package config

import (
	"fmt"
	"os"
	"syscall"
)

// checkOwnerRoot verifies the file is owned by uid 0. On unix it reads the
// underlying stat structure.
func checkOwnerRoot(info os.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot determine file owner")
	}
	if st.Uid != 0 {
		return fmt.Errorf("not owned by root (uid %d)", st.Uid)
	}
	return nil
}
