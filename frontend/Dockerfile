# Build stage
FROM node:20-alpine AS builder

WORKDIR /app

# Copy package files
COPY package*.json ./

# Install dependencies
RUN npm ci

# Copy source code
COPY . .

# Firebase client config (baked into the SPA at build time).
# Passed base64-encoded to avoid shell quoting issues with JSON in docker build-arg.
ARG VITE_FIREBASE_CONFIG_B64
RUN if [ -n "$VITE_FIREBASE_CONFIG_B64" ]; then \
      export VITE_FIREBASE_CONFIG="$(printf '%s' "$VITE_FIREBASE_CONFIG_B64" | base64 -d)"; \
    fi; \
    npm run build

# Production stage
FROM nginx:alpine

# Upstream API service (overridable at runtime via envsubst if needed)
ENV API_UPSTREAM=agent-api:8080

# Copy built assets from builder
COPY --from=builder /app/dist /usr/share/nginx/html

# Vite base is "./", so without <base href> deep SPA routes (e.g. /chatbot/coded-flows)
# resolve ./favicon.svg → /chatbot/favicon.svg and 404. Mirror internal/frontend/embed.go.
RUN sed -i 's|<head>|<head><base href="/">|' /usr/share/nginx/html/index.html \
 && sed -i 's|</head>|<script>window.__BASE_PATH__="";</script></head>|' /usr/share/nginx/html/index.html

# Copy nginx configuration template
COPY <<EOF /etc/nginx/templates/default.conf.template
server {
    listen 80;
    server_name localhost;
    root /usr/share/nginx/html;
    index index.html;

    client_max_body_size 20m;

    # Gzip compression
    gzip on;
    gzip_types text/plain text/css application/json application/javascript text/xml application/xml application/xml+rss text/javascript;

    # Proxy API to the agent-api service (same-origin for cookie auth)
    location /api/ {
        proxy_pass http://\${API_UPSTREAM};
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header X-Forwarded-Host \$host;
        proxy_read_timeout 60s;
    }

    location = /api {
        return 301 /api/;
    }

    # WebSocket endpoint
    location /ws {
        proxy_pass http://\${API_UPSTREAM};
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }

    # Health for the UI container itself
    location = /healthz {
        access_log off;
        return 200 'ok';
        add_header Content-Type text/plain;
    }

    # Exact root favicon (avoids regex location looking under SPA path prefixes)
    location = /favicon.svg {
        expires 1y;
        add_header Cache-Control "public, immutable";
        try_files /favicon.svg =404;
    }

    # SPA routing: do not try \$uri/ — a public/ dir matching a Vue route
    # (e.g. /landing) would 301 to /landing/ then 403 with no index.
    location / {
        try_files \$uri /index.html;
    }

    # Cache static assets
    location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2|ttf|eot)$ {
        expires 1y;
        add_header Cache-Control "public, immutable";
        try_files \$uri =404;
    }
}
EOF

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
