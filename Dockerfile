FROM golang:1.27-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/worker ./cmd/worker

FROM alpine:3.20 AS api
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/api /app/api
COPY migrations /app/migrations
EXPOSE 8080
ENTRYPOINT ["/app/api"]

FROM alpine:3.20 AS worker
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/worker /app/worker
ENTRYPOINT ["/app/worker"]
