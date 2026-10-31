#!/usr/bin/env bash
# CI-only full-system ARM64 simulation: private QEMU NAT, no host bridge/TUN.
set -euo pipefail
release=${1:?OpenWrt version required}
build=$(realpath "${2:?build directory required}")
plugins=$(realpath "${3:?plugin repository required}")
scratch=$(mktemp -d)
qemu_pid=''
cleanup() {
  local exit_status=$?
  if (( exit_status != 0 )); then
    for log in environment.log auth.log plugins.log http-auth.log restart.log serial.log image-prepare.log; do
      if [[ -f "$build/$log" ]]; then
        printf '\n::group::OpenWrt failure diagnostics: %s\n' "$log"
        tail -n 100 "$build/$log" || true
        echo '::endgroup::'
      fi
    done
  fi
  if [[ -n "$qemu_pid" ]]; then kill "$qemu_pid" 2>/dev/null || true; wait "$qemu_pid" 2>/dev/null || true; fi
  # mktemp owns this directory; it never contains mounts or host device links.
  rm -rf -- "$scratch"
  return "$exit_status"
}
trap cleanup EXIT
case "$release" in
  21.02.7)
    base=https://downloads.openwrt.org/releases/21.02.7/targets/armvirt/64
    kernel=openwrt-21.02.7-armvirt-64-Image
    kernel_sha=09278dbb5e4a50336f003b715f1ff07fc742c3970f8b2bfed3902fa4393c6669
    rootfs=openwrt-21.02.7-armvirt-64-rootfs-ext4.img.gz
    rootfs_sha=d03bfd335948e5be95af80a205f8736fb5bf6f34eb81039c50cf7277bd5f201a
    ;;
  24.10.4)
    base=https://downloads.openwrt.org/releases/24.10.4/targets/armsr/armv8
    kernel=openwrt-24.10.4-armsr-armv8-generic-kernel.bin
    kernel_sha=5175c026ad2fb75ef7db7e6b61889e9304ad444081bb972831f451cc58f974b2
    rootfs=openwrt-24.10.4-armsr-armv8-generic-ext4-rootfs.img.gz
    rootfs_sha=d448813d12bc9b6bb54bcc6fb599dd1aa73ac55fa446163d65100bed8b8bf8ab
    ;;
  *) echo 'Unsupported test image' >&2; exit 1 ;;
esac
download() {
  curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
    --connect-timeout 15 --max-time 180 --retry 2 --output "$2" "$1"
  printf '%s  %s\n' "$3" "$2" | sha256sum --check --strict
}
download "$base/$kernel" "$scratch/kernel" "$kernel_sha"
download "$base/$rootfs" "$scratch/rootfs.gz" "$rootfs_sha"
download 'https://github.com/SagerNet/sing-box/releases/download/v1.14.4/sing-box-1.14.4-linux-arm64.tar.gz' \
  "$scratch/core.tar.gz" 436441f76ee2f7fe21b5b2d426d3cb261a480455a704865789ad533e4273ec26
tar -xzf "$scratch/core.tar.gz" -C "$scratch" --strip-components=1 sing-box-1.14.4-linux-arm64/sing-box
gzip -dc "$scratch/rootfs.gz" > "$scratch/rootfs.img"
truncate -s 256M "$scratch/rootfs.img"
e2fsck -pf "$scratch/rootfs.img" || test "$?" -eq 1
resize2fs "$scratch/rootfs.img"
ssh-keygen -q -t ed25519 -N '' -f "$scratch/key"
tar -czf "$scratch/plugins.tar.gz" -C "$plugins" index.json plugins tests/fixtures.json
cat > "$scratch/network" <<'NETWORK'
config interface 'loopback'
  option device 'lo'
  option ifname 'lo'
  option proto 'static'
  option ipaddr '127.0.0.1'
  option netmask '255.0.0.0'
config interface 'lan'
  option device 'eth0'
  option ifname 'eth0'
  option proto 'static'
  option ipaddr '10.0.2.15'
  option netmask '255.255.255.0'
NETWORK
cat > "$scratch/rc.local" <<'STARTUP'
#!/bin/sh
mkdir -p /opt/plugins
tar -xzf /opt/plugins.tar.gz -C /opt/plugins
/opt/webui/webui.for.singbox service install --addr 0.0.0.0:9090 --log-days 0 > /tmp/webui-install.log 2>&1
/opt/webui/webui.for.singbox service start >> /tmp/webui-install.log 2>&1
exit 0
STARTUP
cat > "$scratch/inject" <<INJECT
mkdir /opt
mkdir /opt/webui
mkdir /etc/dropbear
write $scratch/key.pub /etc/dropbear/authorized_keys
set_inode_field /etc/dropbear/authorized_keys mode 0100600
rm /etc/config/network
write $scratch/network /etc/config/network
rm /etc/rc.local
write $scratch/rc.local /etc/rc.local
set_inode_field /etc/rc.local mode 0100755
write $build/webui.for.singbox /opt/webui/webui.for.singbox
set_inode_field /opt/webui/webui.for.singbox mode 0100755
write $build/auth.test /opt/auth.test
set_inode_field /opt/auth.test mode 0100755
write $build/plugin-goja /opt/plugin-goja
set_inode_field /opt/plugin-goja mode 0100755
write $scratch/sing-box /opt/sing-box
set_inode_field /opt/sing-box mode 0100755
write $scratch/plugins.tar.gz /opt/plugins.tar.gz
INJECT
debugfs -w -f "$scratch/inject" "$scratch/rootfs.img" > "$build/image-prepare.log" 2>&1
debugfs -R 'stat /opt/webui/webui.for.singbox' "$scratch/rootfs.img" | grep 'Type: regular'
qemu-system-aarch64 -machine virt -cpu cortex-a53 -smp 4 -m 512 -nographic -no-reboot \
  -kernel "$scratch/kernel" -append 'root=/dev/vda rootwait console=ttyAMA0' \
  -drive "file=$scratch/rootfs.img,if=none,format=raw,id=rootfs" -device virtio-blk-device,drive=rootfs \
  -netdev user,id=private,restrict=on,hostfwd=tcp:127.0.0.1:49090-:9090,hostfwd=tcp:127.0.0.1:49222-:22 \
  -device virtio-net-device,netdev=private -rtc base=utc > "$build/serial.log" 2>&1 &
