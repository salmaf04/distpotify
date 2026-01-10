FROM node:20-alpine

WORKDIR /app

# =========================
# SISTEMA BASE
# =========================
RUN apk add --no-cache \
    nginx \
    gettext \  
    tzdata \
    git \
    curl \
    ca-certificates

# =========================
# GO OFICIAL
# =========================
ENV GO_VERSION=1.24.0
RUN curl -fsSL https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz \
    | tar -C /usr/local -xz

ENV PATH="/usr/local/go/bin:/root/go/bin:${PATH}"

# =========================
# AIR (HOT RELOAD GO)
# =========================
RUN go install github.com/air-verse/air@v1.52.3

# =========================
# FRONTEND DEPENDENCIAS
# =========================
COPY frontend/package*.json ./frontend/
RUN cd frontend && npm install

# =========================
# PROXY DEPENDENCIAS
# =========================
COPY proxy/go.mod proxy/go.sum ./proxy/
RUN cd proxy && go mod download

# =========================
# NGINX CONFIG TEMPLATE
# =========================
RUN mkdir -p /run/nginx

# Creamos un template de nginx usando variables de entorno
RUN echo 'server {' > /etc/nginx/http.d/default.conf.template && \
    echo '    listen 80;' >> /etc/nginx/http.d/default.conf.template && \
    echo '    client_max_body_size 50M;' >> /etc/nginx/http.d/default.conf.template && \
    echo '' >> /etc/nginx/http.d/default.conf.template && \
    echo '    location / {' >> /etc/nginx/http.d/default.conf.template && \
    echo '        proxy_pass http://127.0.0.1:${FRONTEND_PORT};' >> /etc/nginx/http.d/default.conf.template && \
    echo '        proxy_http_version 1.1;' >> /etc/nginx/http.d/default.conf.template && \
    echo '        proxy_set_header Upgrade $http_upgrade;' >> /etc/nginx/http.d/default.conf.template && \
    echo '        proxy_set_header Connection "upgrade";' >> /etc/nginx/http.d/default.conf.template && \
    echo '        proxy_set_header Host $host;' >> /etc/nginx/http.d/default.conf.template && \
    echo '        proxy_cache_bypass $http_upgrade;' >> /etc/nginx/http.d/default.conf.template && \
    echo '    }' >> /etc/nginx/http.d/default.conf.template && \
    echo '' >> /etc/nginx/http.d/default.conf.template && \
    echo '    location /api/ {' >> /etc/nginx/http.d/default.conf.template && \
    echo '        proxy_pass http://127.0.0.1:${API_PORT}/;' >> /etc/nginx/http.d/default.conf.template && \
    echo '        proxy_set_header Host $host;' >> /etc/nginx/http.d/default.conf.template && \
    echo '        proxy_set_header X-Real-IP $remote_addr;' >> /etc/nginx/http.d/default.conf.template && \
    echo '        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;' >> /etc/nginx/http.d/default.conf.template && \
    echo '        proxy_set_header X-Forwarded-Proto $scheme;' >> /etc/nginx/http.d/default.conf.template && \
    echo '        proxy_connect_timeout 300s;' >> /etc/nginx/http.d/default.conf.template && \
    echo '        proxy_send_timeout 300s;' >> /etc/nginx/http.d/default.conf.template && \
    echo '        proxy_read_timeout 300s;' >> /etc/nginx/http.d/default.conf.template && \
    echo '    }' >> /etc/nginx/http.d/default.conf.template && \
    echo '}' >> /etc/nginx/http.d/default.conf.template

# =========================
# START SCRIPT PARAMETRIZADO
# =========================
ENV FRONTEND_PORT=5173
ENV API_PORT=8081

RUN echo '#!/bin/sh' > /app/start.sh && \
    echo 'echo "Configuring nginx..."' >> /app/start.sh && \
    echo 'envsubst '\''$FRONTEND_PORT $API_PORT'\'' < /etc/nginx/http.d/default.conf.template > /etc/nginx/http.d/default.conf' >> /app/start.sh && \
    echo 'echo "Starting Go proxy with AIR..."' >> /app/start.sh && \
    echo 'cd /app/proxy && air &' >> /app/start.sh && \
    echo 'echo "Starting frontend (Vite dev)..."' >> /app/start.sh && \
    echo 'cd /app/frontend && npm run dev -- --host 0.0.0.0 --port $FRONTEND_PORT &' >> /app/start.sh && \
    echo 'echo "Starting nginx..."' >> /app/start.sh && \
    echo 'nginx -g "daemon off;"' >> /app/start.sh && \
    chmod +x /app/start.sh

EXPOSE 80

CMD ["/app/start.sh"]


