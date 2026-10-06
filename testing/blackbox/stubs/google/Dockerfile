# Built with testing/blackbox as the context (see compose.yml).
FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/google-stub ./stubs/google

# A static binary with no shell: the healthcheck is the binary itself.
FROM scratch

COPY --from=builder /out/google-stub /google-stub

EXPOSE 8080

HEALTHCHECK --interval=2s --timeout=2s --retries=15 CMD ["/google-stub", "healthcheck"]

ENTRYPOINT ["/google-stub"]
