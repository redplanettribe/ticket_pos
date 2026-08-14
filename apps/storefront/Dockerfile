# Production image for the Storefront.
#
# Build context is the REPO ROOT, not apps/storefront: the app transpiles
# workspace packages (@ticket-pos/ui) whose sources live outside its own
# directory, and pnpm's lockfile is a workspace-wide file.
#
#   docker build -f apps/storefront/Dockerfile .
#
# Public browser configuration (NEXT_PUBLIC_*) is inlined into the client
# bundle by `next build`, so it arrives as a build argument. Setting it as a
# runtime environment variable has no effect - the value would be empty in the
# shipped bundle.

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

ARG NEXT_PUBLIC_API_URL
ENV NEXT_PUBLIC_API_URL=$NEXT_PUBLIC_API_URL
ENV NEXT_TELEMETRY_DISABLED=1
ENV NODE_ENV=production

RUN pnpm --filter @ticket-pos/storefront exec next build

# --- runtime ----------------------------------------------------------------
# Standalone output is laid out relative to outputFileTracingRoot (the
# workspace root), so the server entrypoint is apps/storefront/server.js and
# the traced workspace packages sit alongside it under packages/.
FROM node:26-alpine AS runner
WORKDIR /app

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
ENV HOSTNAME=0.0.0.0

COPY --from=builder --chown=node:node /repo/apps/storefront/.next/standalone ./
COPY --from=builder --chown=node:node /repo/apps/storefront/.next/static ./apps/storefront/.next/static

USER node
EXPOSE 3000

CMD ["node", "apps/storefront/server.js"]
