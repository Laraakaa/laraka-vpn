package internal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"syscall"

	"github.com/Laraakaa/laraka-vpn/utils"
	zmq "github.com/pebbe/zmq4"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

type Daemon struct {
	address string

	status DaemonStatus

	socket  *zmq.Socket
	command *exec.Cmd
}

type DaemonCommand string

const (
	DaemonCommand_STATUS = "status"
)

type DaemonStatus struct {
	Status VPNStatus `json:"vpn_status"`
	Uptime int       `json:"uptime"`
}

type VPNStatus string

const (
	VPNStatus_DISCONNECTED = "disconnected"
	VPNStatus_CONNECTING   = "connecting"
	VPNStatus_CONNECTED    = "connected"
)

func NewDaemon(address string) *Daemon {
	// Initialize Viper
	viper.SetConfigName("vpn-daemon")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("/etc/vpn-cli")
	viper.AddConfigPath(".")
	viper.SafeWriteConfig()
	err := viper.ReadInConfig()
	if err != nil {
		utils.Logger.Error("Failed reading configuration", zap.Error(err))
	}

	return &Daemon{
		address: address,

		status: DaemonStatus{
			Status: VPNStatus_DISCONNECTED,
			Uptime: 0,
		},
	}
}

func (d *Daemon) Start() error {
	utils.Logger.Debug("Starting daemon", zap.String("address", d.address))

	zctx, err := zmq.NewContext()
	if err != nil {
		utils.Logger.Error("Failed creating context", zap.Error(err))
		return err
	}

	d.socket, err = zctx.NewSocket(zmq.REP)
	if err != nil {
		utils.Logger.Error("Failed creating socket", zap.Error(err))
		return err
	}
	err = d.socket.Bind(d.address)
	if err != nil {
		utils.Logger.Error("Failed binding socket", zap.Error(err))
		return err
	}

	for {
		// Wait for next request from client
		var msg string
		msg, err = d.socket.Recv(0)
		if err != nil {
			utils.Logger.Error("Failed receiving message", zap.Error(err))
		}
		utils.Logger.Debug("Received message", zap.String("message", msg))

		switch DaemonCommand(msg) {
		case DaemonCommand_STATUS:
			d.handleStatus()
		}
	}
}

func (d *Daemon) Connect() error {
	if d.status.Status == VPNStatus_DISCONNECTED {
		d.status.Status = VPNStatus_CONNECTING
		d.MenuUpdate()

		d.command = exec.Command(
			"openconnect",
			"--protocol=anyconnect",
			"--os=mac-intel",
			"--xmlconfig=/opt/cisco/anyconnect/profile/SWISSCOM-CERTRAS_client_profile.xml",
			"--sslkey=/etc/vpn-cli/cert.key",
			"--certificate=/etc/vpn-cli/cert.pem",
			fmt.Sprintf("--servercert=%s", viper.GetString("vpn.server_cert")),
			"-s vpn-slice "+strings.Join(viper.GetStringSlice("vpn.slices"), " "),
			"Swisscom Secure RAS - Mobile ID",
		)

		stdoutPipe, err := d.command.StdoutPipe()
		if err != nil {
			utils.Logger.Error("Failed getting stdout pipe", zap.Error(err))
			return err
		}
		d.command.Stderr = d.command.Stdout

		err = d.command.Start()
		if err != nil {
			utils.Logger.Error("Failed starting command", zap.Error(err))
			return err
		}
		utils.Logger.Info("OpenConnect VPN started")

		scanner := bufio.NewScanner(stdoutPipe)

		successfulConnectionRegex, err := regexp.Compile(`Configured as (\d+\.\d+\.\d+\.\d+), with SSL connected and DTLS connected`)
		if err != nil {
			utils.Logger.Error("Failed compiling regex: successful connection", zap.Error(err))
		}

		for scanner.Scan() {
			line := scanner.Text()
			utils.Logger.Debug(fmt.Sprintf("OpenConnect: %s", line))

			if successfulConnectionRegex.MatchString(line) {
				utils.Logger.Info("VPN connected successfully")
				d.status.Status = VPNStatus_CONNECTED
				d.MenuUpdate()
			}
		}

	}

	return nil
}

func (d *Daemon) Disconnect() error {
	err := d.command.Process.Signal(syscall.SIGTERM)
	if err != nil {
		utils.Logger.Error("Failed sending SIGTERM to command", zap.Error(err))
		return err
	}

	utils.Logger.Info("OpenConnect VPN stopping...")

	// Wait for the command to exit
	err = d.command.Wait()
	if err != nil {
		utils.Logger.Error("Exited with error", zap.Error(err))
	}

	utils.Logger.Info("OpenConnect VPN stopped")

	d.status.Status = VPNStatus_DISCONNECTED
	d.MenuUpdate()
	return nil
}

func (d *Daemon) sendMessage(msg interface{}) error {
	buffer, err := json.Marshal(msg)
	if err != nil {
		utils.Logger.Error("Failed marshalling message", zap.Error(err))
		return err
	}

	_, err = d.socket.SendMessage(string(buffer))
	if err != nil {
		utils.Logger.Error("Failed sending message", zap.Error(err))
		return err
	}

	return nil
}

func (d *Daemon) handleStatus() {
	err := d.sendMessage(d.status)
	if err != nil {
		utils.Logger.Error("Failed sending status", zap.Error(err))
	}
}
