#!/usr/bin/env bash
# Linux cloud-runner acceptance test; requires root, iproute2, python3, /dev/net/tun.
# From repository root, prepare as the normal build user:
#   go run tests/compat/router-transparent.go "$RUNNER_TEMP/transparent"
#   go build -o "$RUNNER_TEMP/transparent/probe" tests/compat/router-transparent.go
# Then use a separately verified official Linux sing-box 1.14.4 binary:
#   sudo bash tests/compat/router-transparent.sh "$CORE_BINARY" "$RUNNER_TEMP/transparent"
# Three entirely private network namespaces; no host route/DNS/firewall changes.
# This tests IPv4 TUN + real UDP/TCP DNS, not QUIC or encrypted DNS transport.
set -euo pipefail
if [[ ${ROUTER_TRANSPARENT_BOUNDED:-} != 1 ]]; then
  exec timeout --signal=TERM --kill-after=15s 120s env ROUTER_TRANSPARENT_BOUNDED=1 bash "$0" "$@"
fi
[[ $(uname -s) == Linux && $EUID == 0 ]] || { echo 'Requires a Linux root CI job; no fallback/mock mode' >&2; exit 1; }
for tool in ip python3 timeout sysctl sha256sum; do command -v "$tool" >/dev/null; done
[[ -c /dev/net/tun ]] || { echo '/dev/net/tun is unavailable; actual TUN test cannot run' >&2; exit 1; }
core=$(realpath "${1:?verified sing-box 1.14.4 executable required}")
output=$(realpath "${2:?generated fixture directory required}")
helper=$(realpath "$(dirname "$0")/router-transparent.py")
[[ -f "$output/config.json" ]] || { echo 'Run router-transparent.go first' >&2; exit 1; }
[[ -x "$output/probe" ]] || { echo 'Build router-transparent.go as output/probe first' >&2; exit 1; }
# netns does not isolate the host's resolved/DBus sockets. Match OpenWrt's
# absence of resolvectl without changing the host's resolver configuration.
core_path=$(mktemp -d "$output/core-bin.XXXXXX")
for tool in ip nft iptables ip6tables; do
  binary=$(command -v "$tool" || true)
  [[ -z "$binary" ]] || ln -s "$binary" "$core_path/$tool"
done
timeout 5s "$core" version > "$output/core-version.log"
head -n 1 "$output/core-version.log" | grep -qx 'sing-box version 1.14.4' || { echo 'This acceptance fixture requires released sing-box 1.14.4' >&2; exit 1; }
prefix="wut-${BASHPID}-${RANDOM}"
client="$prefix-c"
router="$prefix-r"
origin="$prefix-o"
created=()
core_pid=''
origin_pid=''
ip -j route show table all > "$output/host-routes.before.json"
sha256sum /etc/resolv.conf | cut -d ' ' -f 1 > "$output/host-dns.before.sha256"
cleanup() {
  local result=$? ns pid
  trap - EXIT TERM INT
  set +e
  if (( result != 0 )); then
    tail -n 100 "$output/core.log" "$output/origin.log" "$output/api-ready.log" 2>/dev/null
  fi
  # Only the namespaces successfully created by this invocation are touched.
  for ns in "${created[@]}"; do
    ip -n "$ns" -details rule show > "$output/$ns.rules.log" 2>&1
    ip -n "$ns" route show table all > "$output/$ns.routes.log" 2>&1
    for pid in $(ip netns pids "$ns"); do kill -TERM "$pid" 2>/dev/null; done
  done
  for _ in {1..20}; do
    local alive=0
    for ns in "${created[@]}"; do [[ -z $(ip netns pids "$ns") ]] || alive=1; done
    (( alive == 0 )) && break
    sleep 0.1
  done
  for ns in "${created[@]}"; do
    for pid in $(ip netns pids "$ns"); do kill -KILL "$pid" 2>/dev/null; done
    ip netns delete "$ns" || result=1
  done
  [[ -z "$core_pid" ]] || wait "$core_pid" 2>/dev/null
  [[ -z "$origin_pid" ]] || wait "$origin_pid" 2>/dev/null
  ip -j route show table all > "$output/host-routes.after.json"
  sha256sum /etc/resolv.conf | cut -d ' ' -f 1 > "$output/host-dns.after.sha256"
  cmp -s "$output/host-routes.before.json" "$output/host-routes.after.json" || { echo 'Host routing changed during the test' >&2; result=1; }
  cmp -s "$output/host-dns.before.sha256" "$output/host-dns.after.sha256" || { echo 'Host DNS configuration changed during the test' >&2; result=1; }
  exit "$result"
}
trap cleanup EXIT
trap 'exit 143' TERM
trap 'exit 130' INT
for ns in "$client" "$router" "$origin"; do
  ip netns add "$ns"
  created+=("$ns")
  ip -n "$ns" link set lo up
