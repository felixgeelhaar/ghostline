FROM golang:1.26-alpine
ARG TARGETARCH
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
RUN apk add --no-cache curl git nodejs npm && \
    case "$TARGETARCH" in \
      amd64) nox_sha=db25d1d9c8a7e17782a80ba12f152ed7ae0db05f67af73bf502d81ee3fee98dd; warden_sha=6a8f0c1ec025f476b6781b06f8bc968bf6faae593b09dc29c245e9e87d07b264 ;; \
      arm64) nox_sha=f91219eefbf1d511299386885bb6a6832af2c06f13ce0329cb5c856342dbfeb0; warden_sha=10ec008c1ce91835a2f314e68179786dc372f87f91c1f00010dbf1cf96fb80d4 ;; \
      *) echo "Unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac && \
    curl -fsSL "https://github.com/Nox-HQ/nox/releases/download/v1.57.0/nox_1.57.0_linux_${TARGETARCH}.tar.gz" -o /tmp/nox.tar.gz && \
    echo "$nox_sha  /tmp/nox.tar.gz" | sha256sum -c - && \
    tar -xzf /tmp/nox.tar.gz -C /tmp nox && mv /tmp/nox /usr/local/bin/nox && \
    case "$TARGETARCH" in \
      amd64) warden_sha=6a8f0c1ec025f476b6781b06f8bc968bf6faae593b09dc29c245e9e87d07b264 ;; \
      arm64) warden_sha=10ec008c1ce91835a2f314e68179786dc372f87f91c1f00010dbf1cf96fb80d4 ;; \
      *) echo "Unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac && \
    curl -fsSL "https://github.com/klarlabs-studio/warden/releases/download/v0.46.1/warden_0.46.1_linux_${TARGETARCH}.tar.gz" -o /tmp/warden.tar.gz && \
    echo "$warden_sha  /tmp/warden.tar.gz" | sha256sum -c - && \
    tar -xzf /tmp/warden.tar.gz -C /usr/local/bin warden && \
    rm /tmp/warden.tar.gz /tmp/nox.tar.gz
COPY backend ./backend
ENV HOST=0.0.0.0 PORT=8080
EXPOSE 8080
CMD ["go", "run", "./backend"]
