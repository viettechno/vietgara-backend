# The builder runs on the build machine's platform and cross-compiles for
# the target (e.g. an arm64 Mac building the linux/amd64 staging image), so
# no emulation is needed.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/vietgara-backend ./cmd/server
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/purge-test-users ./cmd/purge-test-users

FROM alpine:3.22

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=builder /out/vietgara-backend /app/vietgara-backend
COPY --from=builder /out/purge-test-users /app/purge-test-users
COPY db/migrations /app/db/migrations
RUN mkdir -p /app/data/uploads

EXPOSE 50051 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=5 \
  CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1

ENTRYPOINT ["/app/vietgara-backend"]
