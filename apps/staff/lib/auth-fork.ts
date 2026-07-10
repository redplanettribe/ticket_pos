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
  error: { code: string; message: string } | null;
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

export async function applyAuthFork(
  session: SessionForkInput,
): Promise<{ path: string; error?: string }> {
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
        return {
          path: fork.path,
          error: envelope.error?.message ?? "Could not select organization",
        };
      }
    } catch {
      return { path: fork.path, error: "Could not select organization" };
    }
  }

  return { path: fork.path };
}
