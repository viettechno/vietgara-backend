# The builder runs on the build machine's platform and cross-compiles for
# the target (e.g. an arm64 Mac building the linux/amd64 staging image), so
# no emulation is needed.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

# Target platform for the final image. BuildKit (compose up --build,
# buildx, CI image builds) injects TARGETOS/TARGETARCH automatically; when
# they are empty (plain docker build without BuildKit) fall back to the
# builder's own platform. Never hardcode a default here: a default like
# amd64 silently ships a foreign-arch binary inside a native-arch image,
# which then crashes under emulation (flaky SIGSEGV, seen in gRPC
# transport goroutines on Apple Silicon).
ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS="${TARGETOS:-$(go env GOOS)}" GOARCH="${TARGETARCH:-$(go env GOARCH)}" go build -trimpath -ldflags='-s -w' -o /out/vietgara-backend ./cmd/server
RUN CGO_ENABLED=0 GOOS="${TARGETOS:-$(go env GOOS)}" GOARCH="${TARGETARCH:-$(go env GOARCH)}" go build -trimpath -ldflags='-s -w' -o /out/purge-test-users ./cmd/purge-test-users

FROM alpine:3.22

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=builder /out/vietgara-backend /app/vietgara-backend
COPY --from=builder /out/purge-test-users /app/purge-test-users
COPY db/migrations /app/db/migrations
# The backend runs as an unprivileged user (Security Review SR-11). It owns
# only the uploads directory; a volume mounted there takes this ownership
# when it is created empty.
RUN addgroup -S -g 10001 vietgara && adduser -S -D -H -u 10001 -G vietgara vietgara \
    && mkdir -p /app/data/uploads && chown -R vietgara:vietgara /app/data

USER 10001:10001

EXPOSE 50051 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=5 \
  CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1

ENTRYPOINT ["/app/vietgara-backend"]
