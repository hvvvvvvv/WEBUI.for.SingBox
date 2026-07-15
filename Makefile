OUTPUT_DIR ?= build/bin
APP_VERSION ?= 1.0.1
VERSION ?= $(APP_VERSION)
BUF ?= buf

ifeq ($(OS),Windows_NT)
BINARY_NAME ?= webui.for.singbox.server.exe
CHECK_TOOL = where $(1) >NUL 2>NUL || (echo $(1) is required but was not found in PATH. 1>&2 && exit 1)
MKDIR_P = powershell -NoProfile -Command "New-Item -ItemType Directory -Force '$(OUTPUT_DIR)' | Out-Null"
RM_RF = powershell -NoProfile -Command "Remove-Item -Recurse -Force '$(OUTPUT_DIR)' -ErrorAction SilentlyContinue"
CHECK_FILE = powershell -NoProfile -Command "if (-not (Test-Path -LiteralPath '$(1)' -PathType Leaf)) { Write-Error '$(1) is required but was not found.'; exit 1 }"
CHECK_DIR = powershell -NoProfile -Command "if (-not (Test-Path -LiteralPath '$(1)' -PathType Container)) { Write-Error '$(1)/ is required but was not found.'; exit 1 }"
else
BINARY_NAME ?= webui.for.singbox.server
CHECK_TOOL = command -v $(1) >/dev/null 2>&1 || { echo "$(1) is required but was not found in PATH." >&2; exit 1; }
MKDIR_P = mkdir -p $(OUTPUT_DIR)
RM_RF = rm -rf $(OUTPUT_DIR)
CHECK_FILE = test -f $(1) || { echo "$(1) is required but was not found." >&2; exit 1; }
CHECK_DIR = test -d $(1) || { echo "$(1)/ is required but was not found." >&2; exit 1; }
endif

OUTPUT_PATH := $(OUTPUT_DIR)/$(BINARY_NAME)
GO_LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build frontend backend check-tools proto proto-lint proto-check check-proto-tools print-version clean

build: check-tools frontend backend

check-tools:
	@$(call CHECK_TOOL,go)
	@$(call CHECK_TOOL,pnpm)

frontend:
	pnpm --dir frontend install --frozen-lockfile
	pnpm --dir frontend build

backend:
	$(MKDIR_P)
	go build -trimpath -ldflags="$(GO_LDFLAGS)" -o $(OUTPUT_PATH) .

proto: check-proto-tools
	$(BUF) generate

proto-lint: check-proto-tools
	$(BUF) lint

proto-check: proto-lint

print-version:
	@echo $(VERSION)

check-proto-tools:
	@$(call CHECK_TOOL,$(BUF))
	@$(call CHECK_FILE,buf.yaml)
	@$(call CHECK_FILE,buf.gen.yaml)
	@$(call CHECK_DIR,proto)

clean:
	$(RM_RF)
