package internal

import (
	"encoding/json"

	"github.com/Laraakaa/laraka-vpn/utils"
	zmq "github.com/pebbe/zmq4"
	"go.uber.org/zap"
)

type DaemonClient struct {
	zctx   *zmq.Context
	socket *zmq.Socket
}

func NewDaemonClient(address string) (*DaemonClient, error) {
	utils.Logger.Debug("Creating daemon client", zap.String("address", address))

	var dc DaemonClient
	var err error

	dc.zctx, err = zmq.NewContext()
	if err != nil {
		utils.Logger.Error("Failed creating context", zap.Error(err))
		return nil, err
	}

	dc.socket, err = dc.zctx.NewSocket(zmq.REQ)
	if err != nil {
		utils.Logger.Error("Failed creating socket", zap.Error(err))
		return nil, err
	}

	err = dc.socket.Connect(address)
	if err != nil {
		utils.Logger.Error("Failed connecting socket", zap.Error(err))
		return nil, err
	}

	utils.Logger.Debug("Created new daemon client")

	/* for i := 0; i < 10; i++ {
		dc.socket.SendMessage()
		fmt.Printf("Sending request %d...\n", i)
		s.Send("Hello", 0)

		msg, _ := s.Recv(0)
		fmt.Printf("Received reply %d [ %s ]\n", i, msg)
	} */

	// defer dc.socket.Close()

	return &dc, nil
}

func (dc *DaemonClient) GetStatus() (*DaemonStatus, error) {
	utils.Logger.Debug("Getting status from daemon")
	_, err := dc.socket.SendMessage(DaemonCommand_STATUS)
	if err != nil {
		utils.Logger.Error("Failed sending message", zap.Error(err))
		return nil, err
	}

	msg, err := dc.socket.Recv(0)
	if err != nil {
		utils.Logger.Error("Failed receiving message", zap.Error(err))
		return nil, err
	}
	utils.Logger.Debug("Received message", zap.String("status", msg))

	status := DaemonStatus{}
	err = json.Unmarshal([]byte(msg), &status)
	if err != nil {
		panic(err)
	}

	return &status, nil
}

func (dc *DaemonClient) Connect() error {
	utils.Logger.Debug("Sending connect command to daemon")
	_, err := dc.socket.SendMessage(DaemonCommand_CONNECT)
	if err != nil {
		utils.Logger.Error("Failed sending connect command", zap.Error(err))
		return err
	}
	return nil
}

func (dc *DaemonClient) Disconnect() error {
	utils.Logger.Debug("Sending disconnect command to daemon")
	_, err := dc.socket.SendMessage(DaemonCommand_DISCONNECT)
	if err != nil {
		utils.Logger.Error("Failed sending disconnect command", zap.Error(err))
		return err
	}
	return nil
}

func (dc *DaemonClient) Close() {
	utils.Logger.Debug("Closing daemon client")
	if dc.socket != nil {
		dc.socket.Close()
	}
	if dc.zctx != nil {
		dc.zctx.Term()
	}
}
