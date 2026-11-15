FROM node:24.22.3-alpine3.24@sha256:116b800c3754b440ee0aa3ed2f20f5bf3d99a554bfd9adeea09ea8017f6e85f5 AS frontend-build
WORKDIR /src/frontend
# The pinned upstream image predates the fix; fail the build if the repository
# cannot provide the fixed package.
RUN apk add --no-cache 'zlib>=1.4-r0' \
    && apk info -W /usr/lib/libz.so.1 | grep -Fq 'zlib-1.4-r0'
COPY --chown=65532:65532 frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY --chown=65532:65532 frontend/ ./
RUN npm run build

FROM golang:1.27.4-alpine3.24@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS backend-build
WORKDIR /src
# Keep build tooling on the fixed package and fail closed if it is unavailable.
RUN apk add --no-cache 'zlib>=1.4-r0' \
    && apk info -W /usr/lib/libz.so.1 | grep -Fq 'zlib-1.4-r0'
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
