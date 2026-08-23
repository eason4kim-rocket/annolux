# syntax=docker/dockerfile:1.7
FROM golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36 AS site-release
WORKDIR /src
COPY go.mod go.sum ./
COPY tools/sitegen/ tools/sitegen/
COPY openapi/ openapi/
COPY website/ website/
RUN go run ./tools/sitegen --check --release

FROM golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36 AS site-development
WORKDIR /src
COPY go.mod go.sum ./
COPY tools/sitegen/ tools/sitegen/
COPY openapi/ openapi/
COPY website/ website/
RUN go run ./tools/sitegen

# Production publication is fail-closed: only a fresh, release-mode dist can
# reach this target. The legal values stay out of Docker build args/history.
FROM nginxinc/nginx-unprivileged:1.30.4-alpine@sha256:44e36330f74d4f3a1d4e222acca9e23b401fb87811a7597024502bb759c4dd49 AS web
COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=site-release /src/website/dist/ /usr/share/nginx/html/
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["wget", "--spider", "-q", "http://127.0.0.1:8080/healthz"]

# CI audits intentionally use placeholder legal text and therefore build this
# non-publishable target after generating a fresh development dist.
FROM nginxinc/nginx-unprivileged:1.30.4-alpine@sha256:44e36330f74d4f3a1d4e222acca9e23b401fb87811a7597024502bb759c4dd49 AS web-test
COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=site-development /src/website/dist/ /usr/share/nginx/html/
EXPOSE 8080

FROM golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36 AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/annolux-api ./cmd/annolux-api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/annolux-index ./cmd/annolux-index && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/annolux-mcp ./cmd/annolux-mcp && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/annolux-healthcheck ./cmd/annolux-healthcheck && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/annolux-admin ./cmd/annolux-admin

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS api
WORKDIR /data
COPY --from=go-builder /out/annolux-api /usr/local/bin/annolux-api
COPY --from=go-builder /out/annolux-index /usr/local/bin/annolux-index
COPY --from=go-builder /out/annolux-healthcheck /usr/local/bin/annolux-healthcheck
COPY --from=go-builder /out/annolux-admin /usr/local/bin/annolux-admin
ENV ANNOLUX_LISTEN_ADDR=0.0.0.0:8080 \
    ANNOLUX_INDEX_PATH=/data/index.db \
    ANNOLUX_ACCOUNTS_PATH=/data/accounts.db
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD ["/usr/local/bin/annolux-healthcheck"]
ENTRYPOINT ["/usr/local/bin/annolux-api"]

FROM scratch AS mcp
COPY --from=go-builder /out/annolux-mcp /annolux-mcp
LABEL org.opencontainers.image.title="Annolux MCP" \
      org.opencontainers.image.description="One-tool MCP client for Annolux Search" \
      org.opencontainers.image.source="https://github.com/eason4kim-rocket/annolux" \
      org.opencontainers.image.licenses="Apache-2.0" \
      io.modelcontextprotocol.server.name="io.github.eason4kim-rocket/annolux"
ENTRYPOINT ["/annolux-mcp"]
