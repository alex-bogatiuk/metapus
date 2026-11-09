# syntax=docker/dockerfile:1.7

# Stage 1: Install dependencies
FROM node:22-alpine@sha256:16e22a550f3863206a3f701448c45f7912c6896a62de43add43bb9c86130c3e2 AS deps
WORKDIR /app
COPY package.json package-lock.json* ./
ARG NPM_HTTPS_PROXY=""
RUN --mount=type=cache,id=metapus-frontend-npm,target=/root/.npm \
    HTTPS_PROXY="${NPM_HTTPS_PROXY}" https_proxy="${NPM_HTTPS_PROXY}" \
    npm ci --prefer-offline --no-audit --no-fund \
    --fetch-retries=5 \
    --fetch-retry-mintimeout=2000 \
    --fetch-retry-maxtimeout=30000

# Stage 2: Build the application
FROM node:22-alpine@sha256:16e22a550f3863206a3f701448c45f7912c6896a62de43add43bb9c86130c3e2 AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .

ARG NEXT_PUBLIC_TENANT_ID=""
ARG API_PROXY_URL="http://localhost:8080"
ENV NEXT_PUBLIC_TENANT_ID=${NEXT_PUBLIC_TENANT_ID}
ENV API_PROXY_URL=${API_PROXY_URL}
ENV NEXT_TELEMETRY_DISABLED=1

RUN npm run build

# Stage 3: Production runtime
FROM node:22-alpine@sha256:16e22a550f3863206a3f701448c45f7912c6896a62de43add43bb9c86130c3e2 AS runner
WORKDIR /app

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1

RUN addgroup --system --gid 1001 nodejs && \
    adduser --system --uid 1001 nextjs

COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static
COPY --from=builder --chown=nextjs:nodejs /app/public ./public

USER nextjs
EXPOSE 3000
ENV PORT=3000
ENV HOSTNAME="0.0.0.0"

CMD ["node", "server.js"]
