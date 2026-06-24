package settings

import (
	"context"

	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
)

type Backend interface {
	GetAppSettings(context.Context, *connect.Request[appv1.GetAppSettingsRequest]) (*connect.Response[appv1.GetAppSettingsResponse], error)
	SaveAppSettings(context.Context, *connect.Request[appv1.SaveAppSettingsRequest]) (*connect.Response[appv1.SaveAppSettingsResponse], error)
}

type Service struct {
	backend Backend
}

func NewService(backend Backend) *Service {
	return &Service{backend: backend}
}

func (s *Service) GetAppSettings(ctx context.Context, req *connect.Request[appv1.GetAppSettingsRequest]) (*connect.Response[appv1.GetAppSettingsResponse], error) {
	return s.backend.GetAppSettings(ctx, req)
}

func (s *Service) SaveAppSettings(ctx context.Context, req *connect.Request[appv1.SaveAppSettingsRequest]) (*connect.Response[appv1.SaveAppSettingsResponse], error) {
	return s.backend.SaveAppSettings(ctx, req)
}
