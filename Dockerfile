# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/prompt-guard ./cmd/prompt-guard

FROM scratch

WORKDIR /app

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/prompt-guard /prompt-guard
COPY configs /app/configs
COPY configs/config.example.yaml /app/configs/config.yaml

EXPOSE 8099

USER 65532:65532

ENTRYPOINT ["/prompt-guard"]
CMD ["-config", "/app/configs/config.yaml"]
