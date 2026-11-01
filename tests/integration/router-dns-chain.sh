#!/usr/bin/env bash
# Run only in a disposable Linux CI job. All links/firewall rules/services live
# in owned netns; core cannot call the host's resolvectl/DBus. This models an
# OpenWrt-style DNS chain, not actual GL firmware or AdGuard Home filtering.
set -euo pipefail
if [[ ${ROUTER_DNS_CHAIN_BOUNDED:-} != 1 ]]; then
  exec timeout --signal=TERM --kill-after=15s 240s env ROUTER_DNS_CHAIN_BOUNDED=1 bash "$0" "$@"
fi
[[ $(uname -s) == Linux && $EUID == 0 ]] || { echo 'Requires a Linux root CI job' >&2; exit 1; }
for tool in ip nft python3 timeout sysctl sha256sum; do command -v "$tool" >/dev/null; done
[[ -c /dev/net/tun ]] || { echo 'Real TUN device is required; no mock fallback' >&2; exit 1; }
core=$(realpath "${1:?verified sing-box 1.14.4 executable required}")
source=$(realpath "${2:?existing generated transparent fixture directory required}")
output=$(realpath -m "${3:?new DNS evidence directory required}")
helper=$(realpath "$(dirname "$0")/router-dns-chain.py")
[[ -f "$source/config.json" && -x "$source/probe" ]] || { echo 'Prepare router-transparent.go first' >&2; exit 1; }
[[ ! -e "$output" ]] || { echo 'Use a fresh evidence directory' >&2; exit 1; }
mkdir -p "$output/core-bin"
for tool in ip nft iptables ip6tables; do
  binary=$(command -v "$tool" || true)
  [[ -z "$binary" ]] || ln -s "$binary" "$output/core-bin/$tool"
