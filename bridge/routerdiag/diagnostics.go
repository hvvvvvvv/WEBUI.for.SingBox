// Package routerdiag provides bounded, read-only checks of router prerequisites.
package routerdiag

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Status string

const (
	StatusOK          Status = "ok"
	StatusWarning     Status = "warning"
	StatusUnsupported Status = "unsupported"
	collectionTimeout        = 1500 * time.Millisecond
	socketTableLimit  int64  = 128 * 1024
)

type Check struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Status  Status `json:"status"`
	Message string `json:"message"`
}

type Report struct {
	OS     string  `json:"os"`
	Arch   string  `json:"arch"`
	Checks []Check `json:"checks"`
}

type sources struct {
	os, arch string
	uid      int
	read     func(context.Context, string, int64) ([]byte, error)
	stat     func(string) (fs.FileInfo, error)
	lookup   func(string) (string, error)
}

// Filesystem calls cannot always be interrupted (for example a PATH entry on a
// stalled mount). Keep at most one worker alive even if its caller times out.
var workerSlot = make(chan struct{}, 1)

// Collect never executes a command, binds a socket, writes a system setting, or
// contacts the network. A successful prerequisite check is not an end-to-end
// assertion that transparent proxying is supported by the firmware.
func Collect(ctx context.Context) Report {
	return collect(ctx, sources{
		os: runtime.GOOS, arch: runtime.GOARCH, uid: os.Geteuid(),
		read: readLimitedFile, stat: os.Stat, lookup: exec.LookPath,
	})
}

func collect(ctx context.Context, source sources) Report {
	report := Report{OS: source.os, Arch: source.arch, Checks: []Check{}}
	if source.os != "linux" {
		report.Checks = append(report.Checks, Check{"platform", "Platform", StatusUnsupported,
			"Router prerequisites are checked only on Linux; this host is " + source.os + "/" + source.arch + "."})
		return report
	}
	report.Checks = append(report.Checks, Check{"platform", "Platform", StatusOK, "Linux/" + source.arch + "; firmware capabilities are checked separately."})
	ctx, cancel := context.WithTimeout(ctx, collectionTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return interrupted(report)
	}
	select {
	case workerSlot <- struct{}{}:
	default:
		report.Checks = append(report.Checks, Check{"collection", "Collection", StatusWarning, "A previous diagnostic is still reading system information; retry later."})
		return report
	}
	results := make(chan Check, 12)
	go func() {
		defer func() {
			<-workerSlot
			close(results)
		}()
		checks := []func(context.Context, sources) Check{
			checkUID, checkCapabilities, checkTUN, checkFirmware,
			commandCheck("nft"), commandCheck("iptables"), commandCheck("fw4"),
			forwardingCheck(false), forwardingCheck(true), checkDNSListeners,
		}
		for _, check := range checks {
			if ctx.Err() != nil {
				return
			}
			results <- check(ctx, source)
		}
	}()
	for {
		select {
		case check, ok := <-results:
			if !ok {
				if ctx.Err() != nil {
					return interrupted(report)
				}
				return report
			}
			report.Checks = append(report.Checks, check)
		case <-ctx.Done():
			return interrupted(report)
		}
	}
}

func interrupted(report Report) Report {
	report.Checks = append(report.Checks, Check{"collection", "Collection", StatusWarning, "Diagnostic canceled or time limit reached; results may be incomplete."})
	return report
}

var errReadLimit = errors.New("diagnostic file exceeds size limit")

func readLimitedFile(ctx context.Context, path string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errReadLimit
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

func checkUID(_ context.Context, source sources) Check {
	if source.uid < 0 {
		return Check{"uid", "Effective user", StatusWarning, "Effective UID could not be determined."}
	}
	return Check{"uid", "Effective user", StatusOK, fmt.Sprintf("Effective UID: %d. UID alone does not prove network administration capability.", source.uid)}
}

func checkCapabilities(ctx context.Context, source sources) Check {
	data, err := source.read(ctx, "/proc/self/status", 8192)
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			key, value, ok := strings.Cut(line, ":")
			if !ok || key != "CapEff" {
				continue
			}
			capabilities, err := strconv.ParseUint(strings.TrimSpace(value), 16, 64)
			if err != nil {
				break
			}
			if capabilities&(uint64(1)<<12) != 0 {
				return Check{"net_admin", "CAP_NET_ADMIN", StatusOK, "Effective CAP_NET_ADMIN is present in this process; device and namespace restrictions may still apply."}
			}
			return Check{"net_admin", "CAP_NET_ADMIN", StatusWarning, "Effective CAP_NET_ADMIN is absent; managing TUN interfaces and routes normally requires it."}
		}
	}
	return Check{"net_admin", "CAP_NET_ADMIN", StatusWarning, "Effective network administration capability is unknown; /proc/self/status could not be read or parsed."}
}

