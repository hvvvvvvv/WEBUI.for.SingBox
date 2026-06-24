package rpcutil

import (
	"errors"

	connect "connectrpc.com/connect"
)

type InvalidArgumentError struct {
	Message string
}

func (e InvalidArgumentError) Error() string {
	return e.Message
}

func AsConnectError(err error) error {
	if err == nil {
		return nil
	}
	var invalidArgument InvalidArgumentError
	if errors.As(err, &invalidArgument) {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	var connectError *connect.Error
	if errors.As(err, &connectError) {
		return err
	}
	return connect.NewError(connect.CodeInternal, err)
}
