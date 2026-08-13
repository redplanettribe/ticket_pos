# Production image for Staff.
#
# Same pattern as apps/storefront/Dockerfile (see "Frontend image pattern" in
# docs/gcp-deployment.md): build context is the REPO ROOT, because the app
# transpiles workspace packages (@ticket-pos/ui) whose sources live outside its
# own directory and pnpm's lockfile is workspace-wide.
#
#   docker build -f apps/staff/Dockerfile .
#
# Unlike the Storefront, Staff has no NEXT_PUBLIC_* build arguments: the browser
# never calls the Go API directly. It calls Staff's own route handlers, which
# hold the httpOnly session cookie and proxy onwards using the server-only,
# runtime API_URL.

FROM node:26-alpine AS base
ENV PNPM_HOME=/pnpm
ENV PATH=$PNPM_HOME:$PATH
RUN npm install -g pnpm@9.15.4

# --- dependencies -----------------------------------------------------------
# Manifests only, so the install layer is cached until a package.json or the
# lockfile changes. Every workspace member's manifest is required: pnpm refuses
# a --frozen-lockfile install whose workspace does not match the lockfile.
FROM base AS deps
WORKDIR /repo
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/staff/package.json apps/staff/
COPY apps/storefront/package.json apps/storefront/
COPY packages/api-client/package.json packages/api-client/
COPY packages/locale/package.json packages/locale/
COPY packages/ui/package.json packages/ui/
COPY e2e/package.json e2e/
RUN pnpm install --frozen-lockfile

# --- build ------------------------------------------------------------------
FROM base AS builder
WORKDIR /repo
COPY --from=deps /repo ./
# .dockerignore keeps node_modules out of the context, so this does not clobber
# the installed tree above.
COPY . .

ENV NEXT_TELEMETRY_DISABLED=1
ENV NODE_ENV=production

RUN pnpm --filter @ticket-pos/staff exec next build

# --- runtime ----------------------------------------------------------------
# Standalone output is laid out relative to outputFileTracingRoot (the
# workspace root), so the server entrypoint is apps/staff/server.js and the
# traced workspace packages sit alongside it under packages/.
FROM node:26-alpine AS runner
WORKDIR /app

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3001
ENV HOSTNAME=0.0.0.0

COPY --from=builder --chown=node:node /repo/apps/staff/.next/standalone ./
COPY --from=builder --chown=node:node /repo/apps/staff/.next/static ./apps/staff/.next/static

USER node
EXPOSE 3001

CMD ["node", "apps/staff/server.js"]
