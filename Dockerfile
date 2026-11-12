FROM golang:1.27-alpine@sha256:eb61c3873c97416fff0116553fff65d67a3549f72e11d9758849a689cdd8b523
LABEL maintainer="Ghostline maintainers"
WORKDIR /app
COPY --chown=65532:65532 go.mod go.sum ./
RUN go mod download
# zlib: the base tag is still on 1.3.2-r1; require the fixed 1.4-r0 package.
RUN apk add --no-cache curl 'zlib>=1.4-r0'
COPY --chown=65532:65532 backend ./backend
ENV HOST=0.0.0.0 PORT=8080 HOME=/tmp GOCACHE=/tmp/go-cache
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=6 --start-period=30s CMD curl -fsS http://localhost:8080/api/health || exit 1
CMD ["go", "run", "./backend"]
