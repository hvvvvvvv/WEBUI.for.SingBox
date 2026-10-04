package profile

import (
	"bytes"
	"context"
	"fmt"

	"os"
	"reflect"
	"testing"

	"guiforcores/bridge/storage"
	commonv1 "guiforcores/gen/common/v1"
	profilev1 "guiforcores/gen/profile/v1"

	connect "connectrpc.com/connect"
)

func profileExpected(state *commonv1.ResourceState, id string) *commonv1.ExpectedRevision {
	return &commonv1.ExpectedRevision{
		InstanceId: state.GetInstanceId(),
		Revision:   state.GetItemRevisions()[id],
	}
}

type recordingProfileChanges struct {
	changes [][]string
}

func (r *recordingProfileChanges) ProfilesChanged(ids []string) {
	r.changes = append(r.changes, append([]string(nil), ids...))
}

func TestLegacyTunDNSModesLoadWithoutRewritingAndSaveNewFormat(t *testing.T) {
	for _, tc := range []struct {
		name     string
		typeName string
		mode     string
		natName  string
		wantMode string
	}{
		{name: "missing mode with persisted NAT", typeName: "4", natName: "endpointindependentnat", wantMode: "hijack"},
		{name: "empty mode with snake NAT", typeName: "4", mode: "dnsmode: ''", natName: "endpoint_independent_nat", wantMode: "hijack"},
		{name: "native persisted mode with camel NAT", typeName: "4", mode: "dnsmode: native", natName: "endpointIndependentNat", wantMode: "native"},
		{name: "numeric enum and snake mode", typeName: "4", mode: "dns_mode: disabled", natName: "endpoint_independent_nat", wantMode: "disabled"},
		{name: "numeric enum and camel mode", typeName: "4", mode: "dnsMode: native", natName: "endpointIndependentNat", wantMode: "native"},
		{name: "legacy enum and snake mode", typeName: "tun", mode: "dns_mode: disabled", natName: "endpoint_independent_nat", wantMode: "disabled"},
		{name: "legacy enum and camel mode", typeName: "tun", mode: "dnsMode: native", natName: "endpointIndependentNat", wantMode: "native"},
		{name: "legacy enum and persisted mode", typeName: "tun", mode: "dnsmode: hijack", natName: "endpointindependentnat", wantMode: "hijack"},
		{name: "invalid mode retained for generation validation", typeName: "4", mode: "dnsmode: unsupported", natName: "endpoint-independent-nat", wantMode: "unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := storage.NewPaths(t.TempDir())
			service := NewService(paths, nil)
			original := []byte(fmt.Sprintf(`# Keep this comment and formatting on reads.
- id: profile
  inbounds:
    - type: %s
      enable: true
      tun:
        address: [172.18.0.1/30]
        %s
        %s: true
`, tc.typeName, tc.mode, tc.natName))
			if err := os.MkdirAll(paths.Resolve("data"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(paths.Resolve(profilesFilePath), original, 0o644); err != nil {
				t.Fatal(err)
			}
			profiles, err := service.Load()
			if err != nil {
				t.Fatalf("load legacy TUN profile: %v", err)
			}
			tun := profiles[0].GetInbounds()[0].GetTun()
			if tun.GetDnsMode() != tc.wantMode || !reflect.DeepEqual(tun.GetAddress(), []string{"172.18.0.1/30"}) {
				t.Fatalf("unexpected loaded TUN settings: %#v", tun)
			}
			readBytes, err := os.ReadFile(paths.Resolve(profilesFilePath))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(readBytes, original) {
				t.Fatalf("load rewrote legacy TUN profile:\n%s", readBytes)
			}
			if err := service.saveProfiles(profiles); err != nil {
				t.Fatalf("save legacy TUN profile: %v", err)
			}
			written, err := os.ReadFile(paths.Resolve(profilesFilePath))
			if err != nil {
				t.Fatal(err)
			}
			for _, removed := range []string{"endpointindependentnat", "endpoint_independent_nat", "endpointIndependentNat", "endpoint-independent-nat", "dns_address"} {
				if bytes.Contains(written, []byte(removed+":")) {
					t.Fatalf("removed or unsupported field %q persisted:\n%s", removed, written)
				}
			}
			loaded, err := service.Load()
			if err != nil {
				t.Fatalf("reload saved TUN profile: %v", err)
			}
			if got := loaded[0].GetInbounds()[0].GetTun().GetDnsMode(); got != tc.wantMode {
				t.Fatalf("reloaded DNS mode = %q, want %q", got, tc.wantMode)
			}
		})
	}
}

func TestUnchangedProfileSaveNormalizesLegacyTunYAMLWithoutAdvancingRevision(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, nil)
	if err := os.MkdirAll(paths.Resolve("data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Resolve(profilesFilePath), []byte(`- id: profile
  inbounds:
    - type: 4
      enable: true
      tun:
        endpoint_independent_nat: true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.ListProfiles(context.Background(), connect.NewRequest(&profilev1.ListProfilesRequest{}))
	if err != nil {
		t.Fatalf("read legacy profile: %v", err)
	}
	changes := &recordingProfileChanges{}
	service.SetChangeHandler(changes)
	response, err := service.UpdateProfile(context.Background(), connect.NewRequest(&profilev1.UpdateProfileRequest{
		Profile: snapshot.Msg.GetProfiles()[0], ExpectedRevision: profileExpected(snapshot.Msg.GetState(), "profile"),
	}))
	if err != nil {
		t.Fatalf("save unchanged profile: %v", err)
	}
	if response.Msg.GetState().GetStateRevision() != snapshot.Msg.GetState().GetStateRevision() || len(changes.changes) != 0 {
		t.Fatalf("normalizing the persisted format changed profile revisions or notifications: %#v, %#v", response.Msg.GetState(), changes.changes)
	}
	written, err := os.ReadFile(paths.Resolve(profilesFilePath))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(written, []byte("endpoint_independent_nat")) || !bytes.Contains(written, []byte("dnsmode: hijack")) {
		t.Fatalf("unchanged save did not write the current TUN format:\n%s", written)
	}
}

func TestCreateProfileRejectsDuplicateIDs(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, nil)
	if _, err := service.CreateProfile(context.Background(), connect.NewRequest(&profilev1.CreateProfileRequest{
		Profile: &profilev1.Profile{Id: "duplicate", Name: "First"},
	})); err != nil {
		t.Fatal(err)
	}
	_, err := service.CreateProfile(context.Background(), connect.NewRequest(&profilev1.CreateProfileRequest{
		Profile: &profilev1.Profile{Id: "duplicate", Name: "Second"},
	}))
	if connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("duplicate create code = %v", connect.CodeOf(err))
	}

	profiles, loadErr := service.loadProfiles()
	if loadErr != nil {
		t.Fatalf("load profiles: %v", loadErr)
	}
	if len(profiles) != 1 || profiles[0].GetName() != "First" {
		t.Fatalf("duplicate create changed stored data: %#v", profiles)
	}
}

