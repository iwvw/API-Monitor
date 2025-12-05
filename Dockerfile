FROM node:18-alpine

# Create app directory
WORKDIR /app

# Install dependencies
COPY package.json package-lock.json* ./
RUN npm ci --only=production

# Copy app source
COPY . .

ENV NODE_ENV=production
ENV PORT=3000

EXPOSE 3000

# Default command (runs as root for simplicity, or can be changed to non-root user)
# For production deployments, consider using a non-root user and proper volume mounts
CMD ["npm", "start"]