done
# Only core gets this PATH. In particular, netns alone does not isolate the
# host's systemd-resolved Unix sockets; absence of resolvectl is intentional.
core_path="$output/core-bin"
timeout 5s "$core" version > "$output/core-version.log"
head -n 1 "$output/core-version.log" | grep -qx 'sing-box version 1.14.4' || { echo 'Pinned 1.14.4 is required' >&2; exit 1; }
ip -j route show table all > "$output/host-routes.before.json"
sha256sum /etc/resolv.conf | cut -d ' ' -f 1 > "$output/host-dns.before.sha256"
created=()
pids=()
case_output="$output"
dispose() {
  local ns pid alive result=0
  local remaining=()
  for ns in "${created[@]}"; do
    ip -n "$ns" -details rule show > "$case_output/$ns.rules.log" 2>&1
    ip -n "$ns" route show table all > "$case_output/$ns.routes.log" 2>&1
    ip netns exec "$ns" nft list ruleset > "$case_output/$ns.nft.log" 2>&1
    for pid in $(ip netns pids "$ns"); do kill -TERM "$pid" 2>/dev/null || true; done
  done
  for _ in {1..20}; do
    alive=0
    for ns in "${created[@]}"; do [[ -z $(ip netns pids "$ns") ]] || alive=1; done
    (( alive == 0 )) && break
    sleep 0.1
  done
  for ns in "${created[@]}"; do
    for pid in $(ip netns pids "$ns"); do kill -KILL "$pid" 2>/dev/null || true; done
    if ! ip netns delete "$ns"; then remaining+=("$ns"); result=1; fi
  done
  for pid in "${pids[@]}"; do wait "$pid" 2>/dev/null || true; done
  created=("${remaining[@]}")
  pids=()
  return "$result"
}
cleanup() {
  local result=$?
  trap - EXIT TERM INT
  set +e
  if (( result != 0 )); then
    tail -n 80 "$case_output/core.log" "$case_output/router.log" "$case_output/origin.log" "$case_output/api-ready.log" 2>/dev/null
  fi
  dispose || result=1
  ip -j route show table all > "$output/host-routes.after.json"
  sha256sum /etc/resolv.conf | cut -d ' ' -f 1 > "$output/host-dns.after.sha256"
  cmp -s "$output/host-routes.before.json" "$output/host-routes.after.json" || { echo 'Host routes changed' >&2; result=1; }
  cmp -s "$output/host-dns.before.sha256" "$output/host-dns.after.sha256" || { echo 'Host resolver file changed' >&2; result=1; }
  exit "$result"
}
trap cleanup EXIT
trap 'exit 143' TERM
trap 'exit 130' INT
for redirect in false true; do
  for mode in native hijack; do
    case_output="$output/$mode-$redirect"
    python3 "$helper" prepare "$case_output" "$source/config.json" "$mode" "$redirect"
    prefix="wud-${BASHPID}-${RANDOM}"
    client="$prefix-c" router="$prefix-r" origin="$prefix-o"
    for ns in "$client" "$router" "$origin"; do
      ip netns add "$ns"
      created+=("$ns")
      ip -n "$ns" link set lo up
    done
    ip -n "$router" link add lan0 type veth peer name eth0 netns "$client"
    ip -n "$router" link add wan0 type veth peer name eth0 netns "$origin"
    ip -n "$router" link add dns0 type veth peer name dns0 netns "$origin"
    ip -n "$client" address add 10.203.0.2/24 dev eth0
    ip -n "$client" link set eth0 up
    ip -n "$client" route add default via 10.203.0.1
    ip -n "$router" address add 10.203.0.1/24 dev lan0
    ip -n "$router" address add 10.204.0.1/24 dev wan0
    ip -n "$router" address add 10.205.0.1/24 dev dns0
    for interface in lan0 wan0 dns0; do ip -n "$router" link set "$interface" up; done
    ip -n "$router" route add default via 10.204.0.2
    ip -n "$origin" address add 10.204.0.2/24 dev eth0
    ip -n "$origin" address add 10.205.0.2/24 dev dns0
    ip -n "$origin" link set eth0 up
    ip -n "$origin" link set dns0 up
    ip -n "$origin" address add 203.0.113.10/32 dev lo
    ip -n "$origin" address add 198.51.100.20/32 dev lo
    ip -n "$origin" route add 10.203.0.0/24 via 10.204.0.1
    # No namespace connects to the host/Internet. The extra connected subnet
    # is not excluded from TUN, unlike WAN fixture/control traffic.
    ip netns exec "$router" sysctl -qw net.ipv4.ip_forward=1 net.ipv4.conf.all.rp_filter=0 net.ipv4.conf.default.rp_filter=0 net.ipv4.conf.lan0.rp_filter=0 net.ipv4.conf.wan0.rp_filter=0 net.ipv4.conf.dns0.rp_filter=0
    ip netns exec "$origin" sysctl -qw net.ipv4.conf.all.rp_filter=0 net.ipv4.conf.eth0.rp_filter=0 net.ipv4.conf.dns0.rp_filter=0
    ip netns exec "$origin" python3 -u "$helper" serve "$case_output" origin > "$case_output/origin.log" 2>&1 &
    origin_pid=$!
    pids+=("$origin_pid")
    ip netns exec "$router" python3 -u "$helper" serve "$case_output" router > "$case_output/router.log" 2>&1 &
    router_pid=$!
    pids+=("$router_pid")
    for _ in {1..50}; do
      [[ ! -f "$case_output/origin.ready" || ! -f "$case_output/router.ready" ]] || break
      kill -0 "$origin_pid" "$router_pid"
      sleep 0.1
    done
    [[ -f "$case_output/origin.ready" && -f "$case_output/router.ready" ]] || { echo 'DNS fixtures not ready' >&2; exit 1; }
    ip netns exec "$router" env PATH="$core_path" "$core" check -c "$case_output/config.json" -D "$case_output" > "$case_output/core-check.log" 2>&1
    ip netns exec "$router" env PATH="$core_path" "$core" run --disable-color -c "$case_output/config.json" -D "$case_output" > "$case_output/core.log" 2>&1 &
    core_pid=$!
    pids+=("$core_pid")
    ready=0
    for _ in {1..50}; do
      kill -0 "$core_pid"
      if ip netns exec "$router" "$source/probe" mode "$case_output/config.json" rule > "$case_output/api-ready.log" 2>&1; then ready=1; break; fi
      sleep 0.1
    done
    (( ready == 1 )) || { echo 'Native core API not ready' >&2; exit 1; }
    ip -n "$router" -j -details -statistics link show dev webui-test-tun > "$case_output/tun.before.json"
    for transport in udp tcp; do
      for path in lan attached loopback; do
        ns="$client"
        [[ "$path" != loopback ]] || ns="$router"
        ip netns exec "$ns" python3 "$helper" probe "$case_output" "$mode" "$redirect" "$path" "$transport" | tee -a "$case_output/probes.log"
      done
    done
    ip -n "$router" -j -details -statistics link show dev webui-test-tun > "$case_output/tun.after.json"
    python3 "$helper" verify "$case_output" | tee -a "$output/matrix.log"
    dispose
  done
done
[[ $(wc -l < "$output/matrix.log") == 4 ]] || { echo 'Incomplete mode matrix' >&2; exit 1; }
echo 'PASS: 4 real-core configurations; 24 DNS + HTTP probes with exact per-hop assertions' | tee -a "$output/matrix.log"
