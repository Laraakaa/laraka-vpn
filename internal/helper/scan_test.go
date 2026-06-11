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
