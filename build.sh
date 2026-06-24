#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="${ROOT_DIR}/frontend"
OUTPUT_DIR="${ROOT_DIR}/build/bin"
BINARY_NAME="${BINARY_NAME:-webui.for.singbox.server}"
OUTPUT_PATH="${OUTPUT_DIR}/${BINARY_NAME}"

if ! command -v go >/dev/null 2>&1; then
  echo "go is required but was not found in PATH." >&2
  exit 1
fi

if ! command -v pnpm >/dev/null 2>&1; then
  echo "pnpm is required but was not found in PATH." >&2
  exit 1
fi

echo "==> Building frontend"
if [ ! -d "${FRONTEND_DIR}/node_modules" ]; then
  pnpm --dir "${FRONTEND_DIR}" install --frozen-lockfile
fi
pnpm --dir "${FRONTEND_DIR}" build

echo "==> Building backend"
mkdir -p "${OUTPUT_DIR}"
go build -trimpath -ldflags="-s -w" -o "${OUTPUT_PATH}" "${ROOT_DIR}"

echo "==> Built ${OUTPUT_PATH}"