func TestProfileEntityConflictsDoNotOverwriteNewerData(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, nil)
	if err := service.saveProfiles([]*profilev1.Profile{{Id: "first", Name: "First"}, {Id: "second", Name: "Second"}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.ListProfiles(context.Background(), connect.NewRequest(&profilev1.ListProfilesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	firstRevision := profileExpected(snapshot.Msg.GetState(), "first")
	secondRevision := profileExpected(snapshot.Msg.GetState(), "second")

	firstUpdate, err := service.UpdateProfile(context.Background(), connect.NewRequest(&profilev1.UpdateProfileRequest{
		Profile:          &profilev1.Profile{Id: "first", Name: "First from client A"},
		ExpectedRevision: firstRevision,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if firstUpdate.Msg.GetState().GetItemRevision() <= firstRevision.GetRevision() {
		t.Fatalf("item revision did not advance: %#v", firstUpdate.Msg.GetState())
	}

	_, err = service.UpdateProfile(context.Background(), connect.NewRequest(&profilev1.UpdateProfileRequest{
		Profile:          &profilev1.Profile{Id: "first", Name: "stale overwrite"},
		ExpectedRevision: firstRevision,
	}))
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale update code = %v", connect.CodeOf(err))
	}
	stored, err := service.GetProfile(context.Background(), connect.NewRequest(&profilev1.GetProfileRequest{Id: "first"}))
	if err != nil {
		t.Fatal(err)
	}
	if stored.Msg.GetProfile().GetName() != "First from client A" {
		t.Fatalf("stale update overwrote profile: %#v", stored.Msg.GetProfile())
	}

	if _, err := service.UpdateProfile(context.Background(), connect.NewRequest(&profilev1.UpdateProfileRequest{
		Profile:          &profilev1.Profile{Id: "second", Name: "Second from client B"},
		ExpectedRevision: secondRevision,
	})); err != nil {
		t.Fatalf("editing a different entity should succeed: %v", err)
	}
}

func TestProfileOrderingConflictsOnlyWithOrderingChanges(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, nil)
	if err := service.saveProfiles([]*profilev1.Profile{{Id: "first", Name: "First"}, {Id: "second", Name: "Second"}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.ListProfiles(context.Background(), connect.NewRequest(&profilev1.ListProfilesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	orderRevision := &commonv1.ExpectedRevision{
		InstanceId: snapshot.Msg.GetState().GetInstanceId(),
		Revision:   snapshot.Msg.GetState().GetOrderRevision(),
	}

	if _, err := service.UpdateProfile(context.Background(), connect.NewRequest(&profilev1.UpdateProfileRequest{
		Profile:          &profilev1.Profile{Id: "first", Name: "Edited"},
		ExpectedRevision: profileExpected(snapshot.Msg.GetState(), "first"),
	})); err != nil {
		t.Fatal(err)
	}
	reordered, err := service.ReorderProfiles(context.Background(), connect.NewRequest(&profilev1.ReorderProfilesRequest{
		Ids:                   []string{"second", "first"},
		ExpectedOrderRevision: orderRevision,
	}))
	if err != nil {
		t.Fatalf("content edit should not conflict with ordering: %v", err)
	}

	_, err = service.ReorderProfiles(context.Background(), connect.NewRequest(&profilev1.ReorderProfilesRequest{
		Ids:                   []string{"first", "second"},
		ExpectedOrderRevision: orderRevision,
	}))
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale order code = %v; state = %#v", connect.CodeOf(err), reordered.Msg.GetState())
	}

	latest, err := service.ListProfiles(context.Background(), connect.NewRequest(&profilev1.ListProfilesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	beforeCreate := &commonv1.ExpectedRevision{
		InstanceId: latest.Msg.GetState().GetInstanceId(), Revision: latest.Msg.GetState().GetOrderRevision(),
	}
	if _, err := service.CreateProfile(context.Background(), connect.NewRequest(&profilev1.CreateProfileRequest{
		Profile: &profilev1.Profile{Id: "third", Name: "Third"},
	})); err != nil {
		t.Fatal(err)
	}
	_, err = service.ReorderProfiles(context.Background(), connect.NewRequest(&profilev1.ReorderProfilesRequest{
		Ids: []string{"first", "second"}, ExpectedOrderRevision: beforeCreate,
	}))
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("create/order conflict code = %v", connect.CodeOf(err))
	}
}

func TestProfileNoOpAndServerRestartVersionBehavior(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, nil)
	if err := service.saveProfiles([]*profilev1.Profile{{Id: "profile", Name: "Profile"}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.ListProfiles(context.Background(), connect.NewRequest(&profilev1.ListProfilesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	handler := &recordingProfileChanges{}
	service.SetChangeHandler(handler)
	response, err := service.UpdateProfile(context.Background(), connect.NewRequest(&profilev1.UpdateProfileRequest{
		Profile:          &profilev1.Profile{Id: "profile", Name: "Profile"},
		ExpectedRevision: profileExpected(snapshot.Msg.GetState(), "profile"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.GetState().GetStateRevision() != snapshot.Msg.GetState().GetStateRevision() {
		t.Fatalf("no-op update advanced state: before=%d after=%d", snapshot.Msg.GetState().GetStateRevision(), response.Msg.GetState().GetStateRevision())
	}
	if len(handler.changes) != 0 {
		t.Fatalf("no-op update notified kernel: %#v", handler.changes)
	}

	restarted := NewService(paths, nil)
	_, err = restarted.UpdateProfile(context.Background(), connect.NewRequest(&profilev1.UpdateProfileRequest{
		Profile:          &profilev1.Profile{Id: "profile", Name: "After restart"},
		ExpectedRevision: profileExpected(snapshot.Msg.GetState(), "profile"),
	}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("old instance revision code = %v", connect.CodeOf(err))
	}
}
