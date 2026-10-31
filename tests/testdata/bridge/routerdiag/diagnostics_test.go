package routerdiag

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testFileInfo struct{ mode fs.FileMode }

func (testFileInfo) Name() string        { return "tun" }
func (testFileInfo) Size() int64         { return 0 }
func (i testFileInfo) Mode() fs.FileMode { return i.mode }
func (testFileInfo) ModTime() time.Time  { return time.Time{} }
func (i testFileInfo) IsDir() bool       { return i.mode.IsDir() }
func (testFileInfo) Sys() any            { return nil }

func linuxSources() (sources, map[string]string) {
	files := map[string]string{
		"/proc/self/status":                      "Name:\ttest\nCapEff:\t0000000000001000\n",
		"/etc/openwrt_release":                   "DISTRIB_ID='OpenWrt'\nDISTRIB_RELEASE='21.02'\nDISTRIB_TARGET='mediatek/filogic'\nSECRET='not-reported'\n",
		"/proc/sys/net/ipv4/ip_forward":          "1\n",
		"/proc/sys/net/ipv6/conf/all/forwarding": "0\n",
		"/proc/net/tcp":                          "sl local_address rem_address st\n0: 0100007F:0035 00000000:0000 0A\n",
		"/proc/net/tcp6":                         "sl local_address rem_address st\n",
		"/proc/net/udp":                          "sl local_address rem_address st\n0: 00000000:0035 00000000:0000 07\n",
		"/proc/net/udp6":                         "sl local_address rem_address st\n",
	}
	return sources{
		os: "linux", arch: "arm64", uid: 0,
		read: func(ctx context.Context, path string, limit int64) ([]byte, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			data, ok := files[path]
			if !ok {
				return nil, fs.ErrNotExist
			}
			if int64(len(data)) > limit {
				return nil, errReadLimit
			}
			return []byte(data), nil
		},
		stat: func(path string) (fs.FileInfo, error) {
			if path != "/dev/net/tun" {
				return nil, fs.ErrNotExist
			}
			return testFileInfo{fs.ModeDevice | fs.ModeCharDevice}, nil
		},
		lookup: func(name string) (string, error) {
			if name == "iptables" {
				return "/usr/sbin/iptables", nil
			}
			return "", fs.ErrNotExist
		},
	}, files
}

func findCheck(t *testing.T, report Report, id string) Check {
	t.Helper()
	for _, check := range report.Checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("missing %s check: %+v", id, report)
	return Check{}
}

func TestLinuxPrerequisitesDoNotClaimCompleteCompatibility(t *testing.T) {
	source, _ := linuxSources()
	report := collect(context.Background(), source)
	if report.OS != "linux" || report.Arch != "arm64" {
		t.Fatalf("platform lost: %+v", report)
	}
	for id, status := range map[string]Status{"platform": StatusOK, "uid": StatusOK, "net_admin": StatusOK, "tun": StatusOK, "openwrt": StatusOK, "nft": StatusWarning, "iptables": StatusOK, "fw4": StatusWarning, "ipv4_forwarding": StatusOK, "ipv6_forwarding": StatusWarning, "dns53": StatusOK} {
		if got := findCheck(t, report, id).Status; got != status {
			t.Errorf("%s=%s want=%s", id, got, status)
		}
	}
	if !strings.Contains(findCheck(t, report, "iptables").Message, "not verified") {
		t.Fatal("command availability claimed full support")
	}
	if !strings.Contains(findCheck(t, report, "dns53").Message, "not a port conflict") {
		t.Fatal("normal DNS service was labeled a conflict")
	}
	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, sensitive := range []string{"not-reported", "0100007F", "00000000", "CapEff"} {
		if strings.Contains(string(payload), sensitive) {
			t.Errorf("raw system information leaked: %s", sensitive)
		}
	}
	if !strings.Contains(string(payload), `"checks"`) || !strings.Contains(string(payload), `"status":"ok"`) {
		t.Fatalf("invalid JSON contract: %s", payload)
	}
}

func TestNonLinuxDoesNotProbeTheHost(t *testing.T) {
	source, _ := linuxSources()
	source.os = "windows"
	source.read = func(context.Context, string, int64) ([]byte, error) {
		t.Error("read on unsupported OS")
		return nil, nil
	}
	source.stat = func(string) (fs.FileInfo, error) { t.Error("stat on unsupported OS"); return nil, nil }
	source.lookup = func(string) (string, error) { t.Error("lookup on unsupported OS"); return "", nil }
	report := collect(context.Background(), source)
	if len(report.Checks) != 1 || report.Checks[0].Status != StatusUnsupported {
		t.Fatalf("unexpected result: %+v", report)
	}
}

