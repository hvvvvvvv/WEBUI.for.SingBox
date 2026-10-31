package profile

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"guiforcores/bridge/storage"
	commonv1 "guiforcores/gen/common/v1"
	profilev1 "guiforcores/gen/profile/v1"
)

func TestRouterPluginSnapshotPersistsAndInvalidUpdatePreservesProfile(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, nil)
	manifest := `{"apiVersion":"webui.router/v1","id":"example.lan-dns","name":"局域网 DNS","description":"","version":"1.0.0","kind":"dns-hijack","config":{"inbound":""}}`
	profile := &profilev1.Profile{Id: "router-profile", RouterPlugins: []*profilev1.RouterPlugin{{Id: "instance-1", Enabled: false, ManifestJson: manifest}}}
	created, err := service.CreateProfile(context.Background(), connect.NewRequest(&profilev1.CreateProfileRequest{Profile: profile}))
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewService(paths, nil).FindByID(profile.Id)
	if err != nil || !proto.Equal(profile, reloaded) {
		t.Fatalf("snapshot lost across restart: %#v %v", reloaded, err)
	}
	invalid := proto.Clone(profile).(*profilev1.Profile)
	invalid.RouterPlugins[0].Enabled = true
	_, err = service.UpdateProfile(context.Background(), connect.NewRequest(&profilev1.UpdateProfileRequest{
		Profile: invalid, ExpectedRevision: &commonv1.ExpectedRevision{InstanceId: created.Msg.State.InstanceId, Revision: created.Msg.State.ItemRevision},
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("enabled incomplete plugin was saved: %v", err)
	}
	after, err := service.FindByID(profile.Id)
	if err != nil || !proto.Equal(profile, after) {
		t.Fatal("invalid update changed saved snapshot")
	}
}
