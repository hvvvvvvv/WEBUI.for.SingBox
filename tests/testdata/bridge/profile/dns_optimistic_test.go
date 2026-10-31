package profile

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"guiforcores/bridge/storage"
	profilev1 "guiforcores/gen/profile/v1"
)

func TestOptimisticDNSCacheSurvivesProfilePersistence(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	s := NewService(paths, nil)
	profile := &profilev1.Profile{Id: "dns-cache", Dns: &profilev1.Dns{
		Optimistic: &profilev1.DnsOptimisticCache{Enabled: true, Timeout: "12h"},
	}}
	if _, err := s.CreateProfile(context.Background(), connect.NewRequest(&profilev1.CreateProfileRequest{Profile: profile})); err != nil {
		t.Fatal(err)
	}
	// Recreate the service to ensure the value comes back from disk, not memory.
	restarted := NewService(paths, nil)
	loaded, err := restarted.FindByID(profile.Id)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.GetDns().GetOptimistic()
	if !got.GetEnabled() || got.GetTimeout() != "12h" {
		t.Fatalf("saved optimistic cache settings were lost: %#v", got)
	}
}
