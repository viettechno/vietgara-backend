FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/vietgara-backend ./cmd/server

FROM alpine:3.22

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=builder /out/vietgara-backend /app/vietgara-backend
COPY db/migrations /app/db/migrations

EXPOSE 50051

ENTRYPOINT ["/app/vietgara-backend"]
