/**
 * The options both Next apps give Sentry's build plugin (ADR 0079, #776).
 *
 * The build talks to Sentry only when the deploy hands it a token
 * (SENTRY_AUTH_TOKEN, a BuildKit secret in the image build). Then it produces
 * source maps, uploads them to the app's own project, and deletes the browser
 * ones from the output; the Dockerfile deletes whatever map is left and refuses
 * an image that still holds one. The browser maps are "hidden": no bundle
 * names its map, so nothing ever asks for one.
 *
 * With no token - CI, a local build, `make prod`, a deploy before the operator
 * has created the secret - the options are what they were before this ticket:
 * no source maps, no upload, no release, no telemetry, so the build contacts
 * nothing.
 *
 * The upload fails soft. An unreachable Sentry or a wrong token is one warning
 * line and the build goes on: that release's browser stack traces stay
 * minified, and the monitoring vendor never blocks a release.
 *
 * The build creates no release. Uploaded files are matched to events by the
 * debug ids the plugin injects, not by release, and the deploy workflow creates
 * the release once for all three projects and marks each deploy
 * (scripts/sentry-release.sh). That keeps the commit out of the image's build
 * stage, whose cache a new commit would otherwise invalidate.
 */

/** The Sentry organization of every project of this product. */
export const SENTRY_ORG = "redplanettribe";

/** The subset of the plugin's options this product sets. */
export interface SentryBuildOptions {
  telemetry: false;
  silent: boolean;
  sourcemaps: { disable: boolean; deleteSourcemapsAfterUpload?: boolean };
  release: { create: false; finalize: false };
  authToken?: string;
  org?: string;
  project?: string;
  widenClientFileUpload?: boolean;
  errorHandler?: (err: Error) => void;
}

/**
 * The plugin's options for the app whose Sentry project is `project`, read
 * from the build's environment.
 */
export function sentryBuildOptions(
  project: string,
  env: Record<string, string | undefined>,
  warn: (line: string) => void = console.warn,
): SentryBuildOptions {
  const authToken = env.SENTRY_AUTH_TOKEN?.trim();
  const release = { create: false, finalize: false } as const;
  if (!authToken) {
    return { telemetry: false, silent: true, sourcemaps: { disable: true }, release };
  }
  return {
    telemetry: false,
    // The upload says what it sent, in the build log.
    silent: false,
    authToken,
    org: SENTRY_ORG,
    project,
    // Every chunk under static/chunks, not only the page and layout entries:
    // our own lib code sits in shared chunks too.
    widenClientFileUpload: true,
    sourcemaps: { disable: false, deleteSourcemapsAfterUpload: true },
    release,
    errorHandler: (err) => {
      warn(`warning: source map upload to Sentry failed, the build continues without it: ${err.message}`);
    },
  };
}
