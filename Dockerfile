FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /docklab-backend ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates docker-cli
WORKDIR /
COPY --from=builder /docklab-backend /docklab-backend

EXPOSE 8080
ENTRYPOINT ["/docklab-backend"]
