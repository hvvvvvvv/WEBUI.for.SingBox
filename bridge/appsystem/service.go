package appsystem

import (
	"context"
	"errors"
	"strings"

	"guiforcores/bridge/platform"
	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
)

type Service struct {
	platform *platform.Service
}

func NewService(platformService *platform.Service) *Service {
	return &Service{platform: platformService}
}

func (s *Service) GetInterfaces(
	_ context.Context,
	_ *connect.Request[appv1.GetInterfacesRequest],
) (*connect.Response[appv1.GetInterfacesResponse], error) {
	result := s.platform.GetInterfaces()
	if !result.Flag {
		return nil, connect.NewError(connect.CodeInternal, errors.New(result.Data))
	}
	interfaces := []string{}
	if result.Data != "" {
		interfaces = strings.Split(result.Data, "|")
	}
	return connect.NewResponse(&appv1.GetInterfacesResponse{Interfaces: interfaces}), nil
}
