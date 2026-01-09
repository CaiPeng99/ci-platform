# Build stage
FROM node:20-alpine AS builder

WORKDIR /app

# Copy package files
COPY package.json package-lock.json* ./

# Install dependencies
RUN npm ci

# Copy source code
COPY public ./public
COPY src ./src

# Build for production (CRA outputs to 'build' folder)
RUN npm run build

# Runtime stage
FROM nginx:alpine

# Copy built assets (CRA uses 'build' not 'dist')
COPY --from=builder /app/build /usr/share/nginx/html

# Copy nginx config for SPA routing
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]