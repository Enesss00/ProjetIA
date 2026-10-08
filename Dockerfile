# GHOST NET — single-image build: compile the web client, compile the Go
# server, then ship a tiny static binary that serves both.

# 1. Build the web client -> /web/dist
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# 2. Build the Go server (pure-Go SQLite, so CGO off -> fully static binary)
FROM golang:1.24-alpine AS server
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.build=docker" \
    -o /out/ghostnet ./cmd/ghostnet

# 3. Final minimal image
FROM alpine:3.20
RUN adduser -D -u 10001 ghost && mkdir -p /app/data && chown ghost /app/data
WORKDIR /app
COPY --from=server /out/ghostnet /app/ghostnet
COPY --from=web /web/dist /app/web/dist
USER ghost
EXPOSE 8080
ENV PORT=8080
# The server reads $PORT; data dir persists the SQLite game journals.
CMD ["/app/ghostnet", "serve", "-static", "/app/web/dist", "-db", "/app/data"]
