package helper

import (
	"bufio"
	"io"
	"regexp"

	"github.com/Laraakaa/laraka-vpn/internal/ipc"
)

// Salvage organ A (from the old internal/daemon.go stdout scanner). These
// patterns are compiled once and matched against each line openconnect writes
// to its merged stdout/stderr stream.
var (
	// successRe matches the line openconnect prints once the tunnel is fully
	// established; capture group 1 is the assigned tunnel IP address.
	successRe = regexp.MustCompile(`Configured as (\d+\.\d+\.\d+\.\d+), with SSL connected and DTLS connected`)
	// failureRe matches a fatal reconnect failure; capture group 1 is the host.
	failureRe = regexp.MustCompile(`Failed to reconnect to host ([a-zA-Z0-9.-]+): Can't assign requested address`)
)

// scanEvent is a classified state transition derived from one line of
// openconnect output.
type scanEvent struct {
	State  ipc.State
	Detail string // assigned IP on success, host on failure
}

// classifyLine inspects a single line of openconnect output and reports a
// state transition if the line is significant. ok is false for lines that do
// not change state (the vast majority), so the caller can ignore them.
func classifyLine(line string) (ev scanEvent, ok bool) {
	if m := successRe.FindStringSubmatch(line); m != nil {
		return scanEvent{State: ipc.StateConnected, Detail: m[1]}, true
	}
	if m := failureRe.FindStringSubmatch(line); m != nil {
		return scanEvent{State: ipc.StateDegraded, Detail: m[1]}, true
	}
	return scanEvent{}, false
}

// scanOutput reads openconnect output line by line and invokes emit for every
// significant state transition. It returns when r reaches EOF or errors. This
// is factored out of the supervisor so it can be unit-tested with canned
// output and without launching openconnect.
func scanOutput(r io.Reader, emit func(scanEvent)) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if ev, ok := classifyLine(scanner.Text()); ok {
			emit(ev)
		}
	}
}
