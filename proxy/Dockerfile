FROM golang:1.24-alpine

WORKDIR /app

# Instalar air para hot-reload
RUN go install github.com/air-verse/air@v1.52.3

COPY go.mod go.sum ./
RUN go mod download

COPY . .

EXPOSE 8081

CMD ["air"]
