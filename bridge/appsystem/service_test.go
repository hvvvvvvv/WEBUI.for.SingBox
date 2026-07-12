package appsystem

import (
	"context"
	"testing"

	"guiforcores/bridge/platform"
	"guiforcores/bridge/storage"
	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
)

func TestGetPlatformReturnsBackendOS(t *testing.T) {
	platformService := platform.NewService(
		storage.NewPaths(t.TempDir()),
		nil,
		platform.Environment{OS: "test-os"},
	)
	service := NewService(platformService)

	response, err := service.GetPlatform(
		context.Background(),
		connect.NewRequest(&appv1.GetPlatformRequest{}),
	)
	if err != nil {
		t.Fatalf("GetPlatform returned error: %v", err)
	}
	if response.Msg.GetOs() != "test-os" {
		t.Fatalf("expected test-os, got %q", response.Msg.GetOs())
	}
}
