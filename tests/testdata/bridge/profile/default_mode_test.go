package profile

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"guiforcores/bridge/storage"
	profilev1 "guiforcores/gen/profile/v1"
)

func TestDefaultModeSurvivesProfilePersistence(t *testing.T) {
	for _, mode := range []string{"", "global", "rule", "direct"} {
		t.Run(mode, func(t *testing.T) {
			paths := storage.NewPaths(t.TempDir())
			service := NewService(paths, nil)
			profile := &profilev1.Profile{Id: "mode", Experimental: &profilev1.Experimental{DefaultMode: mode}}
			if _, err := service.CreateProfile(context.Background(), connect.NewRequest(&profilev1.CreateProfileRequest{Profile: profile})); err != nil {
				t.Fatal(err)
			}
			loaded, err := NewService(paths, nil).FindByID(profile.Id)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.GetExperimental().GetDefaultMode() != mode {
				t.Fatalf("mode lost: %v", loaded.GetExperimental())
			}
		})
	}
}
