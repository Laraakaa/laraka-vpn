// Package config loads and validates the two configuration domains used by the
// two-process design:
//
//   - RootConfig: read by the privileged HELPER from a root-owned file
//     (/etc/vpn-cli/vpn-daemon.yaml). It is the sole source of the --servercert
//     pin, the vpn-slice route set, absolute binary paths, the host allowlist
//     that bounds where the helper will dial, and the UID authorized to drive
//     the privileged socket. The agent can never override any of it (§2, §4,
//     §10).
//
//   - UserConfig: read by the AGENT in the user's Aqua session. It holds the
//     PKCS#11 cert URI (keychain identity), the AnyConnect profile path, the
//     server argument, and the servercert pin used during the --authenticate
//     phase.
//
// Both loaders validate file ownership and permissions before trusting the
// contents (§10c).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// DefaultRootConfigDir is the root-owned configuration directory.
const DefaultRootConfigDir = "/etc/vpn-cli"

// DefaultRootConfigName is the viper config name (no extension) for the root
// helper config.
const DefaultRootConfigName = "vpn-daemon"

// RootConfig is the privileged helper's view of the world. Every field that
// influences what root executes or where it routes traffic lives here, never
// in agent-supplied IPC.
type RootConfig struct {
	// ServerCert is the openconnect --servercert pin (e.g.
	// "pin-sha256:...."). Required.
	ServerCert string

	// Slices is the vpn-slice subnet list. Required and non-empty.
	Slices []string

	// OpenconnectPath / VPNSlicePath are absolute paths to the executables.
	// Absolute paths only — never resolved through $PATH (§10c).
	OpenconnectPath string
	VPNSlicePath    string

	// AllowedUID is the single UID permitted to drive the privileged socket
	// (§4, §9). Used to build ipc.AllowUID.
	AllowedUID uint32

	// Allowlist bounds the hosts the helper will dial on the agent's behalf
	// (§10b). Built from the raw allowlist entries at load time.
	Allowlist *HostAllowlist

	// HelperSocket is the privileged socket path (root-owned dir).
	HelperSocket string
}

// UserConfig is the agent's view: everything needed to run the keychain-signing
// --authenticate phase. It carries no routing or execution authority.
type UserConfig struct {
	// PKCS11URI selects the keychain identity (id= is authoritative).
	PKCS11URI string

	// Profile is the AnyConnect XML profile path.
	Profile string

	// ServerArg is the trailing openconnect server argument
	// ("Swisscom Secure RAS - Mobile ID").
	ServerArg string

	// ServerCert is the --servercert pin used during --authenticate.
	ServerCert string

	// OpenconnectPath is the absolute path to the openconnect executable used
	// for the --authenticate phase. Absolute only — never resolved through
	// $PATH (§10c).
	OpenconnectPath string

	// AgentSocket is the user (CLI⇄agent) socket path.
	AgentSocket string
}

// LoadRootConfig reads and validates the root helper configuration. If dir is
// empty, DefaultRootConfigDir is used. The file must be root-owned and not
// group/world writable (§10c); requirePerms=false skips that check for tests.
func LoadRootConfig(dir string, requirePerms bool) (*RootConfig, error) {
	if dir == "" {
		dir = DefaultRootConfigDir
	}
	v := viper.New()
	v.SetConfigName(DefaultRootConfigName)
	v.SetConfigType("yaml")
	v.AddConfigPath(dir)

	// Defaults for fields that have stable, safe values.
	v.SetDefault("vpn.openconnect_path", "/opt/homebrew/bin/openconnect")
	v.SetDefault("vpn.vpn_slice_path", "/opt/homebrew/bin/vpn-slice")
	v.SetDefault("ipc.helper_socket", "/var/run/laraka-vpn/helper.sock")
	v.SetDefault("vpn.allow_ip_literals", false)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("config: reading root config in %s: %w", dir, err)
	}

	if requirePerms {
		if err := checkSecureFile(v.ConfigFileUsed()); err != nil {
			return nil, err
		}
	}

	cfg := &RootConfig{
		ServerCert:      strings.TrimSpace(v.GetString("vpn.server_cert")),
		Slices:          cleanStrings(v.GetStringSlice("vpn.slices")),
		OpenconnectPath: v.GetString("vpn.openconnect_path"),
		VPNSlicePath:    v.GetString("vpn.vpn_slice_path"),
		HelperSocket:    v.GetString("ipc.helper_socket"),
	}

	// AllowedUID: required, validated to fit a uint32 and to be non-root by
	// policy (root driving its own privileged socket would defeat §4).
	if !v.IsSet("ipc.allowed_uid") {
		return nil, fmt.Errorf("config: ipc.allowed_uid is required")
	}
	uid := v.GetInt("ipc.allowed_uid")
	if uid <= 0 {
		return nil, fmt.Errorf("config: ipc.allowed_uid must be a positive non-root uid, got %d", uid)
	}
	cfg.AllowedUID = uint32(uid)

	// Host allowlist (§10b). Required; an empty allowlist is rejected by
	// NewHostAllowlist.
	allowIP := v.GetBool("vpn.allow_ip_literals")
	al, err := NewHostAllowlist(v.GetStringSlice("vpn.allowed_hosts"), allowIP)
	if err != nil {
		return nil, err
	}
	cfg.Allowlist = al

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *RootConfig) validate() error {
	if !strings.HasPrefix(c.ServerCert, "pin-sha256:") {
		return fmt.Errorf("config: vpn.server_cert must be a pin-sha256 value")
	}
	if len(c.Slices) == 0 {
		return fmt.Errorf("config: vpn.slices must not be empty")
	}
	if !filepath.IsAbs(c.OpenconnectPath) {
		return fmt.Errorf("config: vpn.openconnect_path must be absolute, got %q", c.OpenconnectPath)
	}
	if !filepath.IsAbs(c.VPNSlicePath) {
		return fmt.Errorf("config: vpn.vpn_slice_path must be absolute, got %q", c.VPNSlicePath)
	}
	if !filepath.IsAbs(c.HelperSocket) {
		return fmt.Errorf("config: ipc.helper_socket must be absolute, got %q", c.HelperSocket)
	}
	return nil
}

