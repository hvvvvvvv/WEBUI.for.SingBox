# Stage 1: Build frontend
FROM node:22-alpine AS frontend
WORKDIR /src/frontend
RUN corepack enable && corepack prepare pnpm@latest --activate
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

# Stage 2: Build Go binary
FROM golang:1.26-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/frontend/dist ./frontend/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /gui.for.singbox.server .

# Stage 3: Runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=backend /gui.for.singbox.server ./

VOLUME /app/data

ENV GFS_HOST=0.0.0.0
ENV GFS_PORT=9090
EXPOSE 9090

ENTRYPOINT ["sh", "-c", "exec ./gui.for.singbox.server --addr ${GFS_HOST}:${GFS_PORT}"]
