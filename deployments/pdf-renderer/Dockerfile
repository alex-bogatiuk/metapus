FROM golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ internal/
COPY pkg/ pkg/
COPY cmd/pdf-renderer/ cmd/pdf-renderer/
COPY cmd/pdf-renderer-smoke/ cmd/pdf-renderer-smoke/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /pdf-renderer ./cmd/pdf-renderer
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /pdf-renderer-smoke ./cmd/pdf-renderer-smoke

FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d AS runtime
RUN apk add --no-cache chromium font-dejavu font-noto \
    && addgroup -g 10001 renderer && adduser -D -u 10001 -G renderer renderer
COPY --from=builder /pdf-renderer /usr/local/bin/pdf-renderer
USER 10001:10001
ENV HOME=/tmp
EXPOSE 8081
HEALTHCHECK --interval=20s --timeout=5s --start-period=40s CMD ["/usr/local/bin/pdf-renderer", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/pdf-renderer"]

# Verification tools are added only to this test target, never to the shipped image.
FROM runtime AS smoke
USER root
RUN apk add --no-cache poppler-utils
COPY --from=builder /pdf-renderer-smoke /usr/local/bin/pdf-renderer-smoke
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/pdf-renderer-smoke"]

FROM runtime AS release