done
# Both ends are created inside owned namespaces; no host veth or bridge exists.
ip -n "$router" link add lan0 type veth peer name eth0 netns "$client"
ip -n "$router" link add wan0 type veth peer name eth0 netns "$origin"
ip -n "$client" address add 10.203.0.2/24 dev eth0
ip -n "$client" link set eth0 up
ip -n "$client" route add default via 10.203.0.1
ip -n "$router" address add 10.203.0.1/24 dev lan0
ip -n "$router" address add 10.204.0.1/24 dev wan0
ip -n "$router" link set lan0 up
ip -n "$router" link set wan0 up
ip -n "$router" route add default via 10.204.0.2
ip -n "$origin" address add 10.204.0.2/24 dev eth0
ip -n "$origin" link set eth0 up
ip -n "$origin" address add 203.0.113.10/32 dev lo
ip -n "$origin" address add 198.51.100.20/32 dev lo
ip -n "$origin" route add 10.203.0.0/24 via 10.204.0.1
# The synthetic Internet has no default route and no interface into the host.
ip netns exec "$router" sysctl -qw net.ipv4.ip_forward=1 net.ipv4.conf.all.rp_filter=0 net.ipv4.conf.default.rp_filter=0 net.ipv4.conf.lan0.rp_filter=0 net.ipv4.conf.wan0.rp_filter=0
rm -f "$output/fixture.ready"
ip netns exec "$origin" python3 -u "$helper" serve "$output" > "$output/origin.log" 2>&1 &
origin_pid=$!
for _ in {1..50}; do
  [[ ! -f "$output/fixture.ready" ]] || break
  kill -0 "$origin_pid"
  sleep 0.1
done
[[ -f "$output/fixture.ready" ]] || { echo 'Fixture services did not start' >&2; exit 1; }
ip netns exec "$router" env PATH="$core_path" "$core" check -c "$output/config.json" -D "$output" > "$output/core-check.log" 2>&1
ip netns exec "$router" env PATH="$core_path" "$core" run --disable-color -c "$output/config.json" -D "$output" > "$output/core.log" 2>&1 &
core_pid=$!
ready=0
for _ in {1..100}; do
  kill -0 "$core_pid"
  if ip netns exec "$router" "$output/probe" mode "$output/config.json" rule > "$output/api-ready.log" 2>&1; then ready=1; break; fi
  sleep 0.1
done
(( ready == 1 )) || { echo 'Native core API did not become ready' >&2; exit 1; }
ip -n "$router" -j -details -statistics link show dev webui-test-tun > "$output/tun.before.json"
set_mode() { ip netns exec "$router" "$output/probe" mode "$output/config.json" "$1"; }
probe() { ip netns exec "$client" python3 "$helper" request "$@" | tee -a "$output/assertions.jsonl"; }
: > "$output/assertions.jsonl"
probe google.com 203.0.113.10 proxy tcp rule-google
probe baidu.com 198.51.100.20 direct udp rule-cn
set_mode global
probe google.com 203.0.113.10 proxy tcp global-google
probe baidu.com 198.51.100.20 proxy tcp global-cn
# Diagnostic counterfactual: an erroneous DNS answer in the CN fixture CIDR
# hits GeoIP-CN before GeoLocation-!CN. This is not evidence of live DNS pollution.
ip netns exec "$origin" python3 "$helper" pollution on
set_mode rule
probe google.com 198.51.100.20 direct tcp polluted-rule-google
set_mode global
probe google.com 198.51.100.20 proxy tcp polluted-global-google
ip netns exec "$origin" python3 "$helper" pollution off
set_mode rule
probe google.com 203.0.113.10 proxy tcp restored-rule-google
ip netns exec "$origin" python3 "$helper" verify > "$output/metrics.json"
ip -n "$router" -j -details -statistics link show dev webui-test-tun > "$output/tun.after.json"
python3 - "$output" <<'PY'
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
before = json.loads((root / 'tun.before.json').read_text())[0]
after = json.loads((root / 'tun.after.json').read_text())[0]
assert after['linkinfo']['info_kind'] == 'tun', 'No real kernel TUN interface'
assert after['linkinfo']['info_data']['type'] == 'tun', 'Expected L3 TUN, not TAP'
def packets(link):
    return link.get('stats64', link.get('stats', {}))['rx']['packets']
assert packets(after) > packets(before), 'No packets crossed the real TUN device'
samples = (root / 'assertions.jsonl').read_text().splitlines()
assert len(samples) == 7, f'Expected seven completed data-path assertions, got {len(samples)}'
core_log = (root / 'core.log').read_text()
assert 'sniffed protocol: http, domain: google.com' in core_log, 'Google HTTP Host was not sniffed by the real core'
for rule_set in ('GeoLocation-!CN', 'GeoSite-CN', 'GeoIP-CN'):
    assert any('match[' in line and rule_set in line for line in core_log.splitlines()), f'No actual match for {rule_set}; final fallback could hide a regression'
summary = f'PASS: real TUN RX packets {packets(before)} -> {packets(after)}; 7 DNS/data-path assertions; 6 remote rule sets'
evidence = [summary, *samples, 'Fixture metrics:', (root / 'metrics.json').read_text(),
            'TUN before:', json.dumps(before), 'TUN after:', json.dumps(after)]
(root / 'evidence.log').write_text('\n'.join(evidence) + '\n')
print(summary)
PY
