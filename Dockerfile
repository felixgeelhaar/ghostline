FROM golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb
LABEL maintainer="Ghostline maintainers"
WORKDIR /app
COPY --chown=65532:65532 go.mod go.sum ./
RUN go mod download
# zlib: ALPINE-CVE-2026-4738 is fixed in 1.4-r0, which the pinned base image does not ship yet.
RUN apk add --no-cache curl && apk upgrade --no-cache zlib
COPY --chown=65532:65532 backend ./backend
ENV HOST=0.0.0.0 PORT=8080 HOME=/tmp GOCACHE=/tmp/go-cache
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=6 --start-period=30s CMD curl -fsS http://localhost:8080/api/health || exit 1
CMD ["go", "run", "./backend"]
