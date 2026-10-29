# Build the Next.js frontend once; no Node process is needed at runtime.
FROM --platform=$BUILDPLATFORM node:22-alpine@sha256:fe4f6d74e20370acb04ad889077d14d0f64a9c8ea6a42bc60cddf1e844699914 AS web
WORKDIR /web
ENV NEXT_TELEMETRY_DISABLED=1
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-fund
COPY web/ ./
RUN npm run build

# Must be >= the `go` directive in go.mod — this image sets GOTOOLCHAIN=local,
# so an older builder hard-fails with "go.mod requires go >= X". Bump in
# lockstep with any go.mod bump.
FROM --platform=$BUILDPLATFORM golang:1.26.10-alpine3.23@sha256:a07f231c19b03e6106cf442f02a5bcf262147e744d15b7b5e4b6d81d420c70a0 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/hello-kupe ./cmd/hello-kupe

# Runtime stage: distroless for minimal attack surface.
FROM gcr.io/distroless/static:nonroot@sha256:963fa6c544fe5ce420f1f54fb88b6fb01479f054c8056d0f74cc2c6000df5240

WORKDIR /

COPY --from=builder /out/hello-kupe /hello-kupe
COPY --from=web /web/out /web
ENV WEB_ROOT=/web

EXPOSE 8080

ENTRYPOINT ["/hello-kupe"]
