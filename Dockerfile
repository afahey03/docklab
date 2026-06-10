FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /docklab-backend ./cmd/server

FROM hashicorp/terraform:1.9.8 AS terraform

FROM alpine:3.20
# kubectl backs the optional DOKLAB_RUNTIME=kubernetes backend.
RUN apk add --no-cache ca-certificates docker-cli kubectl
WORKDIR /
COPY --from=builder /docklab-backend /docklab-backend
COPY --from=terraform /bin/terraform /usr/local/bin/terraform

EXPOSE 8080
ENTRYPOINT ["/docklab-backend"]
