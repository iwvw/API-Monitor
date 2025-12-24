# API Monitor Agent Dockerfile (Multi-stage build)

# Stage 1: Build the binary
FROM node:18-slim AS builder
WORKDIR /app
COPY . .
RUN npm install
# Install pkg and build for linux
RUN npx pkg . --out-path dist --targets node18-linux-x64

# Stage 2: Final runtime image
FROM debian:stable-slim
WORKDIR /app
# Copy the binary from the builder stage
COPY --from=builder /app/dist/api-monitor-agent-linux /app/agent-bin

# Default environment variables
ENV API_MONITOR_SERVER="http://your-dashboard-url"
ENV API_MONITOR_SERVER_ID=""
ENV API_MONITOR_KEY=""
ENV API_MONITOR_INTERVAL="1500"

# Metadata
LABEL maintainer="API Monitor Team"
LABEL description="API Monitor Agent Container"

# Entrypoint to run the binary
ENTRYPOINT ["/app/agent-bin"]
