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
#
# Sentry's DSN is likewise a runtime value (SENTRY_DSN, with SENTRY_ENVIRONMENT),
# set on the service by Terraform and absent by default; the browser receives
# it through the page. The only Sentry build argument is SENTRY_RELEASE, below.

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
COPY packages/sentry-rules/package.json packages/sentry-rules/
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

# The Sentry auth token, when the deploy has one, is a BuildKit secret: it is
# in this step's environment only, never in a layer, the image or the build
# cache key. With it, next build uploads the source maps to ticket-pos-staff
# and fails soft (sentryBuildOptions in @ticket-pos/sentry-rules); without it,
# as in CI and every local build, no source map is produced for the browser and
# nothing talks to Sentry. Every map left in the output is deleted in the same
# step, uploaded or not, so none reaches the image: the runner stage refuses
# one. SENTRY_CLI_NO_TELEMETRY keeps the upload's CLI from reporting on itself.
RUN --mount=type=secret,id=sentry_auth_token,env=SENTRY_AUTH_TOKEN \
    SENTRY_CLI_NO_TELEMETRY=1 pnpm --filter @ticket-pos/staff exec next build \
    && find apps/staff/.next -name '*.map' -delete

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

# The commit SHA this image was built from, which Sentry reports as the release
# (ADR 0079), read at runtime by lib/sentry.ts in the server and handed to the
# browser through the page. The deploy workflow passes it; a build without it
# still works, and its reports carry no release. Declared in this stage only, so
# a new commit does not invalidate the build stage's cache.
ARG SENTRY_RELEASE=""
ENV SENTRY_RELEASE=${SENTRY_RELEASE}

COPY --from=builder --chown=node:node /repo/apps/staff/.next/standalone ./
COPY --from=builder --chown=node:node /repo/apps/staff/.next/static ./apps/staff/.next/static
# The standalone output leaves public/ out. Without it the manifest's icons 404.
COPY --from=builder --chown=node:node /repo/apps/staff/public ./apps/staff/public
RUN test -s apps/staff/public/icon-192.png && test -s apps/staff/public/icon-512.png \
    || { echo "error: the manifest icons are missing from the image" >&2; exit 1; }

# No source map ships, whether or not the upload ran: a .map anywhere in the
# image fails the build (ADR 0079, #776). The source is not published.
RUN if find /app -name '*.map' | grep .; then \
      echo "error: source maps in the image, listed above; they must never be served" >&2; exit 1; \
    fi

USER node
EXPOSE 3001

CMD ["node", "apps/staff/server.js"]