qemu_pid=$!
ssh_opts=(-i "$scratch/key" -p 49222 -o BatchMode=yes -o ConnectTimeout=3 -o StrictHostKeyChecking=accept-new -o "UserKnownHostsFile=$scratch/known_hosts")
ready=false
for attempt in $(seq 1 180); do
  if curl --max-time 2 --fail --silent http://127.0.0.1:49090/api/app/start-time > /dev/null; then ready=true; break; fi
  kill -0 "$qemu_pid" || break
  sleep 1
done
if [[ "$ready" != true ]]; then
  cat "$build/serial.log"
  ssh "${ssh_opts[@]}" root@127.0.0.1 'cat /tmp/webui-install.log; logread | tail -60' || true
  exit 1
fi
ssh "${ssh_opts[@]}" root@127.0.0.1 'set -e; uname -a; cat /etc/openwrt_release; free; date -u; status=$(/opt/webui/webui.for.singbox service status); echo "$status"; test "$status" = running; cat /etc/init.d/webui.for.singbox' > "$build/environment.log"
ssh "${ssh_opts[@]}" root@127.0.0.1 '/opt/auth.test -test.timeout=8m -test.v' > "$build/auth.log" 2>&1
# TCG emulates the entire CPU: allow the test harness time for all fixture
# compilation and file I/O. Production per-hook/pipeline limits stay unchanged.
ssh "${ssh_opts[@]}" root@127.0.0.1 'WEBUI_PLUGIN_SUITE_TIMEOUT=10m SING_BOX_114_PATH=/opt/sing-box /opt/plugin-goja /opt/plugins' > "$build/plugins.log" 2>&1
# Compare clocks immediately before host-generated TOTP codes. A broken guest
# clock is a test failure, never silently corrected to make authentication pass.
host_before=$(date +%s)
guest_time=$(ssh "${ssh_opts[@]}" root@127.0.0.1 'date +%s')
host_after=$(date +%s)
[[ "$guest_time" =~ ^[0-9]+$ ]] || { echo 'Invalid guest clock response' >&2; exit 1; }
host_time=$(((host_before + host_after) / 2))
clock_skew=$((guest_time - host_time))
printf 'host_epoch=%s guest_epoch=%s clock_skew_seconds=%s\n' "$host_time" "$guest_time" "$clock_skew" >> "$build/environment.log"
if (( clock_skew < -30 || clock_skew > 30 )); then
  echo "Guest clock differs from host by ${clock_skew}s (maximum 30s); refusing to adjust the VM clock" >&2
  exit 1
fi
node tests/compat/auth-smoke.mjs http://127.0.0.1:49090 > "$build/http-auth.log"
read_start_time() {
  curl --max-time 3 --fail --silent http://127.0.0.1:49090/api/app/start-time |
    node --input-type=module -e '
      import assert from "node:assert/strict";
      let raw = "";
      for await (const chunk of process.stdin) raw += chunk;
      const value = JSON.parse(raw).started_at;
      assert.ok(Number.isSafeInteger(value) && value > 0, "Invalid server start time");
      process.stdout.write(String(value));'
}
started_before=$(read_start_time)
printf 'started_before=%s\n' "$started_before" > "$build/restart.log"
ssh "${ssh_opts[@]}" root@127.0.0.1 'set -e; /opt/webui/webui.for.singbox service stop' >> "$build/restart.log" 2>&1
stopped=false
for attempt in $(seq 1 45); do
  status=$(ssh "${ssh_opts[@]}" root@127.0.0.1 '/opt/webui/webui.for.singbox service status')
  # Do not treat an HTTP error response as a closed listener.
  if [[ "$status" == stopped ]] && ! curl --max-time 2 --silent --output /dev/null http://127.0.0.1:49090/api/app/start-time; then
    stopped=true; break
  fi
  sleep 1
done
[[ "$stopped" == true ]] || { echo 'Service did not stop and close its HTTP listener' >&2; exit 1; }
echo 'Stopped state and closed HTTP listener confirmed.' >> "$build/restart.log"
ssh "${ssh_opts[@]}" root@127.0.0.1 'set -e; /opt/webui/webui.for.singbox service start' >> "$build/restart.log" 2>&1
restarted=false
for attempt in $(seq 1 60); do
  if started_after=$(read_start_time 2>/dev/null); then
    status=$(ssh "${ssh_opts[@]}" root@127.0.0.1 '/opt/webui/webui.for.singbox service status')
    if [[ "$status" == running && "$started_after" != "$started_before" ]]; then
      restarted=true; break
    fi
  fi
  sleep 1
done
[[ "$restarted" == true ]] || { echo 'Service did not restart with a new process start time' >&2; exit 1; }
printf 'started_after=%s\n' "$started_after" >> "$build/restart.log"
node tests/compat/auth-smoke.mjs http://127.0.0.1:49090 verify-persisted >> "$build/restart.log"
cat "$build/environment.log" "$build/plugins.log" "$build/http-auth.log" "$build/restart.log"
echo 'OpenWrt ARM64 VM checks passed; this is not a GL.iNet hardware test.'
