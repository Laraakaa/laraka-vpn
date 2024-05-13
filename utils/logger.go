package utils

import (
	"go.uber.org/zap"
)

var Logger *zap.Logger

func InitCLILogger() error {
	var err error
	Logger, err = zap.NewDevelopment()
	if err != nil {
		return err
	}

	return nil
}
