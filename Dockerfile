FROM golang:1.26-alpine
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
RUN apk add --no-cache curl
COPY backend ./backend
ENV HOST=0.0.0.0 PORT=8080
EXPOSE 8080
CMD ["go", "run", "./backend"]
