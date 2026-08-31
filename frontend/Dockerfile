# Build stage
FROM node:20-alpine AS builder

WORKDIR /app

# Copy package files
COPY package*.json ./

# Install dependencies
RUN npm ci

# Copy source code
COPY . .

# Firebase client config (baked into the SPA at build time)
ARG VITE_FIREBASE_CONFIG
ENV VITE_FIREBASE_CONFIG=$VITE_FIREBASE_CONFIG

# Build the application
RUN npm run build

# Production stage
FROM nginx:alpine

# Upstream API service (overridable at runtime via envsubst if needed)
ENV API_UPSTREAM=agent-api:8080

# Copy built assets from builder
COPY --from=builder /app/dist /usr/share/nginx/html

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

    # Handle SPA routing - serve index.html for all routes
    location / {
        try_files \$uri \$uri/ /index.html;
    }

    # Cache static assets
    location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2|ttf|eot)$ {
        expires 1y;
        add_header Cache-Control "public, immutable";
    }
}
EOF

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
