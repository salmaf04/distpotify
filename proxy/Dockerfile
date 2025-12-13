# ~/Distribuidos-Proyecto/spotify/proxy/Dockerfile
FROM golang:1.24-alpine AS builder

WORKDIR /app

RUN go install github.com/air-verse/air@v1.52.3

# Usa el go.mod/go.sum de ESTE módulo (proxy)
COPY go.mod go.sum ./
RUN go mod download

# Copia el código del proxy (este directorio)
COPY . .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app .
EXPOSE 8081
CMD ["air"]