# Usamos la imagen base de Go (NO Alpine pelado) para tener el compilador
FROM golang:1.24-alpine

WORKDIR /app

# 1. Instalamos Air
RUN go install github.com/air-verse/air@v1.52.3

# Usa el go.mod/go.sum de ESTE módulo (proxy)
COPY go.mod go.sum ./
RUN go mod download

# Copia el código del proxy
COPY . .

EXPOSE 8081

# 2. Arrancamos con Air en lugar del binario compilado
CMD ["air"]