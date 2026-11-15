FROM node:22.23.5-alpine3.24@sha256:1314fb30ec37592d5845e5a160d522c0e680972aa534549940b8d73bde88481a AS frontend-build
WORKDIR /src/frontend
# zlib: ALPINE-CVE-2026-4738 is fixed in 1.4-r0; fail the image build if unavailable.
RUN apk add --no-cache 'zlib>=1.4-r0'
COPY --chown=65532:65532 frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY --chown=65532:65532 frontend/ ./
RUN npm run build

FROM golang:1.27.4-alpine3.24@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS backend-build
WORKDIR /src
# zlib: ALPINE-CVE-2026-4738 is fixed in 1.4-r0; fail the image build if unavailable.
RUN apk add --no-cache 'zlib>=1.4-r0'
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