func checkTUN(_ context.Context, source sources) Check {
	info, err := source.stat("/dev/net/tun")
	if err != nil {
		return Check{"tun", "TUN device", StatusWarning, "/dev/net/tun is unavailable or unreadable; check the firmware's matching TUN module."}
	}
	if info.Mode()&fs.ModeCharDevice == 0 {
		return Check{"tun", "TUN device", StatusWarning, "/dev/net/tun exists but is not a character device."}
	}
	return Check{"tun", "TUN device", StatusOK, "/dev/net/tun is a character device. Opening or creating a TUN interface was not attempted."}
}

func checkFirmware(ctx context.Context, source sources) Check {
	data, err := source.read(ctx, "/etc/openwrt_release", 4096)
	if err != nil {
		return Check{"openwrt", "OpenWrt firmware", StatusWarning, "OpenWrt release information is unavailable; this may be another Linux distribution or a restricted environment."}
	}
	var fields []string
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "DISTRIB_ID", "DISTRIB_RELEASE", "DISTRIB_TARGET":
			value = strings.Trim(strings.TrimSpace(value), "\"'")
			value = strings.Map(func(r rune) rune {
				if unicode.IsControl(r) {
					return -1
				}
				return r
			}, value)
			runes := []rune(value)
			if len(runes) > 80 {
				value = string(runes[:80])
			}
			if value != "" && len(fields) < 3 {
				fields = append(fields, value)
			}
		}
	}
	if len(fields) == 0 {
		return Check{"openwrt", "OpenWrt firmware", StatusWarning, "Release file exists, but firmware identifiers could not be parsed."}
	}
	return Check{"openwrt", "OpenWrt firmware", StatusOK, strings.Join(fields, " / ") + ". A version label does not identify the active firewall backend."}
}

func commandCheck(name string) func(context.Context, sources) Check {
	return func(_ context.Context, source sources) Check {
		if _, err := source.lookup(name); err != nil {
			return Check{name, name + " command", StatusWarning, name + " was not found in PATH; no command was executed."}
		}
		return Check{name, name + " command", StatusOK, name + " was found in PATH. Kernel modules, active rules and firmware compatibility were not verified."}
	}
}

func forwardingCheck(ipv6 bool) func(context.Context, sources) Check {
	return func(ctx context.Context, source sources) Check {
		id, label, path := "ipv4_forwarding", "IPv4 forwarding", "/proc/sys/net/ipv4/ip_forward"
		if ipv6 {
			id, label, path = "ipv6_forwarding", "IPv6 forwarding", "/proc/sys/net/ipv6/conf/all/forwarding"
		}
		data, err := source.read(ctx, path, 64)
		if err != nil {
			return Check{id, label, StatusWarning, "Forwarding state could not be read; no setting was changed."}
		}
		switch strings.TrimSpace(string(data)) {
		case "1":
			return Check{id, label, StatusOK, "Forwarding is enabled in this network namespace; routing and firewall rules were not inspected."}
		case "0":
			return Check{id, label, StatusWarning, "Forwarding is disabled in this network namespace; this diagnostic does not enable it."}
		default:
			return Check{id, label, StatusWarning, "Forwarding state is unrecognized; no setting was changed."}
		}
	}
}

func checkDNSListeners(ctx context.Context, source sources) Check {
	var protocols []string
	incomplete := false
	for _, protocol := range []string{"tcp", "tcp6", "udp", "udp6"} {
		data, err := source.read(ctx, "/proc/net/"+protocol, socketTableLimit)
		if err != nil {
			incomplete = true
			continue
		}
		if hasDNSListener(data, strings.HasPrefix(protocol, "tcp")) {
			protocols = append(protocols, protocol)
		}
	}
	if len(protocols) > 0 {
		message := "Port 53 listening/bound sockets were observed (" + strings.Join(protocols, ", ") + "). This may be the normal DNS service, not a port conflict."
		if incomplete {
			message += " Some socket tables could not be read completely."
		}
		return Check{"dns53", "Existing DNS sockets", StatusOK, message}
	}
	if incomplete {
		return Check{"dns53", "Existing DNS sockets", StatusWarning, "Socket tables could not be read completely; port 53 usage is unknown."}
	}
	return Check{"dns53", "Existing DNS sockets", StatusOK, "No port 53 listener/bound UDP socket was observed in this namespace. This snapshot does not guarantee that a bind would succeed."}
}

func hasDNSListener(data []byte, tcp bool) bool {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || (tcp && !strings.EqualFold(fields[3], "0A")) {
			continue
		}
		_, port, ok := strings.Cut(fields[1], ":")
		if !ok {
			continue
		}
		value, err := strconv.ParseUint(port, 16, 16)
		if err == nil && value == 53 {
			return true
		}
	}
	return false
}
