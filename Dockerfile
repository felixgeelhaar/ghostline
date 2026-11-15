FROM node:24.22.3-alpine3.23@sha256:3946fb783fdce8a2812136ea694bc95c698c4f4096f3fd18568dc27a8ac389dc AS frontend-build
WORKDIR /src/frontend
# Upgrade the builder's Alpine package; the pinned upstream image predates the fix.
RUN apk add --no-cache 'zlib>=1.4-r0' \
    && apk version -t "$(apk info -v zlib | sed 's/^zlib-//')" 1.4-r0 | grep -Eq '^(>|=)$'
COPY --chown=65532:65532 frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY --chown=65532:65532 frontend/ ./
RUN npm run build

FROM golang:1.27.4-alpine3.23@sha256:146e3b9c7c1e2ec3d3f5348c8a686151a75e1976e50efac9fc022ebe446a9a82 AS backend-build
WORKDIR /src
# Upgrade the builder's Alpine package; the pinned upstream image predates the fix.
RUN apk add --no-cache 'zlib>=1.4-r0' \
    && apk version -t "$(apk info -v zlib | sed 's/^zlib-//')" 1.4-r0 | grep -Eq '^(>|=)$'
COPY --chown=65532:65532 go.mod go.sum ./
RUN go mod download
COPY --chown=65532:65532 backend ./backend
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ghostline ./backend

FROM scratch AS runtime
LABEL maintainer="Ghostline maintainers"
WORKDIR /app
COPY --from=backend-build --chown=65532:65532 /out/ghostline /ghostline
COPY --from=backend-build --chown=65532:65532 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=frontend-build --chown=65532:65532 /src/frontend/dist /app/frontend/dist
ENV HOST=0.0.0.0 PORT=8080 HOME=/tmp \
    GIN_MODE=release \
    GHOSTLINE_ENV=production GHOSTLINE_AUTH_MODE=required \
    GHOSTLINE_STATIC_DIR=/app/frontend/dist
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=6 --start-period=10s CMD ["/ghostline", "healthcheck"]
CMD ["/ghostline"]
