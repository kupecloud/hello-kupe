# Must be >= the `go` directive in go.mod — this image sets GOTOOLCHAIN=local,
# so an older builder hard-fails with "go.mod requires go >= X". Bump in
# lockstep with any go.mod bump.
FROM golang:1.26.6-alpine3.23@sha256:e57c41c1d5864341031181b0db34b9a537bb5773eb6428e4e5bdaea0f9135406 AS builder

WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/hello-kupe ./cmd/hello-kupe

# Runtime stage: distroless for minimal attack surface.
FROM gcr.io/distroless/static:nonroot@sha256:963fa6c544fe5ce420f1f54fb88b6fb01479f054c8056d0f74cc2c6000df5240

WORKDIR /

COPY --from=builder /out/hello-kupe /hello-kupe

EXPOSE 8080

ENTRYPOINT ["/hello-kupe"]
