FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /docklab-backend ./cmd/server

FROM gcr.io/distroless/static-debian12
WORKDIR /
COPY --from=builder /docklab-backend /docklab-backend

EXPOSE 8080
ENTRYPOINT ["/docklab-backend"]
