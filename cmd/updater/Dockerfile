# Metapus Updater Agent — multi-stage build
FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.Version=${VERSION}" \
    -o /bin/updater ./cmd/updater

# Runtime stage — minimal image
FROM gcr.io/distroless/static-debian12

COPY --from=builder /bin/updater /updater
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

VOLUME ["/data"]

ENTRYPOINT ["/updater"]
