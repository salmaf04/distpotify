FROM golang:1.24-alpine

WORKDIR /app

# 1. Instalamos Air (Hot Reload)
RUN go install github.com/air-verse/air@v1.52.3

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# NO compilamos aquí con "go build -o main ." porque Air lo hará al arrancar.
EXPOSE 3003

# 2. El comando por defecto ahora es "air"
CMD ["air"]