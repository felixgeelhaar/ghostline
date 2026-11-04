FROM golang:1.26-alpine@sha256:a2012b4a1d1f9f7e9f44e0d39b805a901e856b1dd80b4e6ee4338db547050308
WORKDIR /app
COPY --chown=65532:65532 go.mod go.sum ./
RUN go mod download
RUN apk add --no-cache curl
COPY --chown=65532:65532 backend ./backend
ENV HOST=0.0.0.0 PORT=8080 HOME=/tmp GOCACHE=/tmp/go-cache
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=6 --start-period=30s CMD curl -fsS http://localhost:8080/api/health || exit 1
CMD ["go", "run", "./backend"]
