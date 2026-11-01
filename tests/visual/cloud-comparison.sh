#!/usr/bin/env bash
set -euo pipefail
root=$PWD
results="$root/build/visual-results"
mkdir -p "$results"
app_pid=''
cleanup() {
  if [[ -n "$app_pid" ]]; then
    kill -- -"$app_pid" 2>/dev/null || true
    wait "$app_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT
if [[ "$VISUAL_TARGET" == official ]]; then
  cd "$root/reference"
  # Only the cloud runner gets a virtual desktop. Real Wails Go bindings and
  # persistence are served to Chromium; no bridge mocks or production profiles.
  # The pinned source is unchanged. Suppress the separate Vite watcher because
  # we already built the official assets and need a stable reference capture.
  node --input-type=module -e '
    import fs from "node:fs";
    const p = JSON.parse(fs.readFileSync("wails.json", "utf8"));
    p["frontend:dev:watcher"] = "";
    p["frontend:dev:serverUrl"] = "";
    fs.writeFileSync("wails.json", JSON.stringify(p, null, 2));'
  setsid xvfb-run -a dbus-run-session -- wails dev -s -skipbindings -nosyncgomod \
    -noreload -tags webkit2_41 -assetdir frontend/dist -devserver 127.0.0.1:34115 \
    > "$results/application.log" 2>&1 &
  app_pid=$!
  url=http://127.0.0.1:34115
  git rev-parse HEAD > "$results/source.txt"
else
  cd "$root/build/visual"
  setsid ./webui.for.singbox.server --addr 127.0.0.1:34115 --log-days 0 \
    > "$results/application.log" 2>&1 &
  app_pid=$!
  url=http://127.0.0.1:34115
  git -C "$root" rev-parse HEAD > "$results/source.txt"
fi
ready=false
for attempt in $(seq 1 180); do
  if curl --max-time 2 --fail --silent "$url" > /dev/null; then ready=true; break; fi
  kill -0 "$app_pid" || break
  sleep 2
done
if [[ "$ready" != true ]]; then cat "$results/application.log"; exit 1; fi
cd "$root"
node tests/visual/compare.mjs "$url" "$results"
