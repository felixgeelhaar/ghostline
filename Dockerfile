FROM node:22.23.5-alpine3.23@sha256:6868d4695b947ee120bffaa463fecd68cde370d7bfec92955e7c8ac576b4f475 AS frontend-build
WORKDIR /src/frontend
# zlib: ALPINE-CVE-2026-4738 is fixed in 1.4-r0; fail the image build if unavailable.
RUN apk add --no-cache 'zlib>=1.4-r0'
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.27.4-alpine3.23@sha256:146e3b9c7c1e2ec3d3f5348c8a686151a75e1976e50efac9fc022ebe446a9a82 AS backend-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY backend ./backend
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ghostline ./backend

FROM golang:1.27.4-alpine3.23@sha256:146e3b9c7c1e2ec3d3f5348c8a686151a75e1976e50efac9fc022ebe446a9a82 AS runtime
LABEL maintainer="Ghostline maintainers"
WORKDIR /app
# zlib: ALPINE-CVE-2026-4738 is fixed in 1.4-r0; fail the image build if unavailable.
RUN apk add --no-cache curl 'zlib>=1.4-r0' \
    && mkdir -p /app/frontend \
    && chown 65532:65532 /app
COPY --from=backend-build --chown=65532:65532 /out/ghostline /usr/local/bin/ghostline
COPY --from=frontend-build --chown=65532:65532 /src/frontend/dist /app/frontend/dist
ENV HOST=0.0.0.0 PORT=8080 HOME=/tmp GOCACHE=/tmp/go-cache \
    GHOSTLINE_ENV=production GHOSTLINE_AUTH_MODE=required \
    GHOSTLINE_STATIC_DIR=/app/frontend/dist
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=6 --start-period=30s CMD curl -fsS http://localhost:8080/api/health || exit 1
CMD ["/usr/local/bin/ghostline"]
