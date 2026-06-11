package helper

import (
	"strings"
	"testing"

	"github.com/Laraakaa/laraka-vpn/internal/ipc"
)

func TestClassifyLine(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantOK     bool
		wantState  ipc.State
		wantDetail string
	}{
		{
			name:       "success",
			line:       "Configured as 10.1.2.3, with SSL connected and DTLS connected",
			wantOK:     true,
			wantState:  ipc.StateConnected,
			wantDetail: "10.1.2.3",
		},
		{
			name:       "success embedded in noise",
			line:       "foo Configured as 192.168.50.7, with SSL connected and DTLS connected bar",
			wantOK:     true,
			wantState:  ipc.StateConnected,
			wantDetail: "192.168.50.7",
		},
		{
			// Regression for the connect-stuck bug: a dual-stack tunnel
			// prints " + <ipv6>/<prefix>" before the comma and the second
			// transport may be "in progress" (or "disabled"), so the old
			// pattern that required IPv4 immediately before "," and a literal
			// "DTLS connected" never matched and state hung at connecting.
			name:       "dual-stack success with ESP in progress",
			line:       "Configured as 10.45.12.175 + fdab:c123:d456:e789::2da/64, with SSL connected and ESP in progress",
			wantOK:     true,
			wantState:  ipc.StateConnected,
			wantDetail: "10.45.12.175",
		},
		{
			name:       "success with SSL compression token",
			line:       "Configured as 10.1.2.3, with SSL + deflate connected and DTLS + lzs connected",
			wantOK:     true,
			wantState:  ipc.StateConnected,
			wantDetail: "10.1.2.3",
		},
		{
			name:   "ssl not yet connected stays unmatched",
			line:   "Configured as 10.1.2.3 + fdab:c123:d456:e789::2da/64, with SSL in progress and DTLS in progress",
			wantOK: false,
		},
		{
			name:       "failure",
			line:       "Failed to reconnect to host gw1.example.com: Can't assign requested address",
			wantOK:     true,
			wantState:  ipc.StateDegraded,
			wantDetail: "gw1.example.com",
		},
		{
			name:   "irrelevant line",
			line:   "POST https://vpn.example.com/CSCOSSLC/tunnel",
			wantOK: false,
		},
		{
			name:   "empty",
			line:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, ok := classifyLine(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("classifyLine(%q) ok = %v, want %v", tt.line, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if ev.State != tt.wantState {
				t.Errorf("state = %q, want %q", ev.State, tt.wantState)
			}
			if ev.Detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", ev.Detail, tt.wantDetail)
			}
		})
	}
}

func TestScanOutput(t *testing.T) {
	input := strings.Join([]string{
		"Connecting to vpn.example.com",
		"SSL negotiation with vpn.example.com",
		"Configured as 10.9.8.7, with SSL connected and DTLS connected",
		"some periodic keepalive noise",
		"Failed to reconnect to host gw2.example.com: Can't assign requested address",
		"trailing noise",
	}, "\n")

	var got []scanEvent
	scanOutput(strings.NewReader(input), func(ev scanEvent) {
		got = append(got, ev)
	})

	want := []scanEvent{
		{State: ipc.StateConnected, Detail: "10.9.8.7"},
		{State: ipc.StateDegraded, Detail: "gw2.example.com"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
