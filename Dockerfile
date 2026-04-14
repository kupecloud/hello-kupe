FROM golang:1.26.2-alpine3.23 AS builder

WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/hello-kupe ./cmd/hello-kupe

# Runtime stage: distroless for minimal attack surface.
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /out/hello-kupe /hello-kupe

EXPOSE 8080

ENTRYPOINT ["/hello-kupe"]
