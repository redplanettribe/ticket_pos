export type SessionForkInput = {
  active_member: unknown | null;
  memberships: Array<{ member_id: string }>;
};

export type AuthForkResult = {
  path: string;
  autoSelectMemberId?: string;
};

type Envelope<T> = {
  data: T | null;
  error: { code: string; message: string; details?: unknown } | null;
};

export function resolveAuthForkPath(session: SessionForkInput): AuthForkResult {
  if (session.active_member) return { path: "/" };
  if (session.memberships.length === 0) return { path: "/organizations/new" };
  if (session.memberships.length === 1) {
    return { path: "/", autoSelectMemberId: session.memberships[0].member_id };
  }
  return { path: "/select-organization" };
}

export function resolveAuthForkRedirectPath(session: SessionForkInput): string {
  const fork = resolveAuthForkPath(session);
  if (fork.autoSelectMemberId) {
    return "/select-organization";
  }
  return fork.path;
}

export type SignedInLandingInput = SessionForkInput & {
  /** True when this session's email is on the platform operator allowlist. */
  is_platform_operator?: boolean;
};

/**
 * Where a request already carrying a valid Staff Session belongs.
 *
 * This is the fork above with the operator exception folded in, extracted so
 * that the two callers in middleware.ts cannot drift apart: the visitor who
 * asks for a page their session does not fit, and the visitor who asks for the
 * sign-in page while already signed in. Both are answering the same question —
 * "this person is authenticated, where do they go?" — and a signed-in Member
 * bounced off /login must land exactly where the same Member lands when the
 * middleware redirects them anywhere else.
 *
 * The operator exception is ADR 0015: operator authority is orthogonal to
 * Membership, so a pure operator has no Membership to fork on and the
 * create-organization prompt would be a dead end. Their dashboard is /operator.
 * An operator who *does* hold an active Membership is an ordinary Member here
 * and forks like one.
 */
export function signedInLandingPath(session: SignedInLandingInput): string {
  if (!session.active_member && session.is_platform_operator) {
    return "/operator";
  }
  return resolveAuthForkRedirectPath(session);
}

/**
 * What came back when the fork's auto-selection did not work.
 *
 * `apiError` is the API's own `error` object — its CODE and its English message —
 * or null when the call never got an answer at all. It is NOT a sentence: this
 * module is pure logic and stays free of copy, so the caller resolves it through
 * lib/api-errors and falls back to a sentence from its own namespace when
 * `apiError` is null (messages/README.md, "lib/ returns tokens").
 *
 * It used to return "Could not select organization" from here, which was one
 * English sentence a Spanish reader would have met at the end of an otherwise
 * Spanish sign-in.
 */
export type AuthForkFailure = {
  apiError: { code: string; message: string; details?: unknown } | null;
};

export async function applyAuthFork(
  session: SessionForkInput,
): Promise<{ path: string; failure?: AuthForkFailure }> {
  const fork = resolveAuthForkPath(session);

  if (fork.autoSelectMemberId) {
    try {
      const response = await fetch("/api/auth/select-organization", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ member_id: fork.autoSelectMemberId }),
      });
      const envelope = (await response.json()) as Envelope<unknown>;
      if (!response.ok || envelope.error) {
        return { path: fork.path, failure: { apiError: envelope.error } };
      }
    } catch {
      // Never reached the API, so there is no verdict to report — only that it
      // failed.
      return { path: fork.path, failure: { apiError: null } };
    }
  }

  return { path: fork.path };
}
