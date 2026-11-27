# The frontend build output is static, and Go cross-compiles, so both build stages run on
# the build machine's platform; only the scratch runtime differs per target platform.
FROM --platform=$BUILDPLATFORM node:26.13.1-bookworm-slim@sha256:5df79989ed685e09f0a98019db4cebfb22fb9df1a53c0161664a011f7ff6b0f1 AS frontend-build
WORKDIR /src/frontend
# DLA-4941-1 / CVE-2025-6020: no node bookworm-slim digest ships pam 1.5.2-6+deb12u3 yet,
# so upgrade it here and fail the build if the fixed version is not installed.
RUN apt-get update \
 && apt-get install -y --no-install-recommends --only-upgrade libpam0g libpam-modules libpam-modules-bin libpam-runtime \
 && dpkg --compare-versions "$(dpkg-query -W -f='${Version}' libpam0g)" ge 1.5.2-6+deb12u3 \
 && rm -rf /var/lib/apt/lists/*
COPY --chown=65532:65532 frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY --chown=65532:65532 frontend/ ./
# The release tag shown in the footer; release.yml passes the validated tag, and any
# other build is labelled dev.
ARG GHOSTLINE_VERSION=dev
RUN VITE_GHOSTLINE_VERSION=$GHOSTLINE_VERSION npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS backend-build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY --chown=65532:65532 go.mod go.sum ./
RUN go mod download
COPY --chown=65532:65532 backend ./backend
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/ghostline ./backend

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
