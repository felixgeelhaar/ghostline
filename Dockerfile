FROM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf
LABEL maintainer="Ghostline maintainers"
WORKDIR /app
COPY --chown=65532:65532 go.mod go.sum ./
RUN go mod download
# zlib: ALPINE-CVE-2026-4738 is fixed in 1.4-r0, which the pinned base image does not ship yet.
# The version constraint fails the build if the fixed package cannot be installed.
RUN apk add --no-cache curl 'zlib>=1.4-r0'
COPY --chown=65532:65532 backend ./backend
ENV HOST=0.0.0.0 PORT=8080 HOME=/tmp GOCACHE=/tmp/go-cache
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=6 --start-period=30s CMD curl -fsS http://localhost:8080/api/health || exit 1
CMD ["go", "run", "./backend"]
