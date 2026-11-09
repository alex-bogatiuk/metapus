FROM golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/public-test-metrics-sink ./cmd/public-test-metrics-sink
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /public-test-metrics-sink ./cmd/public-test-metrics-sink

FROM gcr.io/distroless/static-debian12@sha256:a9fcaedd4c9b59e12dd65d954f0b5044f19b0647a8a3712e77205df9e7b102cd
COPY --from=build /public-test-metrics-sink /public-test-metrics-sink
USER 65532:65532
EXPOSE 4317
ENTRYPOINT ["/public-test-metrics-sink"]
