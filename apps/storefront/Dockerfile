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
#
# Sentry's DSN is the exception to that: a runtime value (SENTRY_DSN, with
# SENTRY_ENVIRONMENT), set on the service by Terraform and absent by default;
# the browser receives it through the page. The only Sentry build argument is
# SENTRY_RELEASE, below.

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

ARG NEXT_PUBLIC_API_URL
ENV NEXT_PUBLIC_API_URL=$NEXT_PUBLIC_API_URL
ENV NEXT_TELEMETRY_DISABLED=1
ENV NODE_ENV=production

# The Sentry auth token, when the deploy has one, is a BuildKit secret: it is
# in this step's environment only, never in a layer, the image or the build
# cache key. With it, next build uploads the source maps to ticket-pos-storefront
# and fails soft (sentryBuildOptions in @ticket-pos/sentry-rules); without it,
# as in CI and every local build, no source map is produced for the browser and
# nothing talks to Sentry. Every map left in the output is deleted in the same
# step, uploaded or not, so none reaches the image: the runner stage refuses
# one. SENTRY_CLI_NO_TELEMETRY keeps the upload's CLI from reporting on itself.
RUN --mount=type=secret,id=sentry_auth_token,env=SENTRY_AUTH_TOKEN \
    SENTRY_CLI_NO_TELEMETRY=1 pnpm --filter @ticket-pos/storefront exec next build \
    && find apps/storefront/.next -name '*.map' -delete

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

# The commit SHA this image was built from, which Sentry reports as the release
# (ADR 0079), read at runtime by lib/sentry.ts in the server and handed to the
# browser through the page. The deploy workflow passes it; a build without it
# still works, and its reports carry no release. Declared in this stage only, so
# a new commit does not invalidate the build stage's cache.
ARG SENTRY_RELEASE=""
ENV SENTRY_RELEASE=${SENTRY_RELEASE}

COPY --from=builder --chown=node:node /repo/apps/storefront/.next/standalone ./
COPY --from=builder --chown=node:node /repo/apps/storefront/.next/static ./apps/storefront/.next/static

# No source map ships, whether or not the upload ran: a .map anywhere in the
# image fails the build (ADR 0079, #776). The source is not published.
RUN if find /app -name '*.map' | grep .; then \
      echo "error: source maps in the image, listed above; they must never be served" >&2; exit 1; \
    fi

USER node
EXPOSE 3000

CMD ["node", "apps/storefront/server.js"]