// SliceArg returns the value for openconnect's -s flag, i.e.
// "vpn-slice <subnet> <subnet> ...". Built entirely from root config; this is
// the correct form that sidesteps the pre-existing daemon.go join bug.
func (c *RootConfig) SliceArg() string {
	return "vpn-slice " + strings.Join(c.Slices, " ")
}

// LoadUserConfig reads and validates the agent's user configuration. If path is
// empty it searches the user config name in dir (or the current directory).
func LoadUserConfig(dir string) (*UserConfig, error) {
	v := viper.New()
	v.SetConfigName("vpn-agent")
	v.SetConfigType("yaml")
	if dir != "" {
		v.AddConfigPath(dir)
	}
	v.AddConfigPath(".")

	v.SetDefault("agent.profile", "/opt/cisco/anyconnect/profile/SWISSCOM-CERTRAS_client_profile.xml")
	v.SetDefault("agent.server_arg", "Swisscom Secure RAS - Mobile ID")
	v.SetDefault("agent.openconnect_path", "/opt/homebrew/bin/openconnect")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("config: reading user config: %w", err)
	}

	cfg := &UserConfig{
		PKCS11URI:       strings.TrimSpace(v.GetString("agent.pkcs11_uri")),
		Profile:         v.GetString("agent.profile"),
		ServerArg:       v.GetString("agent.server_arg"),
		ServerCert:      strings.TrimSpace(v.GetString("agent.server_cert")),
		OpenconnectPath: v.GetString("agent.openconnect_path"),
		AgentSocket:     v.GetString("ipc.agent_socket"),
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *UserConfig) validate() error {
	if !strings.HasPrefix(c.PKCS11URI, "pkcs11:") {
		return fmt.Errorf("config: agent.pkcs11_uri must be a pkcs11: URI")
	}
	if !strings.HasPrefix(c.ServerCert, "pin-sha256:") {
		return fmt.Errorf("config: agent.server_cert must be a pin-sha256 value")
	}
	if c.Profile == "" {
		return fmt.Errorf("config: agent.profile must not be empty")
	}
	if c.ServerArg == "" {
		return fmt.Errorf("config: agent.server_arg must not be empty")
	}
	if !filepath.IsAbs(c.OpenconnectPath) {
		return fmt.Errorf("config: agent.openconnect_path must be absolute, got %q", c.OpenconnectPath)
	}
	return nil
}

// checkSecureFile verifies the config file is owned by root and is not writable
// by group or other (§10c). A tampered or world-writable root config would let
// an attacker steer what root executes.
func checkSecureFile(path string) error {
	if path == "" {
		return fmt.Errorf("config: no config file was loaded")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("config: stat %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("config: %s is a symlink (refusing to follow)", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("config: %s is not a regular file", path)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("config: %s is group/world writable (mode %o)", path, info.Mode().Perm())
	}
	if err := checkOwnerRoot(info); err != nil {
		return fmt.Errorf("config: %s: %w", path, err)
	}
	return nil
}

func cleanStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
