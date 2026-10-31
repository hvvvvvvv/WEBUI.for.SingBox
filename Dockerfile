# syntax=docker/dockerfile:1

# Build the frontend once on the native builder platform. Some frontend build
# dependencies do not ship native binaries for every target architecture.
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend-builder
WORKDIR /src/frontend
RUN npm install --global pnpm@latest
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

# Cross-compile the Go backend on the native builder platform for each target.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend-builder
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG APP_REPOSITORY=Ghostdehole/WEBUI.for.SingBox
WORKDIR /src
RUN apk add --no-cache make
COPY go.mod go.sum Makefile ./
RUN go mod download
COPY . .
COPY --from=frontend-builder /src/frontend/dist ./frontend/dist
RUN export CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH"; \
  if [ "$TARGETARCH" = "arm" ]; then export GOARM="${TARGETVARIANT#v}"; fi; \
  make backend OUTPUT_DIR=/out APP_REPOSITORY="$APP_REPOSITORY"

# Runtime image for the selected target platform.
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=backend-builder /out/webui.for.singbox.server ./
COPY --chmod=755 docker/entrypoint.sh /app/entrypoint.sh

VOLUME /app/data

ENV GFS_HOST=0.0.0.0
ENV GFS_PORT=9090
EXPOSE 9090

ENTRYPOINT ["/app/entrypoint.sh"]
