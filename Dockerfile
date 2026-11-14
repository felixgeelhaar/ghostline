FROM node:26.13.1-alpine3.24@sha256:6bd9ab43c0ec0190ca7b2d193906cb8b7edf6e7f4afa43ddeccd6452710bffb5 AS frontend-build
RUN apk add --no-cache 'zlib>=1.4-r0'
WORKDIR /src/frontend
COPY --chown=0:0 frontend/package*.json ./
RUN npm ci
COPY --chown=0:0 frontend/ ./
RUN npm run build

FROM golang:1.27.4-alpine3.24@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS backend-build
RUN apk add --no-cache 'zlib>=1.4-r0'
WORKDIR /src
COPY --chown=0:0 go.mod go.sum ./
RUN go mod download
COPY --chown=0:0 backend/ ./backend/
COPY --from=frontend-build --chown=0:0 /src/frontend/dist/ ./backend/web/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ghostline ./backend

FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
LABEL maintainer="Ghostline maintainers"
RUN apk add --no-cache ca-certificates curl 'zlib>=1.4-r0' \
    && addgroup -S -g 65532 ghostline \
    && adduser -S -D -H -u 65532 -G ghostline ghostline
COPY --from=backend-build --chown=65532:65532 /out/ghostline /usr/local/bin/ghostline
ENV HOST=0.0.0.0 PORT=8080 HOME=/tmp
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=6 --start-period=30s CMD curl -fsS http://localhost:8080/api/health || exit 1
CMD ["/usr/local/bin/ghostline"]
