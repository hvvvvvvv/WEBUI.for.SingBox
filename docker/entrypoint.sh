#!/bin/sh
set -eu

listen_host=${GFS_HOST:-0.0.0.0}
listen_port=${GFS_PORT:-9090}
case "$listen_host" in
  \[*\]) ;;
  *:*) listen_host="[$listen_host]" ;;
esac

# Preserve user-supplied flags and forward container signals directly to Go.
exec /app/webui.for.singbox.server --addr "$listen_host:$listen_port" "$@"
