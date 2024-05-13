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

	return &dc, nil
}

func (dc *DaemonClient) GetStatus() (*DaemonStatus, error) {
	utils.Logger.Debug("Getting status from daemon")
	_, err := dc.socket.SendMessage("status")
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