func TestCapabilityAndDeviceFailuresAreNotTreatedAsRootSuccess(t *testing.T) {
	for _, capability := range []string{"CapEff:\t0000000000000000\n", "CapEff:\tinvalid\n", "Name:\ttest\n"} {
		source, files := linuxSources()
		files["/proc/self/status"] = capability
		source.stat = func(string) (fs.FileInfo, error) { return testFileInfo{0600}, nil }
		report := collect(context.Background(), source)
		if findCheck(t, report, "net_admin").Status != StatusWarning || findCheck(t, report, "tun").Status != StatusWarning {
			t.Fatalf("root UID masked capability/device failure: %+v", report)
		}
	}
	source, _ := linuxSources()
	source.read = func(context.Context, string, int64) ([]byte, error) { return nil, fs.ErrPermission }
	source.stat = func(string) (fs.FileInfo, error) { return nil, fs.ErrPermission }
	report := collect(context.Background(), source)
	for _, id := range []string{"net_admin", "tun", "openwrt", "ipv4_forwarding", "ipv6_forwarding", "dns53"} {
		if findCheck(t, report, id).Status != StatusWarning {
			t.Errorf("unreadable %s incorrectly reported available", id)
		}
	}
}

func TestDNSParsingOnlyReportsLocalListeningOrBoundPort53(t *testing.T) {
	for _, tc := range []struct {
		line      string
		tcp, want bool
	}{
		{"0: 0100007F:0035 00000000:0000 0A", true, true},
		{"0: 0100007F:0035 00000000:0000 01", true, false},
		{"0: 0100007F:C000 08080808:0035 01", true, false},
		{"0: 00000000000000000000000000000000:0035 00000000000000000000000000000000:0000 0A", true, true},
		{"0: 00000000:0035 00000000:0000 07", false, true},
		{"0: 00000000:0035 08080808:0035 01", false, true},
		{"malformed", false, false},
	} {
		if got := hasDNSListener([]byte(tc.line), tc.tcp); got != tc.want {
			t.Errorf("%q tcp=%v got=%v", tc.line, tc.tcp, got)
		}
	}
	source, files := linuxSources()
	files["/proc/net/tcp"], files["/proc/net/udp"] = "", ""
	report := collect(context.Background(), source)
	if !strings.Contains(findCheck(t, report, "dns53").Message, "does not guarantee") {
		t.Fatal("empty socket snapshot claimed port was free")
	}
	delete(files, "/proc/net/tcp6")
	if findCheck(t, collect(context.Background(), source), "dns53").Status != StatusWarning {
		t.Fatal("incomplete table snapshot claimed port was free")
	}
}

func TestFileReadsEnforceLimitsAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized")
	if err := os.WriteFile(path, []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}
	if data, err := readLimitedFile(context.Background(), path, 4); !errors.Is(err, errReadLimit) || data != nil {
		t.Fatalf("read exceeded limit: data=%q error=%v", data, err)
	}
	if data, err := readLimitedFile(context.Background(), path, 5); err != nil || string(data) != "12345" {
		t.Fatalf("exact-sized read failed: %q %v", data, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readLimitedFile(ctx, path, 5); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read returned %v", err)
	}
	source, files := linuxSources()
	files["/proc/net/tcp"] = strings.Repeat("x", int(socketTableLimit)+1)
	files["/proc/net/udp"] = ""
	if findCheck(t, collect(context.Background(), source), "dns53").Status != StatusWarning {
		t.Fatal("oversized table was treated as a complete snapshot")
	}
}

func TestCanceledCollectionAndBlockedLookupHaveBoundedLifetime(t *testing.T) {
	source, _ := linuxSources()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source.read = func(context.Context, string, int64) ([]byte, error) {
		t.Error("canceled collection read a file")
		return nil, nil
	}
	if findCheck(t, collect(ctx, source), "collection").Status != StatusWarning {
		t.Fatal("cancellation not reported")
	}
	source, _ = linuxSources()
	entered, release, workerReturned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	source.lookup = func(string) (string, error) {
		close(entered)
		<-release
		close(workerReturned)
		return "", fs.ErrNotExist
	}
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	result := make(chan Report, 1)
	go func() { result <- collect(ctx, source) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("lookup did not start")
	}
	select {
	case report := <-result:
		if findCheck(t, report, "collection").Status != StatusWarning {
			t.Error("timeout not reported")
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("blocked lookup prevented timely return")
	}
	other, _ := linuxSources()
	if !strings.Contains(findCheck(t, collect(context.Background(), other), "collection").Message, "previous diagnostic") {
		t.Error("a second blocked worker was allowed")
	}
	close(release)
	<-workerReturned
	// Wait for the sole worker's deferred slot release before other tests run.
	deadline := time.Now().Add(time.Second)
	for len(workerSlot) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(workerSlot) != 0 {
		t.Fatal("worker did not release its slot")
	}
}
