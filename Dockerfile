# Stage 1: Build application
FROM golang:1.26-alpine AS builder
WORKDIR /src
RUN apk add --no-cache make nodejs npm \
  && npm install --global pnpm@latest
COPY go.mod go.sum Makefile ./
COPY frontend/package.json frontend/pnpm-lock.yaml ./frontend/
RUN go mod download
RUN pnpm --dir frontend install --frozen-lockfile
COPY . .
RUN CGO_ENABLED=0 make build OUTPUT_DIR=/out

# Stage 2: Runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/webui.for.singbox.server ./

VOLUME /app/data

ENV GFS_HOST=0.0.0.0
ENV GFS_PORT=9090
EXPOSE 9090

ENTRYPOINT ["sh", "-c", "exec ./webui.for.singbox.server --addr ${GFS_HOST}:${GFS_PORT}"]
