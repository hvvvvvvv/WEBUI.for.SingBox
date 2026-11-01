package profile

import (
	"context"
	"os"
	"testing"

	"connectrpc.com/connect"
	"guiforcores/bridge/storage"
	profilev1 "guiforcores/gen/profile/v1"
)

func TestDefaultHTTPClientSurvivesProfilePersistence(t *testing.T) {
	for _, id := range []string{"proxy-id", "direct-id", ""} {
		t.Run(id, func(t *testing.T) {
			paths := storage.NewPaths(t.TempDir())
			service := NewService(paths, nil)
			profile := &profilev1.Profile{Id: "download-policy", Route: &profilev1.Route{DefaultHttpClient: id, Final: "final-id"}}
			if _, err := service.CreateProfile(context.Background(), connect.NewRequest(&profilev1.CreateProfileRequest{Profile: profile})); err != nil {
				t.Fatal(err)
			}
			loaded, err := NewService(paths, nil).FindByID(profile.Id)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.GetRoute().GetDefaultHttpClient() != id || loaded.GetRoute().GetFinal() != "final-id" {
				t.Fatalf("download policy lost after restart: %#v", loaded.GetRoute())
			}
		})
	}
}

func TestOldProfileWithoutDefaultHTTPClientKeepsAutomaticSelection(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	if err := os.MkdirAll(paths.Resolve("data"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte("- id: legacy\n  route:\n    final: fallback-id\n    ruleset:\n      - tag: rules\n        type: 3\n        downloaddetour: direct-id\n")
	if err := os.WriteFile(paths.Resolve(profilesFilePath), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewService(paths, nil).FindByID("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.GetRoute().GetDefaultHttpClient() != "" || loaded.GetRoute().GetRuleSet()[0].GetDownloadDetour() != "direct-id" {
		t.Fatalf("legacy download policy was rewritten: %#v", loaded.GetRoute())
	}
	after, err := os.ReadFile(paths.Resolve(profilesFilePath))
	if err != nil || string(after) != string(raw) {
		t.Fatalf("loading legacy profile changed storage: %s (%v)", after, err)
	}
}
